#!/usr/bin/env bash
# Cross-compila el .exe de Windows (solo bandeja, SIN ventana de consola) desde Linux.
# Requiere mingw-w64:  sudo pacman -S mingw-w64-gcc   (Arch/CachyOS)
set -euo pipefail
cd "$(dirname "$0")"

CC=x86_64-w64-mingw32-gcc
command -v "$CC" >/dev/null || { echo "falta $CC — instalá mingw-w64 (sudo pacman -S mingw-w64-gcc)"; exit 1; }

CGO_ENABLED=1 CC="$CC" GOOS=windows GOARCH=amd64 \
  go build -ldflags "-H windowsgui -s -w" -o aecode-voice-bridge.exe .

echo "OK -> $(pwd)/aecode-voice-bridge.exe"
