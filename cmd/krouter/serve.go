package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/kinthaiofficial/krouter/data"
	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/freeproviders"
	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/notifications"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/providers"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	minimaxadapter "github.com/kinthaiofficial/krouter/internal/providers/minimax"
	openaiadapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/proxycfg"
	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/kinthaiofficial/krouter/internal/subpricing"
	"github.com/kinthaiofficial/krouter/internal/upgrade"
	"github.com/spf13/cobra"
)

// newServeCommand returns the "serve" subcommand.
// Runs the proxy daemon in the foreground; the OS service manager handles
// backgrounding (see DECISIONS.md D-012).
func newServeCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run daemon (foreground)",
		Long: `Start the krouter daemon. Typically invoked by LaunchAgent (macOS),
systemd --user (Linux), or Task Scheduler (Windows), not directly by users.

The daemon listens on two ports:
  Proxy port      127.0.0.1:8402  Agent-facing (no auth)
  Management port 127.0.0.1:8403  GUI/CLI-facing (Bearer auth)`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			logLevel, _ := cmd.Flags().GetString("log-level")
			proxyPort, _ := cmd.Flags().GetInt("proxy-port")
			mgmtPort, _ := cmd.Flags().GetInt("management-port")

			// If the proxy port is already in use, wait for it to be freed before
			// proceeding. During reinstall the old binary may still be shutting
			// down when launchd/systemd starts the new one; waiting here means
			// the new binary picks up the port as soon as the old one releases it
			// instead of hitting launchd's 10 s ThrottleInterval restart delay.
			// If the port is still busy after 10 s a permanent instance is running
			// and we exit silently to avoid token-file clobbering.
			if conn, err := net.DialTimeout("tcp",
				fmt.Sprintf("127.0.0.1:%d", proxyPort), 200*time.Millisecond); err == nil {
				conn.Close()
				if !waitPortFree(fmt.Sprintf("127.0.0.1:%d", proxyPort), 10*time.Second, 100*time.Millisecond) {
					return nil
				}
			}

			logger := logging.New(logLevel)
			logger.Info("starting krouter",
				"version", Version,
				"build_time", BuildTime,
				"proxy_port", proxyPort,
				"mgmt_port", mgmtPort,
			)

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			// Open SQLite store.
			dbPath, err := defaultDBPath()
			if err != nil {
				return fmt.Errorf("resolve db path: %w", err)
			}
			store, err := storage.Open(dbPath)
			if err != nil {
				return fmt.Errorf("open storage: %w", err)
			}
			defer func() { _ = store.Close() }()
			if err := store.Migrate(); err != nil {
				return fmt.Errorf("storage migration: %w", err)
			}
			logger.Info("storage ready", "path", dbPath)

			// Settings manager — must be created before the provider registry so
			// providers can read keys from settings at request time.
			configPath, _ := cmd.Flags().GetString("config")
			settings := config.New(configPath)

			// Proxy-aware transport — auto-detects OS system proxy (macOS scutil,
			// Windows registry, Linux gsettings) and bypasses domestic China hosts.
			proxymgr := proxycfg.New()
			transport := &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				Proxy:               proxymgr.ProxyFunc(),
			}
			sharedClient := &http.Client{Transport: transport}

			// Provider registry — loaded from provider_config DB table plus two
			// hardcoded entries that require special protocol handling.
			reg := providers.New()
			reg.Register(anthropicadapter.New("https://api.anthropic.com", sharedClient))
			reg.Register(minimaxadapter.New(sharedClient)) // transparent proxy — auth header comes from the agent (OpenClaw OAuth)
			loadProvidersFromDB(ctx, store, reg, sharedClient)

			// Routing engine.
			engine := routing.New(reg)
			engine.WithHealth(store)

			// Pricing service — must be created before engine.WithPricing.
			pricingSvc := pricing.New(store)
			engine.WithPricing(pricingSvc)

			// Subscription quota source — wraps store for the routing engine.
			engine.WithSubscription(newSubscriptionSource(store))

			// Anthropic quota source — budget-based downgrade logic.
			quotaSrc := newQuotaSource(store, settings)
			engine.WithQuota(quotaSrc)

			// Per-agent routing overrides from settings.
			engine.WithOverrides(settings)

			// Note: there used to be a `FreeProviderSource` wiring here that
			// intersected `inherited_endpoints` with the curated
			// `data/free_tokens.json` catalog and routed to "free credit"
			// providers first. That bias was wrong — the catalog is only
			// for dashboard discovery (and can never be exhaustive). The
			// routing engine now picks cheapest by per-token cost from the
			// `token_price_api` table; a model priced at $0 wins
			// automatically, no catalog filter required.

			// Proxy server.
			proxySrv := proxy.New(
				proxy.WithLogger(logger),
				proxy.WithEngine(engine),
				proxy.WithRegistry(reg),
				proxy.WithStore(store),
				proxy.WithPricing(pricingSvc),
			)

			// Proxy refresh — re-detects OS proxy every 60s (handles VPN/network changes).
			go proxymgr.RefreshLoop(ctx, transport, 60*time.Second)

			// Proxy-aware HTTP client for background services (announcements, upgrade,
			// pricing sync). Uses the same proxy detection as the provider transport.
			bgTransport := &http.Transport{
				Proxy:           proxymgr.ProxyFunc(),
				IdleConnTimeout: 90 * time.Second,
			}
			pricingSvc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: bgTransport})

			// LiteLLM is the source for per-token pricing only. The routable
			// model list comes from live /v1/models discovery (see
			// ApplyDiscoveredModelsToRegistry / RefreshModelsIfStale below).
			pricingSvc.StartSync(ctx, 24*time.Hour)

			// Subscription pricing remote sync (spec/05 §11.4). Primary URL
			// is krouter.kinthai.ai (gives us fleet access-log stats); falls
			// back to GitHub raw on primary error.
			subPricingSvc := subpricing.New(store, logger).
				WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: bgTransport}).
				WithVersion(Version)
			go subPricingSvc.StartSync(ctx, 24*time.Hour)

			// Free-credit provider catalog (spec/06). Seed from the
			// embedded data/free_tokens.json on first launch (idempotent
			// upsert — safe to run every startup), then sync from
			// krouter.kinthai.ai every 24 h so policy edits land in
			// daemons within a day rather than waiting for a binary
			// release.
			freeProvidersSvc := freeproviders.New(store, logger).
				WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: bgTransport}).
				WithVersion(Version)
			if n, err := freeProvidersSvc.ApplyEmbedded(ctx, data.FreeTokensSeedJSON); err != nil {
				logger.Warn("free providers: embed seed failed", "err", err)
			} else {
				logger.Info("free providers: seeded from embed", "rows", n)
			}
			go freeProvidersSvc.StartSync(ctx, 24*time.Hour)

			// MiniMax subscription quota poller. OAuth token is resolved with
			// inherited_endpoints.extras_json as the preferred source (populated
			// by agentscan from the user's OpenClaw auth-profiles.json), with
			// the in-memory request-header cache as a fallback for users on
			// older daemon configurations or who haven't enabled OpenClaw yet.
			minimaxPoller := minimaxadapter.NewQuotaPoller(store, &http.Client{
				Timeout: 15 * time.Second, Transport: bgTransport,
			}).WithTokenResolver(func(ctx context.Context) string {
				if t := readMinimaxOAuthFromInheritedEndpoints(ctx, store); t != "" {
					return t
				}
				return minimaxadapter.GetCachedToken()
			})
			go minimaxPoller.Start(ctx)

			// Notifications service — polls CDN feed every 6h.
			notifSvc := notifications.New(store, settings, reg, Version)
			notifSvc.WithHTTPClient(&http.Client{Timeout: 15 * time.Second, Transport: bgTransport})
			go func() {
				if err := notifSvc.Start(ctx); err != nil {
					logger.Warn("notifications service stopped", "err", err)
				}
			}()

			// Upgrade service — checks for new versions every 24h.
			upgradeSvc, err := upgrade.New(Version)
			if err != nil {
				logger.Warn("upgrade service init failed", "err", err)
			} else {
				upgradeSvc.WithHTTPClient(&http.Client{Timeout: 30 * time.Second, Transport: bgTransport})
				go upgradeSvc.Start(ctx, 24*time.Hour)
			}

			// Remote-access service.
			remoteSvc := remote.New(store)

			// Management API server.
			apiSrv := api.New(store, Version, proxyPort, mgmtPort)
			apiSrv.SetBuildTime(BuildTime)
			apiSrv.SetPricing(pricingSvc)
			if upgradeSvc != nil {
				apiSrv.SetUpgrade(upgradeSvc)
			}
			apiSrv.SetRemote(remoteSvc)
			apiSrv.SetRegistry(reg)
			apiSrv.SetSettings(settings)
			apiSrv.SetProxyManager(proxymgr)
			apiSrv.SetMinimaxPoller(minimaxPoller)
			// Wire the quota-exhaustion SSE event (spec/05 §12.3). Done
			// after apiSrv exists so the closure can capture it; the
			// poller's first cycle is ~20s after Start, plenty of time
			// for apiSrv to be fully constructed.
			minimaxPoller.WithExhaustCallback(func(provider, tier string, highspeed bool, windowEnd time.Time) {
				apiSrv.Broadcast("subscription_exhausted", map[string]any{
					"provider":   provider,
					"tier":       tier,
					"highspeed":  highspeed,
					"window_end": windowEnd.UTC().Format(time.RFC3339),
				})
			})
			// Auto-rescan + dashboard notice when MiniMax rejects our OAuth
			// token (spec/05 §15.2). The rescan re-reads OpenClaw's
			// auth-profiles.json so a token OpenClaw silently refreshed in
			// the background gets picked up; the SSE event lets the user
			// know to re-login if the rescan doesn't help.
			minimaxPoller.WithUnauthorizedCallback(func() {
				agentscan.RunAll(ctx, store, logger)
				apiSrv.Broadcast("subscription_unauthorized", map[string]any{
					"provider": "minimax",
				})
			})
			// Broadcast `subscription_pricing_updated` when the remote sync
			// successfully writes new rows. Dashboard refetches /internal/
			// subscription/status so users see fresh prices without a
			// manual refresh. The sync loop is already running by this
			// point; installing the callback now means the very first
			// real update fires the broadcast.
			subPricingSvc.WithUpdateCallback(func(count int) {
				apiSrv.Broadcast("subscription_pricing_updated", map[string]any{
					"count": count,
				})
			})
			apiSrv.SetSSEDebug(proxySrv.GetLastSSECapture)

			// Agent inheritance — refresh inherited_endpoints from each enabled
			// AI agent's config file. Runs early so model discovery and the
			// MiniMax quota poller can rely on freshly-extracted API keys and
			// OAuth tokens.
			//
			// Before RunAll, ImportPending picks up wizard selections (see
			// spec/04 §4) the installer wrote to pending-agents.json.
			go func() {
				timer := time.NewTimer(2 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
					agentscan.ImportPending(ctx, store, logger)
					agentscan.RunAll(ctx, store, logger)
				case <-ctx.Done():
				}
			}()

			// Periodic rescan — picks up config changes the user made to
			// their agent files between daemon restarts (spec/04 §14
			// "Hot reload"). 5-minute cadence balances latency against
			// the cost of re-reading small config files; SSE broadcast
			// lets the dashboard react before its own refetchInterval
			// fires.
			go agentscan.StartPeriodicRescan(ctx, store, logger, 5*time.Minute, func() {
				apiSrv.Broadcast("agents_changed", map[string]any{
					"source": "periodic_rescan",
				})
			})

			// Lazy model discovery from live traffic: when a request flows for a
			// model, learn that model's provider's full /v1/models list using the
			// request's own key. Covers agents whose key krouter can't read from
			// config (Cursor keychain, Claude Code env). Deduped + stale-guarded.
			proxySrv.SetModelObserver(func(requestedModel, key string) {
				provider := pricingSvc.ProviderForModel(requestedModel)
				if provider == "" {
					return
				}
				apiSrv.DiscoverIfStale(context.Background(), provider, key)
			})

			// Load cached /v1/models results into the registry now so the
			// routing engine has accurate model availability immediately, before
			// any fresh discovery runs.
			apiSrv.ApplyDiscoveredModelsToRegistry(ctx)

			// Model discovery — re-syncs stale cached model lists on daemon start.
			go func() {
				timer := time.NewTimer(10 * time.Second)
				defer timer.Stop()
				select {
				case <-timer.C:
					apiSrv.RefreshModelsIfStale(ctx)
				case <-ctx.Done():
				}
			}()

			// Broadcast completed requests as SSE events so the Web UI updates live.
			// Payload mirrors /internal/logs row shape so the Router page can render
			// the same Request/Response diff card immediately on each event without
			// a follow-up fetch — includes per-model rates and the baseline cost
			// projection so the savings banner renders without UI-side computation.
			proxySrv.SetOnComplete(func(rec storage.RequestRecord) {
				payload := map[string]any{
					"id":              rec.ID,
					"ts":              rec.Timestamp.UTC().Format(time.RFC3339),
					"agent":           rec.Agent,
					"protocol":        rec.Protocol,
					"requested_model": rec.RequestedModel,
					"provider":        rec.Provider,
					"model":           rec.Model,
					"input_tokens":    rec.InputTokens,
					"output_tokens":   rec.OutputTokens,
					"cached_tokens":   rec.CachedTokens,
					"cost_micro_usd":  rec.CostMicroUSD,
					"cost_usd":        float64(rec.CostMicroUSD) / 1_000_000,
					"latency_ms":      rec.LatencyMS,
					"status_code":     rec.StatusCode,
					"error_message":   rec.ErrorMessage,
				}
				if pricingSvc != nil {
					payload["requested_provider"] = pricingSvc.ProviderFor(rec.RequestedModel)
					inMT, outMT := pricingSvc.PriceFor(rec.RequestedModel)
					payload["requested_input_per_mtok"] = inMT
					payload["requested_output_per_mtok"] = outMT
					inMT, outMT = pricingSvc.PriceFor(rec.Model)
					payload["routed_input_per_mtok"] = inMT
					payload["routed_output_per_mtok"] = outMT
					payload["baseline_cost_usd"] = float64(pricingSvc.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens)) / 1_000_000
				}
				apiSrv.Broadcast("request_completed", payload)
			})

			// Budget monitor — fires SSE events when spend crosses 80%/95%/100%
			// of the daily (or weekly) limit so the dashboard can warn the user.
			go monitorBudget(ctx, quotaSrc, func(event string, data any) {
				apiSrv.Broadcast(event, data)
			})

			// Start management API. When remote access is toggled, the API
			// restarts to switch between plain HTTP (127.0.0.1) and TLS (0.0.0.0).
			go func() {
				runManagementAPI(ctx, apiSrv, remoteSvc, mgmtPort, logger)
			}()

			logger.Info("proxy listening", "host", "127.0.0.1", "port", proxyPort)

			// Proxy blocks; returns on ctx cancellation or fatal error.
			_ = proxySrv.Serve(ctx, "127.0.0.1", proxyPort)

			logger.Info("daemon stopped")
			return nil
		},
	}

	cmd.Flags().Int("proxy-port", 8402, "Proxy port (always bound to 127.0.0.1)")
	cmd.Flags().Int("management-port", 8403, "Management API port")
	cmd.Flags().String("log-level", "info", "Log level: debug/info/warn/error")
	cmd.Flags().String("config", "", "Config file path (default: ~/.kinthai/settings.json)")

	return cmd
}

