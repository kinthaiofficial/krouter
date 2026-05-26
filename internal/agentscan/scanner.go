// Package agentscan implements the per-AI-app configuration scanners that
// krouter uses to inherit endpoint, API key, and OAuth token data from apps
// the user already has configured on their machine (OpenClaw, Claude Code,
// Cursor, Cline, etc.). See spec/04-agent-inheritance.md.
//
// Each app is represented by a single value of type Scanner. The Scanner
// knows its display name, default config-file path, and how to parse that file
// into a slice of InheritedEndpoint. Scan() is a pure function — it never
// modifies the app's config and never writes to the krouter database; the
// caller (typically internal/api) persists the result.
//
// Adding support for a new app is purely additive:
//
//  1. Create internal/agentscan/<app>.go with a Scanner implementation.
//  2. Append the new Scanner to Scanners in registry.go.
//
// No external manifest, no daemon restart logic, no config-file describing how
// to parse another config-file. The Scanner interface IS the abstraction.
package agentscan

import "context"

// InheritedEndpoint is one vendor endpoint extracted from an agent's config.
// Provider is the krouter-internal vendor name (e.g. "anthropic",
// "minimax-portal", "openrouter") and should match an entry in provider_config
// when one exists; unknown providers are still recorded so the dashboard can
// surface them.
type InheritedEndpoint struct {
	// Provider names the vendor as krouter knows it. Empty providers are dropped.
	Provider string

	// EndpointURL is the upstream base URL the agent was configured to call.
	// It may already point at krouter's proxy (http://127.0.0.1:8402) if the
	// agent has been connected previously; callers should treat that as "the
	// user already routes via krouter for this provider".
	EndpointURL string

	// ProtocolHint is the agent's own declaration of the wire protocol, e.g.
	// "anthropic-messages" or "openai-chat". Empty when the agent doesn't say.
	ProtocolHint string

	// APIKey is the static API key from the agent config, if any. May be empty
	// when the agent uses OAuth or some other auth mechanism — in that case the
	// real credential lives in ExtrasJSON.
	APIKey string

	// ExtrasJSON carries auth- or vendor-specific data that doesn't fit the
	// common fields above. Examples: minimax subscription OAuth tokens,
	// custom headers, deployment names. Always a valid JSON object string (or
	// empty). Consumers parse on demand.
	ExtrasJSON string
}

// Scanner is implemented once per supported AI app.
//
// All methods must be safe to call concurrently. Scan must not panic; any
// error during parsing should be returned so the caller can surface it in the
// UI. A returned nil slice with nil error is valid and means "config exists
// but no vendors were configured in it".
type Scanner interface {
	// AppID returns the stable identifier used in the app_settings table
	// and the routing engine. Must be lowercase, ASCII, and not change between
	// releases (changing it amounts to a DB migration).
	AppID() string

	// DisplayName returns the human-readable name shown in the wizard and
	// dashboard. Free-form; may be localized in the future.
	DisplayName() string

	// DefaultConfigPath returns the absolute path that this agent installs to
	// by convention on the current host. Used to seed the wizard's "Path"
	// field when the user has not provided an override yet.
	DefaultConfigPath() string

	// Scan reads the config file at configPath and extracts the vendor
	// endpoints the user has configured. configPath is typically either the
	// value returned by DefaultConfigPath or a user-supplied override stored
	// in agent_settings.config_path; Scanner implementations should not
	// re-derive the path themselves.
	//
	// ctx is honoured for any I/O operations that might block.
	Scan(ctx context.Context, configPath string) ([]InheritedEndpoint, error)
}
