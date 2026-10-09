package kiosk

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"io"
	"os"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// Scanned pages contain one raster image. Return it without a browser PDF viewer.
// Other PDFs retain the existing PDF preview rather than showing only part of a page.
func scanPreviewPNG(path string) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var data []byte
	count := 0
	err = api.ExtractImages(f, []string{"1"}, func(img model.Image, _ bool, _ int) error {
		if img.Reader == nil || img.IsImgMask || img.Thumb {
			return nil
		}
		count++
		if count == 1 {
			var e error
			data, e = io.ReadAll(img.Reader)
			return e
		}
		return nil
	}, model.NewDefaultConfiguration())
	if err != nil {
		return nil, err
	}
	if count != 1 {
		return nil, fmt.Errorf("scan contains %d images", count)
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	var out bytes.Buffer
	if err := png.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