// runManagementAPI starts the management API and hot-swaps between plain HTTP
// and TLS based on the remote-access service state. When remote is enabled the
// API restarts on 0.0.0.0 with self-signed TLS; on disable it restarts on 127.0.0.1.
func runManagementAPI(ctx context.Context, apiSrv *api.Server, remoteSvc *remote.Service, mgmtPort int, logger logging.Logger) {
	for {
		st := remoteSvc.GetStatus()
		enabledCh := remoteSvc.EnabledCh()

		serveCtx, cancel := context.WithCancel(ctx)

		if st.Enabled {
			hostname, _ := os.Hostname()
			lanIPs := localLANIPs()
			certPEM, keyPEM, err := remote.LoadOrGenerateCert(hostname, lanIPs)
			if err != nil {
				logger.Warn("remote: cannot load TLS cert, falling back to plain HTTP", "err", err)
				logger.Info("management API listening (plain)", "host", "127.0.0.1", "port", mgmtPort)
				go func() { _ = apiSrv.Serve(serveCtx, "127.0.0.1", mgmtPort) }()
			} else {
				logger.Info("management API listening (TLS)", "host", "0.0.0.0", "port", mgmtPort)
				go func() { _ = apiSrv.ServeWithTLS(serveCtx, "0.0.0.0", mgmtPort, certPEM, keyPEM) }()
			}
		} else {
			logger.Info("management API listening (plain)", "host", "127.0.0.1", "port", mgmtPort)
			go func() { _ = apiSrv.Serve(serveCtx, "127.0.0.1", mgmtPort) }()
		}

		select {
		case <-ctx.Done():
			cancel()
			return
		case <-enabledCh:
			// Remote enabled state changed — cancel current server and restart.
			cancel()
			// Brief pause to let the port be released.
			time.Sleep(100 * time.Millisecond)
		}
	}
}

