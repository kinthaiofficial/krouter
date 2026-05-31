#!/usr/bin/env bash
# krouter Wine / Windows integration test suite
# Usage: ./run_tests.sh [--wineprefix PATH] [--bin PATH/TO/krouter-windows.exe]
#
# Requires: wine 9+, curl, jq
# All HTTP calls go to 127.0.0.1 — Wine processes share the host network stack.

set -euo pipefail

# ── Config ────────────────────────────────────────────────────────────────────
WINEPREFIX="${WINEPREFIX:-$HOME/.wine-krouter-test}"
KROUTER_BIN="${KROUTER_BIN:-}"          # auto-detected if empty
PROXY_PORT=8402
MGMT_PORT=8403
DAEMON_PID=""
PASS=0
FAIL=0
SKIP=0

# ── Colours ───────────────────────────────────────────────────────────────────
RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
# NOTE: plain assignment, not ((VAR++)) — under `set -e`, a post-increment that
# returns the pre-increment value 0 exits non-zero and would abort the suite.
ok()   { echo -e "${GREEN}PASS${NC} $1"; PASS=$((PASS+1)); }
fail() { echo -e "${RED}FAIL${NC} $1"; FAIL=$((FAIL+1)); }
skip() { echo -e "${YELLOW}SKIP${NC} $1"; SKIP=$((SKIP+1)); }
info() { echo -e "     $1"; }

# ── Helpers ───────────────────────────────────────────────────────────────────
wait_port() {
    local port=$1 timeout=${2:-15}
    for i in $(seq 1 $timeout); do
        if curl -s --max-time 1 "http://127.0.0.1:$port/health" >/dev/null 2>&1; then
            return 0
        fi
        sleep 1
    done
    return 1
}

token() {
    cat "$HOME/.kinthai/internal-token" 2>/dev/null || \
    cat "$WINEPREFIX/drive_c/users/$USER/.kinthai/internal-token" 2>/dev/null || \
    echo ""
}

get() {
    local path=$1 port=${2:-$MGMT_PORT}
    curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer $(token)" \
        "http://127.0.0.1:$port$path"
}

post() {
    local path=$1 body=${2:-"{}"} port=${3:-$MGMT_PORT}
    curl -s -w "\n%{http_code}" \
        -H "Authorization: Bearer $(token)" \
        -H "Content-Type: application/json" \
        -d "$body" \
        "http://127.0.0.1:$port$path"
}

http_code() { tail -1 <<< "$1"; }
body()      { head -n -1 <<< "$1"; }

# ── Find krouter binary ───────────────────────────────────────────────────────
find_bin() {
    if [[ -n "$KROUTER_BIN" ]]; then
        echo "$KROUTER_BIN"; return
    fi
    # Common locations after krouter-setup.exe installs
    local candidates=(
        "$WINEPREFIX/drive_c/users/$USER/AppData/Local/kinthai/krouter.exe"
        "$WINEPREFIX/drive_c/users/$(whoami)/AppData/Local/kinthai/krouter.exe"
        "/tmp/krouter-windows.exe"
        "$(pwd)/krouter-windows.exe"
    )
    for f in "${candidates[@]}"; do
        [[ -f "$f" ]] && echo "$f" && return
    done
    echo ""
}

# ── Setup: start daemon if not already running ────────────────────────────────
start_daemon() {
    if curl -s --max-time 1 "http://127.0.0.1:$MGMT_PORT/health" >/dev/null 2>&1; then
        info "daemon already running on :$MGMT_PORT"
        return 0
    fi

    local bin
    bin=$(find_bin)
    if [[ -z "$bin" ]]; then
        echo "ERROR: krouter binary not found. Set KROUTER_BIN or install via krouter-setup.exe first."
        exit 1
    fi

    info "Starting: WINEPREFIX=$WINEPREFIX wine \"$bin\" serve"
    WINEPREFIX="$WINEPREFIX" wine "$bin" serve \
        --proxy-port "$PROXY_PORT" \
        --management-port "$MGMT_PORT" \
        --log-level info \
        > /tmp/krouter-wine-daemon.log 2>&1 &
    DAEMON_PID=$!
    info "Daemon PID: $DAEMON_PID (log: /tmp/krouter-wine-daemon.log)"

    if ! wait_port $MGMT_PORT 20; then
        echo "ERROR: daemon did not start within 20s. Last log:"
        tail -20 /tmp/krouter-wine-daemon.log
        exit 1
    fi
    info "Daemon up."
}

# ── Teardown ──────────────────────────────────────────────────────────────────
stop_daemon() {
    if [[ -n "$DAEMON_PID" ]]; then
        kill "$DAEMON_PID" 2>/dev/null || true
        # Also kill any lingering wineserver processes for this prefix.
        WINEPREFIX="$WINEPREFIX" wineserver -k 2>/dev/null || true
    fi
}
trap stop_daemon EXIT

