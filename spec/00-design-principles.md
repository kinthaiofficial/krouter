# spec/00 — Design Principles

> **Status**: living document (this file evolves with the codebase).
> **Audience**: every contributor and reviewer. Read this *before* designing a new feature; reference it *during* code review.
> **Scope**: cross-cutting rules that every later spec (`spec/04`, `spec/05`, …) and every PR is expected to respect.

The principles below were extracted from working sessions on the agent-inheritance, subscription-quota, and pricing subsystems. Each principle is paired with the concrete incident or trade-off that produced it, so a future contributor can decide whether the principle still holds when their case looks different.

---

## A. Product positioning

### A1. Take over, do not configure
krouter's core differentiator is that it adopts the AI agents the user is already using — OpenClaw, Claude Code, Cursor, etc. — and inherits their vendor endpoints, API keys, and OAuth tokens automatically. **Do not build features that require the user to re-type information they already configured elsewhere.**

**How to apply.** When a feature asks the user for a piece of data, first check whether an agent's config file already has it. If yes, write a Scanner (`spec/04`) instead of a settings field.

### A2. At least one agent is the minimum viable install
A daemon with no enabled agent has nothing to route. The wizard's "Agent Paths" step deliberately greys out the "Continue" button until at least one agent is checked. Do not add a "Skip" button anywhere in the install flow that would let the user dismiss the agent picker.

### A3. The selling point is configuration cost, not feature count
Comparable LLM routers expose every knob; krouter's value is that the user never sees a knob they didn't already turn. When adding settings, ask: "is this something the user already declared somewhere? If so, inherit it. If not, can we infer it from the codebase / vendor's API?"

---

## B. Transparent proxy invariants

### B1. Only the `model` field may be rewritten
The HTTP body and every header the agent sent — including Authorization — must reach the upstream byte-for-byte except for the routing engine's model override. If you find yourself reaching into the body for any other reason, you are violating this rule and breaking an undocumented contract the client agent relies on.

### B2. Same-protocol routing only
A request that came in as Anthropic Messages format may only be routed to another Anthropic-protocol endpoint. Same for OpenAI Chat Completions. No cross-protocol rewrites — the routing engine never sees a `claude-3.7-sonnet` request and forwards to `gpt-4o`. Each protocol's quirks (streaming format, tool-call shape, stop-reason vocabulary) defeat naive bridging.

### B3. Protocol is a first-class extension point
Anthropic and OpenAI aren't the only candidates; future protocols (Gemini, Cohere, …) need to slot in cleanly. A new protocol = add a Go engine + register it; never paper-thin "is this anthropic-ish?" checks.

### B4. Don't read shell environment outside the user's own process
Scanners read **the agent's config file**. They do not read `~/.zshrc` to fish out `ANTHROPIC_API_KEY=`, do not read `~/.aws/credentials`, do not scrape `~/.config/gcloud`. The user has already declared what they want shared with krouter when they pointed an agent at it.

### B5. The user's API key is the request's responsibility
For transparent proxy paths (Claude Code, MiniMax), the request carries the key the user already had configured. krouter does not "store keys for the user" — credentials scanned from agent configs live only in the daemon's memory (`agentscan.CredStore`, repopulated from the agent's own config files at startup and on every rescan) and are **never persisted to SQLite or any file**. The `inherited_endpoints` table keeps endpoint metadata only; its `api_key` column was dropped in migration 022. See `cmd/krouter/key_resolver.go::resolveProviderKeyForRouting`.

---

## C. Data architecture

### C1. The DB only reflects user actions, never pre-populated defaults
`agent_settings` rows are written **only** when the user explicitly enables an agent, adjusts its path, or rescan-fails. A daemon that boots into a fresh DB sees an empty table and emits an empty `/internal/agents/configured` payload. Pre-populating rows for "agents the user might want" is forbidden — it makes "is this what the user chose?" indistinguishable from "is this what some past krouter version assumed?".

