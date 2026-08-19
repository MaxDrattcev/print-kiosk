package kiosk

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type endSessionRequest struct {
	PrintJobID       string `json:"print_job_id"`
	ScanJobID        string `json:"scan_job_id"`
	CopyJobID        string `json:"copy_job_id"`
	EmailSessionID   string `json:"email_session_id"`
	MaxSessionID     string `json:"max_session_id"`
	MaxScanSessionID string `json:"max_scan_session_id"`
}

func parseOptionalID(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if _, err := uuid.Parse(raw); err != nil {
		return ""
	}
	return raw
}

// EndSession drops in-progress jobs and local files when the visitor leaves
// (idle timeout or explicit home). Next visitor must not see previous files.
func (h *Handler) EndSession(c *gin.Context) {
	var in endSessionRequest
	_ = c.ShouldBindJSON(&in)

	if id := parseOptionalID(in.PrintJobID); id != "" && h.jobs != nil {
		h.jobs.Cleanup(id)
	}
	if id := parseOptionalID(in.ScanJobID); id != "" && h.scans != nil {
		h.scans.Abandon(id)
	}
	if id := parseOptionalID(in.CopyJobID); id != "" && h.copies != nil {
		h.copies.Delete(id)
	}
	if id := parseOptionalID(in.EmailSessionID); id != "" && h.mail != nil {
		h.mail.Abandon(id)
	}
	if h.max != nil {
		if id := parseOptionalID(in.MaxSessionID); id != "" {
			h.max.AbandonPrint(id)
		}
		if id := parseOptionalID(in.MaxScanSessionID); id != "" {
			h.max.AbandonScan(id)
		}
	}

	c.JSON(http.StatusOK, gin.H{"ok": true})
}