# ═════════════════════════════════════════════════════════════════════════════
echo ""
echo "══════════════════════════════════════════"
echo "  krouter Wine integration tests"
echo "  WINEPREFIX: $WINEPREFIX"
echo "══════════════════════════════════════════"
echo ""

start_daemon

# ── T01: health endpoint ──────────────────────────────────────────────────────
r=$(curl -s -w "\n%{http_code}" "http://127.0.0.1:$MGMT_PORT/health")
if [[ $(http_code "$r") == "200" ]]; then
    ok "T01 /health returns 200"
else
    fail "T01 /health expected 200, got $(http_code "$r")"
fi

# ── T02: auth token present ───────────────────────────────────────────────────
tok=$(token)
if [[ -n "$tok" ]]; then
    ok "T02 internal-token file exists (len=${#tok})"
else
    fail "T02 internal-token not found"
fi

# ── T03: CSRF guard rejects foreign Origin ───────────────────────────────────
# Auth is Origin-based CSRF, not token-required (see internal/api/auth.go): a
# browser request carrying a foreign Origin is blocked with 403.
r=$(curl -s -w "\n%{http_code}" -H "Origin: https://evil.example" \
    "http://127.0.0.1:$MGMT_PORT/internal/status")
if [[ $(http_code "$r") == "403" ]]; then
    ok "T03 foreign-Origin request rejected with 403 (CSRF guard)"
else
    fail "T03 foreign-Origin expected 403, got $(http_code "$r")"
fi

# ── T04: /internal/status with auth ──────────────────────────────────────────
r=$(get "/internal/status")
code=$(http_code "$r")
if [[ "$code" == "200" ]]; then
    version=$(body "$r" | jq -r '.version // empty' 2>/dev/null)
    ok "T04 /internal/status 200 (version=$version)"
else
    fail "T04 /internal/status expected 200, got $code"
    info "$(body "$r")"
fi

# ── T05: web UI serves HTML ───────────────────────────────────────────────────
# The dashboard is mounted at /krouter/ (not the old /ui/).
r=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $(token)" \
    "http://127.0.0.1:$MGMT_PORT/krouter/")
code=$(http_code "$r")
if [[ "$code" == "200" ]] && body "$r" | grep -q "<!doctype html\|<!DOCTYPE html"; then
    ok "T05 /krouter/ returns HTML"
elif [[ "$code" == "200" ]]; then
    fail "T05 /krouter/ returned 200 but content is not HTML"
    info "$(body "$r" | head -5)"
else
    fail "T05 /krouter/ expected 200, got $code"
fi

# ── T06: SPA fallback (deep link) ─────────────────────────────────────────────
r=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $(token)" \
    "http://127.0.0.1:$MGMT_PORT/krouter/logs")
code=$(http_code "$r")
if [[ "$code" == "200" ]] && body "$r" | grep -q "<!doctype html\|<!DOCTYPE html"; then
    ok "T06 /krouter/logs SPA fallback returns index.html"
else
    fail "T06 /krouter/logs SPA fallback expected 200+HTML, got $code"
fi

# ── T07: dashboard's own Origin allowed without token ────────────────────────
# MGMT_PORT defaults to 8403, which is the single allowedOrigin the CSRF guard
# permits — so a same-origin browser GET succeeds with no Authorization header.
r=$(curl -s -w "\n%{http_code}" -H "Origin: http://127.0.0.1:$MGMT_PORT" \
    "http://127.0.0.1:$MGMT_PORT/internal/status")
if [[ $(http_code "$r") == "200" ]]; then
    ok "T07 dashboard-Origin request allowed without token (200)"
else
    fail "T07 dashboard-Origin expected 200, got $(http_code "$r")"
fi

# ── T08: no-Origin CLI request allowed without token ─────────────────────────
# Requests with no Origin header (curl / CLI) carry no CSRF risk → allowed.
r=$(curl -s -w "\n%{http_code}" "http://127.0.0.1:$MGMT_PORT/internal/status")
if [[ $(http_code "$r") == "200" ]]; then
    ok "T08 no-Origin CLI request allowed without token (200)"
else
    fail "T08 no-Origin request expected 200, got $(http_code "$r")"
fi

# ── T09: valid Bearer token overrides CSRF guard ─────────────────────────────
# A valid token authorizes unconditionally, even from a foreign Origin.
r=$(curl -s -w "\n%{http_code}" \
    -H "Authorization: Bearer $(token)" \
    -H "Origin: https://evil.example" \
    "http://127.0.0.1:$MGMT_PORT/internal/status")
if [[ $(http_code "$r") == "200" ]]; then
    ok "T09 valid token + foreign Origin allowed (token bypasses CSRF)"
else
    fail "T09 token+foreign-Origin expected 200, got $(http_code "$r")"
fi

