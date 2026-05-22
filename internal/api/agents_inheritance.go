package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────

// supportedAgentJSON is the JSON shape returned by GET /internal/agents/supported.
// Comes from the Scanner registry compiled into this binary.
type supportedAgentJSON struct {
	AgentID     string `json:"agent_id"`
	DisplayName string `json:"display_name"`
	DefaultPath string `json:"default_path"`
}

// configuredAgentJSON is the JSON shape returned by GET /internal/agents/configured.
// Comes from agent_settings + a count of inherited endpoints.
type configuredAgentJSON struct {
	AgentID         string `json:"agent_id"`
	Enabled         bool   `json:"enabled"`
	ConfigPath      string `json:"config_path"`
	LastScannedAt   *int64 `json:"last_scanned_at,omitempty"`
	LastError       string `json:"last_error,omitempty"`
	InheritedCount  int    `json:"inherited_count"`
}

// ─── GET /internal/agents/supported ────────────────────────────────────────

func (s *Server) handleAgentsSupported(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out := make([]supportedAgentJSON, 0, len(agentscan.Scanners))
	for _, sc := range agentscan.Scanners {
		out = append(out, supportedAgentJSON{
			AgentID:     sc.AgentID(),
			DisplayName: sc.DisplayName(),
			DefaultPath: sc.DefaultConfigPath(),
		})
	}
	writeJSON(w, out)
}

// ─── GET /internal/agents/configured ───────────────────────────────────────

func (s *Server) handleAgentsConfigured(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []configuredAgentJSON{})
		return
	}
	settings, err := s.store.ListAgentSettings(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	out := make([]configuredAgentJSON, 0, len(settings))
	for _, st := range settings {
		count := 0
		if eps, err := s.store.ListInheritedEndpointsByAgent(r.Context(), st.AgentID); err == nil {
			count = len(eps)
		}
		out = append(out, configuredAgentJSON{
			AgentID:        st.AgentID,
			Enabled:        st.Enabled,
			ConfigPath:     st.ConfigPath,
			LastScannedAt:  st.LastScannedAt,
			LastError:      st.LastError,
			InheritedCount: count,
		})
	}
	writeJSON(w, out)
}

// ─── POST /internal/agents/{id}/rescan ─────────────────────────────────────
//
// Body (optional): {"path": "/custom/config/path"}
//
// If path is provided, agent_settings.config_path is updated to it before the
// scan runs. Otherwise the existing stored path (or, for first-time use, the
// scanner's default) is used.

func (s *Server) doAgentRescan(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scanner := agentscan.Get(agentID)
	if scanner == nil {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}

	var body struct {
		Path string `json:"path"`
	}
	if r.ContentLength > 0 {
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}
	}

	ctx := r.Context()
	configPath, err := s.resolveAgentConfigPath(ctx, scanner, body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := agentscan.ScanOne(ctx, s.store, scanner, configPath); err != nil {
		// scan error → still 200, the error is surfaced in the response body and
		// last_error so the dashboard can show it as part of the agent row.
		writeJSON(w, map[string]any{
			"ok":               false,
			"agent_id":         agentID,
			"config_path":      configPath,
			"inherited_count":  0,
			"error":            err.Error(),
		})
		s.Broadcast("agents_changed", map[string]any{"agent_id": agentID})
		return
	}

	eps, _ := s.store.ListInheritedEndpointsByAgent(ctx, agentID)
	writeJSON(w, map[string]any{
		"ok":              true,
		"agent_id":        agentID,
		"config_path":     configPath,
		"inherited_count": len(eps),
	})
	s.Broadcast("agents_changed", map[string]any{"agent_id": agentID})
}

// resolveAgentConfigPath persists path (if non-empty) and returns the
// effective config_path for the scan. If the agent row doesn't exist yet, a
// fresh row is inserted with enabled=true (the rescan request implies the
// user wants this agent included).
func (s *Server) resolveAgentConfigPath(ctx context.Context, scanner agentscan.Scanner, path string) (string, error) {
	if s.store == nil {
		return "", nil
	}
	existing, err := s.store.GetAgentSetting(ctx, scanner.AgentID())
	if err != nil {
		return "", err
	}
	if path == "" {
		if existing != nil {
			return existing.ConfigPath, nil
		}
		return scanner.DefaultConfigPath(), nil
	}
	enabled := true
	if existing != nil {
		enabled = existing.Enabled
	}
	return path, s.store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID:    scanner.AgentID(),
		Enabled:    enabled,
		ConfigPath: path,
	})
}

