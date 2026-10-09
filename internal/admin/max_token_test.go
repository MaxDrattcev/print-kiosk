package admin

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"print-kiosk/internal/storage"
)

func TestMAXTokenRevealRequiresAuthAndSettingsStayMasked(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSettingsRepo(db)
	const fixture = "fixture-not-a-real-token"
	if err := repo.SetMany(map[string]string{storage.SettingMaxBotToken: fixture}); err != nil {
		t.Fatal(err)
	}
	h := &Handler{settings: repo, sessions: NewSessionStore()}
	r := gin.New()
	auth := r.Group("/api/admin", h.RequireAuth())
	auth.POST("/max/token/reveal", h.RevealMAXToken)
	auth.GET("/settings", h.GetSettings)
	unauth := httptest.NewRecorder()
	r.ServeHTTP(unauth, httptest.NewRequest(http.MethodPost, "/api/admin/max/token/reveal", nil))
	if unauth.Code != http.StatusUnauthorized || strings.Contains(unauth.Body.String(), fixture) {
		t.Fatal("token endpoint must require authentication")
	}
	session, err := h.sessions.Create("specialist")
	if err != nil {
		t.Fatal(err)
	}
	request := func(method, path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, nil)
		req.AddCookie(&http.Cookie{Name: sessionCookieName, Value: session})
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res
	}
	revealed := request(http.MethodPost, "/api/admin/max/token/reveal")
	var body struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal(revealed.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if revealed.Code != http.StatusOK || body.Token != fixture || revealed.Header().Get("Cache-Control") != "no-store" {
		t.Fatal("authenticated reveal failed or response is cacheable")
	}
	masked := request(http.MethodGet, "/api/admin/settings")
	if masked.Code != http.StatusOK || strings.Contains(masked.Body.String(), fixture) || !strings.Contains(masked.Body.String(), "********") {
		t.Fatal("regular settings must keep token masked")
	}
}
