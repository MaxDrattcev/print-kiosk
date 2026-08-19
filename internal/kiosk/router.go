package kiosk

import (
	"database/sql"
	"embed"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/copyjob"
	"print-kiosk/internal/mailinbox"
	"print-kiosk/internal/maxsvc"
	"print-kiosk/internal/printjob"
	"print-kiosk/internal/scanjob"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
)

//go:embed web/*
var webFS embed.FS

func RegisterRoutes(r *gin.Engine, cfg *config.Config, settings *storage.SettingsRepo, db *sql.DB) (*maxsvc.Service, error) {
	jobs, err := printjob.NewService(printjob.Options{
		JobsDir:         cfg.Paths.PrintJobs,
		LibreOfficePath: cfg.Paths.LibreOffice,
		PrinterName:     cfg.Printer.Name,
		SumatraPath:     cfg.Printer.Sumatra,
		DryRun:          cfg.Printer.DryRun,
	})
	if err != nil {
		return nil, fmt.Errorf("print jobs: %w", err)
	}

	scans, err := scanjob.NewService(cfg.ScanJobsDir(), cfg.Printer.DryRun)
	if err != nil {
		return nil, fmt.Errorf("scan jobs: %w", err)
	}

	copies, err := copyjob.NewService(filepath.Join(cfg.DataRoot(), "copy-jobs"), jobs, cfg.Printer.DryRun)
	if err != nil {
		return nil, fmt.Errorf("copy jobs: %w", err)
	}

	mail, err := mailinbox.NewService(cfg.EmailInboxDir())
	if err != nil {
		return nil, fmt.Errorf("email inbox: %w", err)
	}

	st := stats.NewRepo(db)
	maxSvc, err := maxsvc.New(settings, st, filepath.Join(cfg.DataRoot(), "max-inbox"))
	if err != nil {
		return nil, fmt.Errorf("max service: %w", err)
	}

	h := NewHandler(cfg, settings, jobs, scans, copies, mail, maxSvc, st)

	r.GET("/api/kiosk/info", h.Info)
	r.POST("/api/kiosk/session/end", h.EndSession)
	r.GET("/api/kiosk/usb/drives", h.ListUSBDrives)
	r.GET("/api/kiosk/usb/browse", h.BrowseUSB)
	r.POST("/api/kiosk/print/prepare", h.PreparePrint)
	r.GET("/api/kiosk/print/jobs/:id", h.GetPrintJob)
	r.GET("/api/kiosk/print/jobs/:id/preview", h.PreviewPrintJob)
	r.POST("/api/kiosk/print/jobs/:id/quote", h.QuotePrintJob)
	r.POST("/api/kiosk/print/jobs/:id/pay", h.PayPrintJob)
	r.POST("/api/kiosk/print/jobs/:id/print", h.ExecutePrintJob)

	r.GET("/api/kiosk/email/info", h.EmailInfo)
	r.POST("/api/kiosk/email/sessions", h.StartEmailSession)
	r.GET("/api/kiosk/email/sessions/:id", h.GetEmailSession)
	r.POST("/api/kiosk/email/sessions/:id/confirm", h.ConfirmEmailSession)
	r.POST("/api/kiosk/email/sessions/:id/reject", h.RejectEmailSession)
	r.POST("/api/kiosk/email/sessions/:id/printed", h.MarkEmailPrinted)
	r.POST("/api/kiosk/email/prepare", h.PrepareEmailPrint)

	r.GET("/api/kiosk/max/info", h.MaxInfo)
	r.POST("/api/kiosk/max/print/sessions", h.StartMaxPrintSession)
	r.GET("/api/kiosk/max/print/sessions/:id", h.GetMaxPrintSession)
	r.POST("/api/kiosk/max/print/sessions/:id/confirm", h.ConfirmMaxPrintSession)
	r.POST("/api/kiosk/max/print/sessions/:id/reject", h.RejectMaxPrintSession)
	r.POST("/api/kiosk/max/print/sessions/:id/printed", h.MarkMaxPrinted)
	r.POST("/api/kiosk/max/print/prepare", h.PrepareMaxPrint)
	r.POST("/api/kiosk/scan/jobs/:id/max/sessions", h.StartMaxScanSession)
	r.GET("/api/kiosk/max/scan/sessions/:sid", h.GetMaxScanSession)
	r.POST("/api/kiosk/max/scan/sessions/:sid/complete", h.CompleteMaxScanSession)

	r.POST("/api/kiosk/scan/jobs", h.CreateScanJob)
	r.GET("/api/kiosk/scan/jobs/:id", h.GetScanJob)
	r.GET("/api/kiosk/scan/jobs/:id/preview", h.PreviewScanJob)
	r.POST("/api/kiosk/scan/jobs/:id/pay", h.PayScanJob)
	r.POST("/api/kiosk/scan/jobs/:id/scan", h.ExecuteScanJob)
	r.POST("/api/kiosk/scan/jobs/:id/name", h.NameScanJob)
	r.POST("/api/kiosk/scan/jobs/:id/save-usb", h.SaveScanToUSB)
	r.POST("/api/kiosk/scan/jobs/:id/send-email", h.SendScanEmail)

	r.POST("/api/kiosk/copy/jobs", h.CreateCopyJob)
	r.GET("/api/kiosk/copy/jobs/:id", h.GetCopyJob)
	r.POST("/api/kiosk/copy/jobs/:id/quote", h.QuoteCopyJob)
	r.POST("/api/kiosk/copy/jobs/:id/pay", h.PayCopyJob)
	r.POST("/api/kiosk/copy/jobs/:id/copy", h.ExecuteCopyJob)

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return nil, err
	}

	r.GET("/", func(c *gin.Context) {
		serveFile(c, static, "index.html")
	})
	r.GET("/print/", func(c *gin.Context) {
		serveFile(c, static, "print.html")
	})
	r.GET("/print/usb/", func(c *gin.Context) {
		serveFile(c, static, "print-usb.html")
	})
	r.GET("/print/usb/browse/", func(c *gin.Context) {
		serveFile(c, static, "print-usb-browse.html")
	})
	r.GET("/print/setup/", func(c *gin.Context) {
		serveFile(c, static, "print-setup.html")
	})
	r.GET("/scan/", func(c *gin.Context) {
		serveFile(c, static, "scan.html")
	})
	r.GET("/scan/name/", func(c *gin.Context) {
		serveFile(c, static, "scan-name.html")
	})
	r.GET("/scan/delivery/", func(c *gin.Context) {
		serveFile(c, static, "scan-delivery.html")
	})
	r.GET("/scan/usb/", func(c *gin.Context) {
		serveFile(c, static, "scan-usb.html")
	})
	r.GET("/scan/email/", func(c *gin.Context) {
		serveFile(c, static, "scan-email.html")
	})
	r.GET("/scan/max/", func(c *gin.Context) {
		serveFile(c, static, "scan-max.html")
	})
	r.GET("/copy/", func(c *gin.Context) {
		serveFile(c, static, "copy.html")
	})
	r.GET("/copy/setup/", func(c *gin.Context) {
		serveFile(c, static, "copy-setup.html")
	})
	r.GET("/print/email/", func(c *gin.Context) {
		serveFile(c, static, "print-email.html")
	})
	r.GET("/print/email/wait/", func(c *gin.Context) {
		serveFile(c, static, "print-email-wait.html")
	})
	r.GET("/print/email/files/", func(c *gin.Context) {
		serveFile(c, static, "print-email-files.html")
	})
	r.GET("/print/max/", func(c *gin.Context) {
		serveFile(c, static, "print-max.html")
	})
	r.GET("/print/max/wait/", func(c *gin.Context) {
		serveFile(c, static, "print-max-wait.html")
	})
	r.GET("/print/max/files/", func(c *gin.Context) {
		serveFile(c, static, "print-max-files.html")
	})
	r.GET("/print/telegram/", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/print/max/")
	})
	r.GET("/static/*filepath", func(c *gin.Context) {
		name := strings.TrimPrefix(path.Clean("/"+c.Param("filepath")), "/")
		if name == "" || strings.Contains(name, "..") {
			c.Status(http.StatusNotFound)
			return
		}
		serveFile(c, static, name)
	})

	return maxSvc, nil
}

func serveFile(c *gin.Context, static fs.FS, name string) {
	data, err := fs.ReadFile(static, name)
	if err != nil {
		c.Status(http.StatusNotFound)
		return
	}
	c.Data(http.StatusOK, contentType(name), data)
}

func contentType(name string) string {
	switch path.Ext(name) {
	case ".html":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js":
		return "application/javascript; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	default:
		return "application/octet-stream"
	}
}
