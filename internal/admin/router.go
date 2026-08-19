package admin

import (
	"database/sql"
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"print-kiosk/internal/config"
	"print-kiosk/internal/stats"
	"print-kiosk/internal/storage"
)

//go:embed web/*
var webFS embed.FS

func RegisterRoutes(r *gin.Engine, cfg *config.Config, settings *storage.SettingsRepo, db *sql.DB) error {
	sessions := NewSessionStore()
	h := NewHandler(cfg, sessions, settings, stats.NewRepo(db))

	api := r.Group("/api/admin")
	{
		api.POST("/login", h.Login)
		api.POST("/logout", h.Logout)

		auth := api.Group("")
		auth.Use(h.RequireAuth())
		{
			auth.GET("/me", h.Me)
			auth.GET("/overview", h.Overview)
			auth.GET("/settings", h.GetSettings)
			auth.PUT("/settings", h.UpdateSettings)
			auth.POST("/email/test", h.TestEmail)
			auth.POST("/max/test", h.TestMAX)
		}
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		return err
	}

	r.GET("/admin", func(c *gin.Context) {
		c.Redirect(http.StatusFound, "/admin/")
	})
	r.GET("/admin/*filepath", serveAdminStatic(static))

	return nil
}

func serveAdminStatic(static fs.FS) gin.HandlerFunc {
	return func(c *gin.Context) {
		name := c.Param("filepath")
		name = path.Clean("/" + name)
		if name == "/" {
			name = "/index.html"
		}
		name = strings.TrimPrefix(name, "/")
		if name == "" || strings.Contains(name, "..") {
			c.Status(http.StatusNotFound)
			return
		}

		data, err := fs.ReadFile(static, name)
		if err != nil {
			c.Status(http.StatusNotFound)
			return
		}

		c.Data(http.StatusOK, contentType(name), data)
	}
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
	case ".png":
		return "image/png"
	default:
		return "application/octet-stream"
	}
}
