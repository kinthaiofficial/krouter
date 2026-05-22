package agentscan_test

import (
	"context"
	"errors"
	"testing"
	"time"

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

func TestStartPeriodicRescan_TicksAndStopsOnCtxCancel(t *testing.T) {
	s := newRunnerStore(t)

	// Pre-seed: one enabled agent with a fake scanner that records each
	// invocation. ScanOne is invoked once per RunAll, so a tick count is
	// directly observable via len(scanner.calls).
	calls := 0
	scanner := callCountingScanner{id: "openclaw", incr: func() { calls++ }}
	setScannersFor(t, scanner)
	require.NoError(t, s.UpsertAgentSetting(context.Background(), storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	// Fast ticker so the test finishes quickly. onTick increments a separate
	// counter — must run after every RunAll, exactly once per tick.
	ticks := 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(ctx, s, logging.New("error"), 20*time.Millisecond, func() {
			ticks++
		})
		close(done)
	}()

	// Allow ~3-5 ticks.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done // ensure the loop exited before we read the counters

	assert.GreaterOrEqual(t, calls, 2, "scanner should have been invoked at least 2 times across ticks")
	assert.GreaterOrEqual(t, ticks, 2, "onTick should have fired at least 2 times")
	// Allow at most one tick where RunAll returned early because ctx was
	// cancelled mid-call (after the ticker fired but before
	// store.ListAgentSettings completed). In that race, onTick still fires
	// but ScanOne is skipped — harmless and unobservable to callers.
	assert.LessOrEqual(t, ticks-calls, 1,
		"ticks and scanner calls should agree, allowing at most one cancel-race delta")
}

func TestStartPeriodicRescan_ZeroIntervalReturnsImmediately(t *testing.T) {
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(
			context.Background(), newRunnerStore(t), logging.New("error"),
			0, func() {})
		close(done)
	}()
	select {
	case <-done:
		// good — function returned without entering the loop
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StartPeriodicRescan with interval=0 did not return immediately")
	}
}

func TestStartPeriodicRescan_NilStoreReturnsImmediately(t *testing.T) {
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(
			context.Background(), nil, logging.New("error"),
			10*time.Millisecond, func() {})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("StartPeriodicRescan with nil store did not return immediately")
	}
}

func TestStartPeriodicRescan_NilOnTickDoesNotPanic(t *testing.T) {
	// onTick=nil is valid; the loop should just skip the callback.
	s := newRunnerStore(t)
	setScannersFor(t, callCountingScanner{id: "openclaw"})
	require.NoError(t, s.UpsertAgentSetting(context.Background(), storage.AgentSetting{
		AgentID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(ctx, s, logging.New("error"), 20*time.Millisecond, nil)
		close(done)
	}()
	time.Sleep(60 * time.Millisecond)
	cancel()
	<-done
}

// callCountingScanner is a Scanner stub that runs an incrementer in Scan().
// Lives in this test file rather than runner_test.go's fakeScanner because we
// need to side-effect from inside Scan to count loop iterations.
type callCountingScanner struct {
	id   string
	incr func()
}

func (s callCountingScanner) AgentID() string           { return s.id }
func (s callCountingScanner) DisplayName() string       { return s.id }
func (s callCountingScanner) DefaultConfigPath() string { return "/d" }
func (s callCountingScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	if s.incr != nil {
		s.incr()
	}
	return nil, nil
}
