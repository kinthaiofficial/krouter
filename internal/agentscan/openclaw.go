package agentscan

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// OpenClawScanner extracts vendor endpoint metadata from an OpenClaw config
// file (typically ~/.openclaw/openclaw.json).
//
// Additional sources used:
//   - ~/.openclaw/agents/main/agent/auth-profiles.json carries OAuth access
//     tokens for OAuth-style providers like minimax-portal. When that file is
//     present and contains a profile for a provider that also appears in the
//     main config, the OAuth token is attached to the relevant
//     InheritedEndpoint via ExtrasJSON.
type OpenClawScanner struct{}

func (OpenClawScanner) AgentID() string     { return "openclaw" }
func (OpenClawScanner) DisplayName() string { return "OpenClaw" }

func (OpenClawScanner) DefaultConfigPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "/.openclaw/openclaw.json"
	}
	return filepath.Join(home, ".openclaw", "openclaw.json")
}

// WatchPaths reports every file Scan reads: the main config plus each
// sub-agent's models.json and auth-profiles.json. Lets the periodic rescan
// notice a sub-agent or OAuth-token change even when the main config is
// untouched. Implements agentscan.PathWatcher.
func (OpenClawScanner) WatchPaths(configPath string) []string {
	paths := []string{configPath}
	agentsDir := filepath.Join(filepath.Dir(configPath), "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return paths
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		base := filepath.Join(agentsDir, ent.Name(), "agent")
		paths = append(paths, filepath.Join(base, "models.json"), filepath.Join(base, "auth-profiles.json"))
	}
	return paths
}

// openClawConfig mirrors only the fields of openclaw.json that this Scanner
// needs to read. Unknown fields are silently ignored.
type openClawConfig struct {
	Models struct {
		Providers map[string]openClawProvider `json:"providers"`
	} `json:"models"`
}

type openClawProvider struct {
	BaseURL    string `json:"baseUrl"`
	API        string `json:"api"`
	APIKey     string `json:"apiKey"`
	AuthHeader any    `json:"authHeader"` // can be bool or string depending on OpenClaw version
}

// openClawAuthProfiles mirrors ~/.openclaw/agents/<agent>/agent/auth-profiles.json.
type openClawAuthProfiles struct {
	Profiles map[string]openClawAuthProfile `json:"profiles"`
}

type openClawAuthProfile struct {
	Type     string `json:"type"`     // "oauth"
	Provider string `json:"provider"` // e.g. "minimax-portal"
	Access   string `json:"access"`   // OAuth access token
}

func (s OpenClawScanner) Scan(ctx context.Context, configPath string) ([]InheritedEndpoint, error) {
	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("read openclaw config: %w", err)
	}

	var cfg openClawConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse openclaw config: %w", err)
	}

	// Map provider name → OAuth access token discovered in any agent's
	// auth-profiles.json file. We scan all <agent>/agent/auth-profiles.json
	// siblings of the main config because OpenClaw supports multiple agents
	// (main / claude / etc.) and the token is per-agent.
	oauthTokens := loadOpenClawOAuthTokens(filepath.Dir(configPath))

	out := make([]InheritedEndpoint, 0, len(cfg.Models.Providers))
	for name, p := range cfg.Models.Providers {
		if name == "" || p.BaseURL == "" {
			continue
		}
		ep := InheritedEndpoint{
			Provider:     name,
			EndpointURL:  p.BaseURL,
			ProtocolHint: p.API,
			APIKey:       p.APIKey,
		}
		if token := oauthTokens[name]; token != "" {
			// Encode as a JSON object so callers can extend without breaking
			// existing readers.
			extras, _ := json.Marshal(map[string]string{
				"oauth_token": token,
				"purpose":     "subscription_oauth",
				"source":      "openclaw_auth_profile",
			})
			ep.ExtrasJSON = string(extras)
		}
		out = append(out, ep)
	}
	return out, nil
}

// loadOpenClawOAuthTokens walks the agents/*/agent/auth-profiles.json files
// rooted at openclawDir and returns a provider→accessToken map. Missing files
// and parse errors are silently ignored — callers fall back to API-key-only
// auth in that case.
func loadOpenClawOAuthTokens(openclawDir string) map[string]string {
	out := map[string]string{}
	agentsDir := filepath.Join(openclawDir, "agents")
	entries, err := os.ReadDir(agentsDir)
	if err != nil {
		return out
	}
	for _, ent := range entries {
		if !ent.IsDir() {
			continue
		}
		profilePath := filepath.Join(agentsDir, ent.Name(), "agent", "auth-profiles.json")
		raw, err := os.ReadFile(profilePath)
		if err != nil {
			continue
		}
		var pf openClawAuthProfiles
		if err := json.Unmarshal(raw, &pf); err != nil {
			continue
		}
		for _, prof := range pf.Profiles {
			if prof.Type == "oauth" && prof.Provider != "" && prof.Access != "" {
				// Last one wins if multiple agents have profiles for the same
				// provider; in practice they should all carry the same user's
				// token, so this is fine.
				out[prof.Provider] = prof.Access
			}
		}
	}
	return out
}
