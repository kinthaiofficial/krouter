package storage

// providerAliases maps a vendor's natural provider name — as it may appear in an
// AI app's config (e.g. OpenClaw's models.json) — to krouter's canonical adapter
// name, when the two differ. Inheritance matches on the canonical form so a user
// who named the vendor "dashscope" still has their key resolved for krouter's
// "qwen" adapter, without having to rename anything in their agent config.
//
// This mirrors pricing.LiteLLMToKrouterProvider, which maps the same vendor
// divergences from LiteLLM's naming. The two maps cover different input sources
// (agent config vs LiteLLM pricing data) that currently happen to coincide; keep
// them in sync when adding a vendor whose name differs from its krouter adapter.
var providerAliases = map[string]string{
	"dashscope":    "qwen",      // Aliyun DashScope → krouter qwen adapter
	"together_ai":  "together",  // Together AI
	"fireworks_ai": "fireworks", // Fireworks AI
}

// canonicalProviderName returns the krouter adapter name for a provider name,
// collapsing known vendor aliases. Unknown names pass through unchanged.
func canonicalProviderName(name string) string {
	if c, ok := providerAliases[name]; ok {
		return c
	}
	return name
}
