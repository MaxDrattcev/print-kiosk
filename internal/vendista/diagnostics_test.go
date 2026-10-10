package vendista

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestDiagnoseRequiresFreshMatchingDelivery(t *testing.T) {
	for _, mismatch := range []bool{false, true} {
		t.Run(fmt.Sprint(mismatch), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				switch r.URL.Path {
				case "/terminals/7":
					fmt.Fprint(w, `{"success":true,"item":{"id":7}}`)
				case "/transactions":
					fmt.Fprint(w, `{"success":true,"items":[{"term_id":7,"time":"2026-10-10T12:00:00Z"},{"term_id":8,"time":"2026-10-11T12:00:00Z"}]}`)
				case "/terminals/7/commands":
					var body map[string]any
					json.NewDecoder(r.Body).Decode(&body)
					if r.Method != "POST" || body["command_id"] != float64(25) || body["life_time_interval"] != "00:00:15" {
						t.Errorf("unexpected diagnostic: %v", body)
					}
					fmt.Fprint(w, `{"success":true,"item":{"id":55}}`)
				case "/terminals/7/commands/55":
					id := 55
					if mismatch {
						id = 54
					}
					fmt.Fprintf(w, `{"success":true,"item":{"id":%d,"terminal_id":7,"command_id":25,"time_delivered":"2026-10-10T12:00:00Z"}}`, id)
				default:
					t.Error(r.URL.Path)
				}
			}))
			defer server.Close()
			c := &Client{Base: server.URL, Terminal: "7", Token: "secret", HTTP: server.Client(), Poll: time.Millisecond}
			result := c.Diagnose(context.Background())
			if (result["status"] == "ok") == mismatch {
				t.Fatalf("wrong delivery result: %v", result)
			}
			if result["last_operation_time"] != "2026-10-10T12:00:00Z" {
				t.Fatalf("wrong last operation: %v", result)
			}
		})
	}
}
