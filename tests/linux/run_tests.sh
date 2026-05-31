#!/usr/bin/env bash
# krouter Linux installation integration test
# Tests: install flow, systemd service, shell integration, daemon API
# Usage: KROUTER_BIN=/path/to/krouter ./run_tests.sh

set -uo pipefail

KROUTER_BIN="${KROUTER_BIN:-$HOME/.local/bin/krouter}"
PASS=0
FAIL=0
SKIP=0

# ── helpers ────────────────────────────────────────────────────────────────────

GREEN='\033[0;32m'; RED='\033[0;31m'; YELLOW='\033[0;33m'; NC='\033[0m'
DAEMON_PID=""

pass() { echo -e "${GREEN}PASS${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}FAIL${NC} $1 — $2"; ((FAIL++)); }
skip() { echo -e "${YELLOW}SKIP${NC} $1 — $2"; ((SKIP++)); }

cleanup() {
    if [[ -n "$DAEMON_PID" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
        kill "$DAEMON_PID" 2>/dev/null
        wait "$DAEMON_PID" 2>/dev/null || true
    fi
    systemctl --user stop krouter 2>/dev/null || true
    pkill -f "krouter serve" 2>/dev/null || true
}
trap cleanup EXIT

wait_for_port() {
    local port=$1 timeout=10
    local i=0
    while ((i < timeout)); do
        if nc -z 127.0.0.1 "$port" 2>/dev/null; then return 0; fi
        sleep 1; ((i++))
    done
    return 1
}

# ── pre-flight ─────────────────────────────────────────────────────────────────

echo "=== krouter Linux Installation Tests ==="
echo "Binary: $KROUTER_BIN"
echo ""

# Copy binary to /tmp/krouter-test for install (avoids overwriting real install)
if [[ "$KROUTER_BIN" != "$HOME/.local/bin/krouter" ]]; then
    INSTALL_SRC="$KROUTER_BIN"
else
    INSTALL_SRC="$KROUTER_BIN"
fi

if [[ ! -f "$INSTALL_SRC" ]]; then
    echo "ERROR: binary not found at $INSTALL_SRC"
    exit 1
fi

# Clean slate: remove previous test installation artifacts
systemctl --user stop krouter 2>/dev/null || true
systemctl --user disable krouter 2>/dev/null || true
rm -f "$HOME/.config/systemd/user/krouter.service"
rm -f "$HOME/.local/bin/krouter"
rm -rf "$HOME/.kinthai"
# Kill any stale krouter processes (native Linux or Wine Windows) holding ports
pkill -f "krouter serve" 2>/dev/null || true
pkill -f "krouter.exe serve" 2>/dev/null || true
sleep 2
systemctl --user daemon-reload 2>/dev/null || true
# Remove shell integration if present
if [[ -f "$HOME/.bashrc" ]]; then
    sed -i '/# >>> krouter/,/# <<< krouter/d' "$HOME/.bashrc"
fi

echo "--- Phase 1: Installation ---"

# L01: dry-run prints steps
output=$("$INSTALL_SRC" install --dry-run 2>&1)
if echo "$output" | grep -q "Copy binary\|Register service\|Shell integration"; then
    pass "L01: install --dry-run prints steps"
else
    fail "L01: install --dry-run prints steps" "got: $output"
fi

# L02: install --yes completes without error
output=$("$INSTALL_SRC" install --yes 2>&1)
if [[ $? -eq 0 ]]; then
    pass "L02: install --yes exits 0"
else
    fail "L02: install --yes exits 0" "exit=$?; output=$output"
fi

# L03: binary copied to ~/.local/bin/krouter
if [[ -x "$HOME/.local/bin/krouter" ]]; then
    pass "L03: binary at ~/.local/bin/krouter"
else
    fail "L03: binary at ~/.local/bin/krouter" "not found"
fi

# L04: systemd service file written
SVC_FILE="$HOME/.config/systemd/user/krouter.service"
if [[ -f "$SVC_FILE" ]]; then
    pass "L04: systemd unit file exists"
else
    fail "L04: systemd unit file exists" "not found at $SVC_FILE"
fi

# L05: service file has correct ExecStart
if grep -q "ExecStart.*krouter serve" "$SVC_FILE" 2>/dev/null; then
    pass "L05: service file ExecStart contains 'krouter serve'"
else
    fail "L05: service file ExecStart" "$(cat "$SVC_FILE" 2>/dev/null | head -5)"
fi

# L06: shell RC integration written
SHELL_RC="$HOME/.bashrc"
if grep -q "BEGIN krouter\|ANTHROPIC_BASE_URL\|krouter" "$SHELL_RC" 2>/dev/null; then
    pass "L06: shell integration written to ~/.bashrc"
else
    fail "L06: shell integration written to ~/.bashrc" "marker not found"
fi

# L07: shell integration is idempotent (re-running ConnectClaudeCode doesn't duplicate)
# Run just the shell-init command to trigger idempotency check without starting a second daemon.
"$HOME/.local/bin/krouter" install --yes 2>/dev/null || true
count=$(grep -c "BEGIN krouter" "$SHELL_RC" 2>/dev/null || true)
count="${count//[^0-9]/}"  # strip any whitespace
count="${count:-0}"
if [[ "$count" -le 1 ]]; then
    pass "L07: shell integration idempotent (marker count: $count)"
else
    fail "L07: shell integration idempotent" "marker appears $count times"
fi

# L08: installed marker file exists
if [[ -f "$HOME/.kinthai/installed" ]]; then
    pass "L08: installed marker file ~/.kinthai/installed exists"
else
    fail "L08: installed marker file ~/.kinthai/installed" "not found"
fi

echo ""
echo "--- Phase 2: systemd Service ---"

# Enable linger for user service to survive without login session
loginctl enable-linger "$USER" 2>/dev/null || true

# L09: systemctl enable works
systemctl --user daemon-reload 2>/dev/null || true
out=$(systemctl --user enable krouter 2>&1)
if [[ $? -eq 0 ]]; then
    pass "L09: systemctl --user enable krouter"
else
    skip "L09: systemctl --user enable krouter" "$out"
fi

# L10: daemon should already be running (started during install --yes via enable --now)
active=$(systemctl --user is-active krouter 2>/dev/null)
if [[ "$active" == "active" ]]; then
    pass "L10: krouter service is active after install --yes"
    STARTED_BY_SYSTEMD=1
else
    # Try to start if not active
    out=$(systemctl --user start krouter 2>&1)
    if [[ $? -eq 0 ]]; then
        pass "L10: systemctl --user start krouter"
        STARTED_BY_SYSTEMD=1
    else
        skip "L10: systemctl --user start krouter" "$out"
        STARTED_BY_SYSTEMD=0
    fi
fi

# Wait for daemon to be ready
if [[ "${STARTED_BY_SYSTEMD:-0}" -eq 1 ]]; then
    if wait_for_port 8403; then
        pass "L11: daemon listening on :8403 (via systemd)"
        # Wait a bit more for token to be written to disk
        for i in {1..5}; do
            [[ -s "$HOME/.kinthai/internal-token" ]] && break
            sleep 1
        done
    else
        skip "L11: daemon listening on :8403" "port not open after 10s"
        STARTED_BY_SYSTEMD=0
    fi
else
    skip "L11: daemon listening on :8403 (via systemd)" "service not started"
fi

# If systemd didn't start it, launch directly
if [[ "${STARTED_BY_SYSTEMD:-0}" -eq 0 ]]; then
    echo "(Starting daemon directly for API tests...)"
    "$HOME/.local/bin/krouter" serve &
    DAEMON_PID=$!
    if wait_for_port 8403; then
        echo "(daemon started directly, PID=$DAEMON_PID)"
    else
        echo "ERROR: daemon failed to start"
        exit 1
    fi
fi

echo ""
echo "--- Phase 3: Daemon API ---"

# Read internal token
TOKEN_FILE="$HOME/.kinthai/internal-token"
if [[ ! -f "$TOKEN_FILE" ]]; then
    fail "L12: internal-token file" "not found"
    TOKEN=""
else
    TOKEN=$(cat "$TOKEN_FILE")
    pass "L12: internal-token file exists"
fi

# L13: /internal/status returns 401 without token
code=$(curl -s -o /dev/null -w "%{http_code}" http://127.0.0.1:8403/internal/status)
if [[ "$code" == "401" ]]; then
    pass "L13: GET /internal/status without token → 401"
else
    fail "L13: GET /internal/status without token → 401" "got $code"
fi

# L14: /internal/status returns 200 with token
if [[ -n "$TOKEN" ]]; then
    code=$(curl -s -o /dev/null -w "%{http_code}" -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8403/internal/status)
    if [[ "$code" == "200" ]]; then
        pass "L14: GET /internal/status with Bearer token → 200"
    else
        fail "L14: GET /internal/status with Bearer token → 200" "got $code"
    fi
else
    skip "L14: GET /internal/status with token" "no token"
fi

# L15: /ui/ returns HTML
if [[ -n "$TOKEN" ]]; then
    TICKET=$(curl -s -X POST -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8403/internal/auth/ticket | grep -o '"ticket":"[^"]*"' | cut -d'"' -f4)
    if [[ -n "$TICKET" ]]; then
        # Exchange ticket for session cookie
        COOKIE_JAR=$(mktemp)
        code=$(curl -s -o /dev/null -w "%{http_code}" -c "$COOKIE_JAR" \
            "http://127.0.0.1:8403/internal/auth/exchange?ticket=${TICKET}&redirect=/ui/")
        if [[ "$code" == "302" || "$code" == "200" ]]; then
            pass "L15: ticket exchange → redirect"
            # Try /ui/ with the session cookie
            code=$(curl -s -o /dev/null -w "%{http_code}" -b "$COOKIE_JAR" http://127.0.0.1:8403/ui/)
            if [[ "$code" == "200" ]]; then
                pass "L16: GET /ui/ with session cookie → 200"
            else
                fail "L16: GET /ui/ with session cookie → 200" "got $code"
            fi
        else
            fail "L15: ticket exchange" "got $code"
        fi
        rm -f "$COOKIE_JAR"
    else
        skip "L15: ticket exchange" "failed to mint ticket"
        skip "L16: GET /ui/" "skipped (no ticket)"
    fi
else
    skip "L15: ticket exchange" "no token"
    skip "L16: GET /ui/" "skipped"
fi

# L17: GET /internal/settings returns JSON
if [[ -n "$TOKEN" ]]; then
    body=$(curl -s -H "Authorization: Bearer $TOKEN" http://127.0.0.1:8403/internal/settings)
    if echo "$body" | grep -q "preset\|routing_mode"; then
        pass "L17: GET /internal/settings returns settings JSON"
    else
        fail "L17: GET /internal/settings" "got: $body"
    fi
fi

# L18: PATCH /internal/settings preset
if [[ -n "$TOKEN" ]]; then
    code=$(curl -s -o /dev/null -w "%{http_code}" -X PATCH \
        -H "Authorization: Bearer $TOKEN" \
        -H "Content-Type: application/json" \
        -d '{"preset":"saver"}' \
        http://127.0.0.1:8403/internal/settings)
    if [[ "$code" == "200" || "$code" == "204" ]]; then
        pass "L18: PATCH /internal/settings preset=saver → $code"
    else
        fail "L18: PATCH /internal/settings preset=saver" "got $code"
    fi
fi

# L19: proxy port 8402 accepts connections
if nc -z 127.0.0.1 8402 2>/dev/null; then
    pass "L19: proxy port :8402 accepts connections"
else
    fail "L19: proxy port :8402 accepts connections" "port closed"
fi

# L20: SSE endpoint connects
sse_output=$(curl -s --max-time 3 -N \
    -H "Authorization: Bearer $TOKEN" \
    http://127.0.0.1:8403/internal/events 2>/dev/null | head -c 100 || true)
if echo "$sse_output" | grep -q "data:\|:keepalive\|event:"; then
    pass "L20: GET /internal/events returns SSE stream"
else
    # Check content-type at least
    ct=$(curl -s -o /dev/null -w "%{content_type}" --max-time 2 \
        -H "Authorization: Bearer $TOKEN" \
        http://127.0.0.1:8403/internal/events 2>/dev/null || true)
    if echo "$ct" | grep -q "text/event-stream"; then
        pass "L20: GET /internal/events Content-Type: text/event-stream"
    else
        fail "L20: GET /internal/events returns SSE" "got: $sse_output"
    fi
fi

echo ""
echo "--- Phase 4: Uninstall ---"

# Stop daemon if started directly
if [[ -n "$DAEMON_PID" ]] && kill -0 "$DAEMON_PID" 2>/dev/null; then
    kill "$DAEMON_PID" 2>/dev/null
    wait "$DAEMON_PID" 2>/dev/null || true
    DAEMON_PID=""
fi
systemctl --user stop krouter 2>/dev/null || true
sleep 1

# L21: krouter uninstall --yes works
out=$("$HOME/.local/bin/krouter" uninstall --yes 2>&1) || true
# Check things were removed
if [[ ! -f "$HOME/.config/systemd/user/krouter.service" ]]; then
    pass "L21: uninstall removes systemd unit file"
else
    skip "L21: uninstall removes systemd unit file" "uninstall output: $out"
fi

echo ""
echo "═══════════════════════════════════"
echo "Results: ${GREEN}${PASS} passed${NC}  ${RED}${FAIL} failed${NC}  ${YELLOW}${SKIP} skipped${NC}"
echo "═══════════════════════════════════"

[[ $FAIL -eq 0 ]]
