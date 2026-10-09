package kiosk

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"print-kiosk/internal/device"
	"testing"
)

func TestScanPreviewPNG(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scan.png")
	pdf := filepath.Join(dir, "scan.pdf")
	img := image.NewRGBA(image.Rect(0, 0, 210, 297))
	for y := 0; y < 297; y++ {
		for x := 0; x < 210; x++ {
			img.Set(x, y, color.White)
		}
	}
	img.Set(20, 20, color.Black)
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = device.ImageToA4PDF(src, pdf); err != nil {
		t.Fatal(err)
	}
	data, err := scanPreviewPNG(pdf)
	if err != nil {
		t.Fatal(err)
	}
	got, err := png.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	if got.Bounds().Dx() != 210 || got.Bounds().Dy() != 297 {
		t.Fatalf("unexpected dimensions %v", got.Bounds())
	}
	r, _, _, _ := got.At(20, 20).RGBA()
	if r != 0 {
		t.Fatal("scan detail was lost")
	}
}

func TestScanPreviewCache(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "scan.png")
	pdf := filepath.Join(dir, "scan.pdf")
	f, err := os.Create(src)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, image.NewGray(image.Rect(0, 0, 1700, 2400))); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err := device.ImageToA4PDF(src, pdf); err != nil {
		t.Fatal(err)
	}
	first, err := cachedScanPreviewPNG(pdf)
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := png.DecodeConfig(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Height != 1600 || cfg.Width > 1600 {
		t.Fatalf("preview too large: %v", cfg)
	}
	second, err := cachedScanPreviewPNG(pdf)
	if err != nil || !bytes.Equal(first, second) {
		t.Fatalf("cache mismatch: %v", err)
	}
	files, _ := filepath.Glob(pdf + ".preview-*.png")
	if len(files) != 1 {
		t.Fatalf("expected one cache file, got %d", len(files))
	}
}