// ─── POST /internal/agents/{id}/enable | /disable ──────────────────────────

func (s *Server) doAgentEnable(w http.ResponseWriter, r *http.Request, agentID string) {
	s.setAgentEnabled(w, r, agentID, true)
}

func (s *Server) doAgentDisable(w http.ResponseWriter, r *http.Request, agentID string) {
	s.setAgentEnabled(w, r, agentID, false)
}

func (s *Server) setAgentEnabled(w http.ResponseWriter, r *http.Request, agentID string, enabled bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scanner := agentscan.Get(agentID)
	if scanner == nil {
		http.Error(w, "unknown agent", http.StatusNotFound)
		return
	}
	ctx := r.Context()

	// Upsert with the desired enabled flag, preserving config_path if a row exists.
	existing, err := s.store.GetAgentSetting(ctx, agentID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	configPath := scanner.DefaultConfigPath()
	if existing != nil {
		configPath = existing.ConfigPath
	}
	if err := s.store.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID:    agentID,
		Enabled:    enabled,
		ConfigPath: configPath,
	}); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Disabling clears inherited rows so routing engine ignores them immediately.
	if !enabled {
		_ = s.store.ReplaceInheritedEndpoints(ctx, agentID, nil)
	}

	writeJSON(w, map[string]any{"ok": true, "agent_id": agentID, "enabled": enabled})
	s.Broadcast("agents_changed", map[string]any{"agent_id": agentID})
}

// ─── DELETE /internal/agents/{id} ──────────────────────────────────────────

func (s *Server) doAgentDelete(w http.ResponseWriter, r *http.Request, agentID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.store.DeleteAgentSetting(r.Context(), agentID); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "agent_id": agentID})
	s.Broadcast("agents_changed", map[string]any{"agent_id": agentID})
}

// ─── Dispatch helpers ──────────────────────────────────────────────────────

// inheritanceActionDispatch is called from handleAgentAction's default branch
// when the action is one of the inheritance verbs (rescan / enable / disable).
// Returns true if it handled the request.
func (s *Server) inheritanceActionDispatch(w http.ResponseWriter, r *http.Request, name, action string) bool {
	switch action {
	case "rescan":
		s.doAgentRescan(w, r, name)
		return true
	case "enable":
		s.doAgentEnable(w, r, name)
		return true
	case "disable":
		s.doAgentDisable(w, r, name)
		return true
	}
	return false
}

// resolveProviderKey returns the API key for providerName inherited from any
// enabled AI agent's config. Returning "" means "no credential available —
// skip this provider".
func (s *Server) resolveProviderKey(ctx context.Context, providerName string) string {
	if s.store != nil {
		if eps, err := s.store.FindInheritedEndpointsByProvider(ctx, providerName); err == nil {
			for _, ep := range eps {
				if ep.APIKey != "" {
					return ep.APIKey
				}
			}
		}
	}
	return ""
}

// providersWithCredentials returns the set of provider names for which an
// inherited endpoint from an enabled agent carries a non-empty credential.
// Used by RefreshModelsIfStale to decide what to discover.
func (s *Server) providersWithCredentials(ctx context.Context) []string {
	set := map[string]struct{}{}
	if s.store != nil {
		if eps, err := s.store.ListInheritedEndpoints(ctx); err == nil {
			for _, ep := range eps {
				if ep.APIKey != "" {
					set[ep.Provider] = struct{}{}
				}
			}
		}
	}
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	return out
}

// agentRootDispatch handles methods sent to /internal/agents/{id} (no
// trailing action). Currently only DELETE is meaningful. Returns true if it
// handled the request.
func (s *Server) agentRootDispatch(w http.ResponseWriter, r *http.Request) bool {
	tail := strings.TrimPrefix(r.URL.Path, "/internal/agents/")
	if tail == "" || strings.Contains(tail, "/") {
		return false
	}
	if r.Method == http.MethodDelete {
		s.doAgentDelete(w, r, tail)
		return true
	}
	return false
}
