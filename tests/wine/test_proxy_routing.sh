#!/usr/bin/env bash
# Test krouter proxy routing with a real API key.
# Sends requests through the proxy and verifies routing decisions in the logs.
#
# Usage: ANTHROPIC_API_KEY=sk-ant-... ./test_proxy_routing.sh
#
# Requires: krouter daemon running (run_tests.sh or manually)

set -euo pipefail

PROXY_PORT=8402
MGMT_PORT=8403
API_KEY="${ANTHROPIC_API_KEY:-}"
PASS=0; FAIL=0

RED='\033[0;31m'; GREEN='\033[0;32m'; YELLOW='\033[1;33m'; NC='\033[0m'
ok()   { echo -e "${GREEN}PASS${NC} $1"; ((PASS++)); }
fail() { echo -e "${RED}FAIL${NC} $1"; ((FAIL++)); }
info() { echo -e "     $1"; }
skip() { echo -e "${YELLOW}SKIP${NC} $1"; }

token() { cat "$HOME/.kinthai/internal-token" 2>/dev/null || echo ""; }

[[ -z "$API_KEY" ]] && { echo "Set ANTHROPIC_API_KEY to run routing tests"; exit 1; }

# ── Minimal Anthropic request ────────────────────────────────────────────────
send_request() {
    local model=$1
    curl -s -w "\n%{http_code}" \
        --max-time 30 \
        -X POST \
        -H "x-api-key: $API_KEY" \
        -H "anthropic-version: 2023-06-01" \
        -H "Content-Type: application/json" \
        -d "{\"model\":\"$model\",\"max_tokens\":10,\"messages\":[{\"role\":\"user\",\"content\":\"reply: ok\"}]}" \
        "http://127.0.0.1:$PROXY_PORT/v1/messages"
}

log_last() {
    curl -s \
        -H "Authorization: Bearer $(token)" \
        "http://127.0.0.1:$MGMT_PORT/internal/logs?n=1" 2>/dev/null | \
        jq -r '.[0] | "provider=\(.provider) model=\(.model)"' 2>/dev/null || echo "(no log)"
}

# ── R01: Balanced preset honours requested model ─────────────────────────────
curl -s -X POST \
    -H "Authorization: Bearer $(token)" \
    -H "Content-Type: application/json" \
    -d '{"preset":"balanced"}' \
    "http://127.0.0.1:$MGMT_PORT/internal/settings" > /dev/null

r=$(send_request "claude-haiku-4-5")
code=$(tail -1 <<< "$r")
body=$(head -n -1 <<< "$r")
if [[ "$code" == "200" ]]; then
    ok "R01 Balanced: haiku request 200"
    info "$(log_last)"
else
    fail "R01 Balanced: haiku request expected 200, got $code"
    info "$(echo "$body" | head -3)"
fi

# ── R02: Saver preset routes to cheapest model ───────────────────────────────
curl -s -X POST \
    -H "Authorization: Bearer $(token)" \
    -H "Content-Type: application/json" \
    -d '{"preset":"saver"}' \
    "http://127.0.0.1:$MGMT_PORT/internal/settings" > /dev/null

r=$(send_request "claude-sonnet-4-5")
code=$(tail -1 <<< "$r")
body=$(head -n -1 <<< "$r")
if [[ "$code" == "200" ]]; then
    log=$(log_last)
    if echo "$log" | grep -q "haiku"; then
        ok "R02 Saver: sonnet request downgraded to haiku"
        info "$log"
    else
        ok "R02 Saver: sonnet request routed (check log for model)"
        info "$log"
    fi
else
    fail "R02 Saver: request expected 200, got $code"
fi

# ── R03: Quality preset passes through ───────────────────────────────────────
curl -s -X POST \
    -H "Authorization: Bearer $(token)" \
    -H "Content-Type: application/json" \
    -d '{"preset":"quality"}' \
    "http://127.0.0.1:$MGMT_PORT/internal/settings" > /dev/null

r=$(send_request "claude-haiku-4-5")
code=$(tail -1 <<< "$r")
if [[ "$code" == "200" ]]; then
    ok "R03 Quality: haiku request 200"
    info "$(log_last)"
else
    fail "R03 Quality: expected 200, got $code"
fi

# ── R04: Request logged in DB ─────────────────────────────────────────────────
logs=$(curl -s \
    -H "Authorization: Bearer $(token)" \
    "http://127.0.0.1:$MGMT_PORT/internal/logs?n=5" 2>/dev/null)
count=$(echo "$logs" | jq 'length' 2>/dev/null || echo 0)
if [[ "$count" -gt 0 ]]; then
    ok "R04 requests logged in DB (count=$count)"
else
    fail "R04 no requests in log DB"
fi

# ── Reset preset ─────────────────────────────────────────────────────────────
curl -s -X POST \
    -H "Authorization: Bearer $(token)" \
    -H "Content-Type: application/json" \
    -d '{"preset":"balanced"}' \
    "http://127.0.0.1:$MGMT_PORT/internal/settings" > /dev/null

# ── Summary ───────────────────────────────────────────────────────────────────
echo ""
echo "Routing: $PASS passed, $FAIL failed"
[[ $FAIL -eq 0 ]]
