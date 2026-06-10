package agentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// PathWatcher is an optional Scanner extension. A scanner that reads more than
// its primary config file (e.g. OpenClaw, which also reads per-agent
// models.json / auth-profiles.json) implements this so the periodic rescan can
// detect changes across all of them. Scanners that don't implement it are
// assumed to read only configPath.
type PathWatcher interface {
	WatchPaths(configPath string) []string
}

// configUnchangedSince reports whether none of a scanner's input files have
// been modified since lastScannedAt (ms UTC). Used by the periodic rescan to
// skip re-parsing unchanged configs. Returns false ("must scan") when never
// scanned, on the first sight of any file at-or-after the last scan, or when a
// watched file's mtime cannot be determined as older. Manual rescans call
// ScanOne directly and are never gated by this.
func configUnchangedSince(scanner Scanner, configPath string, lastScannedAt *int64) bool {
	if lastScannedAt == nil {
		return false
	}
	paths := []string{configPath}
	if w, ok := scanner.(PathWatcher); ok {
		if wp := w.WatchPaths(configPath); len(wp) > 0 {
			paths = wp
		}
	}
	for _, p := range paths {
		fi, err := os.Stat(p)
		if err != nil {
			continue // missing/unreadable file: nothing changed to read there
		}
		if fi.ModTime().UnixMilli() >= *lastScannedAt {
			return false // modified at or after our last scan → rescan
		}
	}
	return true
}

// RunAll walks every enabled row in app_settings, invokes the corresponding
// Scanner against the user-saved config_path, writes the resulting endpoint
// metadata into inherited_endpoints, and loads the scanned credentials into
// creds (memory only). Errors per app are recorded in app_settings.last_error
// and never propagated; one bad app must not prevent the rest from running,
// and must never crash the daemon.
//
// force bypasses the configUnchangedSince mtime gate. Daemon startup MUST
// pass force=true: the credential store starts empty after a restart, and an
// unchanged config file would otherwise skip the scan that repopulates it.
// The periodic rescan passes force=false to keep the 1-minute poll cheap.
//
// The single-app variant ScanOne is used when the user clicks "rescan" on
// one app in the dashboard.
func RunAll(ctx context.Context, store *storage.Store, creds *CredStore, logger logging.Logger, force bool) {
	if store == nil {
		return
	}
	settings, err := store.ListAppSettings(ctx)
	if err != nil {
		logger.Warn("app_inheritance: list settings failed", "err", err)
		return
	}
	for _, setting := range settings {
		if !setting.Enabled {
			// Keep memory consistent with the enabled set even if the
			// disable happened while the daemon wasn't looking.
			if creds != nil {
				creds.RemoveApp(setting.AppID)
			}
			continue
		}
		scanner := Get(setting.AppID)
		if scanner == nil {
			// app_settings has a row for an app this build doesn't know
			// about (downgrade, future app, …). Skip silently; the row
			// stays so a future upgrade picks it up.
			continue
		}
		// Skip the parse + DB write + SSE broadcast when nothing this scanner
		// reads has changed since the last scan. Keeps the 1-minute poll cheap.
		if !force && configUnchangedSince(scanner, setting.ConfigPath, setting.LastScannedAt) {
			continue
		}
		if err := scanOneRecovered(ctx, store, creds, scanner, setting.ConfigPath); err != nil {
			logger.Warn("app_inheritance: scan failed",
				"app", setting.AppID, "err", err)
			// ScanOne already recorded the error in app_settings.last_error.
		}
	}
}

// scanOneRecovered wraps ScanOne with a panic recovery so one misbehaving
// scanner (malformed config triggering a parser panic, etc.) cannot take
// down the periodic rescan loop or the daemon — failures must not spread.
func scanOneRecovered(ctx context.Context, store *storage.Store, creds *CredStore, scanner Scanner, configPath string) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("scanner panicked: %v", r)
		}
	}()
	return ScanOne(ctx, store, creds, scanner, configPath)
}

