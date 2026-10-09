package printjob

import (
	"fmt"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Printer copy counts repeat separate documents, so a single-page document
// needs explicit repeated pages to place its copies on opposite sheet sides.
func prepareSinglePageDuplexCopies(path string, opt PrintOptions) (string, PrintOptions, func(), error) {
	noop := func() {}
	if !opt.Duplex || opt.Copies < 2 || !strings.EqualFold(filepath.Ext(path), ".pdf") {
		return path, opt, noop, nil
	}
	count, err := pageCount(path)
	if err != nil {
		return "", opt, noop, err
	}
	pages, _, err := ParsePageRange(opt.PageRange, count)
	if err != nil {
		return "", opt, noop, err
	}
	if len(pages) != 1 {
		return path, opt, noop, nil
	}
	dir, err := os.MkdirTemp("", "print-kiosk-duplex-*")
	if err != nil {
		return "", opt, noop, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	out := filepath.Join(dir, "duplex-copies.pdf")
	sequence := make([]string, opt.Copies)
	for i := range sequence {
		sequence[i] = strconv.Itoa(pages[0])
	}
	if err := api.CollectFile(path, out, sequence, nil); err != nil {
		cleanup()
		return "", opt, noop, fmt.Errorf("подготовка двусторонних копий: %w", err)
	}
	opt.Copies = 1
	opt.PageRange = ""
	return out, opt, cleanup, nil
}
