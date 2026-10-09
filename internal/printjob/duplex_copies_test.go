package printjob

import (
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"os"
	"path/filepath"
	"print-kiosk/internal/device"
	"testing"
)

func TestSinglePageCopiesBecomeDuplexPages(t *testing.T) {
	source := filepath.Join(t.TempDir(), "source.pdf")
	if err := device.ImageToA4PDF(filepath.Join("testdata", "colors.webp"), source); err != nil {
		t.Fatal(err)
	}
	for _, copies := range []int{2, 3, 4} {
		opt := PrintOptions{Duplex: true, Copies: copies, Orientation: "landscape", PageRange: "1", Scale: "fit"}
		path, actual, cleanup, err := prepareSinglePageDuplexCopies(source, opt)
		if err != nil {
			t.Fatal(err)
		}
		count, err := api.PageCountFile(path)
		if err != nil || count != copies {
			t.Fatalf("copies %d: pages=%d err=%v", copies, count, err)
		}
		if actual.Copies != 1 || actual.PageRange != "" || !actual.Duplex || actual.Orientation != opt.Orientation {
			t.Fatalf("options=%+v", actual)
		}
		original, err := api.PageCountFile(source)
		if err != nil || original != 1 {
			t.Fatal("source changed")
		}
		cleanup()
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatal("temporary file not cleaned up")
		}
	}
	path, opt, cleanup, err := prepareSinglePageDuplexCopies(source, PrintOptions{Copies: 2, Duplex: false})
	defer cleanup()
	if err != nil || path != source || opt.Copies != 2 {
		t.Fatal("simplex copies changed")
	}
}
