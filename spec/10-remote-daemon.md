# spec/10-remote-daemon.md — LAN Remote Daemon Access

**Module**: `internal/remote`
**Scope**: M2-M3 (NOT M1)
**Goal**: GUI on machine B can manage daemon on machine A within the same LAN.

---

## 1. Design goals

- Multi-machine workflow: Mac mini + MacBook, Linux VPS/NAS, Raspberry Pi
- Default OFF (zero attack surface unless user opts in)
- Zero extra operations after enable: pairing token auto-generated and displayed
- Single string carries all connection info (IP + port + code + checksum)
- Security: HTTPS + per-device tokens + independent revocation
- Headless-friendly: CLI parity for servers without GUI

---

## 2. Pairing token format

User copies an 18-character string; machine B pastes it; GUI parses everything.

```
Binary structure (11 bytes):
  Byte 0     version           protocol version, 0x01
  Byte 1-4   ipv4              daemon machine's LAN IP
  Byte 5-6   port              uint16, usually 8403
  Byte 7-9   pairing_code      24-bit random (shown to user)
  Byte 10    checksum          CRC-8, detects paste corruption

Base32 Crockford encoding → 16 chars
Plus "KR-" prefix and grouping → 18 chars:

    KR-2KSF-9G7X-AMVR-83NK
```

Encoding rationale:
- Base32 Crockford excludes 0/O/1/I/U for unambiguous reading
- Case-insensitive (manual entry forgiving)
- 18 chars vs hex's 22 chars (saves 18%)
- Checksum standalone — paste error detected immediately, doesn't require server round-trip

---

## 3. Input tolerance

```
Input                       Parser behavior
─────────────────────────────────────────────────────────────
KR-2KSF-9G7X-AMVR-83NK     ✓ Standard format
kr-2ksf-9g7x-amvr-83nk     ✓ Case-insensitive
KR2KSF9G7XAMVR83NK         ✓ No separators
KR 2KSF 9G7X AMVR 83NK     ✓ Space-separated (WeChat paste common)
KR-2KSF-9G7X-AMVR-83NX     ✗ Bad checksum, immediate error
Correct token but expired   ✗ Server returns 401, "Code expired"
```

Parser logic:
1. Strip all non-alphanumeric chars
2. uppercase
3. Verify length == 18
4. Verify "KR" prefix
5. Base32 Crockford decode → 11 bytes
6. CRC-8 verify
7. Parse IP / port / code / version fields

---

## 4. Pairing flow

```
Machine A (daemon + GUI)              Machine B (GUI only)

1. User: Settings → Network
   Toggle [Remote daemon access] ON
   ↓
2. GUI calls:
   POST /internal/remote/enable
   (via 127.0.0.1:8403, with local token)
   ↓
3. daemon:
   - Switch management port: 127.0.0.1 → 0.0.0.0:8403
   - Generate self-signed cert if not exists
   - Generate random pairing_code
   - Encode KR- token with LAN IP + port + code
   - Store in memory active_pairings, expires in 5 min
   ↓
4. GUI displays: KR-2KSF-9G7X-AMVR-83NK
   Countdown: "Refreshes in 4:23"
   [Copy] button

   --- User copies token, sends to machine B (WeChat / walks over) ---

                                         5. User clicks "+ Connect to remote"
                                         ↓
                                         6. Pastes KR- token
                                            GUI parses locally:
                                            - CRC-8 verify
                                            - IP: 192.168.1.10
                                            - Port: 8403
                                            - Code: 847291
                                            ↓
                                         7. "Connecting to 192.168.1.10..."
                                            ↓
                                         8. First HTTPS handshake:
                                            Certificate fingerprint dialog
                                            "Trust this device?"
                                            User clicks Trust → pin cert
                                            ↓
5'. daemon receives ◄─────────────────── 9. POST
    POST /internal/pairing/exchange         https://192.168.1.10:8403/
    Body: { code: "847291",                  internal/pairing/exchange
            device_name: "MacBook Pro" }     Body: { code, device_name }
    ↓
6'. daemon verifies:
    - code in active_pairings?
    - not expired?
    - not already used?
    Pass → generate 64-byte token
    Store in SQLite paired_devices
    Destroy this pairing code immediately
    Generate new pairing_code (for next device)
    ↓
7'. Return token ────────────────────► 10. GUI receives token
    { token: "...",                            Stores in
      daemon_id: "...",                        ~/.kinthai/contexts.json
      server_version: "..." }                  ↓
                                         11. All subsequent API calls:
8'. GUI updates display:                     Authorization: Bearer <token>
    Paired devices (2) ← added
    New pairing code appears
```

