package agentscan_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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

func (f fakeScanner) AppID() string             { return f.id }
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

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/tmp/x",
	}))

	scanner := fakeScanner{
		id: "openclaw", name: "OpenClaw", path: "/tmp/x",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "https://api.anthropic.com"},
			{Provider: "minimax-portal", EndpointURL: "http://127.0.0.1:8402",
				APIKey: "sk-api-foo", ExtrasJSON: `{"k":"v"}`},
		},
	}
	require.NoError(t, agentscan.ScanOne(ctx, s, agentscan.NewCredStore(), scanner, "/tmp/x"))

	endpoints, err := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	require.Len(t, endpoints, 2)

	got, _ := s.GetAppSetting(ctx, "openclaw")
	require.NotNil(t, got.LastScannedAt)
	assert.Empty(t, got.LastError)
}

func TestScanOne_ScannerErrorPersistsLastError(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/tmp/x",
	}))

	scanner := fakeScanner{id: "openclaw", err: errors.New("simulated parse failure")}
	err := agentscan.ScanOne(ctx, s, agentscan.NewCredStore(), scanner, "/tmp/x")
	require.Error(t, err)

	got, _ := s.GetAppSetting(ctx, "openclaw")
	assert.Equal(t, "simulated parse failure", got.LastError)
	require.NotNil(t, got.LastScannedAt)

	eps, _ := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	assert.Empty(t, eps)
}

func TestScanOne_OverwritesPreviousEndpoints(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	require.NoError(t, agentscan.ScanOne(ctx, s, agentscan.NewCredStore(), fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u1"},
			{Provider: "deepseek", EndpointURL: "u2"},
		},
	}, "/x"))

	require.NoError(t, agentscan.ScanOne(ctx, s, agentscan.NewCredStore(), fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "anthropic", EndpointURL: "u1-new"},
		},
	}, "/x"))

	eps, _ := s.ListInheritedEndpointsByApp(ctx, "openclaw")
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

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "claude-code", Enabled: false, ConfigPath: "/y",
	}))
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "future-agent-xyz", Enabled: true, ConfigPath: "/z",
	}))

	agentscan.RunAll(ctx, s, agentscan.NewCredStore(), logging.New("error"), true)

	enabledEps, _ := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	assert.Len(t, enabledEps, 1)

	disabledEps, _ := s.ListInheritedEndpointsByApp(ctx, "claude-code")
	assert.Empty(t, disabledEps)

	phantomEps, _ := s.ListInheritedEndpointsByApp(ctx, "future-agent-xyz")
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

	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "claude-code", Enabled: true, ConfigPath: "/y",
	}))

	agentscan.RunAll(ctx, s, agentscan.NewCredStore(), logging.New("error"), true)

	failed, _ := s.GetAppSetting(ctx, "openclaw")
	assert.NotEmpty(t, failed.LastError)
	failedEps, _ := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	assert.Empty(t, failedEps)

	ok, _ := s.GetAppSetting(ctx, "claude-code")
	assert.Empty(t, ok.LastError)
	okEps, _ := s.ListInheritedEndpointsByApp(ctx, "claude-code")
	assert.Len(t, okEps, 1)
}

func TestRunAll_NilStoreIsNoOp(t *testing.T) {
	agentscan.RunAll(context.Background(), nil, agentscan.NewCredStore(), logging.New("error"), true)
}

func TestStartPeriodicRescan_TicksAndStopsOnCtxCancel(t *testing.T) {
	s := newRunnerStore(t)

	// Pre-seed: one enabled agent with a fake scanner that records each
	// invocation. ScanOne is invoked once per RunAll, so a tick count is
	// directly observable via len(scanner.calls).
	calls := 0
	scanner := callCountingScanner{id: "openclaw", incr: func() { calls++ }}
	setScannersFor(t, scanner)
	require.NoError(t, s.UpsertAppSetting(context.Background(), storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	// Fast ticker so the test finishes quickly. onTick increments a separate
	// counter — must run after every RunAll, exactly once per tick.
	ticks := 0
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(ctx, s, agentscan.NewCredStore(), logging.New("error"), 20*time.Millisecond, func() {
			ticks++
		})
		close(done)
	}()

	// Allow ~3-5 ticks.
	time.Sleep(100 * time.Millisecond)
	cancel()
	<-done // ensure the loop exited before we read the counters

	assert.GreaterOrEqual(t, ticks, 2, "onTick should fire on every tick regardless of short-circuit")
	assert.GreaterOrEqual(t, calls, 1, "scanner runs on the first tick (no prior scan recorded)")
	// After the first scan, the mtime short-circuit skips ScanOne on subsequent
	// ticks because the config file is unchanged (here it is absent), so the
	// scanner is invoked far fewer times than the loop ticks. This is the
	// intended 1-minute-poll behavior — onTick still fires every tick.
	assert.LessOrEqual(t, calls, ticks, "short-circuit means scanner calls never exceed ticks")
}

func TestRunAll_SkipsUnchangedConfigButRescansAfterChange(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()

	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "openclaw.json")
	require.NoError(t, os.WriteFile(cfgPath, []byte(`{}`), 0644))
	// Anchor the initial mtime in the past so the first scan's timestamp is
	// unambiguously newer (avoids a same-millisecond race in the skip check).
	past := time.Now().Add(-time.Hour)
	require.NoError(t, os.Chtimes(cfgPath, past, past))

	calls := 0
	setScannersFor(t, callCountingScanner{id: "openclaw", incr: func() { calls++ }})
	require.NoError(t, s.UpsertAppSetting(ctx, storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: cfgPath,
	}))

	agentscan.RunAll(ctx, s, agentscan.NewCredStore(), logging.New("error"), false)
	require.Equal(t, 1, calls, "first run always scans (no prior scan recorded)")

	agentscan.RunAll(ctx, s, agentscan.NewCredStore(), logging.New("error"), false)
	require.Equal(t, 1, calls, "unchanged config must be skipped")

	// Bump the config mtime into the future so it is strictly newer than the
	// last scan timestamp, forcing a rescan.
	future := time.Now().Add(2 * time.Second)
	require.NoError(t, os.Chtimes(cfgPath, future, future))

	agentscan.RunAll(ctx, s, agentscan.NewCredStore(), logging.New("error"), false)
	require.Equal(t, 2, calls, "changed config must be rescanned")
}