### C2. Single source of truth, no parallel tables
When two consumers of a piece of data drift apart, they will eventually disagree. The pricing-table bug ([PR #1 review history](../CHANGELOG.md)) is the canonical case: an independent `internal/providers/minimax/pricing.go` lookup got out of sync with `internal/storage/subscription_quota.go::minimaxPlanPriceCNY` by a factor of 1043×. Whenever a value can be derived from a single source, **derive it everywhere from that source**. Document the source-of-truth function in the package-level docstring so reviewers spot duplications.

### C3. Code, not data-driven manifests, defines extension points
A new agent = add a Go file implementing `agentscan.Scanner` and append the value to `agentscan.Scanners`. There is no `inheritance_rules.json` describing how to parse agent config; that meta-layer was deliberately rejected (`spec/04 §15`). If a thing is "different per agent", express it as Go code, not as YAML or JSON that the daemon then has to interpret.

### C4. Follow upstream rather than maintain in-house
Vendor pricing changes; vendor model lists change. Don't try to keep an internal mirror current by hand. Pull from LiteLLM's published `model_prices.json` (ETag-conditional), call `/v1/models` on each provider on a schedule. The in-tree fallback values (`pricing.go::staticPrices`) exist only for first-launch / offline scenarios — they are not authoritative.

### C5. Endpoint lists are inherited, not enumerated
The set of "providers krouter supports" is the union of `provider_config` (builtins) plus whatever the user's agents already mention. When a new vendor X appears in OpenClaw, the user's krouter starts knowing about X on the next periodic rescan; nobody needs to ship a new krouter binary that lists X.

---

## D. User experience

### D1. Don't ask the user for what they already declared
If the user enabled OpenClaw and OpenClaw has DeepSeek configured with an API key, **never** show a "DeepSeek API key" input field in the dashboard's Settings page. Show that DeepSeek is configured via OpenClaw, with the agent name as the source. Manual override (`settings.ProviderKeys`) is still available, but it's a corner case, not the front-line UX.

### D2. Don't auto-hunt unknown paths
Each Scanner declares one default config path. If that path is wrong, show the user a "Path:" text field they can edit; do not try `~/.config/foo`, `~/Library/Application Support/foo`, `/opt/foo/etc` automatically. Auto-hunting hides ambiguity and creates support tickets when two of the candidates happen to exist.

### D3. Reuse the same component for Wizard and Dashboard
The list of supported agents, the row layout, the "enable / disable" toggle — these should render identically in the installer wizard and the dashboard. Maintainers should not have to keep two parallel UIs in sync.

### D4. Apply config changes immediately, broadcast over SSE
When the user toggles an agent, edits a path, or hits Rescan, the daemon writes the change and broadcasts an `agents_changed` event. The dashboard refetches; no daemon restart. Same for subscription quota / pricing data flowing back. Polling intervals are a fallback for when SSE drops, not the primary delivery mechanism.

### D5. Mimic familiar OS installers when listing default paths
Showing every supported agent and its default path — even those not installed — sets the right expectation about scope. Users who only have OpenClaw still see a Claude Code row marked "Not installed". This is how every macOS / Windows installer presents components, and trying to be cleverer than that confuses people.

---

## E. Engineering implementation

### E1. Prefer simplicity over premature abstraction
Three rejected abstractions document the rule:
- `inheritance_rules.json` (data-driven Scanner): rejected; Scanner is Go code.
- `provider_config_variables` (force-feed variables the user already configured): rejected; we already have the data via inheritance.
- `model_catalog` (parallel table for model lists): rejected as redundant; provider-discovery + LiteLLM cover this.

The default assumption when reviewing a new abstraction is "this layer should not exist." Prove the case before adding it.

### E2. Failure isolation
One Scanner failing must not abort the rest of `agentscan.RunAll`. One vendor's quota poll erroring must not crash the daemon. Wrap each per-agent / per-vendor unit of work in its own error scope, record the failure in `last_error`, log a warning, and continue. The daemon must survive a malformed agent config, a 502 from a vendor, a clock that jumps backwards.

### E3. Test what you ship; document what you skip
A scanner implementation goes into Phase 1 only after it has been validated against a real config file from a real installation of the agent. Phase 2 scanners ship after their schema is verified. Speculative scanners stay in design comments, not in `Scanners`. (`spec/04 §16`.)

### E4. Increment in phases
The agent-inheritance feature ships as Phase 1 (OpenClaw + Claude Code), Phase 2 (Cursor / Cline / Codex once their schemas are confirmed), Phase 3 (enterprise agents, per-feature quota). Each phase is independently mergeable; later phases never break earlier ones.

### E5. Be precise; quantify
"Mostly supported" is not a description. "Supported except for vision input on MiniMax-M2.5-vlm" is a description. PR descriptions and spec sections should not contain weasel words; if a scope is partial, list the omissions explicitly.

### E6. Record every non-trivial decision
Every spec doc has a "Confirmed trade-offs" section that captures the decision *and the reason*. Reviewers reading the spec a year later need to know not just what we chose but why an alternative was rejected — otherwise the alternative gets reproposed.

---

## F. Engineering trade-offs

### F1. Don't add complexity for marginal value
"Adding a new agent without releasing a new binary" sounds clean, but it would require a manifest-driven Scanner layer that we explicitly do not want (`E1`). The cost (an extra abstraction the daemon has to interpret) exceeds the value (release cadence is already weekly).

### F2. Define product boundaries explicitly
Vendors the user has not configured in any agent are out of scope. Routing engine never invents a vendor. This rule keeps the architecture tight: the only way for a vendor to "appear" is via an agent.

### F3. Reuse infrastructure
ETag / `sync_meta` for periodic pulls already exists; new data sources reuse it (`pricing.go`, future `subscription_pricing.json`). OAuth flows for inherited tokens go through the user's agent (which already has an OAuth implementation); krouter does not implement its own.

### F4. Delete redundant code aggressively
When a feature subsumes an older approach, delete the older approach in the same PR. The `model_catalog` table was scheduled for deletion the moment model discovery + LiteLLM made it redundant — even though it had no known bugs. Carrying around obsolete tables turns the schema into a museum.

---

## G. Process / meta-principles

### G1. Challenge every layer of abstraction
The default question on any new layer is "could we delete this?" Lots of layers we discussed were collapsed away by repeatedly asking. The `inheritance_rules.json` layer, the `provider_config_variables` table, the `model_catalog` table all failed the question.

### G2. Don't use weasel words
"Covers most cases" is not allowed in spec text. Either enumerate the cases, or say "covers cases X / Y / Z; out of scope: A, B."

### G3. Borrow ideas, don't borrow names
We may study other open-source LLM routers for inspiration, but their variable names, table names, file paths, and API field names do not propagate into krouter. Naming should reflect *our* design vocabulary, not theirs — otherwise readers think the two products work the same way, which is often false.

### G4. Decisions are reversible by default
Every spec records "confirmed trade-offs" with the reason. A future PR may revisit any of them; the spec is not law, it is a snapshot of judgment as of its date. When revisiting, update the trade-off entry rather than silently inverting the choice.

### G5. PR descriptions read like commit messages on steroids
"Summary / Why / What / Test plan / CHANGELOG impact / Relationship to open PRs" — explicit, scannable, no marketing voice. The PR description is the canonical record of intent for that change; future maintainers grep it before they grep the code.

---

## How this document is maintained

- New principles are appended (not interspersed) — keep the section letter / number stable so spec cross-references don't rot.
- A principle is added only after it has been violated in a PR and the violation made the right rule visible. We don't write principles speculatively.
- Each principle's "How to apply" line is the one a reviewer can quote in a PR comment. If a principle has no concrete "what to do" line, it's not actionable enough.
- When a principle becomes obsolete (the underlying premise changed), do not delete it — strike it through and add the date / PR that retired it. Future contributors searching for the old behaviour need to find the old rule.

---

## Cross-references

| Topic | Spec | Implementation |
|---|---|---|
| Agent inheritance Scanner architecture | `spec/04` | `internal/agentscan/` |
| Subscription quota model + pricing | `spec/05` | `internal/storage/subscription_quota.go`, `internal/storage/token_price_sub.go` |
| Resolving an API key | A1, D1 | `cmd/krouter/key_resolver.go::resolveProviderKeyForRouting`, `internal/api/server.go::resolveProviderKey` |
| Periodic rescan | D4 | `internal/agentscan/runner.go::StartPeriodicRescan` |
| Failure isolation | E2 | `internal/agentscan/runner.go::RunAll` |