---

## 5. User interaction

### Machine A GUI (Settings → Network)

```
┌────────────────────────────────────────────────┐
│  Remote daemon access                    ON    │
│  ──────────                                    │
│                                                │
│  Pairing token                                 │
│  ┌──────────────────────────────────────────┐  │
│  │                                          │  │
│  │    KR-2KSF-9G7X-AMVR-83NK                │  │
│  │                                          │  │
│  │    Refreshes in 4:23      [Copy]         │  │
│  └──────────────────────────────────────────┘  │
│                                                │
│  Paste this on your other device.              │
│                                                │
│  Paired devices (2):                           │
│   • MacBook Pro    2 hours ago     [Revoke]    │
│   • iPad Pro       1 day ago       [Revoke]    │
└────────────────────────────────────────────────┘
```

### Machine B GUI (connection dialog)

```
┌────────────────────────────────────────────────┐
│  Connect to remote daemon                      │
│                                                │
│  Paste pairing token from your other device:   │
│  ┌──────────────────────────────────────────┐  │
│  │ KR-2KSF-9G7X-AMVR-83NK                   │  │
│  └──────────────────────────────────────────┘  │
│                                                │
│  ✓ Decoded successfully                        │
│    Target: 192.168.1.10:8403                   │
│                                                │
│  This device's name:                           │
│  ┌──────────────────────────────────────────┐  │
│  │ MacBook Pro                              │  │
│  └──────────────────────────────────────────┘  │
│                                                │
│  [Cancel]                       [Connect]      │
└────────────────────────────────────────────────┘
```

---

## 6. daemon state machine

```
Remote access OFF (default):
  Management port: 127.0.0.1:8403
  active_pairings: {} (empty)
  paired_devices: SQLite persisted (NOT deleted on disable)

User flips ON:
  Switch port: 127.0.0.1 → 0.0.0.0
  OS firewall first-time prompt → user allows (persistent)
  Generate self-signed cert if missing (~/.kinthai/cert.pem)
  Generate pairing_code: 24-bit random
  active_pairings: { code → expires_at }
  Encode KR- token, display via GUI

Pairing code expires (5 min):
  daemon destroys old code, generates new
  GUI subscribes via SSE, displays new token in real-time

Successful exchange:
  daemon immediately destroys used code (single-use)
  Immediately generates new code (for next device)
  paired_devices: append row { device_id, name, token_hash, paired_at }
  GUI updates

User flips OFF:
  Switch port: 0.0.0.0 → 127.0.0.1
  Clear active_pairings
  Existing paired_devices tokens REMAIN VALID
  Next enable → no need to re-pair
  GUI hides pairing section

User revokes a device:
  DELETE /internal/paired-devices/:id
  daemon deletes row from SQLite
  Token immediately invalid (next request from that device → 401)
```

---

## 7. SQLite schema

(Also in spec/05-storage.md, repeated here for completeness.)

```sql
CREATE TABLE paired_devices (
    device_id    TEXT PRIMARY KEY,         -- "dev_xxx_xxx"
    device_name  TEXT NOT NULL,             -- user-provided
    token_hash   TEXT NOT NULL,             -- SHA-256(token)
                                            -- NEVER raw token
    ip_address   TEXT,                      -- pairing-time IP (audit)
    paired_at    TIMESTAMP NOT NULL,
    last_seen_at TIMESTAMP,
    user_agent   TEXT                       -- GUI/CLI/mobile
);

-- Token verification: SHA-256 the incoming token, compare to token_hash
-- daemon never holds plaintext token in memory beyond verification
-- "Zero plaintext token storage" guarantee
```

---

## 8. HTTPS + first-time-trust

Remote mode requires HTTPS. SSH-style trust on first use:

- daemon generates self-signed X.509 cert (ECDSA P-256) on first enable
- CN = machine hostname, SAN includes LAN IP
- Cert stored at `~/.kinthai/cert.pem`, valid 10 years
- Machine B GUI first connects → shows fingerprint (SHA-256 first 8 bytes):
  "Connect to home-mac.local? Fingerprint: 4F:2A:7C:..."
