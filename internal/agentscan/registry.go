package agentscan

// Scanners is the static list of agent scanners compiled into this build of
// krouter. Order is the order shown in the wizard / dashboard UI.
//
// To add a new agent: implement Scanner in a new file in this package, then
// append the new value here. No other change is required; krouter discovers
// the new entry via this slice alone.
//
// Phase 1 of spec/04 ships only the two agents that have been validated
// against real on-disk config schemas. Cursor / Cline / Codex / Hermes etc.
// will be added in follow-up PRs once their config formats are confirmed.
var Scanners = []Scanner{
	OpenClawScanner{},
	ClaudeCodeScanner{},
}

// Get returns the Scanner with the given AgentID, or nil if none is
// registered. Used by API handlers when the user requests a rescan or
// supported-agents listing.
func Get(agentID string) Scanner {
	for _, s := range Scanners {
		if s.AgentID() == agentID {
			return s
		}
	}
	return nil
}

// IDs returns the AgentID of every registered Scanner. Convenience for the
// /internal/agents/supported endpoint.
func IDs() []string {
	out := make([]string, 0, len(Scanners))
	for _, s := range Scanners {
		out = append(out, s.AgentID())
	}
	return out
}
