package printjob

import (
	"bufio"
	"fmt"
	"image"
	"io"
	"math"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

var (
	rgbColorOperator  = regexp.MustCompile(`(?m)([-+]?(?:\d*\.)?\d+)\s+([-+]?(?:\d*\.)?\d+)\s+([-+]?(?:\d*\.)?\d+)\s+(?:rg|RG)\b`)
	cmykColorOperator = regexp.MustCompile(`(?m)([-+]?(?:\d*\.)?\d+)\s+([-+]?(?:\d*\.)?\d+)\s+([-+]?(?:\d*\.)?\d+)\s+([-+]?(?:\d*\.)?\d+)\s+(?:k|K)\b`)
)

// detectDocumentColor returns a conservative initial print recommendation.
// Failure never blocks document preparation; callers fall back to monochrome.
func detectDocumentColor(sourcePath, previewPath string, kind PreviewKind) (bool, error) {
	if kind == PreviewImage {
		f, err := os.Open(sourcePath)
		if err != nil {
			return false, err
		}
		defer f.Close()
		img, _, err := image.Decode(bufio.NewReader(f))
		if err != nil {
			return false, err
		}
		return imageHasColor(img), nil
	}
	return pdfHasColor(previewPath)
}

func imageHasColor(img image.Image) bool {
	b := img.Bounds()
	pixelCount := b.Dx() * b.Dy()
	step := 1
	if pixelCount > 200000 {
		step = int(math.Ceil(math.Sqrt(float64(pixelCount) / 200000)))
	}
	sampled, colored := 0, 0
	for y := b.Min.Y; y < b.Max.Y; y += step {
		for x := b.Min.X; x < b.Max.X; x += step {
			r16, g16, b16, a16 := img.At(x, y).RGBA()
			if a16 < 0x2000 {
				continue
			}
			r, g, blue := int(r16>>8), int(g16>>8), int(b16>>8)
			maxC, minC := max(r, g, blue), min(r, g, blue)
			sampled++
			if maxC-minC >= 18 && maxC >= 35 {
				colored++
			}
		}
	}
	// Ignore isolated compression noise and scanner chromatic aberration.
	return colored >= 24 && colored*200 >= sampled
}

func pdfHasColor(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	contentColor := false
	err = api.ExtractContent(f, nil, func(r io.Reader, _ int) error {
		data, readErr := io.ReadAll(r)
		if readErr == nil && contentUsesColor(string(data)) {
			contentColor = true
		}
		return readErr
	}, model.NewDefaultConfiguration())
	_ = f.Close()
	if err != nil {
		return false, fmt.Errorf("analyse pdf content: %w", err)
	}
	if contentColor {
		return true, nil
	}

	f, err = os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	imageColor := false
	err = api.ExtractImages(f, nil, func(extracted model.Image, _ bool, _ int) error {
		if imageColor || extracted.Reader == nil || extracted.IsImgMask || extracted.Comp < 3 {
			return nil
		}
		img, _, decodeErr := image.Decode(extracted.Reader)
		if decodeErr != nil {
			// A multi-component image is a safer color recommendation when its
			// encoded format cannot be decoded by the standard image readers.
			imageColor = extracted.Comp >= 3
			return nil
		}
		imageColor = imageHasColor(img)
		return nil
	}, model.NewDefaultConfiguration())
	if err != nil {
		return false, fmt.Errorf("analyse pdf images: %w", err)
	}
	return imageColor, nil
}

func contentUsesColor(content string) bool {
	for _, match := range rgbColorOperator.FindAllStringSubmatch(content, -1) {
		r, _ := strconv.ParseFloat(match[1], 64)
		g, _ := strconv.ParseFloat(match[2], 64)
		b, _ := strconv.ParseFloat(match[3], 64)
		if math.Max(r, math.Max(g, b))-math.Min(r, math.Min(g, b)) > .015 {
			return true
		}
	}
	for _, match := range cmykColorOperator.FindAllStringSubmatch(content, -1) {
		for _, component := range match[1:4] {
			value, _ := strconv.ParseFloat(strings.TrimSpace(component), 64)
			if value > .015 {
				return true
			}
		}
	}
	return false
}