# ── T10: GET /internal/settings ──────────────────────────────────────────────
r=$(get "/internal/settings")
code=$(http_code "$r")
if [[ "$code" == "200" ]]; then
    preset=$(body "$r" | jq -r '.preset // empty' 2>/dev/null)
    ok "T10 GET /internal/settings (preset=$preset)"
else
    fail "T10 GET /internal/settings expected 200, got $code"
fi

# ── T11: POST /internal/preset persists ──────────────────────────────────────
# The global routing preset is written via POST /internal/preset (GET/POST),
# not PATCH /internal/settings.
r=$(post "/internal/preset" '{"preset":"saver"}')
code=$(http_code "$r")
if [[ "$code" == "200" ]]; then
    # Read back
    r2=$(get "/internal/preset")
    preset=$(body "$r2" | jq -r '.preset // empty' 2>/dev/null)
    if [[ "$preset" == "saver" ]]; then
        ok "T11 POST /internal/preset=saver persisted"
    else
        fail "T11 preset=saver not persisted (got '$preset')"
    fi
    # Reset to balanced
    post "/internal/preset" '{"preset":"balanced"}' > /dev/null
else
    fail "T11 POST /internal/preset expected 200, got $code"
fi

# ── T12: GET /internal/budget ────────────────────────────────────────────────
r=$(get "/internal/budget")
code=$(http_code "$r")
if [[ "$code" == "200" ]]; then
    date=$(body "$r" | jq -r '.date // empty' 2>/dev/null)
    ok "T12 GET /internal/budget (date=$date)"
else
    fail "T12 GET /internal/budget expected 200, got $code"
fi

# ── T13: GET /internal/usage ─────────────────────────────────────────────────
r=$(get "/internal/usage")
code=$(http_code "$r")
if [[ "$code" == "200" ]]; then
    ok "T13 GET /internal/usage 200"
else
    fail "T13 GET /internal/usage expected 200, got $code"
fi

# ── T14: proxy port open ──────────────────────────────────────────────────────
r=$(curl -s -w "\n%{http_code}" --max-time 3 \
    -X POST \
    -H "x-api-key: sk-test-wine" \
    -H "anthropic-version: 2023-06-01" \
    -H "Content-Type: application/json" \
    -d '{"model":"claude-haiku-4-5","max_tokens":1,"messages":[{"role":"user","content":"hi"}]}' \
    "http://127.0.0.1:$PROXY_PORT/v1/messages")
code=$(http_code "$r")
# We expect 401/4xx from upstream (no real key) — but NOT connection refused.
if [[ "$code" != "000" ]]; then
    ok "T14 proxy :$PROXY_PORT accepts connections (upstream returned $code)"
else
    fail "T14 proxy :$PROXY_PORT connection refused"
fi

# ── T15: SSE endpoint streams ────────────────────────────────────────────────
# --max-time on a streaming endpoint exits 28 (timeout) by design; `|| true`
# keeps `set -o pipefail` from aborting the suite on that expected non-zero.
r=$(curl -s --max-time 3 \
    -H "Authorization: Bearer $(token)" \
    "http://127.0.0.1:$MGMT_PORT/internal/events" 2>&1 | head -5 || true)
if echo "$r" | grep -q "^:"; then
    ok "T15 GET /internal/events streams SSE heartbeat"
else
    fail "T15 /internal/events did not return SSE ping"
    info "$r"
fi

# ── T16: Task Scheduler task registered (Wine) ───────────────────────────────
ts_out=$(WINEPREFIX="$WINEPREFIX" wine cmd.exe /c 'schtasks /Query /TN "krouter-daemon" /FO LIST' 2>/dev/null || true)
if echo "$ts_out" | grep -qi "krouter-daemon"; then
    ok "T16 Task Scheduler 'krouter-daemon' task is registered"
    # Show status
    status=$(echo "$ts_out" | grep -i "status\|状态" | head -1)
    info "$status"
else
    skip "T16 schtasks query skipped (Wine Task Scheduler not available or task not yet registered)"
fi

# ── T17: Windows env vars set via setx ───────────────────────────────────────
env_out=$(WINEPREFIX="$WINEPREFIX" wine reg query "HKCU\\Environment" /v ANTHROPIC_BASE_URL 2>/dev/null || true)
if echo "$env_out" | grep -qi "8402"; then
    ok "T17 ANTHROPIC_BASE_URL set in Windows registry"
else
    skip "T17 ANTHROPIC_BASE_URL not in registry (shell integration not run yet)"
fi

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "══════════════════════════════════════════"
total=$((PASS + FAIL + SKIP))
echo "  Results: $total tests — ${GREEN}$PASS passed${NC}, ${RED}$FAIL failed${NC}, ${YELLOW}$SKIP skipped${NC}"
echo "══════════════════════════════════════════"
echo ""

[[ $FAIL -eq 0 ]]
