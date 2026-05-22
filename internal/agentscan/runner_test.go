package agentscan_test

import (
	"context"
	"errors"
	"testing"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeScanner lets tests inject scan results / errors without touching the
// filesystem.
type fakeScanner struct {
	id      string
	name    string
	path    string
	results []agentscan.InheritedEndpoint
	err     error
}

func (f fakeScanner) AgentID() string           { return f.id }
func (f fakeScanner) DisplayName() string       { return f.name }
func (f fakeScanner) DefaultConfigPath() string { return f.path }
func (f fakeScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	return f.results, f.err
}

func newRunnerStore(t *testing.T) *storage.Store {
	t.Helper()
	s, err := storage.Open(":memory:")
	require.NoError(t, err)
	require.NoError(t, s.Migrate())
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func setScannersFor(t *testing.T, ss ...agentscan.Scanner) {
	t.Helper()
	saved := agentscan.Scanners
	agentscan.Scanners = ss
	t.Cleanup(func() { agentscan.Scanners = saved })
}

func TestScanOne_Success(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()

	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/tmp/x",
	}))

	scanner := fakeScanner{
		id: "openclaw", name: "OpenClaw", path: "/tmp/x",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "https://api.anthropic.com"},
			{Provider: "minimax-portal", EndpointURL: "http://127.0.0.1:8402",
				APIKey: "sk-api-foo", ExtrasJSON: `{"k":"v"}`},
		},
	}
	require.NoError(t, agentscan.ScanOne(ctx, s, scanner, "/tmp/x"))

	endpoints, err := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	require.NoError(t, err)
	require.Len(t, endpoints, 2)

	got, _ := s.GetAgentSetting(ctx, "openclaw")
	require.NotNil(t, got.LastScannedAt)
	assert.Empty(t, got.LastError)
}

func TestScanOne_ScannerErrorPersistsLastError(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/tmp/x",
	}))

	scanner := fakeScanner{id: "openclaw", err: errors.New("simulated parse failure")}
	err := agentscan.ScanOne(ctx, s, scanner, "/tmp/x")
	require.Error(t, err)

	got, _ := s.GetAgentSetting(ctx, "openclaw")
	assert.Equal(t, "simulated parse failure", got.LastError)
	require.NotNil(t, got.LastScannedAt)

	eps, _ := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	assert.Empty(t, eps)
}

func TestScanOne_OverwritesPreviousEndpoints(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	require.NoError(t, agentscan.ScanOne(ctx, s, fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u1"},
			{Provider: "deepseek", EndpointURL: "u2"},
		},
	}, "/x"))

	require.NoError(t, agentscan.ScanOne(ctx, s, fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u1-new"},
		},
	}, "/x"))

	eps, _ := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	require.Len(t, eps, 1)
	assert.Equal(t, "u1-new", eps[0].EndpointURL)
}

func TestRunAll_SkipsDisabledAndUnknown(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()

	enabledScanner := fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u"},
		},
	}
	disabledScanner := fakeScanner{id: "claude-code"}
	setScannersFor(t, enabledScanner, disabledScanner)

	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "claude-code", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "future-agent-xyz", Enabled: true, ConfigPath: "/z",
	}))

	agentscan.RunAll(ctx, s, logging.New("error"))

	enabledEps, _ := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	assert.Len(t, enabledEps, 1)

	disabledEps, _ := s.ListInheritedEndpointsByAgent(ctx, "claude-code")
	assert.Empty(t, disabledEps)

	phantomEps, _ := s.ListInheritedEndpointsByAgent(ctx, "future-agent-xyz")
	assert.Empty(t, phantomEps)
}

func TestRunAll_OneFailureDoesNotAbortOthers(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()

	failing := fakeScanner{id: "openclaw", err: errors.New("simulated")}
	working := fakeScanner{
		id: "claude-code",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u"},
		},
	}
	setScannersFor(t, failing, working)

	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.UpsertAgentSetting(ctx, storage.AgentSetting{
		AgentID: "claude-code", Enabled: true, ConfigPath: "/y",
	}))

	agentscan.RunAll(ctx, s, logging.New("error"))

	failed, _ := s.GetAgentSetting(ctx, "openclaw")
	assert.NotEmpty(t, failed.LastError)
	failedEps, _ := s.ListInheritedEndpointsByAgent(ctx, "openclaw")
	assert.Empty(t, failedEps)

	ok, _ := s.GetAgentSetting(ctx, "claude-code")
	assert.Empty(t, ok.LastError)
	okEps, _ := s.ListInheritedEndpointsByAgent(ctx, "claude-code")
	assert.Len(t, okEps, 1)
}

func TestRunAll_NilStoreIsNoOp(t *testing.T) {
	agentscan.RunAll(context.Background(), nil, logging.New("error"))
}
