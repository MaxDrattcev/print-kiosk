package admin

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestMAXBindingRequiresAuthentication(t *testing.T) {
	h := &Handler{sessions: NewSessionStore()}
	r := gin.New()
	auth := r.Group("/api/admin", h.RequireAuth())
	auth.POST("/max/binding", h.StartMAXBinding)
	auth.GET("/max/binding/:id", h.GetMAXBinding)
	auth.DELETE("/max/binding/:id", h.CancelMAXBinding)
	for _, tc := range []struct{ method, path string }{{"POST", "/api/admin/max/binding"}, {"GET", "/api/admin/max/binding/id"}, {"DELETE", "/api/admin/max/binding/id"}} {
		res := httptest.NewRecorder()
		r.ServeHTTP(res, httptest.NewRequest(tc.method, tc.path, nil))
		if res.Code != http.StatusUnauthorized {
			t.Fatalf("%s = %d", tc.method, res.Code)
		}
	}
}
