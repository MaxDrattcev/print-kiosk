package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/admin"
	"print-kiosk/internal/config"
	"print-kiosk/internal/kiosk"
	"print-kiosk/internal/kioskhost"
	"print-kiosk/internal/libreoffice"
	"print-kiosk/internal/ophistory"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
)

func main() {
	configPath := flag.String("config", "configs/config.yaml", "path to config file")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		slog.Error("failed to load config", "error", err)
		os.Exit(1)
	}

	if err := ensureDirs(cfg); err != nil {
		slog.Error("failed to create directories", "error", err)
		os.Exit(1)
	}

	logger, logFile, err := setupLogger(cfg)
	if err != nil {
		slog.Error("failed to setup logger", "error", err)
		os.Exit(1)
	}
	defer logFile.Close()
	slog.SetDefault(logger)

	db, err := storage.Open(cfg.Database.Path)
	if err != nil {
		slog.Error("failed to open database", "error", err)
		os.Exit(1)
	}
	defer db.Close()

	if err := storage.Migrate(db); err != nil {
		slog.Error("failed to migrate database", "error", err)
		os.Exit(1)
	}
	slog.Info("database ready", "path", cfg.Database.Path)

	if path, err := libreoffice.Ensure(libreoffice.Options{
		Configured:  cfg.Paths.LibreOffice,
		AutoInstall: cfg.ShouldAutoInstallLibreOffice(runtime.GOOS),
		MSIURL:      cfg.Paths.LibreOfficeMSIURL,
	}); err != nil {
		slog.Warn("LibreOffice unavailable — Office documents (doc/xls/ppt) cannot be converted", "error", err)
	} else {
		slog.Info("LibreOffice ready", "path", path)
	}

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.GET("/healthz", func(c *gin.Context) {
		if err := db.Ping(); err != nil {
			c.JSON(http.StatusServiceUnavailable, gin.H{"status": "db_unavailable"})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	settingsRepo := storage.NewSettingsRepo(db)
	statsRepo := stats.NewRepo(db)
	historyRepo := ophistory.NewRepo(db)
	maxSvc, printerSvc, err := kiosk.RegisterRoutes(r, cfg, settingsRepo, statsRepo, historyRepo)
	if err != nil {
		slog.Error("failed to register kiosk routes", "error", err)
		os.Exit(1)
	}
	if err := admin.RegisterRoutes(r, cfg, settingsRepo, statsRepo, historyRepo, printerSvc, maxSvc); err != nil {
		slog.Error("failed to register admin routes", "error", err)
		os.Exit(1)
	}

	ln, err := net.Listen("tcp", cfg.Server.Addr)
	if err != nil {
		slog.Error("failed to listen", "addr", cfg.Server.Addr, "error", err)
		os.Exit(1)
	}

	url := publicURL(cfg.Server.Addr)
	slog.Info("starting server", "addr", cfg.Server.Addr, "url", url)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	uptimeDone := make(chan struct{})
	go func() {
		statsRepo.TrackUptime(ctx)
		close(uptimeDone)
	}()
	go maxSvc.Run(ctx)
	go historyRepo.RunRetention(ctx)

	srv := &http.Server{Handler: r}
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("server stopped", "error", err)
			stop()
		}
	}()

	if cfg.ShouldOpenBrowser(runtime.GOOS) {
		go openBrowserWhenReady(url)
	}

	<-ctx.Done()
	slog.Info("shutting down")
	select {
	case <-uptimeDone:
	case <-time.After(time.Second):
		slog.Warn("uptime counter did not stop in time")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	maxSvc.NotifyShutdown(shutdownCtx)
	_ = kioskhost.CloseBrowser()
	_ = srv.Shutdown(shutdownCtx)
}

func ensureDirs(cfg *config.Config) error {
	dirs := []string{
		filepath.Dir(cfg.Database.Path),
		filepath.Dir(cfg.Logging.Path),
		cfg.Paths.Uploads,
		cfg.Paths.PrintJobs,
		cfg.ScanJobsDir(),
		cfg.EmailInboxDir(),
		filepath.Join(cfg.DataRoot(), "max-inbox"),
		filepath.Join(cfg.DataRoot(), "copy-jobs"),
	}
	for _, dir := range dirs {
		if dir == "" || dir == "." {
			continue
		}
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	return nil
}

func setupLogger(cfg *config.Config) (*slog.Logger, *os.File, error) {
	if err := os.MkdirAll(filepath.Dir(cfg.Logging.Path), 0o755); err != nil {
		return nil, nil, err
	}

	f, err := os.OpenFile(cfg.Logging.Path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, nil, err
	}

	level := slog.LevelInfo
	switch cfg.Logging.Level {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}

	handler := slog.NewJSONHandler(io.MultiWriter(os.Stdout, f), &slog.HandlerOptions{Level: level})
	return slog.New(handler), f, nil
}
