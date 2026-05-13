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

# Copy icon if available (optional).
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
