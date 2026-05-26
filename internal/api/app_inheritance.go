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

// supportedAppJSON is the JSON shape returned by GET /internal/apps/supported.
// Comes from the Scanner registry compiled into this binary.
type supportedAppJSON struct {
	AppID       string `json:"app_id"`
	DisplayName string `json:"display_name"`
	DefaultPath string `json:"default_path"`
}

// configuredAppJSON is the JSON shape returned by GET /internal/apps/configured.
// Comes from app_settings + a count of inherited endpoints.
type configuredAppJSON struct {
	AppID          string `json:"app_id"`
	Enabled        bool   `json:"enabled"`
	ConfigPath     string `json:"config_path"`
	LastScannedAt  *int64 `json:"last_scanned_at,omitempty"`
	LastError      string `json:"last_error,omitempty"`
	InheritedCount int    `json:"inherited_count"`
}

// ─── GET /internal/apps/supported ───────────────────────────────────────────

func (s *Server) handleAppsSupported(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	out := make([]supportedAppJSON, 0, len(agentscan.Scanners))
	for _, sc := range agentscan.Scanners {
		out = append(out, supportedAppJSON{
			AppID:       sc.AppID(),
			DisplayName: sc.DisplayName(),
			DefaultPath: sc.DefaultConfigPath(),
		})
	}
	writeJSON(w, out)
}

// ─── GET /internal/apps/configured ──────────────────────────────────────────

func (s *Server) handleAppsConfigured(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []configuredAppJSON{})
		return
	}
	settings, err := s.store.ListAppSettings(r.Context())
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	out := make([]configuredAppJSON, 0, len(settings))
	for _, st := range settings {
		count := 0
		if eps, err := s.store.ListInheritedEndpointsByApp(r.Context(), st.AppID); err == nil {
			count = len(eps)
		}
		out = append(out, configuredAppJSON{
			AppID:          st.AppID,
			Enabled:        st.Enabled,
			ConfigPath:     st.ConfigPath,
			LastScannedAt:  st.LastScannedAt,
			LastError:      st.LastError,
			InheritedCount: count,
		})
	}
	writeJSON(w, out)
}

// ─── POST /internal/apps/{id}/rescan ────────────────────────────────────────
//
// Body (optional): {"path": "/custom/config/path"}
//
// If path is provided, app_settings.config_path is updated to it before the
// scan runs. Otherwise the existing stored path (or, for first-time use, the
// scanner's default) is used.

func (s *Server) doAppRescan(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scanner := agentscan.Get(appID)
	if scanner == nil {
		http.Error(w, "unknown app", http.StatusNotFound)
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
	configPath, err := s.resolveAppConfigPath(ctx, scanner, body.Path)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	if err := agentscan.ScanOne(ctx, s.store, scanner, configPath); err != nil {
		// scan error → still 200, the error is surfaced in the response body and
		// last_error so the dashboard can show it as part of the app row.
		writeJSON(w, map[string]any{
			"ok":              false,
			"app_id":          appID,
			"config_path":     configPath,
			"inherited_count": 0,
			"error":           err.Error(),
		})
		s.Broadcast("apps_changed", map[string]any{"app_id": appID})
		return
	}

	eps, _ := s.store.ListInheritedEndpointsByApp(ctx, appID)
	writeJSON(w, map[string]any{
		"ok":              true,
		"app_id":          appID,
		"config_path":     configPath,
		"inherited_count": len(eps),
	})
	s.Broadcast("apps_changed", map[string]any{"app_id": appID})
}

// resolveAppConfigPath persists path (if non-empty) and returns the
// effective config_path for the scan. If the app row doesn't exist yet, a
// fresh row is inserted with enabled=true (the rescan request implies the
// user wants this app included).
func (s *Server) resolveAppConfigPath(ctx context.Context, scanner agentscan.Scanner, path string) (string, error) {
	if s.store == nil {
		return "", nil
	}
	existing, err := s.store.GetAppSetting(ctx, scanner.AppID())
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
	return path, s.store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:      scanner.AppID(),
		Enabled:    enabled,
		ConfigPath: path,
	})
}

// ─── POST /internal/apps/{id}/enable | /disable ─────────────────────────────

func (s *Server) doAppEnable(w http.ResponseWriter, r *http.Request, appID string) {
	s.setAppEnabled(w, r, appID, true)
}

func (s *Server) doAppDisable(w http.ResponseWriter, r *http.Request, appID string) {
	s.setAppEnabled(w, r, appID, false)
}

func (s *Server) setAppEnabled(w http.ResponseWriter, r *http.Request, appID string, enabled bool) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	scanner := agentscan.Get(appID)
	if scanner == nil {
		http.Error(w, "unknown app", http.StatusNotFound)
		return
	}
	ctx := r.Context()

	// Upsert with the desired enabled flag, preserving config_path if a row exists.
	existing, err := s.store.GetAppSetting(ctx, appID)
	if err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	configPath := scanner.DefaultConfigPath()
	if existing != nil {
		configPath = existing.ConfigPath
	}
	if err := s.store.UpsertAppSetting(ctx, storage.AppSetting{
		AppID:      appID,
		Enabled:    enabled,
		ConfigPath: configPath,
	}); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}

	// Disabling clears inherited rows so routing engine ignores them immediately.
	if !enabled {
		_ = s.store.ReplaceInheritedEndpoints(ctx, appID, nil)
	}

	writeJSON(w, map[string]any{"ok": true, "app_id": appID, "enabled": enabled})
	s.Broadcast("apps_changed", map[string]any{"app_id": appID})
}

// ─── DELETE /internal/apps/{id} ─────────────────────────────────────────────

func (s *Server) doAppDelete(w http.ResponseWriter, r *http.Request, appID string) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, "store unavailable", http.StatusServiceUnavailable)
		return
	}
	if err := s.store.DeleteAppSetting(r.Context(), appID); err != nil {
		http.Error(w, "database error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]any{"ok": true, "app_id": appID})
	s.Broadcast("apps_changed", map[string]any{"app_id": appID})
}

// ─── Dispatch helpers ──────────────────────────────────────────────────────

// inheritanceActionDispatch is called from handleAppAction's default branch
// when the action is one of the inheritance verbs (rescan / enable / disable).
// Returns true if it handled the request.
func (s *Server) inheritanceActionDispatch(w http.ResponseWriter, r *http.Request, name, action string) bool {
	switch action {
	case "rescan":
		s.doAppRescan(w, r, name)
		return true
	case "enable":
		s.doAppEnable(w, r, name)
		return true
	case "disable":
		s.doAppDisable(w, r, name)
		return true
	}
	return false
}

// resolveProviderKey returns the API key for providerName inherited from any
// enabled AI app's config. Returning "" means "no credential available —
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
// inherited endpoint from an enabled app carries a non-empty credential.
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

// appRootDispatch handles methods sent to /internal/apps/{id} (no
// trailing action). Currently only DELETE is meaningful. Returns true if it
// handled the request.
func (s *Server) appRootDispatch(w http.ResponseWriter, r *http.Request) bool {
	tail := strings.TrimPrefix(r.URL.Path, "/internal/apps/")
	if tail == "" || strings.Contains(tail, "/") {
		return false
	}
	if r.Method == http.MethodDelete {
		s.doAppDelete(w, r, tail)
		return true
	}
	return false
}
