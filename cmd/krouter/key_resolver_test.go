package main

import (
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/stretchr/testify/assert"
)

func TestResolveProviderKeyForRouting_InheritedFromScannedApp(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "deepseek", APIKey: "sk-from-inherited"},
	})

	assert.Equal(t, "sk-from-inherited",
		resolveProviderKeyForRouting(creds, "deepseek"))
}

func TestResolveProviderKeyForRouting_AliasAware(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "dashscope", APIKey: "sk-dashscope"},
	})

	assert.Equal(t, "sk-dashscope", resolveProviderKeyForRouting(creds, "qwen"),
		"a key scanned under the vendor alias must resolve for krouter's adapter name")
}

func TestResolveProviderKeyForRouting_EmptyWhenNothingConfigured(t *testing.T) {
	assert.Empty(t, resolveProviderKeyForRouting(agentscan.NewCredStore(), "deepseek"))
}

func TestResolveProviderKeyForRouting_NilSafe(t *testing.T) {
	assert.Empty(t, resolveProviderKeyForRouting(nil, "deepseek"))
}

func TestResolveProviderKeyForRouting_RemovedAppKeyGone(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("cursor", []agentscan.Credential{
		{AppID: "cursor", Provider: "deepseek", APIKey: "sk-disabled"},
	})
	creds.RemoveApp("cursor")

	assert.Empty(t, resolveProviderKeyForRouting(creds, "deepseek"),
		"a disabled/removed app's key must not be used by routing")
}
