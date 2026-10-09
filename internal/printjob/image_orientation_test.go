package printjob

import (
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestWebPPreviewAndPrintOrientation(t *testing.T) {
	s, err := NewService(Options{JobsDir: t.TempDir(), LibreOfficePath: "missing-libreoffice"})
	if err != nil {
		t.Fatal(err)
	}
	job, err := s.PrepareFromLocal(filepath.Join("testdata", "colors.webp"), "Фото 1.webp")
	if err != nil {
		t.Fatal(err)
	}
	if job.PreviewKind != PreviewImage {
		t.Fatalf("preview kind = %s", job.PreviewKind)
	}
	for _, orientation := range []string{"portrait", "landscape", "portrait"} {
		if err := s.ApplyImageOrientation(job, orientation); err != nil {
			t.Fatal(err)
		}
		dims, err := api.PageDimsFile(job.PreviewPath)
		if err != nil {
			t.Fatal(err)
		}
		if len(dims) != 1 {
			t.Fatalf("page dimensions: %v", dims)
		}
		if (dims[0].Width > dims[0].Height) != (orientation == "landscape") {
			t.Fatalf("%s page dimensions: %v", orientation, dims[0])
		}
	}
}
