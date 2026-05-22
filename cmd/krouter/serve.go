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

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/api"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/notifications"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/proxy"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/proxycfg"
	anthropicadapter "github.com/kinthaiofficial/krouter/internal/providers/anthropic"
	minimaxadapter "github.com/kinthaiofficial/krouter/internal/providers/minimax"
	openaiadapter "github.com/kinthaiofficial/krouter/internal/providers/openai"
	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
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
			engine.WithQuota(newQuotaSource(store, settings))

			// Per-agent routing overrides from settings.
			engine.WithOverrides(settings)

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

			// After each LiteLLM sync, update provider adapter model lists from catalog.
			pricingSvc.OnSync(func(catalog map[string][]string) {
				for litellmProvider, models := range catalog {
					adapterName := litellmProvider
					if mapped, ok := pricing.LiteLLMToKrouterProvider[litellmProvider]; ok {
						adapterName = mapped
					}
					if p, ok := reg.Get(adapterName); ok {
						if ms, ok := p.(providers.ModelSetter); ok {
							ms.SetModels(models)
						}
					}
				}
			})
			pricingSvc.StartSync(ctx, 24*time.Hour)

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

			// Model discovery — re-syncs cached model lists on daemon start.
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
			proxySrv.SetOnComplete(func(rec storage.RequestRecord) {
				apiSrv.Broadcast("request_completed", map[string]any{
					"id":              rec.ID,
					"ts":              rec.Timestamp.UTC().Format(time.RFC3339),
					"agent":           rec.Agent,
					"protocol":        rec.Protocol,
					"requested_model": rec.RequestedModel,
					"provider":        rec.Provider,
					"model":           rec.Model,
					"input_tokens":    rec.InputTokens,
					"output_tokens":   rec.OutputTokens,
					"cost_micro_usd":  rec.CostMicroUSD,
					"cost_usd":        float64(rec.CostMicroUSD) / 1_000_000,
					"latency_ms":      rec.LatencyMS,
					"status_code":     rec.StatusCode,
					"error_message":   rec.ErrorMessage,
				})
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

// weekStartUTC returns the start of the current ISO week (Monday 00:00 UTC).
func weekStartUTC() time.Time {
	now := time.Now().UTC()
	weekday := int(now.Weekday())
	if weekday == 0 {
		weekday = 7 // Sunday → 7
	}
	return now.Truncate(24 * time.Hour).AddDate(0, 0, -(weekday - 1))
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
