#!/bin/bash
# Build krouter macOS .dmg
#
# Usage: VERSION=v1.2.3 DIST=dist ./packaging/macos/build-dmg.sh
#
# Inputs (from $DIST/):
#   krouter-apple-macos           — universal daemon binary
#   krouter-installer-apple-macos — universal installer binary
#
# Output:
#   $DIST/krouter-$VERSION-macos.dmg

set -euo pipefail

VERSION="${VERSION:?VERSION required}"
DIST="${DIST:-dist}"
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

APP_DIR="$(mktemp -d)/krouter.app"
STAGING_DIR="$(mktemp -d)/dmg-staging"

echo "→ Creating krouter.app bundle..."
mkdir -p "$APP_DIR/Contents/MacOS"
mkdir -p "$APP_DIR/Contents/Resources"

# Installer binary is the primary executable (opens browser wizard, no dock icon).
cp "$DIST/krouter-installer-apple-macos" "$APP_DIR/Contents/MacOS/krouter-installer"
chmod +x "$APP_DIR/Contents/MacOS/krouter-installer"

# Daemon binary sits alongside the installer for `krouter serve` etc.
cp "$DIST/krouter-apple-macos" "$APP_DIR/Contents/MacOS/krouter"
chmod +x "$APP_DIR/Contents/MacOS/krouter"

# Inject version into Info.plist.
sed "s|<string>1</string>|<string>${VERSION#v}</string>|g; \
     s|<string>1.0</string>|<string>${VERSION#v}</string>|g" \
  "$SCRIPT_DIR/Info.plist" > "$APP_DIR/Contents/Info.plist"

# Generate AppIcon.icns from source PNG if not already present.
# Use white-background version so transparent areas don't render gray in Finder.
ICON_SRC="$SCRIPT_DIR/../icons/logo512-white.png"
if [ ! -f "$SCRIPT_DIR/AppIcon.icns" ] && [ -f "$ICON_SRC" ]; then
  echo "→ Generating AppIcon.icns from $ICON_SRC..."
  ICONSET="$(mktemp -d)/krouter.iconset"
  mkdir -p "$ICONSET"
  sips -z 16   16   "$ICON_SRC" --out "$ICONSET/icon_16x16.png"    >/dev/null
  sips -z 32   32   "$ICON_SRC" --out "$ICONSET/icon_16x16@2x.png" >/dev/null
  sips -z 32   32   "$ICON_SRC" --out "$ICONSET/icon_32x32.png"    >/dev/null
  sips -z 64   64   "$ICON_SRC" --out "$ICONSET/icon_32x32@2x.png" >/dev/null
  sips -z 128  128  "$ICON_SRC" --out "$ICONSET/icon_128x128.png"   >/dev/null
  sips -z 256  256  "$ICON_SRC" --out "$ICONSET/icon_128x128@2x.png" >/dev/null
  sips -z 256  256  "$ICON_SRC" --out "$ICONSET/icon_256x256.png"   >/dev/null
  sips -z 512  512  "$ICON_SRC" --out "$ICONSET/icon_256x256@2x.png" >/dev/null
  cp "$ICON_SRC" "$ICONSET/icon_512x512.png"
  iconutil -c icns "$ICONSET" -o "$SCRIPT_DIR/AppIcon.icns"
  rm -rf "$ICONSET"
fi

# Copy icon into app bundle.
if [ -f "$SCRIPT_DIR/AppIcon.icns" ]; then
  cp "$SCRIPT_DIR/AppIcon.icns" "$APP_DIR/Contents/Resources/AppIcon.icns"
  /usr/libexec/PlistBuddy -c "Add :CFBundleIconFile string AppIcon" \
    "$APP_DIR/Contents/Info.plist" 2>/dev/null || true
fi

echo "→ Staging DMG contents..."
mkdir -p "$STAGING_DIR"
cp -r "$APP_DIR" "$STAGING_DIR/krouter.app"
# Applications symlink lets users drag-install.
ln -s /Applications "$STAGING_DIR/Applications"

echo "→ Creating DMG..."
DMG_PATH="$DIST/krouter-${VERSION}-macos.dmg"
hdiutil create \
  -volname "krouter ${VERSION}" \
  -srcfolder "$STAGING_DIR" \
  -ov \
  -format UDZO \
  "$DMG_PATH"

echo "✓ Created $DMG_PATH"
