#!/bin/sh
# Post-install script for .deb/.rpm packages.
# Installs the binary to ~/.local/bin and registers the systemd --user service
# for the user who runs this script (or the SUDO_USER when run via sudo).

set -e

BINARY_SRC=/usr/local/bin/krouter

# Determine the target user.
TARGET_USER="${SUDO_USER:-$USER}"
if [ -z "$TARGET_USER" ] || [ "$TARGET_USER" = "root" ]; then
  echo "krouter: skipping user-level setup (no non-root user identified)"
  echo "  Run 'krouter install' manually as your regular user."
  exit 0
fi

TARGET_HOME=$(getent passwd "$TARGET_USER" | cut -d: -f6)
if [ -z "$TARGET_HOME" ]; then
  echo "krouter: cannot determine home directory for $TARGET_USER"
  exit 0
fi

LOCAL_BIN="$TARGET_HOME/.local/bin"
SYSTEMD_DIR="$TARGET_HOME/.config/systemd/user"
SERVICE_FILE="$SYSTEMD_DIR/krouter.service"

# Install binary to ~/.local/bin.
mkdir -p "$LOCAL_BIN"
cp "$BINARY_SRC" "$LOCAL_BIN/krouter"
chmod 755 "$LOCAL_BIN/krouter"
chown "$TARGET_USER" "$LOCAL_BIN/krouter"

# Install systemd --user service.
mkdir -p "$SYSTEMD_DIR"
cat > "$SERVICE_FILE" <<EOF
[Unit]
Description=krouter – local LLM proxy
After=network.target

[Service]
Type=simple
ExecStart=$LOCAL_BIN/krouter serve
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
EOF
chown "$TARGET_USER" "$SERVICE_FILE"

echo "krouter installed for user $TARGET_USER."
echo "  Enable with: systemctl --user enable --now krouter"
