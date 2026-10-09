package scanjob

import "print-kiosk/internal/device"

func performScan(destPDF string, dryRun bool) error {
	return device.ScanToPDF(destPDF, device.ScanOptions{Color: true, DPI: 200}, dryRun)
}
