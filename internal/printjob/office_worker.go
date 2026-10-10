package printjob

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"print-kiosk/internal/executil"
	"runtime"
	"sync"
	"time"
)

// The channel is a FIFO queue; one worker owns the process and isolated profile.
type officeRequest struct {
	source, dir string
	reply       chan officeResult
	queuedAt    time.Time
}
type officeResult struct {
	path string
	err  error
}
type officeWorker struct {
	once         sync.Once
	requests     chan officeRequest
	ctx          context.Context
	cancel       context.CancelFunc
	done         chan struct{}
	cmd          *exec.Cmd
	exited       chan struct{}
	profile, bin string
}

func (w *officeWorker) init(configured string) {
	w.once.Do(func() {
		w.ctx, w.cancel = context.WithCancel(context.Background())
		w.requests = make(chan officeRequest, 16)
		w.done = make(chan struct{})
		go func() {
			defer close(w.done)
			defer w.stop()
			for {
				select {
				case <-w.ctx.Done():
					return
				case r := <-w.requests:
					if r.source != "" {
						slog.Info("Office conversion dequeued", "queue_wait", time.Since(r.queuedAt))
					}
					path, err := w.run(configured, r.source, r.dir)
					r.reply <- officeResult{path, err}
				}
			}
		}()
	})
}
func (w *officeWorker) warm(configured string) {
	// A startup request uses the same queue, so it cannot race a conversion.
	w.init(configured)
	r := officeRequest{reply: make(chan officeResult, 1)}
	select {
	case w.requests <- r:
	case <-w.ctx.Done():
	}
}
func (w *officeWorker) convert(configured, source, dir string) (string, error) {
	w.init(configured)
	r := officeRequest{source: source, dir: dir, reply: make(chan officeResult, 1), queuedAt: time.Now()}
	select {
	case w.requests <- r:
	case <-w.ctx.Done():
		return "", fmt.Errorf("конвертер остановлен")
	}
	select {
	case out := <-r.reply:
		return out.path, out.err
	case <-w.ctx.Done():
		return "", fmt.Errorf("конвертер остановлен")
	}
}
func (w *officeWorker) close() {
	// init is safe even if the service has never converted a document.
	w.init("")
	w.cancel()
	<-w.done
}
func (w *officeWorker) start(configured string) error {
	if w.cmd != nil {
		select {
		case <-w.exited:
			w.stop()
		default:
			return nil
		}
	}
	bin, err := resolveLibreOffice(configured)
	if err != nil {
		return err
	}
	// Prefer the actual binary so stopping it also works with Unix launcher scripts.
	if runtime.GOOS != "windows" {
		actual := filepath.Join(filepath.Dir(bin), "soffice.bin")
		if st, e := os.Stat(actual); e == nil && !st.IsDir() {
			bin = actual
		}
	}
	profile, err := os.MkdirTemp("", "print-kiosk-office-*")
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "-env:UserInstallation="+fileURL(profile), "--headless", "--nologo", "--nodefault", "--norestore", "--accept=pipe,name=printkiosk_"+filepath.Base(profile)+";urp;StarOffice.ComponentContext")
	executil.HideWindow(cmd)
	if err = cmd.Start(); err != nil {
		os.RemoveAll(profile)
		return err
	}
	w.bin, w.profile, w.cmd = bin, profile, cmd
	w.exited = make(chan struct{})
	exited := w.exited
	go func() { _ = cmd.Wait(); close(exited) }()
	slog.Info("LibreOffice worker started", "pid", cmd.Process.Pid)
	return nil
}
func (w *officeWorker) stop() {
	if w.cmd == nil {
		return
	}
	select {
	case <-w.exited:
	default:
		if runtime.GOOS == "windows" {
			cmd := exec.Command("taskkill", "/PID", fmt.Sprint(w.cmd.Process.Pid), "/T", "/F")
			executil.HideWindow(cmd)
			_ = cmd.Run()
		}
		_ = w.cmd.Process.Kill()
		<-w.exited
	}
	_ = os.RemoveAll(w.profile)
	w.cmd = nil
	w.profile = ""
}
func (w *officeWorker) run(configured, source, dir string) (string, error) {
	if source == "" {
		err := w.start(configured)
		if err != nil {
			slog.Warn("LibreOffice warmup failed", "error", err)
		}
		return "", err
	}
	source, err := filepath.Abs(source)
	if err != nil {
		return "", err
	}
	dir, err = filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	started := time.Now()
	for attempt := 0; attempt < 2; attempt++ {
		if err = w.start(configured); err != nil {
			return "", err
		}
		ctx, cancel := context.WithTimeout(w.ctx, 90*time.Second)
		clientBin := w.bin
		if runtime.GOOS == "windows" {
			console := filepath.Join(filepath.Dir(w.bin), "soffice.com")
			if _, e := os.Stat(console); e == nil {
				clientBin = console
			}
		}
		cmd := exec.CommandContext(ctx, clientBin, "-env:UserInstallation="+fileURL(w.profile), "--headless", "--nologo", "--norestore", "--convert-to", convertFilter(source), "--outdir", dir, source)
		executil.HideWindow(cmd)
		cmd.Dir = dir
		// Discard output directly: inherited stdout pipes can keep Wait blocked.
		// Check the PDF concurrently with the lightweight forwarding command.
		cmd.WaitDelay = 200 * time.Millisecond
		if err := cmd.Start(); err != nil {
			cancel()
			w.stop()
			return "", err
		}
		commandDone := make(chan error, 1)
		go func() { commandDone <- cmd.Wait() }()
		var runErr error
		ticker := time.NewTicker(50 * time.Millisecond)
		var stablePath string
		var stableSize int64
		var stableMod time.Time
		var stableSince time.Time
		waiting := true
		for waiting {
			if pdf, ok := findConvertedPDF(dir, source); ok {
				st, e := os.Stat(pdf)
				if e == nil {
					if pdf != stablePath || st.Size() != stableSize || !st.ModTime().Equal(stableMod) {
						stablePath, stableSize, stableMod, stableSince = pdf, st.Size(), st.ModTime(), time.Now()
					} else if time.Since(stableSince) >= 150*time.Millisecond {
						if _, e := pageCount(pdf); e == nil {
							preview := filepath.Join(dir, "preview.pdf")
							if pdf != preview {
								err = os.Rename(pdf, preview)
								if err != nil {
									err = copyFile(pdf, preview)
									if err == nil {
										_ = os.Remove(pdf)
									}
								}
							} else {
								err = nil
							}
							ticker.Stop()
							cancel()
							if err != nil {
								return "", err
							}
							slog.Info("Office document converted", "duration", time.Since(started), "attempt", attempt+1)
							return preview, nil
						}
					}
				}
			}
			select {
			case e := <-commandDone:
				commandDone = nil
				runErr = e
				if e != nil {
					waiting = false
				}
			case <-ctx.Done():
				waiting = false
			case <-w.exited:
				waiting = false
			case <-ticker.C:
			}
		}
		ticker.Stop()
		timedOut := ctx.Err() != nil
		cancel()
		w.stop()
		if timedOut {
			return "", fmt.Errorf("превышено время ожидания конвертации документа")
		}
		err = fmt.Errorf("конвертация LibreOffice не выполнена: %v", runErr)
	}
	return "", err
}
