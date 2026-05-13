#!/bin/bash
# Build krouter Linux .AppImage (amd64)
#
# Usage: VERSION=v1.2.3 ARCH=x86_64 DIST=dist ./packaging/appimage/build.sh
#
# Inputs (from $DIST/):
#   krouter-linux-amd64           — daemon binary (for x86_64)
#   krouter-installer-linux-amd64 — installer binary (for x86_64)
#
# Output:
#   $DIST/krouter-$VERSION-x86_64.AppImage

set -euo pipefail

VERSION="${VERSION:?VERSION required}"
ARCH="${ARCH:-x86_64}"
DIST="${DIST:-dist}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

# Map ARCH to binary suffix.
case "$ARCH" in
  x86_64) BIN_SUFFIX="amd64" ;;
  aarch64) BIN_SUFFIX="arm64" ;;
  *) echo "Unsupported ARCH: $ARCH"; exit 1 ;;
esac

APPDIR="$(mktemp -d)/krouter.AppDir"
mkdir -p "$APPDIR/usr/bin"

echo "→ Populating AppDir..."
cp "$DIST/krouter-linux-${BIN_SUFFIX}"           "$APPDIR/usr/bin/krouter"
cp "$DIST/krouter-installer-linux-${BIN_SUFFIX}" "$APPDIR/usr/bin/krouter-installer"
chmod +x "$APPDIR/usr/bin/krouter" "$APPDIR/usr/bin/krouter-installer"

# Entry point script.
cp "$SCRIPT_DIR/AppRun" "$APPDIR/AppRun"
chmod +x "$APPDIR/AppRun"

# Desktop entry and icon.
cp "$SCRIPT_DIR/krouter.desktop" "$APPDIR/krouter.desktop"

# Icon — use provided .png or generate a minimal placeholder.
if [ -f "$SCRIPT_DIR/krouter.png" ]; then
  cp "$SCRIPT_DIR/krouter.png" "$APPDIR/krouter.png"
elif command -v convert >/dev/null 2>&1; then
  convert -size 256x256 xc:#3b82f6 -fill white \
    -pointsize 72 -gravity center -annotate 0 "k" \
    "$APPDIR/krouter.png"
else
  # Minimal 1×1 blue PNG (fallback — replace with real icon before release).
  printf '%s' \
    'iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAAC0lEQVQI12NgAAIABQ' \
    'AABjkB6QAAAABJRU5ErkJggg==' | base64 -d > "$APPDIR/krouter.png"
fi

# Download appimagetool if not in PATH.
TOOL="$(command -v appimagetool 2>/dev/null || true)"
if [ -z "$TOOL" ]; then
  TOOL="$DIST/appimagetool-${ARCH}.AppImage"
  if [ ! -f "$TOOL" ]; then
    echo "→ Downloading appimagetool..."
    curl -fsSL -o "$TOOL" \
      "https://github.com/AppImage/AppImageKit/releases/download/continuous/appimagetool-${ARCH}.AppImage"
    chmod +x "$TOOL"
  fi
  export APPIMAGE_EXTRACT_AND_RUN=1
fi

echo "→ Building AppImage..."
OUTPUT="$DIST/krouter-${VERSION}-${ARCH}.AppImage"
ARCH="$ARCH" "$TOOL" "$APPDIR" "$OUTPUT"

echo "✓ Created $OUTPUT"