// localLANIPs returns non-loopback IPv4 addresses for TLS SANs.
func localLANIPs() []net.IP {
	var ips []net.IP
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			var ip net.IP
			switch v := addr.(type) {
			case *net.IPNet:
				ip = v.IP
			case *net.IPAddr:
				ip = v.IP
			}
			if ip4 := ip.To4(); ip4 != nil {
				ips = append(ips, ip4)
			}
		}
	}
	return ips
}

// waitPortFree polls addr every interval until it is no longer connectable or
// timeout elapses. Returns true if the port became free, false on timeout.
func waitPortFree(addr string, timeout, interval time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
		if err != nil {
			return true
		}
		conn.Close()
		time.Sleep(interval)
	}
	return false
}

// quotaSource implements routing.QuotaSource by reading quota_state from DB
// and comparing against budget limits in settings.
type quotaSource struct {
	store    *storage.Store
	settings *config.Manager
}

func newQuotaSource(store *storage.Store, settings *config.Manager) *quotaSource {
	return &quotaSource{store: store, settings: settings}
}

func (q *quotaSource) CurrentQuota(ctx context.Context) routing.QuotaState {
	s := q.settings.Get()

	dailyLimitUSD := s.BudgetWarnings["daily"]
	weeklyLimitUSD := s.BudgetWarnings["weekly"]
	const opusTokenSoftCap = 500_000

	var state routing.QuotaState

	if dailyLimitUSD > 0 {
		todayStart := time.Now().UTC().Truncate(24 * time.Hour)
		if cost, err := q.store.SumCostMicroUSD(ctx, todayStart); err == nil {
			state.DailyPercent = float64(cost) / 1_000_000 / dailyLimitUSD
		}
	}
	if weeklyLimitUSD > 0 {
		weekStart := weekStartUTC()
		if cost, err := q.store.SumCostMicroUSD(ctx, weekStart); err == nil {
			state.WeeklyPercent = float64(cost) / 1_000_000 / weeklyLimitUSD
		}
	}
	if qw, err := q.store.GetQuota(ctx, "opus"); err == nil && qw != nil {
		state.OpusPercent = float64(qw.TokensUsed) / opusTokenSoftCap
	}
	return state
}

