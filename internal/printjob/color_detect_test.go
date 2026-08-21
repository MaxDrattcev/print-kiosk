package printjob

import (
	"image"
	"image/color"
	"testing"
)

func TestImageHasColor(t *testing.T) {
	gray := image.NewRGBA(image.Rect(0, 0, 100, 100))
	colorful := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := 0; y < 100; y++ {
		for x := 0; x < 100; x++ {
			gray.Set(x, y, color.RGBA{R: 120, G: 120, B: 120, A: 255})
			colorful.Set(x, y, color.RGBA{R: 45, G: 110, B: 220, A: 255})
		}
	}
	if imageHasColor(gray) {
		t.Fatal("gray image detected as color")
	}
	if !imageHasColor(colorful) {
		t.Fatal("color image detected as gray")
	}
}

func TestContentUsesColor(t *testing.T) {
	if contentUsesColor("0.2 0.2 0.2 rg") {
		t.Fatal("neutral RGB must remain monochrome")
	}
	if !contentUsesColor("0.2 0.4 0.8 rg") {
		t.Fatal("colored RGB operator not detected")
	}
	if contentUsesColor("0 0 0 0.8 k") {
		t.Fatal("black-only CMYK must remain monochrome")
	}
	if !contentUsesColor("0.1 0 0 0.2 k") {
		t.Fatal("colored CMYK operator not detected")
	}
}