- User clicks Trust → GUI stores fingerprint in local contexts.json
- Subsequent connections: GUI verifies fingerprint matches
- Mismatch → warning (MITM attack OR machine reinstall)
- User revokes device on machine A → both sides need re-pair

Why not Let's Encrypt: requires domain + public access. LAN IPs can't get
real certs. Self-signed + trust-on-first-use is SSH's proven model for 20+ years.

---

## 9. Headless CLI (no GUI needed)

```
$ ssh user@server.local
$ krouter remote enable
  ✓ Management port now listening on 0.0.0.0:8403
  ⚠ Please allow port 8403 in your firewall.

$ krouter pair show
  Pairing token:  KR-2KSF-9G7X-AMVR-83NK
  Expires in:     4 min 23 sec
  Watch mode: press Ctrl+C to exit
  (auto-refreshes when new token arrives)

$ krouter pair devices list
  ID         NAME           PAIRED AT       LAST SEEN
  dev_001    MacBook Pro    2026-05-12      2 hours ago
  dev_002    iPad Pro       2026-05-11      1 day ago

$ krouter pair devices revoke dev_001
  ✓ Device "MacBook Pro" revoked. Token invalidated.

$ krouter remote disable
  ✓ Management port back to 127.0.0.1:8403
  (paired devices' tokens preserved; remain valid next time enabled)
```

---

## 10. Security model

**Attack surface (with remote enabled):**
Only management port (8403). Proxy port (8402) stays 127.0.0.1.
Even if daemon is compromised, API keys never leave the machine
(they live in agent's env vars; daemon just forwards them).

**Pairing protection:**
- Default OFF
- Explicit user intent to enable
- 5-min code expiry
- Single-use
- 24-bit space (16M possible values)
- Server-side rate limit: 10 attempts/min (brute-force defense)

**Transport encryption:**
HTTPS + self-signed cert + first-time-trust + subsequent pin.
Even on hostile LAN with passive sniffing, traffic encrypted.

**Token management:**
- One token per device
- SQLite stores SHA-256(token), not plaintext
- Per-device revocation
- Disable remote doesn't invalidate existing tokens (preserves "next time")
- User detects abnormal → one-click revoke all

**OS firewall:**
| Platform | Behavior |
|----------|----------|
| macOS | Application Firewall (off by default). If enabled, first 0.0.0.0 bind prompts. User clicks allow, permanent. |
| Windows | Defender Firewall strict on inbound. First bind shows alert: "Private networks" checkbox. GUI warns user to NOT check "Public". |
| Linux | ufw typically disabled on desktop. firewalld active on servers; GUI shows `firewall-cmd --add-port=8403/tcp --permanent`. |

---

## 11. Why M2-M3, not M1

- M1 focus: "save tokens for local agents" core value. Remote management not day-one.
- Complexity ~1.5x local-only mode (HTTPS, cert, pairing flow, device mgmt UI)
- OS firewall interaction adds cross-platform testing burden
- After M2 architecture stabilizes, lower change risk
- BUT: two-port design (spec/01) is M1, leaves interface ready for M2

---

## 12. Test coverage

- Unit: Base32 Crockford encode/decode + CRC-8
- Unit: token parsing input tolerance (case, spaces, separators)
- Unit: pairing state machine transitions
- Integration: full pairing flow on localhost (two daemon instances)
- Integration: cert fingerprint pinning + mismatch detection

---

## 13. Cross-network / public internet scope

Out of scope for spec 10. M4+ will integrate:
- Tailscale (recommended): users install Tailscale → Tailnet IP → kinthai router works as LAN app
- Cloudflare Tunnel: daemon runs cloudflared, gets HTTPS URL
- SSH Tunnel: GUI auto-creates `ssh -L 8403:127.0.0.1:8403`
- Enterprise mTLS (M5+): central CA, mutual cert auth

Each step builds on previous, no architecture rewrite needed.

---

## 14. Open questions

- Should `revoke` invalidate immediately or with 5s grace period (in case
  user clicked by mistake)? **Decision: immediate, no grace** — security
  beats UX undo for this operation.
