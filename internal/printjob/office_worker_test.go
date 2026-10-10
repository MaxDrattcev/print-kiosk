package printjob

import (
	"image"
	"image/png"
	"os"
	"path/filepath"
	"print-kiosk/internal/device"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestOfficeWorkerIntegration(t *testing.T) {
	if os.Getenv("PRINT_KIOSK_TEST_OFFICE") != "1" {
		t.Skip("requires installed LibreOffice")
	}
	s, err := NewService(Options{JobsDir: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	s.WarmOffice()
	var wg sync.WaitGroup
	for i := 0; i < 3; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			dir := t.TempDir()
			source := filepath.Join(dir, "sample.rtf")
			if err := os.WriteFile(source, []byte(`{\rtf1\ansi Hello kiosk}`), 0600); err != nil {
				t.Error(err)
				return
			}
			path, err := s.convertWithLibreOffice(source, dir)
			if err != nil {
				t.Error(err)
				return
			}
			if n, err := pageCount(path); err != nil || n != 1 {
				t.Errorf("pages=%d err=%v", n, err)
			}
		}()
	}
	wg.Wait()
	if s.office.cmd == nil {
		t.Fatal("worker stopped between documents")
	}
	// The queue is idle here; killing its owned process must be recovered next time.
	oldPID := s.office.cmd.Process.Pid
	_ = s.office.cmd.Process.Kill()
	<-s.office.exited
	dir := t.TempDir()
	source := filepath.Join(dir, "retry.rtf")
	_ = os.WriteFile(source, []byte(`{\rtf1\ansi After restart}`), 0600)
	if _, err := s.convertWithLibreOffice(source, dir); err != nil {
		t.Fatal(err)
	}
	if s.office.cmd.Process.Pid == oldPID {
		t.Fatal("worker was not restarted")
	}
	s.Close()
	if _, err := s.convertWithLibreOffice(source, dir); err == nil {
		t.Fatal("closed worker accepted conversion")
	}
}

func TestOfficeQueueLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell fixture")
	}
	dir := t.TempDir()
	template := filepath.Join(dir, "template.pdf")
	img := image.NewRGBA(image.Rect(0, 0, 20, 20))
	pngPath := filepath.Join(dir, "image.png")
	f, err := os.Create(pngPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	f.Close()
	if err = device.ImageToA4PDF(pngPath, template); err != nil {
		t.Fatal(err)
	}
	t.Setenv("OFFICE_TEST_PDF", template)
	fake := filepath.Join(dir, "office")
	script := `#!/bin/sh
out=""
convert=""
for arg in "$@"; do
 if [ "$previous" = "--outdir" ]; then out="$arg"; fi
 if [ "$arg" = "--convert-to" ]; then convert=1; fi
 previous="$arg"
 last="$arg"
done
if [ -n "$convert" ]; then
 cp "$OFFICE_TEST_PDF" "$out/$(basename "${last%.*}").pdf"
 exec sleep 300
else
 exec sleep 300
fi
`
	if err = os.WriteFile(fake, []byte(script), 0700); err != nil {
		t.Fatal(err)
	}
	s, err := NewService(Options{JobsDir: filepath.Join(dir, "jobs"), LibreOfficePath: fake})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	started := time.Now()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			d := t.TempDir()
			src := filepath.Join(d, "document with spaces.docx")
			os.WriteFile(src, []byte("fixture"), 0600)
			if _, err := s.convertWithLibreOffice(src, d); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if time.Since(started) > 5*time.Second {
		t.Fatal("waited for launcher instead of completed PDFs")
	}
	pid := s.office.cmd.Process.Pid
	profile := s.office.profile
	d := t.TempDir()
	src := filepath.Join(d, "another.xlsx")
	os.WriteFile(src, []byte("fixture"), 0600)
	if _, err = s.convertWithLibreOffice(src, d); err != nil {
		t.Fatal(err)
	}
	if pid != s.office.cmd.Process.Pid {
		t.Fatal("process not reused")
	}
	s.office.cmd.Process.Kill()
	<-s.office.exited
	d = t.TempDir()
	if _, err = s.convertWithLibreOffice(src, d); err != nil {
		t.Fatal(err)
	}
	if pid == s.office.cmd.Process.Pid {
		t.Fatal("process not restarted")
	}
	s.Close()
	if _, err = os.Stat(profile); !os.IsNotExist(err) {
		t.Fatal("profile not removed")
	}
	if _, err = s.convertWithLibreOffice(src, d); err == nil {
		t.Fatal("accepted job after shutdown")
	}
}
