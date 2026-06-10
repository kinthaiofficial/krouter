package main

import (
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/stretchr/testify/assert"
)

func TestReadMinimaxOAuth_FromScannedCreds(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "minimax-portal", OAuthToken: "sk-cp-FROM-AGENT"},
	})

	assert.Equal(t, "sk-cp-FROM-AGENT", readMinimaxOAuthFromCreds(creds))
}

func TestReadMinimaxOAuth_FallbacksToAlternateProviderName(t *testing.T) {
	creds := agentscan.NewCredStore()
	// Some agents may name the provider just "minimax" (no -portal).
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "minimax", OAuthToken: "sk-cp-ALT"},
	})

	assert.Equal(t, "sk-cp-ALT", readMinimaxOAuthFromCreds(creds))
}

func TestReadMinimaxOAuth_EmptyWhenNoOAuthToken(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "minimax-portal", APIKey: "sk-static"},
	})

	assert.Empty(t, readMinimaxOAuthFromCreds(creds),
		"static-key-only credential should not yield an OAuth token")
}

func TestReadMinimaxOAuth_RemovedAppTokenGone(t *testing.T) {
	creds := agentscan.NewCredStore()
	creds.ReplaceApp("openclaw", []agentscan.Credential{
		{AppID: "openclaw", Provider: "minimax-portal", OAuthToken: "sk-cp-DISABLED"},
	})
	creds.RemoveApp("openclaw")

	assert.Empty(t, readMinimaxOAuthFromCreds(creds))
}

func TestReadMinimaxOAuth_NilStoreSafe(t *testing.T) {
	assert.Empty(t, readMinimaxOAuthFromCreds(nil))
}
