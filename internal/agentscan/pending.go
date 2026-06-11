package agentscan

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// PendingFileName is the file the installer writes when the user chooses
// which apps to enable in the wizard's "App Paths" step. The daemon
// reads it on startup and merges the selections into app_settings before
// running the regular inheritance flow. See spec/04 §4.
//
// The file is removed after a successful import; partial failures leave it in
// place so a later daemon launch can retry without losing the wizard input.
const PendingFileName = "pending-agents.json"

// PendingFileDir returns the directory the daemon reads — and the installer
// writes — pending-agents.json in. Resolution order:
//
//  1. $KROUTER_CONFIG_DIR if set (used by tests; also a manual override for
//     site-specific deployments).
//  2. ~/.kinthai/ otherwise.
//
// The daemon already keeps data.db, internal-token, and logs/ under
// ~/.kinthai/, so the pending file sitting next to them keeps the on-disk
// layout coherent. More importantly, this avoids a subtle alignment bug:
// the macOS LaunchAgent plist only injects HOME (see
// internal/config/launchagent_darwin.go) — it does NOT propagate
// XDG_CONFIG_HOME or any other shell env var. If the installer ran from a
// terminal where the user had set XDG_CONFIG_HOME, the installer would
// write the file under that XDG path while the daemon — started by
// launchd with no XDG_CONFIG_HOME — would look elsewhere, silently losing
// the wizard's selections. Tying the path purely to HOME (which both
// processes see) keeps the two binaries in lockstep.
//
// The function never returns an error so callers can `os.MkdirAll` the
// result unconditionally; if the home dir is unavailable we return "" and
// the caller falls back to silent no-op.
func PendingFileDir() string {
	if explicit := os.Getenv("KROUTER_CONFIG_DIR"); explicit != "" {
		return explicit
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".kinthai")
}

// PendingAgent is one entry in pending-agents.json. The installer writes one
// element per app the user explicitly toggled; apps not in the file are
// not modified by the daemon.
//
// Exported because the installer package constructs values of this type when
// handing wizard input off to the daemon.
type PendingAgent struct {
	AppID      string `json:"app_id"`
	Enabled    bool   `json:"enabled"`
	ConfigPath string `json:"config_path"`
}

type pendingFile struct {
	Agents []PendingAgent `json:"agents"`
}

// ImportPending reads pending-agents.json from PendingFileDir, applies each
// row to app_settings (and runs ScanOne when enabled), then removes the
// file. Missing file is a no-op without error. Any individual app's
// failure is logged but does not abort the import; the file is removed only
// when the entire batch processed cleanly so a future daemon start can
// retry on partial failure.
func ImportPending(ctx context.Context, store *storage.Store, creds *CredStore, logger logging.Logger) {
	if store == nil {
		return
	}
	dir := PendingFileDir()
	if dir == "" {
		return
	}
	path := filepath.Join(dir, PendingFileName)

	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return
	}
	if err != nil {
		logger.Warn("pending-agents: read failed", "path", path, "err", err)
		return
	}

	var pf pendingFile
	if err := json.Unmarshal(data, &pf); err != nil {
		logger.Warn("pending-agents: parse failed", "err", err)
		// A malformed file is more useful as a debugging artefact than as a
		// silent skip; rename it so a future start doesn't see it.
		_ = os.Rename(path, path+".malformed")
		return
	}

	allOK := true
	for _, p := range pf.Agents {
		if p.AppID == "" {
			continue
		}
		if err := applyPending(ctx, store, creds, p); err != nil {
			allOK = false
			logger.Warn("pending-agents: apply failed", "app", p.AppID, "err", err)
		}
	}

	if allOK {
		_ = os.Remove(path)
	}
}

func applyPending(ctx context.Context, store *storage.Store, creds *CredStore, p PendingAgent) error {
	// Persisting the user's intent (app_settings row) must succeed for the
	// pending file to be considered cleanly imported. Scan failure on a
	// missing or malformed app config is *expected*: ScanOne records the
	// error on the same row and the user can fix it from the dashboard
	// later. We deliberately do NOT bubble that up as a hard failure here,
	// because doing so would keep pending-agents.json around and let a
	// stale wizard answer overwrite any later dashboard edits on the next
	// daemon start.
	if err := store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:      p.AppID,
		Enabled:    p.Enabled,
		ConfigPath: p.ConfigPath,
	}); err != nil {
		return fmt.Errorf("upsert: %w", err)
	}
	if !p.Enabled {
		// User disabled the app — clear any stale inherited rows (and the
		// in-memory credentials) so routing stops considering them immediately.
		if creds != nil {
			creds.RemoveApp(p.AppID)
		}
		if err := store.ReplaceInheritedEndpoints(ctx, p.AppID, nil); err != nil {
			return fmt.Errorf("clear inherited: %w", err)
		}
		return nil
	}
	scanner := Get(p.AppID)
	if scanner == nil {
		// Pending file references an app this binary doesn't know about
		// (downgrade scenario). The row is preserved so a future upgrade can
		// pick it up, but we don't ScanOne now.
		return nil
	}
	// ScanOne writes any error into app_settings.last_error via
	// RecordAppScan. We swallow the return value here because the user's
	// intent ("enable this app at this path") is already persisted; a
	// failed scan is a recoverable runtime condition, not an import error.
	// The recovered variant matters here: ImportPending runs synchronously
	// during daemon startup, so a scanner panic must not abort the launch.
	_ = scanOneRecovered(ctx, store, creds, scanner, p.ConfigPath)
	return nil
}

// WritePending serializes the given selections into pending-agents.json,
// creating the directory if needed. Used by the installer's
// /api/install/apps/select endpoint to hand state off to the daemon
// without an in-process channel.
func WritePending(agents []PendingAgent) error {
	dir := PendingFileDir()
	if dir == "" {
		return fmt.Errorf("pending-agents: no config dir")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	body, err := json.MarshalIndent(pendingFile{Agents: agents}, "", "  ")
	if err != nil {
		return err
	}
	tmp := filepath.Join(dir, PendingFileName+".tmp")
	if err := os.WriteFile(tmp, body, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, filepath.Join(dir, PendingFileName))
}