// monitorBudget fires SSE events when daily/weekly spend crosses 80%, 95%, or
// 100% of the configured limit, and persists each transition to
// budget_events so the new Budget page can render the timeline even
// after the SSE consumer disconnected. Runs every 60 seconds; tracks
// the previously fired threshold level to avoid repeated notifications
// within the same day.
func monitorBudget(ctx context.Context, qs *quotaSource, broadcast func(string, any)) {
	var lastThreshold float64
	ticker := time.NewTicker(60 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			state := qs.CurrentQuota(ctx)
			maxPct := state.DailyPercent
			if state.WeeklyPercent > maxPct {
				maxPct = state.WeeklyPercent
			}
			newThreshold := budgetThresholdLevel(maxPct)
			if newThreshold > lastThreshold {
				broadcast("budget_warning", map[string]any{
					"daily_percent":  state.DailyPercent,
					"weekly_percent": state.WeeklyPercent,
					"threshold":      newThreshold,
					"blocked":        newThreshold >= 1.0,
				})
				qs.recordBudgetEvent(ctx, eventTypeForThreshold(newThreshold), state.DailyPercent)
			}
			// Reset when a new day/week brings usage back below the first threshold,
			// so we re-notify if spending climbs again. We also record an
			// "unblocked" event so the timeline shows the recovery edge.
			if newThreshold < lastThreshold && maxPct < 0.80 {
				if lastThreshold >= 1.0 {
					qs.recordBudgetEvent(ctx, storage.BudgetEventUnblocked, state.DailyPercent)
				}
				newThreshold = 0
			}
			lastThreshold = newThreshold
		}
	}
}

