package device

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func TestImageToA4PDF(t *testing.T) {
	dir := t.TempDir()
	jpgPath := filepath.Join(dir, "photo.jpg")
	pdfPath := filepath.Join(dir, "out.pdf")

	img := image.NewRGBA(image.Rect(0, 0, 400, 300))
	for y := 0; y < 300; y++ {
		for x := 0; x < 400; x++ {
			img.Set(x, y, color.RGBA{R: 200, G: 40, B: 40, A: 255})
		}
	}
	f, err := os.Create(jpgPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if err := ImageToA4PDF(jpgPath, pdfPath); err != nil {
		t.Fatal(err)
	}

	n, err := api.PageCountFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("pages = %d, want 1", n)
	}
	dims, err := api.PageDimsFile(pdfPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(dims) != 1 {
		t.Fatalf("dims = %v", dims)
	}
	if dims[0].Width < 590 || dims[0].Width > 600 || dims[0].Height < 835 || dims[0].Height > 845 {
		t.Fatalf("page %v is not A4 points", dims[0])
	}
}
