#!/usr/bin/env bash
# Setup openclaw in the Wine environment for krouter integration testing.
# Installs openclaw Windows binary into the Wine prefix and configures it
# to point to krouter at 127.0.0.1:8402.
#
# Usage: ./setup_openclaw.sh [--wineprefix PATH] [--openclaw-bin PATH]
#
# After setup, run: wine openclaw.exe run <agent-name> -- <task>

set -euo pipefail

WINEPREFIX="${WINEPREFIX:-$HOME/.wine-krouter-test}"
OPENCLAW_BIN="${OPENCLAW_BIN:-}"    # path to openclaw-windows.exe

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "${GREEN}OK${NC}   $1"; }
warn() { echo -e "${YELLOW}WARN${NC} $1"; }
die()  { echo -e "${RED}ERROR${NC} $1"; exit 1; }

# ── Find openclaw binary ──────────────────────────────────────────────────────
find_openclaw() {
    [[ -n "$OPENCLAW_BIN" ]] && echo "$OPENCLAW_BIN" && return
    local candidates=(
        "$WINEPREFIX/drive_c/users/$USER/AppData/Local/kinthai/openclaw.exe"
        "/tmp/openclaw-windows.exe"
        "$(pwd)/openclaw-windows.exe"
    )
    for f in "${candidates[@]}"; do [[ -f "$f" ]] && echo "$f" && return; done
    echo ""
}

OPENCLAW_EXE=$(find_openclaw)
[[ -z "$OPENCLAW_EXE" ]] && die "openclaw binary not found. Set OPENCLAW_BIN."

# ── Wine user's AppData ───────────────────────────────────────────────────────
APPDATA="$WINEPREFIX/drive_c/users/$USER/AppData/Roaming"
OPENCLAW_CFG_DIR="$APPDATA/openclaw"
OPENCLAW_JSON="$OPENCLAW_CFG_DIR/openclaw.json"

mkdir -p "$OPENCLAW_CFG_DIR"

# ── Write openclaw.json pointing to krouter ───────────────────────────────────
cat > "$OPENCLAW_JSON" <<'EOF'
{
  "mode": "merge",
  "providers": {
    "anthropic": {
      "baseURL": "http://127.0.0.1:8402",
      "apiKey": "${ANTHROPIC_API_KEY}"
    }
  },
  "defaultProvider": "anthropic"
}
EOF
ok "openclaw.json written → $OPENCLAW_JSON"

# ── Create a minimal test agent ───────────────────────────────────────────────
AGENT_DIR="$OPENCLAW_CFG_DIR/agents/krouter-test"
mkdir -p "$AGENT_DIR"

cat > "$AGENT_DIR/models.json" <<'EOF'
{
  "defaultModel": "claude-haiku-4-5",
  "provider": "anthropic"
}
EOF
ok "test agent 'krouter-test' created"

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "openclaw configured in Wine:"
echo "  Config : $OPENCLAW_JSON"
echo "  Agent  : $AGENT_DIR"
echo ""
echo "Run a test message:"
echo "  WINEPREFIX=$WINEPREFIX ANTHROPIC_API_KEY=<your-key> \\"
echo "    wine \"$OPENCLAW_EXE\" run krouter-test -- \"say hi\""
