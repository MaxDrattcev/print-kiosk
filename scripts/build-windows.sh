#!/usr/bin/env bash
# Cross-compile print-kiosk for Windows (amd64) from macOS/Linux.
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
OUT_DIR="${ROOT}/dist/windows"
OUT_BIN="${OUT_DIR}/print-kiosk.exe"

mkdir -p "${OUT_DIR}"

echo "Building Windows amd64 binary..."
cd "${ROOT}"
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w" -o "${OUT_BIN}" .

cp "${ROOT}/configs/config.example.yaml" "${OUT_DIR}/config.example.yaml"
cp "${ROOT}/deploy/windows/start.bat" "${OUT_DIR}/start.bat"
cp "${ROOT}/deploy/windows/README.txt" "${OUT_DIR}/README.txt"

(
  cd "${OUT_DIR}"
  zip -9 ../print-kiosk-windows-amd64.zip \
    print-kiosk.exe \
    config.example.yaml \
    start.bat \
    README.txt
) || true

echo "OK: ${OUT_BIN}"
echo "Zip: ${ROOT}/dist/print-kiosk-windows-amd64.zip (if zip is installed)"
echo "Copy dist/windows/ (or the zip) to the Windows PC."
