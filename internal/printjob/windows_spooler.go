package printjob

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	windowsJobDiscoveryTimeout  = 5 * time.Second
	windowsJobCompletionTimeout = 30 * time.Minute
	windowsJobPollInterval      = 500 * time.Millisecond
)

type windowsSpoolJob struct {
	ID       int    `json:"id"`
	Status   string `json:"status"`
	Document string `json:"document"`
}

// ProbeWindowsPrinter reads the real Windows queue state without submitting a job.
func ProbeWindowsPrinter(configured string) (name, state string, available bool, err error) {
	printer, err := resolveWindowsPrinter(configured)
	if err != nil {
		return "", "", false, err
	}
	quoted := strings.ReplaceAll(printer, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $p=Get-CimInstance Win32_Printer | Where-Object {$_.Name -eq '%s'} | Select-Object -First 1; if (-not $p) { throw 'printer not found' }; [pscustomobject]@{name=[string]$p.Name; offline=[bool]$p.WorkOffline; status=[string]$p.Status} | ConvertTo-Json -Compress`, quoted)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return printer, "", false, fmt.Errorf("проверка принтера Windows: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	var result struct {
		Name    string `json:"name"`
		Offline bool   `json:"offline"`
		Status  string `json:"status"`
	}
	if err := json.Unmarshal(out, &result); err != nil {
		return printer, "", false, fmt.Errorf("ответ принтера Windows: %w", err)
	}
	if strings.TrimSpace(result.Name) != "" {
		printer = strings.TrimSpace(result.Name)
	}
	state = strings.TrimSpace(result.Status)
	return printer, state, !result.Offline && !strings.EqualFold(state, "Error"), nil
}

func resolveWindowsPrinter(configured string) (string, error) {
	if name := strings.TrimSpace(configured); name != "" {
		return name, nil
	}
	script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $p=Get-CimInstance Win32_Printer | Where-Object {$_.Default -eq $true} | Select-Object -First 1 -ExpandProperty Name; if (-not $p) { throw 'default printer not found' }; [Console]::Write($p)`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("не удалось определить принтер Windows: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	name := strings.TrimSpace(string(out))
	if name == "" {
		return "", fmt.Errorf("принтер Windows по умолчанию не найден")
	}
	return name, nil
}

func listWindowsPrintJobs(printer string) ([]windowsSpoolJob, error) {
	quoted := strings.ReplaceAll(printer, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new(); @((Get-PrintJob -PrinterName '%s') | ForEach-Object { [pscustomobject]@{id=[int]$_.ID; status=[string]$_.JobStatus; document=[string]$_.DocumentName} }) | ConvertTo-Json -Compress`, quoted)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("опрос очереди печати Windows: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	var jobs []windowsSpoolJob
	if text := strings.TrimSpace(string(out)); text != "" && text != "null" {
		if err := json.Unmarshal([]byte(text), &jobs); err != nil {
			return nil, fmt.Errorf("ответ очереди печати Windows: %w", err)
		}
	}
	return jobs, nil
}

func monitorWindowsPrint(printer, filePath string, completion *PrintCompletion, submit func() error) error {
	beforeJobs, err := listWindowsPrintJobs(printer)
	if err != nil {
		return err
	}
	before := make(map[int]struct{}, len(beforeJobs))
	for _, job := range beforeJobs {
		before[job.ID] = struct{}{}
	}

	submitDone := make(chan error, 1)
	go func() { submitDone <- submit() }()

	document := strings.ToLower(strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath)))
	discoveryDeadline := time.Now().Add(windowsJobDiscoveryTimeout)
	completionDeadline := time.Now().Add(windowsJobCompletionTimeout)
	ticker := time.NewTicker(windowsJobPollInterval)
	defer ticker.Stop()

	var trackedID int
	var submitErr error
	var submitFinished bool
	queryFailures := 0
	device := windowsDeviceTracker{}
	var previousDevice windowsDeviceState
	var haveDevice bool
	var reportedDeviceError bool

	for {
		select {
		case submitErr = <-submitDone:
			submitFinished = true
			if submitErr != nil {
				return submitErr
			}
		case <-ticker.C:
			jobs, queryErr := listWindowsPrintJobs(printer)
			if queryErr != nil {
				queryFailures++
				if queryFailures >= 5 {
					return queryErr
				}
				continue
			}
			queryFailures = 0
			deviceState, deviceErr := readWindowsDeviceState(printer)
			if deviceErr == nil {
				if !haveDevice || deviceState != previousDevice {
					slog.Info("windows printer state", "printer", printer, "status", deviceState.Status, "extended", deviceState.Extended, "offline", deviceState.Offline, "error", deviceState.Error)
					previousDevice, haveDevice = deviceState, true
				}
				reportedDeviceError = false
				device.observe(deviceState)
			} else {
				if !reportedDeviceError {
					slog.Warn("windows printer state unavailable", "printer", printer, "error", deviceErr)
					reportedDeviceError = true
				}
				device.idleSamples = 0
			}

			if trackedID == 0 {
				trackedID = selectNewWindowsJob(jobs, before, document)
				if trackedID != 0 {
					slog.Info("windows print job detected", "printer", printer, "spool_job_id", trackedID, "file", filepath.Base(filePath))
				}
			}

			if trackedID != 0 {
				job, found := findWindowsJob(jobs, trackedID)
				if !found {
					if !submitFinished {
						continue
					}
					if deviceErr == nil && deviceState.busy() {
						if time.Now().After(completionDeadline) {
							return fmt.Errorf("превышено время ожидания принтера Windows")
						}
						continue
					}
					if device.sawBusy && deviceErr == nil && deviceState.idle() && device.idleSamples < 2 {
						continue
					}
					if completion != nil {
						// WMI may mirror spooler state rather than physical output.
						completion.Confirmed = false
					}
					slog.Info("windows print job left queue", "printer", printer, "spool_job_id", trackedID, "device_idle_reported", device.confirmed() && deviceErr == nil)
					return nil
				}
				state := normalizeWindowsJobStatus(job.Status)
				if windowsPaperOut(state) {
					return fmt.Errorf("%w: задание Windows %d, состояние %s", ErrPaperOut, trackedID, state)
				}
				if windowsPaperJam(state) {
					return fmt.Errorf("%w: задание Windows %d, состояние %s", ErrPaperJam, trackedID, state)
				}
				if windowsJobFailed(state) {
					return fmt.Errorf("задание Windows %d: состояние %s", trackedID, state)
				}
				if windowsJobCompleted(state) && submitFinished {
					if deviceErr == nil && deviceState.busy() {
						if time.Now().After(completionDeadline) {
							return fmt.Errorf("превышено время ожидания принтера Windows")
						}
						continue
					}
					if device.sawBusy && deviceErr == nil && deviceState.idle() && device.idleSamples < 2 {
						continue
					}
					if completion != nil {
						// WMI may mirror spooler state rather than physical output.
						completion.Confirmed = false
					}
					return nil
				}
			}

			if trackedID == 0 && submitFinished && time.Now().After(discoveryDeadline) {
				if time.Now().After(completionDeadline) {
					return fmt.Errorf("превышено время ожидания принтера Windows")
				}
				// USB printers can consume the spool entry between PowerShell polls.
				// Successful submission without an observed job ID is not a print failure.
				// Keep waiting while the device explicitly reports that it is busy.
				if deviceErr == nil && deviceState.busy() {
					continue
				}
				if device.sawBusy && deviceErr == nil && deviceState.idle() && device.idleSamples < 2 {
					continue
				}
				if completion != nil {
					completion.Confirmed = false
				}
				slog.Warn("windows print submitted without observed spool job", "printer", printer, "file", filepath.Base(filePath), "output_confirmed", false)
				return nil
			}
			if time.Now().After(completionDeadline) {
				return fmt.Errorf("превышено время ожидания задания печати Windows%s", spoolJobSuffix(trackedID))
			}
		}
	}
}

func selectNewWindowsJob(jobs []windowsSpoolJob, before map[int]struct{}, document string) int {
	fallback := 0
	for _, job := range jobs {
		if _, existed := before[job.ID]; existed {
			continue
		}
		if strings.Contains(strings.ToLower(job.Document), document) {
			return job.ID
		}
		if job.ID > fallback {
			fallback = job.ID
		}
	}
	return fallback
}

func findWindowsJob(jobs []windowsSpoolJob, id int) (windowsSpoolJob, bool) {
	for _, job := range jobs {
		if job.ID == id {
			return job, true
		}
	}
	return windowsSpoolJob{}, false
}

func normalizeWindowsJobStatus(status string) string {
	return strings.ToLower(strings.Join(strings.Fields(strings.TrimSpace(status)), " "))
}

func windowsPaperOut(status string) bool {
	for _, marker := range []string{"paperout", "paper out", "out of paper", "no paper"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func windowsPaperJam(status string) bool {
	for _, marker := range []string{"paperjam", "paper jam", "jammed"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func windowsJobFailed(status string) bool {
	for _, marker := range []string{"error", "offline", "paperout", "paper out", "paused", "blocked", "deleted", "deleting", "cancelled", "canceled", "user intervention", "door open", "not available", "out of memory"} {
		if strings.Contains(status, marker) {
			return true
		}
	}
	return false
}

func windowsJobCompleted(status string) bool {
	return strings.Contains(status, "completed") || strings.Contains(status, "printed")
}

func spoolJobSuffix(id int) string {
	if id == 0 {
		return ""
	}
	return " " + strconv.Itoa(id)
}

// Queue completion alone only confirms delivery to the device. Require an
// observed busy -> idle transition before ending the busy screen. This is
// still not proof of physical output: some drivers only mirror queue state.
type windowsDeviceState struct {
	Status   int  `json:"status"`
	Extended int  `json:"extended"`
	Offline  bool `json:"offline"`
	Error    int  `json:"error"`
}

func (s windowsDeviceState) busy() bool {
	if s.Offline || (s.Error != 0 && s.Error != 2 && s.Error != 3 && s.Error != 5) {
		return false
	}
	for _, status := range []int{s.Status, s.Extended} {
		switch status {
		case 4, 5, 10, 13, 14:
			return true
		}
	}
	return false
}
func (s windowsDeviceState) idle() bool {
	return !s.Offline && !s.busy() && (s.Error == 0 || s.Error == 2 || s.Error == 3 || s.Error == 5) && (s.Status == 3 || s.Extended == 3)
}

type windowsDeviceTracker struct {
	sawBusy     bool
	idleSamples int
}

func (t *windowsDeviceTracker) observe(s windowsDeviceState) {
	if s.busy() {
		t.sawBusy = true
	}
	if s.idle() && t.sawBusy {
		t.idleSamples++
	} else {
		t.idleSamples = 0
	}
}
func (t windowsDeviceTracker) confirmed() bool { return t.sawBusy && t.idleSamples >= 2 }
func readWindowsDeviceState(printer string) (windowsDeviceState, error) {
	quoted := strings.ReplaceAll(printer, "'", "''")
	script := fmt.Sprintf(`$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $p=Get-CimInstance Win32_Printer | Where-Object {$_.Name -eq '%s'} | Select-Object -First 1; if (-not $p) { throw 'printer not found' }; [pscustomobject]@{status=[int]$p.PrinterStatus; extended=[int]$p.ExtendedPrinterStatus; offline=[bool]$p.WorkOffline; error=[int]$p.DetectedErrorState} | ConvertTo-Json -Compress`, quoted)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
	if err != nil {
		return windowsDeviceState{}, fmt.Errorf("состояние устройства Windows: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	var state windowsDeviceState
	if err := json.Unmarshal(out, &state); err != nil {
		return state, err
	}
	return state, nil
}