// StartPeriodicRescan runs RunAll on a fixed interval until ctx is cancelled.
// After each tick the optional onTick callback fires (typically wired to an
// SSE broadcast so the dashboard refetches /internal/apps/configured
// without waiting for the next react-query refetchInterval).
//
// Spec/04 §14 — "Hot reload via SSE broadcast on config change." We poll
// rather than depend on fsnotify; RunAll stats each app's config files and
// skips the parse/DB-write/broadcast when nothing changed (configUnchangedSince),
// so a tight 1-minute cadence stays near-free when idle while keeping the
// edit→pickup latency bounded by the interval.
//
// Passing interval <= 0 returns immediately (disabled).
func StartPeriodicRescan(
	ctx context.Context,
	store *storage.Store,
	creds *CredStore,
	logger logging.Logger,
	interval time.Duration,
	onTick func(),
) {
	if interval <= 0 || store == nil {
		return
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			RunAll(ctx, store, creds, logger, false)
			if onTick != nil {
				// A panicking broadcast callback must not kill the rescan loop.
				func() {
					defer func() {
						if r := recover(); r != nil {
							logger.Warn("app_inheritance: onTick panicked", "panic", r)
						}
					}()
					onTick()
				}()
			}
		}
	}
}

// ScanOne executes a single Scanner and persists the result. Returns the
// underlying error so the API layer can surface it to the user; the
// last_error column is always updated regardless of return value.
//
// Scan output is split at this boundary (D-003): credentials (api_key, and
// the oauth_token field inside extras) go to the in-memory creds store only;
// the rows written to SQLite carry endpoint metadata with all credentials
// stripped. The memory store is updated FIRST so there is no window where
// the DB row exists but its credential is unavailable.
func ScanOne(ctx context.Context, store *storage.Store, creds *CredStore, scanner Scanner, configPath string) error {
	now := time.Now().UTC().UnixMilli()

	results, scanErr := scanner.Scan(ctx, configPath)
	if scanErr != nil {
		_ = store.RecordAppScan(ctx, scanner.AppID(), now, scanErr.Error())
		return scanErr
	}

	rows := make([]storage.InheritedEndpoint, 0, len(results))
	var scanned []Credential
	for _, r := range results {
		if r.Provider == "" {
			continue
		}
		oauthToken, sanitizedExtras := splitOAuthToken(r.ExtrasJSON)
		if r.APIKey != "" || oauthToken != "" {
			scanned = append(scanned, Credential{
				AppID:      scanner.AppID(),
				Provider:   r.Provider,
				APIKey:     r.APIKey,
				OAuthToken: oauthToken,
			})
		}
		rows = append(rows, storage.InheritedEndpoint{
			AppID:        scanner.AppID(),
			Provider:     r.Provider,
			EndpointURL:  r.EndpointURL,
			ProtocolHint: r.ProtocolHint,
			ExtrasJSON:   sanitizedExtras,
			CapturedAt:   now,
		})
	}

	if creds != nil {
		creds.ReplaceApp(scanner.AppID(), scanned)
	}

	if err := store.ReplaceInheritedEndpoints(ctx, scanner.AppID(), rows); err != nil {
		_ = store.RecordAppScan(ctx, scanner.AppID(), now, err.Error())
		return err
	}
	return store.RecordAppScan(ctx, scanner.AppID(), now, "")
}

// splitOAuthToken extracts the oauth_token field from an extras JSON blob,
// returning the token and the blob with the field removed (empty string when
// nothing else remains). Non-JSON or token-free extras pass through unchanged.
func splitOAuthToken(extrasJSON string) (token, sanitized string) {
	if extrasJSON == "" {
		return "", ""
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(extrasJSON), &m); err != nil {
		return "", extrasJSON
	}
	t, ok := m["oauth_token"].(string)
	if !ok || t == "" {
		return "", extrasJSON
	}
	delete(m, "oauth_token")
	if len(m) == 0 {
		return t, ""
	}
	rest, err := json.Marshal(m)
	if err != nil {
		return t, ""
	}
	return t, string(rest)
}