// eventTypeForThreshold maps the threshold tier to the storage event-type
// string used by budget_events rows. Keeps the broadcast / DB vocab in
// one place.
func eventTypeForThreshold(threshold float64) string {
	switch {
	case threshold >= 1.0:
		return storage.BudgetEventBlocked
	case threshold >= 0.95:
		return storage.BudgetEventWarning95
	case threshold >= 0.80:
		return storage.BudgetEventWarning80
	default:
		return "" // shouldn't happen — monitor only calls this on rising edges
	}
}

// recordBudgetEvent persists one budget_events row. Best-effort: a DB
// error here would not be actionable from the goroutine, so we log
// nothing and continue — the SSE broadcast already reached live clients.
func (q *quotaSource) recordBudgetEvent(ctx context.Context, eventType string, dailyPercent float64) {
	if eventType == "" {
		return
	}
	s := q.settings.Get()
	dailyLimit := s.BudgetWarnings["daily"]
	// Derive cost from percent × limit (state.DailyPercent already
	// reflects current spend / limit). When limit is 0 we know percent
	// is 0 too, so cost stays 0 and the event row remains coherent.
	dailyCost := dailyPercent * dailyLimit
	_ = q.store.InsertBudgetEvent(ctx, storage.BudgetEvent{
		Timestamp:     time.Now().UTC(),
		EventType:     eventType,
		DailyPercent:  dailyPercent,
		DailyCostUSD:  dailyCost,
		DailyLimitUSD: dailyLimit,
	})
}

