package agentscan

import (
	"context"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// RunAll walks every enabled row in agent_settings, invokes the corresponding
// Scanner against the user-saved config_path, and writes the resulting
// endpoints into inherited_endpoints. Errors per agent are recorded in
// agent_settings.last_error and never propagated; one bad agent must not
// prevent the rest from running, and must never crash the daemon.
//
// Called by serve.go on daemon start. The single-agent variant ScanOne is
// used when the user clicks "rescan" on one agent in the dashboard.
func RunAll(ctx context.Context, store *storage.Store, logger logging.Logger) {
	if store == nil {
		return
	}
	settings, err := store.ListAgentSettings(ctx)
	if err != nil {
		logger.Warn("agent_inheritance: list settings failed", "err", err)
		return
	}
	for _, setting := range settings {
		if !setting.Enabled {
			continue
		}
		scanner := Get(setting.AgentID)
		if scanner == nil {
			// agent_settings has a row for an agent this build doesn't know
			// about (downgrade, future agent, …). Skip silently; the row
			// stays so a future upgrade picks it up.
			continue
		}
		if err := ScanOne(ctx, store, scanner, setting.ConfigPath); err != nil {
			logger.Warn("agent_inheritance: scan failed",
				"agent", setting.AgentID, "err", err)
			// ScanOne already recorded the error in agent_settings.last_error.
		}
	}
}

// StartPeriodicRescan runs RunAll on a fixed interval until ctx is cancelled.
// After each tick the optional onTick callback fires (typically wired to an
// SSE broadcast so the dashboard refetches /internal/agents/configured
// without waiting for the next react-query refetchInterval).
//
// Spec/04 §14 — "Hot reload via SSE broadcast on config change." With no
// fsnotify dependency, a 5-minute polling cadence is the Phase 1 compromise:
// the user's config file is small, re-reading it costs microseconds, and
// the latency between a user editing OpenClaw config and the daemon picking
// it up is bounded by the interval. Phase 2 can layer fsnotify on top for
// real-time updates if the polling latency proves annoying.
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
		_ = store.RecordAgentScan(ctx, scanner.AgentID(), now, scanErr.Error())
		return scanErr
	}

	rows := make([]storage.InheritedEndpoint, 0, len(results))
	for _, r := range results {
		if r.Provider == "" {
			continue
		}
		rows = append(rows, storage.InheritedEndpoint{
			AgentID:      scanner.AgentID(),
			Provider:     r.Provider,
			EndpointURL:  r.EndpointURL,
			ProtocolHint: r.ProtocolHint,
			APIKey:       r.APIKey,
			ExtrasJSON:   r.ExtrasJSON,
			CapturedAt:   now,
		})
	}

	if err := store.ReplaceInheritedEndpoints(ctx, scanner.AgentID(), rows); err != nil {
		_ = store.RecordAgentScan(ctx, scanner.AgentID(), now, err.Error())
		return err
	}
	return store.RecordAgentScan(ctx, scanner.AgentID(), now, "")
}
