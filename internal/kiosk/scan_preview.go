package kiosk

import (
	"bytes"
	"fmt"
	"golang.org/x/image/draw"
	"image"
	"image/png"
	"io"
	"os"
	"sync"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

var scanPreviewLock sync.Mutex

// Cache beside the source so scan-job cleanup removes previews as well.
func cachedScanPreviewPNG(path string) ([]byte, error) {
	scanPreviewLock.Lock()
	defer scanPreviewLock.Unlock()
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	cache := fmt.Sprintf("%s.preview-%d-%d.png", path, info.ModTime().UnixNano(), info.Size())
	if data, err := os.ReadFile(cache); err == nil {
		return data, nil
	}
	data, err := scanPreviewPNG(path)
	if err != nil {
		return nil, err
	}
	// A cache write failure must not prevent displaying the scan.
	_ = os.WriteFile(cache, data, 0600)
	return data, nil
}

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
	bounds := img.Bounds()
	if longest := max(bounds.Dx(), bounds.Dy()); longest > 1600 {
		width := max(1, bounds.Dx()*1600/longest)
		height := max(1, bounds.Dy()*1600/longest)
		resized := image.NewRGBA(image.Rect(0, 0, width, height))
		draw.ApproxBiLinear.Scale(resized, resized.Bounds(), img, bounds, draw.Src, nil)
		img = resized
	}
	var out bytes.Buffer
	encoder := png.Encoder{CompressionLevel: png.BestSpeed}
	if err := encoder.Encode(&out, img); err != nil {
		return nil, err
	}
	return out.Bytes(), nil
}
