package install

import (
	"encoding/json"
	"net/http"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
)

// ─── DTOs ─────────────────────────────────────────────────────────────────

type supportedAppResp struct {
	AppID       string `json:"app_id"`
	DisplayName string `json:"display_name"`
	DefaultPath string `json:"default_path"`
}

type previewRequest struct {
	AppID string `json:"app_id"`
	Path  string `json:"path"`
}

type previewResp struct {
	Endpoints []previewEndpoint `json:"endpoints"`
}

type previewEndpoint struct {
	Provider     string `json:"provider"`
	EndpointURL  string `json:"endpoint_url"`
	ProtocolHint string `json:"protocol_hint,omitempty"`
	HasAPIKey    bool   `json:"has_api_key"`     // never leak the key value
	HasOAuth     bool   `json:"has_oauth_token"` // ExtrasJSON.oauth_token != ""
}

type selectRequest struct {
	Agents []agentscan.PendingAgent `json:"agents"`
}

// ─── GET /api/install/apps/supported ──────────────────────────────────────

// handleAppsSupported returns the Scanner registry compiled into this
// installer binary. Equivalent to the daemon's /internal/apps/supported
// but reachable before the daemon is installed.
func (s *Server) handleAppsSupported(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	out := make([]supportedAppResp, 0, len(agentscan.Scanners))
	for _, sc := range agentscan.Scanners {
		out = append(out, supportedAppResp{
			AppID:       sc.AppID(),
			DisplayName: sc.DisplayName(),
			DefaultPath: sc.DefaultConfigPath(),
		})
	}
	writeJSON(w, out)
}

// ─── POST /api/install/apps/preview ───────────────────────────────────────
//
// Body: {app_id, path}
// Runs the Scanner without persisting anything; used by the wizard to show
// "we'd inherit N vendors" before the user commits.

func (s *Server) handleAppsPreview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body previewRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	scanner := agentscan.Get(body.AppID)
	if scanner == nil {
		writeError(w, http.StatusNotFound, "unknown app")
		return
	}
	path := body.Path
	if path == "" {
		path = scanner.DefaultConfigPath()
	}
	endpoints, err := scanner.Scan(r.Context(), path)
	if err != nil {
		// Surface the error to the wizard so the UI can show "config not
		// found / parse failure" inline. Status is still 200 because the
		// scan ran — just unsuccessfully.
		writeJSON(w, map[string]any{
			"endpoints": []previewEndpoint{},
			"error":     err.Error(),
		})
		return
	}
	out := make([]previewEndpoint, 0, len(endpoints))
	for _, ep := range endpoints {
		out = append(out, previewEndpoint{
			Provider:     ep.Provider,
			EndpointURL:  ep.EndpointURL,
			ProtocolHint: ep.ProtocolHint,
			HasAPIKey:    ep.APIKey != "",
			HasOAuth:     hasOAuthToken(ep.ExtrasJSON),
		})
	}
	writeJSON(w, previewResp{Endpoints: out})
}

// ─── POST /api/install/apps/select ────────────────────────────────────────
//
// Body: {agents: [{app_id, enabled, config_path}, ...]}
//
// Persists the wizard's selection by writing pending-agents.json. The
// daemon picks it up on startup (see agentscan.ImportPending).

func (s *Server) handleAppsSelect(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body selectRequest
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if err := agentscan.WritePending(body.Agents); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]any{"ok": true, "count": len(body.Agents)})
}

// hasOAuthToken reports whether ExtrasJSON carries a non-empty oauth_token.
// We never return the token value itself to the wizard — only the existence
// flag — so a curious user with browser devtools open can't lift it.
func hasOAuthToken(extrasJSON string) bool {
	if extrasJSON == "" {
		return false
	}
	var m map[string]string
	if err := json.Unmarshal([]byte(extrasJSON), &m); err != nil {
		return false
	}
	return m["oauth_token"] != ""
}
