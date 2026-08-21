package device

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// ProbeScanner checks whether the operating system currently exposes a scanner.
// It never starts an acquisition, so it is safe to call from the specialist overview.
func ProbeScanner() (name string, available bool, err error) {
	if runtime.GOOS == "windows" {
		script := `$ErrorActionPreference='Stop'; [Console]::OutputEncoding=[Text.UTF8Encoding]::new(); $m=New-Object -ComObject WIA.DeviceManager; $d=@($m.DeviceInfos | Where-Object {$_.Type -eq 1}) | Select-Object -First 1; if (-not $d) { [Console]::Write(''); exit 0 }; [Console]::Write([string]$d.Properties.Item('Name').Value)`
		out, cmdErr := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-Command", script).CombinedOutput()
		if cmdErr != nil {
			return "", false, fmt.Errorf("проверка WIA: %w", cmdErr)
		}
		name = strings.TrimSpace(string(out))
		return name, name != "", nil
	}

	bin, lookErr := exec.LookPath("scanimage")
	if lookErr != nil {
		return "", false, fmt.Errorf("scanimage не установлен")
	}
	out, cmdErr := exec.Command(bin, "-L").CombinedOutput()
	if cmdErr != nil {
		return "", false, fmt.Errorf("проверка SANE: %w", cmdErr)
	}
	text := strings.TrimSpace(string(out))
	if text == "" || strings.Contains(strings.ToLower(text), "no scanners were identified") {
		return "", false, nil
	}
	return strings.TrimSpace(strings.Split(text, "\n")[0]), true, nil
}
