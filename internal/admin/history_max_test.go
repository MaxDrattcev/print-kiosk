package admin

import (
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"print-kiosk/internal/storage"
)

func TestHistoryMAXAdminAvailability(t *testing.T) {
	db, err := storage.Open(filepath.Join(t.TempDir(), "settings.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	if err := storage.Migrate(db); err != nil {
		t.Fatal(err)
	}
	repo := storage.NewSettingsRepo(db)
	h := &Handler{settings: repo}
	r := gin.New()
	r.GET("/options", h.HistoryMAXOptions)
	r.POST("/send", h.SendHistoryMAXAdmin)
	for _, tc := range []struct {
		id   string
		want bool
	}{{"", false}, {"0", false}, {"-5", false}, {"42", true}} {
		if err := repo.SetMany(map[string]string{storage.SettingMaxAdminID: tc.id}); err != nil {
			t.Fatal(err)
		}
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest("GET", "/options", nil))
		var data struct {
			Configured bool `json:"admin_configured"`
		}
		if err := json.Unmarshal(res.Body.Bytes(), &data); err != nil {
			t.Fatal(err)
		}
		if res.Code != 200 || data.Configured != tc.want {
			t.Fatalf("id %q: %d %s", tc.id, res.Code, res.Body.String())
		}
		if !tc.want {
			res = httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/send", strings.NewReader(`{"report_id":"fixture"}`))
			req.Header.Set("Content-Type", "application/json")
			r.ServeHTTP(res, req)
			if res.Code != 400 || !strings.Contains(res.Body.String(), "ID") {
				t.Fatalf("missing recipient accepted: %d %s", res.Code, res.Body.String())
			}
		}
	}
}
