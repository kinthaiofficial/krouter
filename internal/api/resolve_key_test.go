package api

import (
	"context"
	"sort"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/stretchr/testify/assert"
)

func TestResolveProviderKey_InheritedFromScannedApp(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "deepseek", APIKey: "sk-inherited"},
	})

	assert.Equal(t, "sk-inherited", srv.resolveProviderKey(context.Background(), "deepseek"))
}

func TestResolveProviderKey_EmptyWhenNoCredential(t *testing.T) {
	srv, _ := newTestServer(t)
	assert.Empty(t, srv.resolveProviderKey(context.Background(), "deepseek"))
}

func TestResolveProviderKey_RemovedAppContributesNothing(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.creds.ReplaceApp("cursor", []agentscan.Credential{
		{AppID: "cursor", Provider: "deepseek", APIKey: "sk-disabled-loses"},
	})
	srv.creds.RemoveApp("cursor")

	assert.Empty(t, srv.resolveProviderKey(context.Background(), "deepseek"),
		"disabled agent should not contribute keys")
}

func TestProvidersWithCredentials_InheritedOnly(t *testing.T) {
	srv, _ := newTestServer(t)
	srv.creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "anthropic", APIKey: "sk-anthropic"},
		{AppID: "openclaw", Provider: "groq", APIKey: "sk-groq"},
		{AppID: "openclaw", Provider: "minimax-portal", OAuthToken: "sk-cp-oauth"}, // no API key → exclude
	})

	got := srv.providersWithCredentials(context.Background())
	sort.Strings(got)
	assert.Equal(t, []string{"anthropic", "groq"}, got)
}
