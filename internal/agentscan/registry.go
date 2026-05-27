package agentscan

// Scanners is the static list of app scanners compiled into this build of
// krouter. Order is the order shown in the wizard / dashboard UI.
//
// To add a new app: implement Scanner in a new file in this package, then
// append the new value here. No other change is required; krouter discovers
// the new entry via this slice alone.
//
// Phase 1: OpenClaw and Claude Code (validated against real on-disk schemas).
// Phase 2: Cursor, Hermes, OpenCode, Codex (config formats confirmed).
// Phase 2: Pi (terminal coding agent, ~/.pi/agent/models.json).
var Scanners = []Scanner{
	OpenClawScanner{},
	ClaudeCodeScanner{},
	CursorScanner{},
	HermesScanner{},
	OpenCodeScanner{},
	CodexScanner{},
	PiScanner{},
}

// Get returns the Scanner with the given AppID, or nil if none is
// registered. Used by API handlers when the user requests a rescan or
// supported-apps listing.
func Get(appID string) Scanner {
	for _, s := range Scanners {
		if s.AppID() == appID {
			return s
		}
	}
	return nil
}

// IDs returns the AppID of every registered Scanner. Convenience for the
// /internal/apps/supported endpoint.
func IDs() []string {
	out := make([]string, 0, len(Scanners))
	for _, s := range Scanners {
		out = append(out, s.AppID())
	}
	return out
}