func TestStartPeriodicRescan_ZeroIntervalReturnsImmediately(t *testing.T) {
	// Build the store OUTSIDE the timed goroutine so only the interval<=0
	// early-return is measured. Store setup in the timed path flaked the 200ms
	// budget on slow/cold CI runners — same root cause/fix as the freeproviders
	// sibling test.
	store := newRunnerStore(t)
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(
			context.Background(), store, agentscan.NewCredStore(), logging.New("error"),
			0, func() {})
		close(done)
	}()
	select {
	case <-done:
		// good — function returned without entering the loop
	case <-time.After(2 * time.Second):
		t.Fatal("StartPeriodicRescan with interval=0 did not return immediately")
	}
}

func TestStartPeriodicRescan_NilStoreReturnsImmediately(t *testing.T) {
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(
			context.Background(), nil, agentscan.NewCredStore(), logging.New("error"),
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
	require.NoError(t, s.UpsertAppSetting(context.Background(), storage.AppSetting{
		AppID: "openclaw", Enabled: true, ConfigPath: "/x",
	}))

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(ctx, s, agentscan.NewCredStore(), logging.New("error"), 20*time.Millisecond, nil)
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

func (s callCountingScanner) AppID() string             { return s.id }
func (s callCountingScanner) DisplayName() string       { return s.id }
func (s callCountingScanner) DefaultConfigPath() string { return "/d" }
func (s callCountingScanner) Scan(_ context.Context, _ string) ([]agentscan.InheritedEndpoint, error) {
	if s.incr != nil {
		s.incr()
	}
	return nil, nil
}

// ScanOne must split scan output at the persistence boundary (D-003): API
// keys and OAuth tokens go to the in-memory CredStore only; the SQLite rows
// carry no credential — extras_json is persisted with oauth_token stripped.
func TestScanOne_CredentialsNeverReachStorage(t *testing.T) {
	s := newRunnerStore(t)
	ctx := context.Background()
	creds := agentscan.NewCredStore()

	require.NoError(t, agentscan.ScanOne(ctx, s, creds, fakeScanner{
		id: "openclaw",
		results: []agentscan.InheritedEndpoint{
			{Provider: "deepseek", EndpointURL: "u1", APIKey: "sk-secret"},
			{
				Provider:    "minimax-portal",
				EndpointURL: "u2",
				ExtrasJSON:  `{"oauth_token":"sk-cp-secret","purpose":"subscription_oauth"}`,
			},
		},
	}, "/x"))

	// Memory has both credentials.
	assert.Equal(t, "sk-secret", creds.KeyFor("deepseek"))
	assert.Equal(t, "sk-cp-secret", creds.OAuthTokenFor("minimax-portal"))

	// Storage has neither: no APIKey field exists, and extras_json must not
	// contain the token (other extras fields survive).
	eps, err := s.ListInheritedEndpointsByApp(ctx, "openclaw")
	require.NoError(t, err)
	require.Len(t, eps, 2)
	for _, ep := range eps {
		assert.NotContains(t, ep.ExtrasJSON, "sk-cp-secret",
			"oauth_token must be stripped before extras_json is persisted")
	}
	for _, ep := range eps {
		if ep.Provider == "minimax-portal" {
			assert.Contains(t, ep.ExtrasJSON, "subscription_oauth",
				"non-credential extras fields must survive sanitization")
		}
	}
}

func TestStartPeriodicRescan_ConsumesPendingFile(t *testing.T) {
	// The CLI installer (and a re-run GUI wizard) can write
	// pending-agents.json while the daemon is already running; the rescan
	// tick must import it so app registration doesn't wait for a daemon
	// restart. enabled:false keeps the test free of scanner wiring — the
	// app_settings row is the observable effect either way.
	_ = pinPendingDir(t)
	s := newPendingStore(t)
	require.NoError(t, agentscan.WritePending([]agentscan.PendingAgent{
		{AppID: "openclaw", Enabled: false, ConfigPath: "/x"},
	}))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() {
		agentscan.StartPeriodicRescan(ctx, s, agentscan.NewCredStore(), logging.New("error"), 10*time.Millisecond, nil)
		close(done)
	}()

	deadline := time.After(2 * time.Second)
	for {
		row, err := s.GetAppSetting(context.Background(), "openclaw")
		if err == nil && row != nil {
			assert.False(t, row.Enabled)
			assert.Equal(t, "/x", row.ConfigPath)
			break
		}
		select {
		case <-deadline:
			t.Fatal("pending-agents.json was not imported by the rescan tick")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-done
}
