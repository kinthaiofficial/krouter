# spec/08-upgrade.md — Auto-Update System

**Module**: `internal/upgrade`
**Library**: `github.com/minio/selfupdate` or similar

---

## 1. Goal

Daemon and GUI auto-check for updates every 24h. New version available → notify
user via GUI. User confirms → daemon downloads, verifies signature, atomic
binary replace, restarts itself.

Daemon updates and GUI updates are **independent** — same release tag, but
each binary checks its own version.

---

## 2. Release pipeline

```
1. git tag v1.0.0
2. push tag → GitHub Actions triggers:
3. goreleaser builds binaries for all platforms
4. ECDSA P-256 sign each binary + signed manifest.json
5. Upload to GitHub Releases:
   - krouter-1.0.0-darwin-arm64
   - krouter-1.0.0-darwin-arm64.sig
   - krouter-1.0.0-linux-amd64
   - ... etc
   - manifest.json (contains all SHA-256 + signatures)
   - manifest.json.sig
6. Update "latest" tag pointer
```

`manifest.json` schema:
```json
{
  "version": "1.0.0",
  "released_at": "2026-05-12T10:00:00Z",
  "release_notes_url": "https://github.com/kinthaiofficial/krouter/releases/tag/v1.0.0",
  "min_supported_version": "0.9.0",
  "is_critical": false,
  "binaries": {
    "darwin-arm64": {
      "url": "https://github.com/.../krouter-1.0.0-darwin-arm64",
      "sha256": "abc123...",
      "size": 24386123
    },
    "darwin-amd64": {...},
    "linux-amd64": {...},
    "windows-amd64": {...}
  }
}
```

---

## 3. Client check logic

Background goroutine, every 24h after daemon startup:

```go
const CHECK_URL = "https://github.com/kinthaiofficial/krouter/releases/latest/download/manifest.json"

func (s *Service) checkOnce(ctx context.Context) {
    manifest, err := fetchAndVerify(ctx, CHECK_URL)
    if err != nil {
        log.Warnf("update check failed: %v", err)
        return
    }
    
    if !semver.Greater(manifest.Version, CurrentVersion) {
        return  // already up to date
    }
    
    // Notify GUI via internal API
    s.guiBus.Emit("upgrade:available", manifest)
    
    if manifest.IsCritical || 
       semver.Less(CurrentVersion, manifest.MinSupportedVersion) {
        // Force upgrade flow (see §4)
        s.guiBus.Emit("upgrade:critical", manifest)
    }
}
```

ECDSA signature verification (using embedded public key):
```go
publicKey := //go:embed public_key.pem
manifestBytes := download(manifest.json)
sigBytes := download(manifest.json.sig)

if !verifyECDSA(publicKey, manifestBytes, sigBytes) {
    return errors.New("manifest signature verification failed")
}
```

Each binary download is also signature-verified before applying.

---

## 4. Apply upgrade

User clicks "Update Now" in GUI → daemon:

1. Download new binary (with progress to GUI)
2. Verify SHA-256 vs manifest
3. Verify ECDSA signature
4. Use `selfupdate.Apply()` for atomic replacement:
   - Write new binary to temp file in same directory
   - rename() to current binary path (atomic on POSIX, atomic on Win)
5. Re-exec self with new binary
6. Old daemon process exits cleanly (graceful)

Critical: the proxy port (8402) is briefly unavailable during re-exec.
Window is < 1 second. Document this; acceptable for v1.

---

## 5. Force upgrade (min_supported_version)

If running version < min_supported_version:
- GUI shows blocking modal: "This version is no longer supported. Please update."
- Cannot dismiss
- Routing keeps working (we don't break user mid-session)
- After update, routing resumes normally

Use when:
- Protocol breaking changes (Anthropic API v2)
- Severe security vulnerabilities
- Internal contract changes that break old daemon ↔ new GUI

---

## 6. Platform-specific notes

### macOS

- Code-signed + notarized (Apple Developer Program required)
- After self-replace, may need to re-prompt for permissions? Test.

### Windows

- Code-signed (Sectigo OV cert, ~$85/year)
- AV may flag self-modifying executable; SHA-256 signature helps
- File locking: ensure no handle on old binary before rename

### Linux

- AppImage: replace entire .AppImage file
- Permissions: chmod +x after rename
- Distribution package managers (deb/rpm) don't trigger our self-update;
  rely on the package manager. Detection: if running from /usr/bin, skip self-update.

---

## 7. Public key handling

Private key:
- Generated once, stored in GitHub Actions secret (encrypted)
- Used by goreleaser to sign manifests
- **Never** in repo

Public key:
- Committed to repo as `internal/upgrade/public_key.pem`
- Embedded in binary via `//go:embed`
- Used for signature verification

Key rotation:
- If private key compromised: generate new keypair, publish emergency release
  with new public key, communicate via security advisory + announcement feed.
- Old versions can no longer auto-update. They show "Update available — please
  manually download" warning. Document this procedure.

---

## 8. User control

GUI Settings → Updates:
- ☑ Auto-check for updates (default ON)
- ☑ Auto-download (default ON)
- ☐ Auto-apply (default OFF; ask user)
- Channel: stable (only; M4+ may add beta channel)

---

## 9. Test coverage

- Unit: manifest verification (good + tampered signature)
- Unit: semver comparison
- Unit: file replacement (mock filesystem)
- Integration: full update cycle in CI (mock GitHub Releases)

---

## 10. Open questions

- Should we sign the binary itself in addition to the manifest? The manifest's
  SHA-256 covers integrity; the manifest signature covers authenticity. Adding
  per-binary signatures is belt-and-suspenders. **Decision: yes, sign each
  binary too** — defense in depth, low extra cost.
