package agentscan

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
)

// OpenClawAgent is a per-agent profile inside an OpenClaw install.
// OpenClaw supports running multiple named agents (`main`, `claude`,
// `deepseek`, …) each with their own provider configuration and
// primary model. Different from the existing `InheritedEndpoint` flow:
// that one captures a single flat (provider → endpoint+key) map for
// the whole OpenClaw install; this exposes the per-agent breakdown
// the dashboard's Agents page surfaces.
//
// This is a read-only descriptor — does not write to the inheritance
// table and does not influence routing. Routing still operates on
// `requested_model` from the proxied request.
type OpenClawAgent struct {
	ID           string                       `json:"id"`
	DisplayName  string                       `json:"display_name,omitempty"`
	PrimaryModel string                       `json:"primary_model,omitempty"`
	Workspace    string                       `json:"workspace,omitempty"`
	AgentDir     string                       `json:"agent_dir,omitempty"`
	Providers    []OpenClawAgentProvider   `json:"providers,omitempty"`
	// HasOAuth is true when this agent has a non-empty
	// auth-profiles.json — useful UI hint for "this agent is
	// authenticated via OAuth (e.g. MiniMax portal)".
	HasOAuth bool `json:"has_oauth,omitempty"`
}

// OpenClawAgentProvider is the per-agent view of one provider
// row. APIKey is intentionally NOT included — the dashboard never needs
// the raw key; we emit a `has_api_key` boolean instead so the UI can
// show "✓ configured" without exposing secrets.
type OpenClawAgentProvider struct {
	Provider     string   `json:"provider"`
	BaseURL      string   `json:"base_url,omitempty"`
	Protocol     string   `json:"protocol,omitempty"`     // "anthropic-messages" / "openai-chat"
	Models       []string `json:"models,omitempty"`       // model ids surfaced by this agent
	PrimaryModel string   `json:"primary_model,omitempty"`
	HasAPIKey    bool     `json:"has_api_key,omitempty"`
}

// openClawGlobalConfig is the subset of ~/.openclaw/openclaw.json we
// read for the agent list. Unknown fields are silently ignored.
type openClawGlobalConfig struct {
	Agents struct {
		Defaults struct {
			Model     any            `json:"model"`     // {"primary": "..."} object OR direct string
			Workspace string         `json:"workspace"`
			Models    map[string]any `json:"models"`    // {<id>: {"alias":...}} — used to derive a default model list
		} `json:"defaults"`
		List []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			AgentDir  string `json:"agentDir"`
			Model     string `json:"model"`
			Workspace string `json:"workspace"`
		} `json:"list"`
	} `json:"agents"`
}

// perAgentModels mirrors ~/.openclaw/agents/<id>/agent/models.json.
type perAgentModels struct {
	Providers map[string]struct {
		API       string `json:"api"`
		APIKey    string `json:"apiKey"`
		BaseURL   string `json:"baseUrl"`
		Models    []struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		} `json:"models"`
	} `json:"providers"`
}

// ListOpenClawAgents enumerates the agents inside an OpenClaw
// install at `openclawDir` (the directory containing `openclaw.json`,
// typically `~/.openclaw`). Returns an empty slice when OpenClaw is
// not installed or has no agent list.
//
// Each returned agent is sorted by ID for stable rendering.
func ListOpenClawAgents(openclawDir string) ([]OpenClawAgent, error) {
	globalPath := filepath.Join(openclawDir, "openclaw.json")
	globalRaw, err := os.ReadFile(globalPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []OpenClawAgent{}, nil // OpenClaw not installed
		}
		return nil, err
	}

	var global openClawGlobalConfig
	if err := json.Unmarshal(globalRaw, &global); err != nil {
		return nil, err
	}

	defaultPrimary := extractPrimaryModel(global.Agents.Defaults.Model)

	out := make([]OpenClawAgent, 0, len(global.Agents.List))
	for _, entry := range global.Agents.List {
		if entry.ID == "" {
			continue
		}

		sub := OpenClawAgent{
			ID:           entry.ID,
			DisplayName:  firstNonEmpty(entry.Name, entry.ID),
			PrimaryModel: firstNonEmpty(entry.Model, defaultPrimary),
			Workspace:    entry.Workspace,
		}

		// Resolve agentDir — sometimes specified, sometimes inferred.
		agentDir := entry.AgentDir
		if agentDir == "" {
			agentDir = filepath.Join(openclawDir, "agents", entry.ID, "agent")
		}
		sub.AgentDir = agentDir

		// Per-agent models.json (optional).
		sub.Providers = readAgentProviders(agentDir, sub.PrimaryModel)

		// Per-agent auth-profiles.json — UI cares about presence only.
		sub.HasOAuth = agentHasOAuth(agentDir)

		out = append(out, sub)
	}

	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

// readAgentProviders parses <agentDir>/models.json into the structured
// per-provider summary. Returns nil when the file is absent or invalid —
// the agent then surfaces with only its `agents.list` metadata.
func readAgentProviders(agentDir, primaryModel string) []OpenClawAgentProvider {
	path := filepath.Join(agentDir, "models.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var doc perAgentModels
	if err := json.Unmarshal(raw, &doc); err != nil {
		return nil
	}

	out := make([]OpenClawAgentProvider, 0, len(doc.Providers))
	for name, p := range doc.Providers {
		if name == "" {
			continue
		}
		row := OpenClawAgentProvider{
			Provider:  name,
			BaseURL:   p.BaseURL,
			Protocol:  p.API,
			HasAPIKey: p.APIKey != "",
		}
		for _, m := range p.Models {
			if m.ID != "" {
				row.Models = append(row.Models, m.ID)
			}
		}
		// Echo the agent's primary model on the matching provider row
		// so the UI can highlight which model in this provider's list is
		// the default. The primary model id looks like `<provider>/<model>`
		// in openclaw.json (e.g. `anthropic/claude-haiku-4-5`).
		if primaryModel != "" {
			if prov, mod := splitPrimary(primaryModel); prov == name {
				row.PrimaryModel = mod
			}
		}
		sort.Strings(row.Models)
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Provider < out[j].Provider })
	return out
}

// agentHasOAuth reports whether <agentDir>/auth-profiles.json carries
// at least one OAuth profile. The dashboard surfaces this as a small
// chip on the agent card.
func agentHasOAuth(agentDir string) bool {
	raw, err := os.ReadFile(filepath.Join(agentDir, "auth-profiles.json"))
	if err != nil {
		return false
	}
	var doc openClawAuthProfiles
	if err := json.Unmarshal(raw, &doc); err != nil {
		return false
	}
	for _, p := range doc.Profiles {
		if p.Type == "oauth" && p.Access != "" {
			return true
		}
	}
	return false
}

// extractPrimaryModel reads the `model` field on `agents.defaults` —
// it may be either `{"primary":"<id>"}` or a bare string. We tolerate
// both shapes since the OpenClaw schema has historically been flexible.
func extractPrimaryModel(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		if s, ok := x["primary"].(string); ok {
			return s
		}
	}
	return ""
}

// splitPrimary splits `<provider>/<model>` into its two parts. Returns
// ("", primary) when the string has no slash.
func splitPrimary(primary string) (provider, model string) {
	for i := 0; i < len(primary); i++ {
		if primary[i] == '/' {
			return primary[:i], primary[i+1:]
		}
	}
	return "", primary
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}
