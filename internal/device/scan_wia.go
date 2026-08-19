package device

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func scanWindows(destPDF string, opt ScanOptions) error {
	if path := findNAPS2(); path != "" {
		if err := scanNAPS2(path, destPDF); err == nil {
			return nil
		} else {
			slog.Warn("naps2 scan failed, trying WIA", "error", err)
		}
	}
	return scanWIA(destPDF, opt)
}

func findNAPS2() string {
	candidates := []string{
		`C:\Program Files\NAPS2\NAPS2.Console.exe`,
		`C:\Program Files (x86)\NAPS2\NAPS2.Console.exe`,
	}
	if p, err := exec.LookPath("NAPS2.Console.exe"); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	if p, err := exec.LookPath("naps2.console"); err == nil {
		candidates = append([]string{p}, candidates...)
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}

func scanNAPS2(bin, destPDF string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, "-o", destPDF, "--force")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("сканирование NAPS2: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if st, err := os.Stat(destPDF); err != nil || st.Size() == 0 {
		return fmt.Errorf("сканер не вернул файл")
	}
	return nil
}

func scanWIA(destPDF string, opt ScanOptions) error {
	dir := filepath.Dir(destPDF)
	img := filepath.Join(dir, "scan.png")
	script := filepath.Join(dir, "wia-scan.ps1")
	color := "0"
	if opt.Color {
		color = "1"
	}
	if err := os.WriteFile(script, []byte(wiaScript), 0o644); err != nil {
		return fmt.Errorf("скрипт сканера: %w", err)
	}
	defer os.Remove(script)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, "powershell",
		"-NoProfile", "-NonInteractive", "-ExecutionPolicy", "Bypass",
		"-File", script,
		"-Output", img,
		"-Color", color,
		"-Dpi", fmt.Sprintf("%d", opt.dpi()),
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if strings.Contains(strings.ToLower(msg), "scanner not found") {
			return fmt.Errorf("сканер не найден")
		}
		if msg == "" {
			return fmt.Errorf("сканирование: %w", err)
		}
		return fmt.Errorf("сканирование: %s", msg)
	}
	if st, err := os.Stat(img); err != nil || st.Size() == 0 {
		return fmt.Errorf("сканер не вернул изображение")
	}
	defer os.Remove(img)
	return imageToPDF(img, destPDF)
}

const wiaScript = `param(
  [Parameter(Mandatory=$true)][string]$Output,
  [string]$Color = "1",
  [int]$Dpi = 200
)
$ErrorActionPreference = "Stop"
if ($Dpi -lt 75) { $Dpi = 200 }

$wia = New-Object -ComObject WIA.DeviceManager
$device = $null
foreach ($info in $wia.DeviceInfos) {
  if ($info.Type -eq 1) {
    $device = $info.Connect()
    break
  }
}
if ($null -eq $device) { throw "scanner not found" }
if ($device.Items.Count -lt 1) { throw "scanner not found" }
$item = $device.Items.Item(1)

function Set-WiaProp($obj, $id, $value) {
  try { $obj.Properties.Item([string]$id).Value = $value } catch {}
}

if ($Color -eq "1") {
  Set-WiaProp $item 6146 1
  Set-WiaProp $item 4103 3
} else {
  Set-WiaProp $item 6146 2
  Set-WiaProp $item 4103 2
}
Set-WiaProp $item 6147 $Dpi
Set-WiaProp $item 6148 $Dpi

$png = "{B96B3CAF-0728-11D3-9D7B-0000F81EF32E}"
$image = $null
try { $image = $item.Transfer($png) } catch { $image = $item.Transfer() }
if ($null -eq $image) { throw "scan transfer failed" }
$image.SaveFile($Output)
`
