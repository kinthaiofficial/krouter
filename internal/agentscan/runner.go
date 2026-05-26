package agentscan

import (
	"context"
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
// Scanner against the user-saved config_path, and writes the resulting
// endpoints into inherited_endpoints. Errors per app are recorded in
// app_settings.last_error and never propagated; one bad app must not
// prevent the rest from running, and must never crash the daemon.
//
// Called by serve.go on daemon start. The single-app variant ScanOne is
// used when the user clicks "rescan" on one app in the dashboard.
func RunAll(ctx context.Context, store *storage.Store, logger logging.Logger) {
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
		if configUnchangedSince(scanner, setting.ConfigPath, setting.LastScannedAt) {
			continue
		}
		if err := ScanOne(ctx, store, scanner, setting.ConfigPath); err != nil {
			logger.Warn("app_inheritance: scan failed",
				"app", setting.AppID, "err", err)
			// ScanOne already recorded the error in app_settings.last_error.
		}
	}
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
			RunAll(ctx, store, logger)
			if onTick != nil {
				onTick()
			}
		}
	}
}

// ScanOne executes a single Scanner and persists the result. Returns the
// underlying error so the API layer can surface it to the user; the
// last_error column is always updated regardless of return value.
func ScanOne(ctx context.Context, store *storage.Store, scanner Scanner, configPath string) error {
	now := time.Now().UTC().UnixMilli()

	results, scanErr := scanner.Scan(ctx, configPath)
	if scanErr != nil {
		_ = store.RecordAppScan(ctx, scanner.AppID(), now, scanErr.Error())
		return scanErr
	}

	rows := make([]storage.InheritedEndpoint, 0, len(results))
	for _, r := range results {
		if r.Provider == "" {
			continue
		}
		rows = append(rows, storage.InheritedEndpoint{
			AppID:        scanner.AppID(),
			Provider:     r.Provider,
			EndpointURL:  r.EndpointURL,
			ProtocolHint: r.ProtocolHint,
			APIKey:       r.APIKey,
			ExtrasJSON:   r.ExtrasJSON,
			CapturedAt:   now,
		})
	}

	if err := store.ReplaceInheritedEndpoints(ctx, scanner.AppID(), rows); err != nil {
		_ = store.RecordAppScan(ctx, scanner.AppID(), now, err.Error())
		return err
	}
	return store.RecordAppScan(ctx, scanner.AppID(), now, "")
}