// budgetThresholdLevel maps a spend percentage to the highest crossed tier
// (0 = none, 0.80, 0.95, 1.0).
func budgetThresholdLevel(pct float64) float64 {
	switch {
	case pct >= 1.0:
		return 1.0
	case pct >= 0.95:
		return 0.95
	case pct >= 0.80:
		return 0.80
	default:
		return 0
	}
}

// weekStartUTC returns the start of the current ISO week (Monday 00:00 UTC).
func weekStartUTC() time.Time {
	now := time.Now().UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	return now.Truncate(24*time.Hour).AddDate(0, 0, -(weekday - 1))
}

// subscriptionSource implements routing.SubscriptionSource via the storage layer.
type subscriptionSource struct {
	store *storage.Store
}

func newSubscriptionSource(store *storage.Store) *subscriptionSource {
	return &subscriptionSource{store: store}
}

// GetSubscriptionInfo answers routing's "does this provider have quota I can
// route an anthropic-protocol text request to?" question. The fix here over
// the pre-A3 implementation: we look up the tier whose ModelPattern actually
// covers the model we'd rewrite the request to ("MiniMax-M2.7" or its
// highspeed variant), rather than returning whichever tier happened to come
// out of GetAllSubscriptionQuotas first. The old behaviour could mask
// MiniMax-M* exhaustion when speech-hd or another auxiliary tier still had
// leftover quota — routing would think minimax was available, send a
// MiniMax-M2.7 request, and the upstream would 4xx because the M* tier was
// actually empty.
//
// We prefer the standard (non-highspeed) tier when the user has bought both,
// since the highspeed plans cost ~2× as much per call. Fall back to highspeed
// when only that tier has remaining quota.
//
// Spec/05 §8 + §9. Phase 1 still hardcodes the rewrite target to MiniMax-M2.7
// (the only minimax LLM family krouter routes to today); per-feature quota
// matching for coding-plan-search etc. is Phase 3.
func (s *subscriptionSource) GetSubscriptionInfo(ctx context.Context, provider string) routing.SubscriptionInfo {
	if provider != "minimax" {
		// No other vendor has a subscription model wired up yet.
		return routing.SubscriptionInfo{}
	}
	quotas, err := s.store.GetAllSubscriptionQuotas(ctx)
	if err != nil {
		return routing.SubscriptionInfo{}
	}

	// Try standard then highspeed. Each iteration picks the tier whose
	// ModelPattern wildcard-matches the target model id we'd rewrite to.
	for _, highspeed := range []bool{false, true} {
		targetModel := "MiniMax-M2.7"
		if highspeed {
			targetModel = "MiniMax-M2.7-highspeed"
		}
		for i := range quotas {
			q := &quotas[i]
			if q.Provider != provider {
				continue
			}
			if q.Highspeed != highspeed {
				continue
			}
			if !q.MatchesModel(targetModel) {
				continue
			}
			if !q.IsAvailable() {
				continue
			}
			price := q.PricingFor(ctx, s.store)
			return routing.SubscriptionInfo{
				Available:        true,
				Model:            targetModel,
				Remaining:        q.TotalCount - q.UsedCount,
				Total:            q.TotalCount,
				EffectiveCostUSD: price.EffectiveCostPerCallUSD(),
			}
		}
	}
	return routing.SubscriptionInfo{}
}

// loadProvidersFromDB reads provider_config rows and registers an OpenAI adapter for
// each openai-protocol entry. Anthropic and MiniMax are skipped — they are always
// registered separately with custom protocol logic above.
func loadProvidersFromDB(ctx context.Context, store *storage.Store, reg *providers.Registry, sharedClient *http.Client) {
	cfgs, err := store.GetProviderConfigs(ctx)
	if err != nil {
		return
	}
	for _, cfg := range cfgs {
		cfg := cfg // capture loop variable for closure
		if cfg.Protocol != "openai" {
			continue // anthropic-protocol providers are registered separately
		}
		name := cfg.Name
		keyFn := func() string {
			return resolveProviderKeyForRouting(store, name)
		}
		reg.Register(openaiadapter.NewWithPathReplaceAndKeyFn(name, cfg.BaseURL, cfg.PathPrefix, keyFn, nil, sharedClient))
	}
}

// defaultDBPath returns the default SQLite database path (~/.kinthai/data.db).
func defaultDBPath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return "", err
	}
	return filepath.Join(dir, "data.db"), nil
}
