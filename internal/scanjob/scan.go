package scanjob

import (
	"fmt"
	"log/slog"
	"os"
	"time"
)

// performScan asks the device for a scan. For now this is a stub that
// creates a placeholder PDF (real WIA/TWAIN integration comes later).
func performScan(destPDF string, dryRun bool) error {
	slog.Info("scan started", "dest", destPDF, "dry_run", dryRun)
	// Simulate scanner warm-up / platen scan time.
	time.Sleep(3 * time.Second)
	if err := writePlaceholderPDF(destPDF); err != nil {
		return fmt.Errorf("сохранить скан: %w", err)
	}
	slog.Info("scan finished", "dest", destPDF)
	return nil
}

func writePlaceholderPDF(path string) error {
	// Minimal one-page A4 PDF with a short label.
	const pdf = `%PDF-1.4
1 0 obj<< /Type /Catalog /Pages 2 0 R >>endobj
2 0 obj<< /Type /Pages /Kids [3 0 R] /Count 1 >>endobj
3 0 obj<< /Type /Page /Parent 2 0 R /MediaBox [0 0 595 842] /Contents 4 0 R /Resources<< /Font<< /F1 5 0 R >> >> >>endobj
4 0 obj<< /Length 120 >>stream
BT
/F1 28 Tf
72 720 Td
(PrintStart scan) Tj
0 -40 Td
/F1 16 Tf
(Document scanned successfully) Tj
ET
endstream
endobj
5 0 obj<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica >>endobj
xref
0 6
0000000000 65535 f 
0000000009 00000 n 
0000000058 00000 n 
0000000115 00000 n 
0000000266 00000 n 
0000000438 00000 n 
trailer<< /Size 6 /Root 1 0 R >>
startxref
517
%%EOF
`
	return os.WriteFile(path, []byte(pdf), 0o644)
}
