package maxsvc

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDownloadAttachmentNamesFromContent(t *testing.T) {
	for _, tc := range []struct{ name, original, body, want string }{
		{"unnamed JPEG", "", "\xff\xd8\xff\xe0image", "Фото 1.jpg"},
		{"unnamed PDF", "", "%PDF-1.7\ndocument", "Документ 1.pdf"},
		{"preserve original", "Чек.pdf", "%PDF-1.7\ndocument", "Чек.pdf"},
		{"correct extension", "Фото.png", "\xff\xd8\xff\xe0image", "Фото.jpg"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte(tc.body)) }))
			defer server.Close()
			files, err := (&Service{}).downloadAttachments(context.Background(), t.TempDir(), []remoteFile{{Name: tc.original, URL: server.URL + "/i?r=opaque-token"}})
			if err != nil {
				t.Fatal(err)
			}
			if len(files) != 1 || files[0].Name != tc.want {
				t.Fatalf("files = %+v, want %s", files, tc.want)
			}
			if filepath.Ext(files[0].Path) != filepath.Ext(tc.want) {
				t.Fatalf("incorrect stored extension: %s", files[0].Path)
			}
			data, err := os.ReadFile(files[0].Path)
			if err != nil || string(data) != tc.body {
				t.Fatalf("download content changed: %v", err)
			}
		})
	}
}
