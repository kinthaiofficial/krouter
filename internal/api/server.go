// Package api implements the management HTTP API (/internal/*).
//
// Listens on management port (default 127.0.0.1:8403). Requires Bearer auth
// using the token written to ~/.kinthai/internal-token on daemon startup.
//
// M1.3 endpoints:
//
//	GET  /internal/status
//	GET  /internal/logs?n=50
//
// M1.4 endpoints:
//
//	GET  /internal/preset
//	POST /internal/preset
//	GET  /internal/usage
//
// M2.1 endpoints:
//
//	GET  /internal/announcements
//	POST /internal/announcements/read     body: {"id":"..."}
//	POST /internal/announcements/dismiss  body: {"id":"..."}
//	GET  /internal/announcements/count
//
// Web UI endpoints:
//
//	GET  /internal/events          → SSE stream (Bearer or same-origin browser)
//	GET  /internal/settings        → read all settings
//	PATCH /internal/settings       → update settings fields
//	GET  /internal/budget          → today's cost + savings breakdown
//	GET  /internal/models          → all discovered model IDs grouped by provider
//	POST /internal/models/refresh  → trigger model re-discovery for all configured providers
//
// See spec/01-proxy-layer.md §4 for the full endpoint list.
package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/kinthaiofficial/krouter/internal/agentscan"
	"github.com/kinthaiofficial/krouter/internal/config"
	"github.com/kinthaiofficial/krouter/internal/notify"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/proxycfg"
	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/kinthaiofficial/krouter/internal/upgrade"
)

const defaultPreset = "balanced"

var validPresets = map[string]bool{
	"saver":       true,
	"balanced":    true,
	"quality":     true,
	"passthrough": true,
	"":            true, // empty = reset to type-based default
}

// sseEvent is a single Server-Sent Event.
type sseEvent struct {
	Type string
	Data any
}

// Server is the management API server.
type Server struct {
	token         string
	store         *storage.Store
	pricing       *pricing.Service
	upgrade       *upgrade.Service
	remote        *remote.Service
	registry      *providers.Registry
	settings      *config.Manager
	proxyMgr      interface{ Status() proxycfg.ProxyStatus }
	minimaxPoller subscriptionPoller
	startAt       time.Time
	version       string
	buildTime     string
	ports         struct{ proxy, mgmt int }

	// SSE broadcast.
	subsMu   sync.Mutex
	subs     []chan sseEvent
	notifier *notify.Notifier

	// sseDebugFn, when set, returns the raw bytes of the last Anthropic SSE
	// capture for the /internal/debug/last-sse-capture diagnostic endpoint.
	sseDebugFn func() []byte

	// discoveryInflight dedups concurrent lazy model-discovery calls, keyed by
	// provider name, so a burst of requests triggers at most one /v1/models call.
	discoveryInflight sync.Map
}

// New creates a management API server.
// store may be nil (returns defaults for all store-backed endpoints).
func New(store *storage.Store, version string, proxyPort, mgmtPort int) *Server {
	return &Server{
		store:   store,
		startAt: time.Now(),
		version: version,
		ports:   struct{ proxy, mgmt int }{proxyPort, mgmtPort},
	}
}

// SetBuildTime stores the build timestamp for inclusion in /internal/status.
func (s *Server) SetBuildTime(t string) { s.buildTime = t }

// SetSettings wires in the settings manager for GET/PATCH /internal/settings.
func (s *Server) SetSettings(m *config.Manager) { s.settings = m }

// SetNotifier wires in the desktop notification handler.
func (s *Server) SetNotifier(n *notify.Notifier) { s.notifier = n }

// Broadcast sends an SSE event to all connected browser clients and fires a
// desktop notification when applicable.
func (s *Server) Broadcast(eventType string, data any) {
	ev := sseEvent{Type: eventType, Data: data}
	s.subsMu.Lock()
	for _, ch := range s.subs {
		select {
		case ch <- ev:
		default: // slow client: drop
		}
	}
	s.subsMu.Unlock()

	if s.notifier != nil {
		s.notifier.HandleEvent(eventType, data)
	}
}

// SetPricing wires in the pricing service for cost/savings computation.
func (s *Server) SetPricing(p *pricing.Service) { s.pricing = p }

// SetUpgrade wires in the upgrade service for update status.
func (s *Server) SetUpgrade(u *upgrade.Service) { s.upgrade = u }

// SetRemote wires in the remote-access service.
func (s *Server) SetRemote(r *remote.Service) { s.remote = r }

// SetRegistry wires in the provider registry for the /internal/providers endpoint.
func (s *Server) SetRegistry(r *providers.Registry) { s.registry = r }

// SetProxyManager wires in the proxy manager so /internal/status includes proxy info.
func (s *Server) SetProxyManager(pm interface{ Status() proxycfg.ProxyStatus }) {
	s.proxyMgr = pm
}

// SetMinimaxPoller wires in the MiniMax quota poller so the user can trigger
// an immediate refresh from the dashboard via POST /internal/subscription/refresh.
func (s *Server) SetMinimaxPoller(p subscriptionPoller) {
	s.minimaxPoller = p
}

// subscriptionPoller is the minimal interface the API layer needs from
// internal/providers/minimax.QuotaPoller. Defined here to keep the api
// package's tests free of network dependencies.
type subscriptionPoller interface {
	PollOnce(ctx context.Context) error
}

// SetSSEDebug wires in a function that returns the last captured Anthropic SSE
// buffer for the /internal/debug/last-sse-capture diagnostic endpoint.
func (s *Server) SetSSEDebug(fn func() []byte) { s.sseDebugFn = fn }

// Token returns the internal auth token (available after Serve is called).
func (s *Server) Token() string { return s.token }

// SetTokenForTest pre-sets a token so tests can skip the Serve startup step.
// Do not call outside of tests.
func (s *Server) SetTokenForTest(token string) { s.token = token }

// Serve starts the management API on host:port.
// On startup it generates a random token and writes it to ~/.kinthai/internal-token.
func (s *Server) Serve(ctx context.Context, host string, port int) error {
	token, err := generateToken()
	if err != nil {
		return fmt.Errorf("api: generate token: %w", err)
	}
	s.token = token

	if err := writeInternalToken(token); err != nil {
		return fmt.Errorf("api: write internal-token: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("api server: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// ServeWithTLS starts the management API on host:port using TLS.
// certPEM and keyPEM are PEM-encoded certificate and private key.
func (s *Server) ServeWithTLS(ctx context.Context, host string, port int, certPEM, keyPEM []byte) error {
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return fmt.Errorf("api: load TLS cert: %w", err)
	}

	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:    addr,
		Handler: s.Handler(),
		TLSConfig: &tls.Config{
			Certificates: []tls.Certificate{cert},
			MinVersion:   tls.VersionTLS12,
		},
		ReadHeaderTimeout: 10 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		// Empty certFile/keyFile — cert is already in TLSConfig.
		if err := srv.ListenAndServeTLS("", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("api TLS server: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		return nil
	case err := <-errCh:
		return err
	}
}

// Handler returns the authenticated mux (used in tests without Serve).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Health check — no auth required; used by the installer to detect daemon readiness.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Static Web UI.
	mountUI(mux)

	// All /internal/* endpoints: Bearer OR same-origin browser (CSRF guard built-in).
	auth := s.authMiddleware
	mux.Handle("/internal/status", auth(http.HandlerFunc(s.handleStatus)))
	mux.Handle("/internal/logs", auth(http.HandlerFunc(s.handleLogs)))
	mux.Handle("/internal/preset", auth(http.HandlerFunc(s.handlePreset)))
	mux.Handle("/internal/usage", auth(http.HandlerFunc(s.handleUsage)))
	mux.Handle("/internal/settings", auth(http.HandlerFunc(s.handleSettings)))
	mux.Handle("/internal/budget", auth(http.HandlerFunc(s.handleBudget)))
	mux.Handle("/internal/budget/events", auth(http.HandlerFunc(s.handleBudgetEvents)))
	mux.Handle("/internal/events", auth(http.HandlerFunc(s.handleEvents)))
	mux.Handle("/internal/announcements/read", auth(http.HandlerFunc(s.handleAnnouncementRead)))
	mux.Handle("/internal/announcements/dismiss", auth(http.HandlerFunc(s.handleAnnouncementDismiss)))
	mux.Handle("/internal/announcements/count", auth(http.HandlerFunc(s.handleAnnouncementsCount)))
	mux.Handle("/internal/announcements", auth(http.HandlerFunc(s.handleAnnouncements)))
	mux.Handle("/internal/free-providers", auth(http.HandlerFunc(s.handleFreeProviders)))
	mux.Handle("/internal/update-status", auth(http.HandlerFunc(s.handleUpdateStatus)))
	mux.Handle("/internal/update-check", auth(http.HandlerFunc(s.handleUpdateCheck)))
	mux.Handle("/internal/remote/enable", auth(http.HandlerFunc(s.handleRemoteEnable)))
	mux.Handle("/internal/remote/disable", auth(http.HandlerFunc(s.handleRemoteDisable)))
	mux.Handle("/internal/remote/status", auth(http.HandlerFunc(s.handleRemoteStatus)))
	mux.Handle("/internal/pairing/exchange", auth(http.HandlerFunc(s.handlePairingExchange)))
	mux.Handle("/internal/devices", auth(http.HandlerFunc(s.handleDevices)))
	mux.Handle("/internal/devices/", auth(http.HandlerFunc(s.handleDeviceDelete)))
	mux.Handle("/internal/providers", auth(http.HandlerFunc(s.handleProviders)))
	mux.Handle("/internal/providers/", auth(http.HandlerFunc(s.handleProviderAction)))
	mux.Handle("/internal/apps", auth(http.HandlerFunc(s.handleApps)))
	// Exact-path routes override the catch-all prefix below.
	mux.Handle("/internal/apps/supported", auth(http.HandlerFunc(s.handleAppsSupported)))
	mux.Handle("/internal/apps/configured", auth(http.HandlerFunc(s.handleAppsConfigured)))
	mux.Handle("/internal/apps/", auth(http.HandlerFunc(s.handleAppAction)))
	mux.Handle("/internal/subscription/status", auth(http.HandlerFunc(s.handleSubscriptionStatus)))
	mux.Handle("/internal/subscription/refresh", auth(http.HandlerFunc(s.handleSubscriptionRefresh)))
	mux.Handle("/internal/quota", auth(http.HandlerFunc(s.handleQuota)))
	mux.Handle("/internal/update-apply", auth(http.HandlerFunc(s.handleUpdateApply)))
	mux.Handle("/internal/models/refresh", auth(http.HandlerFunc(s.handleModelsRefresh)))
	mux.Handle("/internal/models", auth(http.HandlerFunc(s.handleModels)))
	mux.Handle("/internal/pricing/status", auth(http.HandlerFunc(s.handlePricingStatus)))
	mux.Handle("/internal/dashboard/stats", auth(http.HandlerFunc(s.handleDashboardStats)))
	mux.Handle("/internal/logs/export", auth(http.HandlerFunc(s.handleLogsExport)))
	mux.Handle("/internal/settings/reset-data", auth(http.HandlerFunc(s.handleResetData)))
	mux.Handle("/internal/settings/uninstall", auth(http.HandlerFunc(s.handleUninstall)))
	mux.Handle("/internal/debug/last-sse-capture", auth(http.HandlerFunc(s.handleDebugSSECapture)))
	return mux
}

// handleSettings handles GET and PATCH /internal/settings.
func (s *Server) handleSettings(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		mgr := s.settings
		if mgr == nil {
			writeJSON(w, config.Settings{Preset: "balanced", Language: "en"})
			return
		}
		writeJSON(w, mgr.Get())

	case http.MethodPatch:
		mgr := s.settings
		if mgr == nil {
			http.Error(w, `{"error":"settings unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		current := mgr.Get()
		var patch struct {
			Preset                 *string            `json:"preset"`
			Language               *string            `json:"language"`
			NotificationCategories map[string]bool    `json:"notification_categories"`
			BudgetWarnings         map[string]float64 `json:"budget_warnings"`
		}
		if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
			http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
			return
		}
		if patch.Preset != nil {
			if !validPresets[*patch.Preset] {
				http.Error(w, `{"error":"preset must be one of: saver, balanced, quality, passthrough"}`, http.StatusBadRequest)
				return
			}
			current.Preset = *patch.Preset
		}
		if patch.Language != nil {
			current.Language = *patch.Language
		}
		if patch.NotificationCategories != nil {
			if current.NotificationCategories == nil {
				current.NotificationCategories = make(map[string]bool)
			}
			for k, v := range patch.NotificationCategories {
				current.NotificationCategories[k] = v
			}
		}
		if patch.BudgetWarnings != nil {
			if current.BudgetWarnings == nil {
				current.BudgetWarnings = make(map[string]float64)
			}
			for k, v := range patch.BudgetWarnings {
				current.BudgetWarnings[k] = v
			}
		}
		if err := mgr.Set(current); err != nil {
			http.Error(w, `{"error":"failed to save settings"}`, http.StatusInternalServerError)
			return
		}
		s.Broadcast("settings_changed", current)
		writeJSON(w, current)

	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

// handleBudget handles GET /internal/budget.
// Returns today's cost and savings breakdown.
func (s *Server) handleBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	todayStart := time.Now().UTC().Truncate(24 * time.Hour)
	var totalCost, totalSavings int64
	var requestsToday int

	if s.store != nil {
		if n, err := s.store.CountRequestsToday(r.Context()); err == nil {
			requestsToday = n
		}
		if c, err := s.store.SumCostMicroUSD(r.Context(), todayStart); err == nil {
			totalCost = c
		}
		if s.pricing != nil {
			if recs, err := s.store.ListRequests(r.Context(), 10000); err == nil {
				for _, rec := range recs {
					if rec.Timestamp.UTC().Before(todayStart) {
						continue
					}
					if rec.CostMicroUSD <= 0 {
						continue
					}
					baseline := s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)
					if saved := baseline - rec.CostMicroUSD; saved > 0 {
						totalSavings += saved
					}
				}
			}
		}
	}

	costTodayUSD := float64(totalCost) / 1_000_000

	resp := map[string]any{
		"date":              todayStart.Format("2006-01-02"),
		"requests_today":    requestsToday,
		"cost_today_usd":    costTodayUSD,
		"savings_today_usd": float64(totalSavings) / 1_000_000,
	}

	if s.settings != nil {
		cfg := s.settings.Get()
		if daily := cfg.BudgetWarnings["daily"]; daily > 0 {
			pct := costTodayUSD / daily
			resp["daily_limit_usd"] = daily
			resp["daily_percent_used"] = pct
			resp["budget_blocked"] = pct >= 1.0
		}
	}

	writeJSON(w, resp)
}

// handleBudgetEvents handles GET /internal/budget/events?limit=N.
// Returns recent budget threshold transitions (warning_80, warning_95,
// blocked, unblocked) newest first, so the Budget page can render a
// "what happened today" timeline. Capped at 500 server-side.
func (s *Server) handleBudgetEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []any{})
		return
	}

	limit := 50
	if v := r.URL.Query().Get("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	events, err := s.store.ListBudgetEvents(r.Context(), limit)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type row struct {
		ID            int64   `json:"id"`
		Timestamp     string  `json:"ts"`
		EventType     string  `json:"event_type"`
		DailyPercent  float64 `json:"daily_percent"`
		DailyCostUSD  float64 `json:"daily_cost_usd"`
		DailyLimitUSD float64 `json:"daily_limit_usd"`
	}
	out := make([]row, 0, len(events))
	for _, e := range events {
		out = append(out, row{
			ID:            e.ID,
			Timestamp:     e.Timestamp.Format(time.RFC3339),
			EventType:     e.EventType,
			DailyPercent:  e.DailyPercent,
			DailyCostUSD:  e.DailyCostUSD,
			DailyLimitUSD: e.DailyLimitUSD,
		})
	}
	writeJSON(w, out)
}

// handleEvents handles GET /internal/events (Server-Sent Events).
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")

	ch := make(chan sseEvent, 16)
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()

	defer func() {
		s.subsMu.Lock()
		for i, sub := range s.subs {
			if sub == ch {
				s.subs = append(s.subs[:i], s.subs[i+1:]...)
				break
			}
		}
		s.subsMu.Unlock()
	}()

	// Send a heartbeat immediately so the client knows the connection is live.
	_, _ = fmt.Fprintf(w, ": ping\n\n")
	flusher.Flush()

	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": ping\n\n")
			flusher.Flush()
		case ev := <-ch:
			data, err := json.Marshal(ev.Data)
			if err != nil {
				continue
			}
			_, _ = fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, data)
			flusher.Flush()
		}
	}
}

// handleStatus handles GET /internal/status.
func (s *Server) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	uptime := int64(time.Since(s.startAt).Seconds())
	resp := map[string]any{
		"status":         "ok",
		"version":        s.version,
		"uptime_seconds": uptime,
		"pid":            os.Getpid(),
		"proxy_port":     s.ports.proxy,
		"mgmt_port":      s.ports.mgmt,
	}
	if s.buildTime != "" {
		resp["build_time"] = s.buildTime
	}
	if s.proxyMgr != nil {
		resp["proxy"] = s.proxyMgr.Status()
	}
	writeJSON(w, resp)
}

// handleLogs handles GET /internal/logs?n=50.
func (s *Server) handleLogs(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	n := 50
	if v := r.URL.Query().Get("n"); v != "" {
		if parsed, err := strconv.Atoi(v); err == nil && parsed > 0 {
			n = parsed
		}
	}
	appFilter := r.URL.Query().Get("app")
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")

	if s.store == nil {
		writeJSON(w, []any{})
		return
	}

	var records []storage.RequestRecord
	var err error
	if fromStr != "" && toStr != "" {
		from, ferr := time.Parse("2006-01-02", fromStr)
		to, terr := time.Parse("2006-01-02", toStr)
		if ferr == nil && terr == nil {
			to = to.Add(24*time.Hour - time.Second) // include all of the 'to' day
			records, err = s.store.ListRequestsInRange(r.Context(), from, to, appFilter, n)
		} else {
			http.Error(w, `{"error":"invalid date format, use YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
	} else if appFilter != "" {
		records, err = s.store.ListRequestsByApp(r.Context(), appFilter, n)
	} else {
		records, err = s.store.ListRequests(r.Context(), n)
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type row struct {
		ID             string  `json:"id"`
		Timestamp      string  `json:"ts"`
		App            string  `json:"app,omitempty"`
		Protocol       string  `json:"protocol"`
		RequestedModel string  `json:"requested_model,omitempty"`
		Provider       string  `json:"provider"`
		Model          string  `json:"model"`
		InputTokens      int     `json:"input_tokens"`
		OutputTokens     int     `json:"output_tokens"`
		CachedTokens     int     `json:"cached_tokens"`
		CacheWriteTokens int     `json:"cache_write_tokens"`
		CostMicroUSD     int64   `json:"cost_micro_usd"`
		CostUSD        float64 `json:"cost_usd"`
		LatencyMS      int64   `json:"latency_ms"`
		StatusCode     int     `json:"status_code"`
		ErrorMessage   string  `json:"error_message,omitempty"`

		// Routing-decision enrichment for the Router dashboard card.
		// All optional / zero-valued for legacy daemons or unknown
		// models — the UI falls back to "—" cleanly.
		RequestedProvider          string  `json:"requested_provider,omitempty"`
		RequestedInputPerMTok      float64 `json:"requested_input_per_mtok,omitempty"`
		RequestedOutputPerMTok     float64 `json:"requested_output_per_mtok,omitempty"`
		RequestedCacheReadPerMTok  float64 `json:"requested_cache_read_per_mtok,omitempty"`
		RoutedInputPerMTok         float64 `json:"routed_input_per_mtok,omitempty"`
		RoutedOutputPerMTok        float64 `json:"routed_output_per_mtok,omitempty"`
		RoutedCacheReadPerMTok     float64 `json:"routed_cache_read_per_mtok,omitempty"`
		// BaselineCostUSD = (requested model's rate) × (actual tokens used).
		// What the user would have paid if krouter hadn't picked a
		// cheaper provider/model. UI computes savings = baseline - actual.
		BaselineCostUSD float64 `json:"baseline_cost_usd,omitempty"`
		RoutingPreset   string  `json:"routing_preset,omitempty"`
	}

	out := make([]row, 0, len(records))
	for _, rec := range records {
		r := row{
			ID:             rec.ID,
			Timestamp:      rec.Timestamp.Format(time.RFC3339),
			App:          rec.App,
			Protocol:       rec.Protocol,
			RequestedModel: rec.RequestedModel,
			Provider:       rec.Provider,
			Model:          rec.Model,
			InputTokens:      rec.InputTokens,
			OutputTokens:     rec.OutputTokens,
			CachedTokens:     rec.CachedTokens,
			CacheWriteTokens: rec.CacheWriteTokens,
			CostMicroUSD:     rec.CostMicroUSD,
			CostUSD:        float64(rec.CostMicroUSD) / 1_000_000,
			LatencyMS:      rec.LatencyMS,
			StatusCode:     rec.StatusCode,
			ErrorMessage:   rec.ErrorMessage,
			RoutingPreset:  rec.RoutingPreset,
		}
		if s.pricing != nil {
			r.RequestedProvider = s.pricing.ProviderFor(rec.RequestedModel)
			r.RequestedInputPerMTok, r.RequestedOutputPerMTok = s.pricing.PriceFor(rec.RequestedModel)
			r.RequestedCacheReadPerMTok = s.pricing.CacheReadPerMTok(rec.RequestedModel)
			r.RoutedInputPerMTok, r.RoutedOutputPerMTok = s.pricing.PriceFor(rec.Model)
			r.RoutedCacheReadPerMTok = s.pricing.CacheReadPerMTok(rec.Model)
			r.BaselineCostUSD = float64(s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)) / 1_000_000
		}
		out = append(out, r)
	}
	writeJSON(w, out)
}

// handlePreset handles GET and POST /internal/preset.
func (s *Server) handlePreset(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case http.MethodGet:
		s.getPreset(w, r)
	case http.MethodPost:
		s.setPreset(w, r)
	default:
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	}
}

func (s *Server) getPreset(w http.ResponseWriter, r *http.Request) {
	preset := defaultPreset
	if s.store != nil {
		if v, ok, err := s.store.GetSetting(r.Context(), "preset"); err == nil && ok {
			preset = v
		}
	}
	writeJSON(w, map[string]string{"preset": preset})
}

func (s *Server) setPreset(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Preset string `json:"preset"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, `{"error":"invalid JSON"}`, http.StatusBadRequest)
		return
	}
	if !validPresets[body.Preset] {
		http.Error(w, `{"error":"preset must be one of: saver, balanced, quality, passthrough"}`, http.StatusBadRequest)
		return
	}
	if s.store != nil {
		if err := s.store.SetSetting(r.Context(), "preset", body.Preset); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]string{"preset": body.Preset})
}

// handleUsage handles GET /internal/usage.
func (s *Server) handleUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	requestsToday := 0
	var costTodayMicroUSD int64
	var savingsTodayMicroUSD int64

	if s.store != nil {
		if n, err := s.store.CountRequestsToday(r.Context()); err == nil {
			requestsToday = n
		}
		todayStart := time.Now().UTC().Truncate(24 * time.Hour)
		if total, err := s.store.SumCostMicroUSD(r.Context(), todayStart); err == nil {
			costTodayMicroUSD = total
		}
		// Compute savings if pricing service is available.
		if s.pricing != nil {
			if recs, err := s.store.ListRequests(r.Context(), 10000); err == nil {
				todayStr := todayStart.Format(time.RFC3339)
				for _, rec := range recs {
					if rec.Timestamp.UTC().Format(time.RFC3339) < todayStr {
						continue
					}
					if rec.CostMicroUSD <= 0 {
						continue
					}
					baseline := s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)
					saved := baseline - rec.CostMicroUSD
					if saved > 0 {
						savingsTodayMicroUSD += saved
					}
				}
			}
		}
	}

	writeJSON(w, map[string]any{
		"requests_today":    requestsToday,
		"cost_today_usd":    float64(costTodayMicroUSD) / 1_000_000,
		"savings_today_usd": float64(savingsTodayMicroUSD) / 1_000_000,
	})
}

// handleAnnouncements handles GET /internal/announcements.
// Returns up to 50 announcements (unread first, dismissed excluded).
func (s *Server) handleAnnouncements(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []any{})
		return
	}
	recs, err := s.store.ListAnnouncements(r.Context(), 50)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type annRow struct {
		ID          string            `json:"id"`
		Type        string            `json:"type"`
		Priority    string            `json:"priority"`
		PublishedAt string            `json:"published_at"`
		ExpiresAt   *string           `json:"expires_at"`
		Title       map[string]string `json:"title"`
		Summary     map[string]string `json:"summary"`
		URL         string            `json:"url"`
		Icon        string            `json:"icon"`
		ReadAt      *string           `json:"read_at"`
		DismissedAt *string           `json:"dismissed_at"`
	}

	out := make([]annRow, 0, len(recs))
	for _, rec := range recs {
		row := annRow{
			ID:          rec.ID,
			Type:        rec.Type,
			Priority:    rec.Priority,
			PublishedAt: rec.PublishedAt.UTC().Format(time.RFC3339),
			URL:         rec.URL,
			Icon:        rec.Icon,
		}
		if rec.ExpiresAt != nil {
			s := rec.ExpiresAt.UTC().Format(time.RFC3339)
			row.ExpiresAt = &s
		}
		if rec.ReadAt != nil {
			s := rec.ReadAt.UTC().Format(time.RFC3339)
			row.ReadAt = &s
		}
		if rec.DismissedAt != nil {
			s := rec.DismissedAt.UTC().Format(time.RFC3339)
			row.DismissedAt = &s
		}
		// Parse title/summary JSON into maps so the client can pick the right language.
		var title, summary map[string]string
		if err := json.Unmarshal([]byte(rec.TitleJSON), &title); err != nil {
			title = map[string]string{"en": rec.TitleJSON}
		}
		if err := json.Unmarshal([]byte(rec.SummaryJSON), &summary); err != nil {
			summary = map[string]string{"en": rec.SummaryJSON}
		}
		row.Title = title
		row.Summary = summary
		out = append(out, row)
	}
	writeJSON(w, out)
}

// handleAnnouncementRead handles POST /internal/announcements/read.
func (s *Server) handleAnnouncementRead(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if s.store != nil {
		if err := s.store.MarkAnnouncementRead(r.Context(), body.ID); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAnnouncementDismiss handles POST /internal/announcements/dismiss.
func (s *Server) handleAnnouncementDismiss(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var body struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.ID == "" {
		http.Error(w, `{"error":"id required"}`, http.StatusBadRequest)
		return
	}
	if s.store != nil {
		if err := s.store.MarkAnnouncementDismissed(r.Context(), body.ID); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleAnnouncementsCount handles GET /internal/announcements/count.
func (s *Server) handleAnnouncementsCount(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	unread := 0
	if s.store != nil {
		if n, err := s.store.CountUnreadAnnouncements(r.Context()); err == nil {
			unread = n
		}
	}
	writeJSON(w, map[string]int{"unread": unread})
}

// handleRemoteEnable handles POST /internal/remote/enable.
func (s *Server) handleRemoteEnable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.remote == nil {
		http.Error(w, `{"error":"remote service unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	// Determine the local IP to embed in the token.
	localIP := localNonLoopbackIP()
	token, err := s.remote.Enable(r.Context(), localIP, uint16(s.ports.mgmt))
	if err != nil {
		http.Error(w, `{"error":"failed to enable remote access"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]string{"token": token})
}

// handleRemoteDisable handles POST /internal/remote/disable.
func (s *Server) handleRemoteDisable(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.remote != nil {
		s.remote.Disable()
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRemoteStatus handles GET /internal/remote/status.
func (s *Server) handleRemoteStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]any{"enabled": false, "token": nil, "expires_in": 0}
	if s.remote != nil {
		st := s.remote.GetStatus()
		resp["enabled"] = st.Enabled
		if st.Enabled {
			resp["token"] = st.Token
			remaining := time.Until(st.PairingExp).Seconds()
			if remaining < 0 {
				remaining = 0
			}
			resp["expires_in"] = int(remaining)
		}
	}
	writeJSON(w, resp)
}

// handlePairingExchange handles POST /internal/pairing/exchange.
func (s *Server) handlePairingExchange(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.remote == nil {
		http.Error(w, `{"error":"remote service unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var body struct {
		Code       string `json:"code"`
		DeviceName string `json:"device_name"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Code == "" {
		http.Error(w, `{"error":"code required"}`, http.StatusBadRequest)
		return
	}

	ipAddr, _, _ := strings.Cut(r.RemoteAddr, ":")
	ua := r.Header.Get("User-Agent")

	deviceToken, err := s.remote.ExchangePairingCode(r.Context(), body.Code, body.DeviceName, ipAddr, ua)
	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusUnauthorized)
		return
	}
	writeJSON(w, map[string]string{"token": deviceToken})
}

// handleDevices handles GET /internal/devices.
func (s *Server) handleDevices(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.remote == nil {
		writeJSON(w, []any{})
		return
	}

	devices, err := s.remote.ListDevices(r.Context())
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type row struct {
		DeviceID   string  `json:"id"`
		DeviceName string  `json:"name"`
		IPAddress  string  `json:"ip_address"`
		PairedAt   string  `json:"paired_at"`
		LastSeenAt *string `json:"last_seen_at"`
	}
	out := make([]row, 0, len(devices))
	for _, d := range devices {
		r := row{
			DeviceID:   d.DeviceID,
			DeviceName: d.DeviceName,
			IPAddress:  d.IPAddress,
			PairedAt:   d.PairedAt.UTC().Format(time.RFC3339),
		}
		if d.LastSeenAt != nil {
			s := d.LastSeenAt.UTC().Format(time.RFC3339)
			r.LastSeenAt = &s
		}
		out = append(out, r)
	}
	writeJSON(w, out)
}

// handleDeviceDelete handles DELETE /internal/devices/{id}.
func (s *Server) handleDeviceDelete(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodDelete {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	deviceID := strings.TrimPrefix(r.URL.Path, "/internal/devices/")
	if deviceID == "" {
		http.Error(w, `{"error":"device id required"}`, http.StatusBadRequest)
		return
	}
	if s.remote != nil {
		if err := s.remote.RevokeDevice(r.Context(), deviceID); err != nil {
			http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
			return
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// localNonLoopbackIP returns the first non-loopback IPv4 address, falling back to 127.0.0.1.
func localNonLoopbackIP() net.IP {
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
				return ip4
			}
		}
	}
	return net.ParseIP("127.0.0.1")
}

// handleUpdateStatus handles GET /internal/update-status.
func (s *Server) handleUpdateStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	resp := map[string]any{
		"current": s.version,
		"latest":  nil,
	}

	if s.upgrade != nil {
		if m := s.upgrade.Latest(); m != nil {
			resp["latest"] = m.Version
			resp["is_critical"] = m.IsCritical
			resp["release_notes_url"] = m.ReleaseNotesURL
		}
	}

	writeJSON(w, resp)
}

// handleUpdateCheck handles POST /internal/update-check.
// Forces a fresh manifest fetch synchronously (off the normal 24 h
// schedule), then returns the same JSON shape as /internal/update-status.
// Used by the About page so the user can open the page and immediately
// see whether a new version is available, instead of waiting up to a
// full day for the periodic ticker.
func (s *Server) handleUpdateCheck(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.upgrade != nil {
		s.upgrade.CheckNow(r.Context())
	}
	// Reuse the read handler's shape so the frontend has one type to model.
	// handleUpdateStatus enforces GET, so flip the method on the clone.
	r2 := r.Clone(r.Context())
	r2.Method = http.MethodGet
	s.handleUpdateStatus(w, r2)
}

// handleUpdateApply handles POST /internal/update-apply.
// Starts the update in the background and returns 200 immediately.
// The daemon process will replace itself and exit; the GUI detects the disconnect.
func (s *Server) handleUpdateApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.upgrade == nil {
		http.Error(w, `{"error":"update service not available"}`, http.StatusServiceUnavailable)
		return
	}
	if s.upgrade.Latest() == nil {
		http.Error(w, `{"error":"no update available"}`, http.StatusConflict)
		return
	}
	// Apply runs in background. On success, broadcast an SSE event so the
	// dashboard shows "Restarting…", then exec the new binary in place of
	// the current process (Unix) or spawn-and-exit (Windows).
	go func() {
		if err := s.upgrade.Apply(context.Background(), nil); err != nil {
			slog.Error("upgrade: apply failed", "err", err)
			s.Broadcast("update_apply_failed", map[string]string{"error": err.Error()})
			return
		}
		s.Broadcast("update_restarting", map[string]string{})
		// Give the SSE event a moment to flush to connected clients before
		// the process image is replaced.
		time.Sleep(300 * time.Millisecond)
		if err := upgrade.Restart(); err != nil {
			slog.Error("upgrade: restart failed", "err", err)
		}
	}()
	writeJSON(w, map[string]string{"status": "applying"})
}

// providerInfoJSON is the JSON shape returned by GET /internal/providers.
type providerInfoJSON struct {
	Name                string  `json:"name"`
	DisplayName         string  `json:"display_name"`
	Protocol            string  `json:"protocol"`
	BaseURL             string  `json:"base_url"`
	PathPrefix          string  `json:"path_prefix,omitempty"`
	IsBuiltin           bool    `json:"is_builtin"`
	Available           bool    `json:"available"`
	Configured          bool    `json:"configured"`
	ConsecutiveFailures int     `json:"consecutive_failures"`
	SuccessRate         float64 `json:"success_rate"`
	LastErrorCode       int     `json:"last_error_code,omitempty"`
	RequestsToday       int     `json:"requests_today"`
	CostTodayUSD        float64 `json:"cost_today_usd"`
	LatencyP50MS        int64   `json:"latency_p50_ms"`
	LatencyP95MS        int64   `json:"latency_p95_ms"`

	// Lifetime totals — across all rows in the requests table for this
	// provider. Used by the Providers dashboard page to show "this
	// provider has handled X requests for $Y" without the user having
	// to filter the Logs page.
	RequestsTotal     int     `json:"requests_total"`
	InputTokensTotal  int64   `json:"input_tokens_total"`
	OutputTokensTotal int64   `json:"output_tokens_total"`
	CachedTokensTotal     int64   `json:"cached_tokens_total"`
	CacheWriteTokensTotal int64   `json:"cache_write_tokens_total"`
	CostTotalUSD          float64 `json:"cost_total_usd"`

	// Catalog meta — model count from model_catalog, so the page can
	// surface "12 models priced" without an extra request.
	ModelCount int `json:"model_count"`
}

// handleProviders handles GET /internal/providers.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	s.doListProviders(w, r)
}

// doListProviders returns all registered providers enriched with DB metadata.
func (s *Server) doListProviders(w http.ResponseWriter, r *http.Request) {
	// Build display-name / base-url index from DB.
	dbMeta := make(map[string]storage.ProviderConfig)
	if s.store != nil {
		if cfgs, err := s.store.GetProviderConfigs(r.Context()); err == nil {
			for _, c := range cfgs {
				dbMeta[c.Name] = c
			}
		}
	}
	// Model count per provider from token_price_api (~2k rows total,
	// grouped by provider so the response is one tiny map regardless
	// of how many providers are registered).
	modelCounts := make(map[string]int)
	if s.store != nil {
		if mc, err := s.store.CountPricesByProvider(r.Context()); err == nil {
			modelCounts = mc
		}
	}

	var out []providerInfoJSON
	if s.registry != nil {
		for _, p := range s.registry.All() {
			configured := true
			if c, ok := p.(providers.Configurable); ok {
				configured = c.HasKey()
			}
			info := providerInfoJSON{
				Name:        p.Name(),
				DisplayName: p.Name(),
				Protocol:    string(p.Protocol()),
				Available:   configured,
				Configured:  configured,
				SuccessRate: 1.0,
			}
			if meta, ok := dbMeta[p.Name()]; ok {
				info.DisplayName = meta.DisplayName
				info.BaseURL = meta.BaseURL
				info.PathPrefix = meta.PathPrefix
				info.IsBuiltin = meta.IsBuiltin
			}
			if s.store != nil {
				// Lifetime totals — show even for not-yet-configured providers
				// so the page tells the full story.
				if tot, err := s.store.ProviderTokenTotalsSince(r.Context(), p.Name(), time.Time{}); err == nil {
					info.RequestsTotal = tot.RequestCount
					info.InputTokensTotal = tot.InputTokens
					info.OutputTokensTotal = tot.OutputTokens
					info.CachedTokensTotal = tot.CachedTokens
					info.CacheWriteTokensTotal = tot.CacheWriteTokens
					info.CostTotalUSD = float64(tot.CostMicroUSD) / 1_000_000
				}
				info.ModelCount = modelCounts[p.Name()]
			}
			if configured && s.store != nil {
				if ps, err := s.store.GetProviderStatus(r.Context(), p.Name()); err == nil && ps != nil {
					info.ConsecutiveFailures = ps.ConsecutiveFailures
					info.SuccessRate = ps.RollingSuccessRate
					info.LastErrorCode = ps.LastErrorCode
				}
				todayStart := time.Now().UTC().Truncate(24 * time.Hour)
				if ps, err := s.store.ProviderStatSince(r.Context(), p.Name(), todayStart); err == nil {
					info.RequestsToday = ps.RequestCount
					info.CostTodayUSD = float64(ps.CostMicroUSD) / 1_000_000
					info.LatencyP50MS = ps.P50MS
					info.LatencyP95MS = ps.P95MS
				}
			}
			out = append(out, info)
		}
	}
	if out == nil {
		out = []providerInfoJSON{}
	}
	writeJSON(w, out)
}

type appStats struct {
	RequestsToday   int     `json:"requests_today"`
	CostTodayUSD    float64 `json:"cost_today_usd"`
	SavingsTodayUSD float64 `json:"savings_today_usd"`
}

type keyHintJSON struct {
	Hint         string `json:"hint"`
	Requests     int    `json:"requests"`
	CostMicroUSD int64  `json:"cost_micro_usd"`
}

type appWithStats struct {
	config.AppStatus
	Stats appStats `json:"stats"`

	// Inheritance fields (spec/04 §8.6). Optional — empty for legacy agents
	// that have no Scanner registered (Cursor / Hermes etc. still go
	// through v2.0.47 connect/disconnect for now). All omitempty so older
	// frontends ignoring these fields keep working.
	Supported      bool   `json:"supported,omitempty"`       // Scanner is compiled in
	Enabled        bool   `json:"enabled,omitempty"`         // agent_settings.enabled = 1
	InheritedCount int    `json:"inherited_count,omitempty"` // count of inherited_endpoints rows
	LastScannedAt  *int64 `json:"last_scanned_at,omitempty"` // ms UTC of most recent scan
	LastError      string `json:"last_error,omitempty"`      // last scanner error, if any

	// Key-hint channels: distinct api_key suffixes seen for this app, sorted
	// by request count. Empty until migration 019 data arrives.
	KeyHints []keyHintJSON `json:"key_hints,omitempty"`
}

// handleApps handles GET /internal/apps.
//
// Returns a unified view spanning two sources, keyed by agent name:
//
//  1. v2.0.47 filesystem detection (config.DetectAppStatuses) — finds AI
//     agents installed on disk, reports whether their baseUrl already
//     points at krouter, and which provider names appear in their config.
//     This list also drives the per-agent today's-stats numbers.
//
//  2. The spec/04 inheritance Scanner registry — every agent the daemon
//     knows how to inherit from, joined against agent_settings (enabled
//     flag + last scan state) and inherited_endpoints (vendor count).
//
// Agents that appear in either source show up in the output. The Scanner
// registry is the authoritative list of "known agents"; v2.0.47 detection
// adds runtime presence information when the agent files exist on disk.
func (s *Server) handleApps(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	ctx := r.Context()
	statuses := config.DetectAppStatuses()
	todayStart := time.Now().UTC().Truncate(24 * time.Hour)

	// Index v2.0.47 detection by name so we can join Scanner-derived data.
	detected := make(map[string]config.AppStatus, len(statuses))
	for _, st := range statuses {
		detected[st.Name] = st
	}

	// Index agent_settings rows (inheritance state) by agent_id.
	settingsByID := make(map[string]storage.AppSetting)
	if s.store != nil {
		if rows, err := s.store.ListAppSettings(ctx); err == nil {
			for _, row := range rows {
				settingsByID[row.AppID] = row
			}
		}
	}

	// Build the union: every Scanner ID + every detected-but-unscanned name.
	type key struct{ name string }
	seen := make(map[string]struct{})
	order := make([]string, 0)
	for _, sc := range agentscan.Scanners {
		if _, ok := seen[sc.AppID()]; !ok {
			seen[sc.AppID()] = struct{}{}
			order = append(order, sc.AppID())
		}
	}
	for _, st := range statuses {
		if _, ok := seen[st.Name]; !ok {
			seen[st.Name] = struct{}{}
			order = append(order, st.Name)
		}
	}

	out := make([]appWithStats, 0, len(order))
	for _, name := range order {
		row := appWithStats{}
		if st, ok := detected[name]; ok {
			row.AppStatus = st
		} else {
			// Scanner-registered agent not detected on disk: emit a stub
			// AppStatus so the JSON still has Name + default empty fields.
			row.AppStatus = config.AppStatus{
				AppInfo: config.AppInfo{Name: name},
			}
		}

		// Today's stats (v2.0.47 behaviour, only for names that have request rows).
		if s.store != nil {
			recs, err := s.store.ListRequestsByApp(ctx, name, 10000)
			if err == nil {
				for _, rec := range recs {
					if rec.Timestamp.UTC().Before(todayStart) {
						continue
					}
					row.Stats.RequestsToday++
					row.Stats.CostTodayUSD += float64(rec.CostMicroUSD) / 1_000_000
					if s.pricing != nil {
						baseline := s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)
						if saved := baseline - rec.CostMicroUSD; saved > 0 {
							row.Stats.SavingsTodayUSD += float64(saved) / 1_000_000
						}
					}
				}
			}
		}

		// Inheritance overlay (spec/04 §8.6).
		if agentscan.Get(name) != nil {
			row.Supported = true
		}
		if cfg, ok := settingsByID[name]; ok {
			row.Enabled = cfg.Enabled
			row.LastScannedAt = cfg.LastScannedAt
			row.LastError = cfg.LastError
			if s.store != nil {
				if eps, err := s.store.ListInheritedEndpointsByApp(ctx, name); err == nil {
					row.InheritedCount = len(eps)
				}
			}
		}

		if s.store != nil {
			if hints, err := s.store.ListKeyHintsByApp(ctx, name); err == nil && len(hints) > 0 {
				row.KeyHints = make([]keyHintJSON, len(hints))
				for i, h := range hints {
					row.KeyHints[i] = keyHintJSON{
						Hint:         h.KeyHint,
						Requests:     h.RequestCount,
						CostMicroUSD: h.CostMicroUSD,
					}
				}
			}
		}

		out = append(out, row)
	}

	writeJSON(w, out)
}

// handleAppAction handles requests to /internal/apps/{name}/{action} and
// /internal/agents/{name} (no trailing action, currently only DELETE).
func (s *Server) handleAppAction(w http.ResponseWriter, r *http.Request) {
	// Bare /internal/apps/{name} (e.g. DELETE) is handled by a helper that
	// inspects the path shape itself.
	if s.appRootDispatch(w, r) {
		return
	}

	tail := strings.TrimPrefix(r.URL.Path, "/internal/apps/")
	slash := strings.LastIndex(tail, "/")
	if slash < 0 {
		http.NotFound(w, r)
		return
	}
	name, action := tail[:slash], tail[slash+1:]

	switch action {
	case "connect":
		s.doAgentConnect(w, r, name)
	case "disconnect":
		s.doAgentDisconnect(w, r, name)
	case "diff":
		s.doAppDiff(w, r, name)
	case "backups":
		s.doAppBackups(w, r, name)
	case "restore":
		s.doAppRestore(w, r, name)
	case "agents":
		s.doAppAgents(w, r, name)
	default:
		// Inheritance verbs (rescan / enable / disable) are dispatched from a
		// neighbouring file so this switch stays focused on the v2.0.47 verbs.
		if s.inheritanceActionDispatch(w, r, name, action) {
			return
		}
		http.NotFound(w, r)
	}
}

func (s *Server) doAgentConnect(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := config.DetectInstalledApps()
	var found *config.AppInfo
	for i := range agents {
		if agents[i].Name == name {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}

	var err error
	switch name {
	case "openclaw":
		err = config.ConnectOpenClaw(found.ConfigPath)
		if err == nil {
			go s.discoverOpenClawModels(found.ConfigPath)
		}
	case "cursor":
		err = config.ConnectCursor(found.ConfigPath)
	case "hermes":
		err = config.ConnectHermes(found.ConfigPath)
	case "claude-code":
		err = config.ConnectClaudeCode(config.DetectShellRC())
	default:
		http.Error(w, `{"error":"agent not supported"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}

	// The agent must be restarted to pick up the rewritten config. We never do
	// this for the user (killing an editor or shell session would be hostile);
	// we just tell the UI to show a notice. restart_kind distinguishes apps
	// that read config at process start ("process") from those that read it
	// from the shell environment at login ("shell", e.g. Claude Code).
	restartKind := "process"
	if name == "claude-code" {
		restartKind = "shell"
	}
	writeJSON(w, map[string]any{
		"ok":            true,
		"needs_restart": true,
		"restart_kind":  restartKind,
		"app":         name,
	})
}

func (s *Server) doAgentDisconnect(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	agents := config.DetectInstalledApps()
	var found *config.AppInfo
	for i := range agents {
		if agents[i].Name == name {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}

	var err error
	switch name {
	case "openclaw":
		err = config.DisconnectOpenClaw(found.ConfigPath)
	case "cursor":
		err = config.DisconnectCursor(found.ConfigPath)
	case "hermes":
		err = config.DisconnectHermes(found.ConfigPath)
	case "claude-code":
		err = config.DisconnectClaudeCode(config.DetectShellRC())
	default:
		http.Error(w, `{"error":"agent not supported"}`, http.StatusBadRequest)
		return
	}

	if err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// doAppDiff handles POST /internal/apps/{name}/diff
// Returns the proposed config changes without applying them.
func (s *Server) doAppDiff(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agents := config.DetectInstalledApps()
	var found *config.AppInfo
	for i := range agents {
		if agents[i].Name == name {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}
	switch name {
	case "openclaw":
		before, after, err := config.PreviewOpenClawConnect(found.ConfigPath)
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, map[string]string{
			"before": string(before),
			"after":  string(after),
		})
	default:
		http.Error(w, `{"error":"diff not supported for this agent"}`, http.StatusBadRequest)
	}
}

// doAppBackups handles GET /internal/apps/{name}/backups
func (s *Server) doAppBackups(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agents := config.DetectInstalledApps()
	var found *config.AppInfo
	for i := range agents {
		if agents[i].Name == name {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}
	if found.ConfigPath == "" {
		writeJSON(w, []config.BackupInfo{})
		return
	}
	writeJSON(w, config.ListBackups(found.ConfigPath))
}

// doAppAgents handles GET /internal/apps/{name}/agents.
// Returns the per-sub-agent profile breakdown for agents that support
// it (currently only OpenClaw). For agents without a sub-agent concept
// the endpoint returns an empty list — UI then renders the existing
// single-card layout.
//
// This endpoint deliberately does NOT touch `inherited_endpoints` or
// the routing path; it's a read-only file-system scan that lets the
// dashboard surface "this OpenClaw install has 4 agents and here
// are their per-sub provider configs". Secrets stay on the daemon —
// the response includes `has_api_key` booleans, not the raw keys.
func (s *Server) doAppAgents(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	switch name {
	case "openclaw":
		home, err := os.UserHomeDir()
		if err != nil {
			http.Error(w, `{"error":"home dir unavailable"}`, http.StatusInternalServerError)
			return
		}
		subs, err := agentscan.ListOpenClawAgents(home + "/.openclaw")
		if err != nil {
			http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
			return
		}
		writeJSON(w, subs)
	default:
		// Agents without sub-agent support — empty list keeps the
		// frontend's "iterate the response" code simple.
		writeJSON(w, []any{})
	}
}

// doAppRestore handles POST /internal/apps/{name}/restore
func (s *Server) doAppRestore(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agents := config.DetectInstalledApps()
	var found *config.AppInfo
	for i := range agents {
		if agents[i].Name == name {
			found = &agents[i]
			break
		}
	}
	if found == nil {
		http.Error(w, `{"error":"app not found"}`, http.StatusNotFound)
		return
	}
	var body struct {
		Filename string `json:"filename"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Filename == "" {
		http.Error(w, `{"error":"filename required"}`, http.StatusBadRequest)
		return
	}
	// Security: ensure filename has no path separators.
	if strings.ContainsAny(body.Filename, "/\\") {
		http.Error(w, `{"error":"invalid filename"}`, http.StatusBadRequest)
		return
	}
	dir := filepath.Dir(found.ConfigPath)
	backupPath := filepath.Join(dir, body.Filename)
	if err := config.RestoreBackup(found.ConfigPath, backupPath); err != nil {
		http.Error(w, `{"error":"`+err.Error()+`"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleDashboardStats handles GET /internal/dashboard/stats.
// Returns 7-day aggregates, provider distribution, and connected agent count.
func (s *Server) handleDashboardStats(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type providerDist struct {
		Name     string  `json:"name"`
		Requests int     `json:"requests"`
		CostUSD  float64 `json:"cost_usd"`
	}
	type weeklyStats struct {
		Requests   int     `json:"requests"`
		CostUSD    float64 `json:"cost_usd"`
		SavingsUSD float64 `json:"savings_usd"`
	}
	type presetStat struct {
		Preset     string  `json:"preset"`
		Requests   int     `json:"requests"`
		SavingsUSD float64 `json:"savings_usd"`
		SavingsPct float64 `json:"savings_pct"`
	}
	type response struct {
		Weekly          weeklyStats    `json:"weekly"`
		Providers       []providerDist `json:"providers"`
		AppsConnected int            `json:"apps_connected"`
		PresetBreakdown []presetStat   `json:"preset_breakdown"`
	}

	var resp response
	resp.Providers = []providerDist{}

	presetOrder := []string{"saver", "balanced", "quality", "passthrough"}
	type presetAccum struct {
		req      int
		cost     float64
		baseline float64
	}
	byPreset := make(map[string]*presetAccum, len(presetOrder))
	for _, p := range presetOrder {
		byPreset[p] = &presetAccum{}
	}

	if s.store != nil {
		weekAgo := time.Now().UTC().Add(-7 * 24 * time.Hour)
		recs, err := s.store.ListRequestsSince(r.Context(), weekAgo, 50000)
		if err == nil {
			byProvider := make(map[string]*providerDist)
			for _, rec := range recs {
				resp.Weekly.Requests++
				cost := float64(rec.CostMicroUSD) / 1_000_000
				resp.Weekly.CostUSD += cost

				var baselineMicro int64
				if s.pricing != nil {
					baselineMicro = s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)
					if saved := baselineMicro - rec.CostMicroUSD; saved > 0 {
						resp.Weekly.SavingsUSD += float64(saved) / 1_000_000
					}
				}

				// Accumulate per-preset stats.
				ps := rec.RoutingPreset
				if ps == "" {
					ps = "balanced" // legacy records without preset default to balanced
				}
				if acc, ok := byPreset[ps]; ok {
					acc.req++
					acc.cost += cost
					acc.baseline += float64(baselineMicro) / 1_000_000
				}

				pd, ok := byProvider[rec.Provider]
				if !ok {
					pd = &providerDist{Name: rec.Provider}
					byProvider[rec.Provider] = pd
				}
				pd.Requests++
				pd.CostUSD += cost
			}
			for _, pd := range byProvider {
				resp.Providers = append(resp.Providers, *pd)
			}
			// Sort by request count descending.
			sort.Slice(resp.Providers, func(i, j int) bool {
				return resp.Providers[i].Requests > resp.Providers[j].Requests
			})
		}
	}

	// Build preset_breakdown in fixed order.
	for _, p := range presetOrder {
		acc := byPreset[p]
		saved := acc.baseline - acc.cost
		if saved < 0 {
			saved = 0
		}
		var pct float64
		if acc.baseline > 0 {
			pct = (saved / acc.baseline) * 100
		}
		resp.PresetBreakdown = append(resp.PresetBreakdown, presetStat{
			Preset:     p,
			Requests:   acc.req,
			SavingsUSD: saved,
			SavingsPct: pct,
		})
	}

	// Count connected agents.
	for _, a := range config.DetectAppStatuses() {
		if a.Connected {
			resp.AppsConnected++
		}
	}

	writeJSON(w, resp)
}

// handleLogsExport handles GET /internal/logs/export?from=YYYY-MM-DD&to=YYYY-MM-DD.
// Returns a CSV file attachment.
func (s *Server) handleLogsExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	fromStr := r.URL.Query().Get("from")
	toStr := r.URL.Query().Get("to")
	appFilter := r.URL.Query().Get("app")

	if s.store == nil {
		http.Error(w, `{"error":"storage unavailable"}`, http.StatusServiceUnavailable)
		return
	}

	var records []storage.RequestRecord
	var err error
	if fromStr != "" && toStr != "" {
		from, ferr := time.Parse("2006-01-02", fromStr)
		to, terr := time.Parse("2006-01-02", toStr)
		if ferr != nil || terr != nil {
			http.Error(w, `{"error":"invalid date format, use YYYY-MM-DD"}`, http.StatusBadRequest)
			return
		}
		to = to.Add(24*time.Hour - time.Second)
		records, err = s.store.ListRequestsInRange(r.Context(), from, to, appFilter, 100000)
	} else {
		records, err = s.store.ListRequestsSince(r.Context(), time.Now().UTC().Add(-30*24*time.Hour), 100000)
	}
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	fname := "krouter-logs-" + time.Now().UTC().Format("2006-01-02") + ".csv"
	w.Header().Set("Content-Type", "text/csv")
	w.Header().Set("Content-Disposition", `attachment; filename="`+fname+`"`)

	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"id", "ts", "app", "protocol", "requested_model", "provider", "model",
		"input_tokens", "output_tokens", "cost_usd", "latency_ms", "status_code"})
	for _, rec := range records {
		_ = cw.Write([]string{
			rec.ID,
			rec.Timestamp.UTC().Format(time.RFC3339),
			rec.App,
			rec.Protocol,
			rec.RequestedModel,
			rec.Provider,
			rec.Model,
			strconv.Itoa(rec.InputTokens),
			strconv.Itoa(rec.OutputTokens),
			fmt.Sprintf("%.6f", float64(rec.CostMicroUSD)/1_000_000),
			strconv.FormatInt(rec.LatencyMS, 10),
			strconv.Itoa(rec.StatusCode),
		})
	}
	cw.Flush()
}

// handleResetData handles POST /internal/settings/reset-data.
func (s *Server) handleResetData(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		http.Error(w, `{"error":"storage unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	if err := s.store.DeleteAllRequests(r.Context()); err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleUninstall handles POST /internal/settings/uninstall.
// Disconnects all connected agents and returns ok.
func (s *Server) handleUninstall(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	agents := config.DetectInstalledApps()
	rcPath := config.DetectShellRC()
	for _, a := range agents {
		switch a.Name {
		case "openclaw":
			_ = config.DisconnectOpenClaw(a.ConfigPath)
		case "cursor":
			_ = config.DisconnectCursor(a.ConfigPath)
		case "hermes":
			_ = config.DisconnectHermes(a.ConfigPath)
		case "claude-code":
			_ = config.DisconnectClaudeCode(rcPath)
		}
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleProviderAction handles /internal/providers/{name}/{action}.
// Supported actions: "test" (ping), "models" (model_catalog list).
func (s *Server) handleProviderAction(w http.ResponseWriter, r *http.Request) {
	tail := strings.TrimPrefix(r.URL.Path, "/internal/providers/")
	slash := strings.LastIndex(tail, "/")
	if slash < 0 {
		http.NotFound(w, r)
		return
	}
	name, action := tail[:slash], tail[slash+1:]
	if action == "models" {
		s.handleProviderModels(w, r, name)
		return
	}
	if action != "test" {
		http.NotFound(w, r)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.registry == nil {
		http.Error(w, `{"error":"registry unavailable"}`, http.StatusServiceUnavailable)
		return
	}
	p, ok := s.registry.Get(name)
	if !ok {
		http.Error(w, `{"error":"provider not found"}`, http.StatusNotFound)
		return
	}
	pinger, ok := p.(providers.Pinger)
	if !ok {
		http.Error(w, `{"error":"provider does not support ping"}`, http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()

	latency, code, err := pinger.Ping(ctx)
	if err != nil {
		writeJSON(w, map[string]any{
			"latency_ms":  latency,
			"status_code": 0,
			"ok":          false,
			"error":       err.Error(),
		})
		return
	}
	writeJSON(w, map[string]any{
		"latency_ms":  latency,
		"status_code": code,
		"ok":          code >= 200 && code < 500, // 401 = reachable for Anthropic
	})
}

// handleProviderModels handles GET /internal/providers/{name}/models.
// Returns the catalogued models for the given provider with their
// per-million-token pricing — used by the Providers dashboard page
// to render an inline price table when the user expands a card.
//
// Reads from token_price_api (the LiteLLM-synced pricing cache —
// ~2k rows refreshed daily). The earlier implementation read from
// `model_catalog`, which the daemon never populates from the normal
// pricing-sync flow, so every provider showed "No models catalogued
// yet" — see fix for the PR #24 regression.
//
// Filtering: token_price_api.provider stores the LiteLLM vendor
// string (e.g. "anthropic", "openai", "gemini"), which for the
// builtin providers matches provider_config.name 1:1.
func (s *Server) handleProviderModels(w http.ResponseWriter, r *http.Request, providerName string) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, []any{})
		return
	}
	prices, err := s.store.GetAllPrices(r.Context())
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type row struct {
		ModelID                string  `json:"model_id"`
		InputPerMTok           float64 `json:"input_per_mtok"`
		OutputPerMTok          float64 `json:"output_per_mtok"`
		CachedInputPerMTok     float64 `json:"cached_input_per_mtok"`
		CacheWritePerMTok      float64 `json:"cache_write_per_mtok,omitempty"`
		CacheWrite1hrPerMTok   float64 `json:"cache_write_1hr_per_mtok,omitempty"`
		MaxTokens              int     `json:"max_tokens"`
	}
	out := make([]row, 0)
	for _, e := range prices {
		if e.Provider != providerName {
			continue
		}
		out = append(out, row{
			ModelID:              e.ModelID,
			InputPerMTok:         e.InputCostPerToken * 1_000_000,
			OutputPerMTok:        e.OutputCostPerToken * 1_000_000,
			CachedInputPerMTok:   e.CachedInputCostPerToken * 1_000_000,
			CacheWritePerMTok:    e.CacheWriteInputCostPerToken * 1_000_000,
			CacheWrite1hrPerMTok: e.CacheWriteInputCostPerToken1hr * 1_000_000,
			MaxTokens:            e.MaxTokens,
		})
	}
	// Stable order so the UI doesn't flicker between refreshes.
	sort.Slice(out, func(i, j int) bool { return out[i].ModelID < out[j].ModelID })
	writeJSON(w, out)
}

// handleQuota handles GET /internal/quota.
func (s *Server) handleQuota(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.store == nil {
		writeJSON(w, []any{})
		return
	}

	quotas, err := s.store.ListQuotas(r.Context())
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}

	type quotaItem struct {
		Window      string    `json:"window"`
		TokensUsed  int64     `json:"tokens_used"`
		WindowStart time.Time `json:"window_start"`
		WindowEnd   time.Time `json:"window_end"`
		UpdatedAt   time.Time `json:"updated_at"`
	}

	out := make([]quotaItem, 0, len(quotas))
	for _, q := range quotas {
		out = append(out, quotaItem{
			Window:      q.WindowType,
			TokensUsed:  q.TokensUsed,
			WindowStart: q.WindowStart,
			WindowEnd:   q.WindowEnd,
			UpdatedAt:   q.UpdatedAt,
		})
	}
	writeJSON(w, out)
}

// handleModels handles GET /internal/models.
// Returns all discovered model IDs grouped by provider.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.store == nil {
		writeJSON(w, map[string]any{})
		return
	}
	all, err := s.store.GetAllDiscoveredModels(r.Context())
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}
	type modelEntry struct {
		ID          string `json:"id"`
		DisplayName string `json:"display_name"`
		FetchedAt   string `json:"fetched_at"`
	}
	result := make(map[string][]modelEntry, len(all))
	for provider, models := range all {
		entries := make([]modelEntry, 0, len(models))
		for _, m := range models {
			entries = append(entries, modelEntry{
				ID:          m.ModelID,
				DisplayName: m.DisplayName,
				FetchedAt:   m.FetchedAt.UTC().Format(time.RFC3339),
			})
		}
		result[provider] = entries
	}
	writeJSON(w, result)
}

// handleModelsRefresh handles POST /internal/models/refresh.
// Triggers asynchronous re-discovery for all configured providers.
func (s *Server) handleModelsRefresh(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	go func() {
		for _, a := range config.DetectInstalledApps() {
			if a.Name == "openclaw" {
				s.discoverOpenClawModels(a.ConfigPath)
				break
			}
		}
	}()
	writeJSON(w, map[string]bool{"ok": true})
}

// handlePricingStatus handles GET /internal/pricing/status.
// Returns pricing sync metadata, model count, top models by usage, and monthly cost/savings.
func (s *Server) handlePricingStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type topModelRow struct {
		Model         string  `json:"model"`
		Provider      string  `json:"provider"`
		Requests      int     `json:"requests"`
		CostUSD       float64 `json:"cost_usd"`
		InputPerMTok  float64 `json:"input_per_mtok"`
		OutputPerMTok float64 `json:"output_per_mtok"`
	}

	type pricingStatusResponse struct {
		LastSyncAt        string        `json:"last_sync_at"` // RFC3339 or ""
		Source            string        `json:"source"`       // "live" | "cache" | "static"
		ModelCount        int           `json:"model_count"`
		TopModels         []topModelRow `json:"top_models"`
		CostThisMonthUSD  float64       `json:"cost_this_month_usd"`
		SavedThisMonthUSD float64       `json:"saved_this_month_usd"`
	}

	resp := pricingStatusResponse{
		TopModels: []topModelRow{},
	}

	// Sync metadata.
	if s.store != nil {
		resp.LastSyncAt, _ = s.store.GetSyncMeta(r.Context(), "last_sync_at")
	}

	// Source classification.
	if resp.LastSyncAt != "" {
		if t, err := time.Parse(time.RFC3339, resp.LastSyncAt); err == nil && time.Since(t) < 25*time.Hour {
			resp.Source = "live"
		} else {
			resp.Source = "cache"
		}
	} else {
		resp.Source = "static"
	}

	// Model count.
	if s.pricing != nil {
		resp.ModelCount = s.pricing.ModelCount()
	}

	// Monthly cost and savings window.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)

	if s.store != nil {
		// Total cost this month.
		if total, err := s.store.SumCostMicroUSD(r.Context(), monthStart); err == nil {
			resp.CostThisMonthUSD = float64(total) / 1_000_000
		}

		// Savings this month.
		if s.pricing != nil {
			if recs, err := s.store.ListRequestsSince(r.Context(), monthStart, 100000); err == nil {
				var savedMicro int64
				for _, rec := range recs {
					if rec.CostMicroUSD <= 0 {
						continue
					}
					baseline := s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens, rec.CachedTokens, rec.CacheWriteTokens)
					if saved := baseline - rec.CostMicroUSD; saved > 0 {
						savedMicro += saved
					}
				}
				resp.SavedThisMonthUSD = float64(savedMicro) / 1_000_000
			}
		}

		// Top 10 models by usage (last 30 days).
		since30d := now.AddDate(0, 0, -30)
		if stats, err := s.store.TopModelsByUsage(r.Context(), since30d, 10); err == nil {
			for _, st := range stats {
				row := topModelRow{
					Model:    st.Model,
					Provider: st.Provider,
					Requests: st.Requests,
					CostUSD:  st.CostUSD,
				}
				if s.pricing != nil {
					row.InputPerMTok, row.OutputPerMTok = s.pricing.PriceFor(st.Model)
				}
				resp.TopModels = append(resp.TopModels, row)
			}
		}
	}

	writeJSON(w, resp)
}

// discoverOpenClawModels runs model discovery for all providers configured in
// the OpenClaw config: Anthropic (live via apiKey) and MiniMax-portal (live if
// apiKey present, otherwise static). Saves to DB, updates the OpenClaw models
// field, and broadcasts an SSE event. Called asynchronously; errors are
// silently ignored to avoid breaking the connect flow.
func (s *Server) discoverOpenClawModels(configPath string) {
	if s.store == nil || s.registry == nil {
		return
	}
	s.discoverOpenClawAnthropic(configPath)
	s.discoverOpenClawMiniMax(configPath)
}

// discoverOpenClawAnthropic runs Anthropic model discovery using the API key
// transiently read from the OpenClaw config.
func (s *Server) discoverOpenClawAnthropic(configPath string) {
	key := config.ReadOpenClawAPIKey(configPath)
	if key == "" {
		return
	}
	p, ok := s.registry.Get("anthropic")
	if !ok {
		return
	}
	disc, ok := p.(providers.ModelDiscoverer)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	infos, err := disc.DiscoverModels(ctx, func() string { return key })
	if err != nil {
		return
	}

	dbModels := make([]storage.DiscoveredModel, 0, len(infos))
	for _, m := range infos {
		dbModels = append(dbModels, storage.DiscoveredModel{
			Provider:    "anthropic",
			ModelID:     m.ID,
			DisplayName: m.DisplayName,
		})
	}
	if err := s.store.SaveDiscoveredModels(ctx, "anthropic", dbModels); err != nil {
		return
	}
	s.applyModelsToRegistry("anthropic", dbModels)

	oclawModels := make([]map[string]any, 0, len(infos))
	for _, m := range infos {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		oclawModels = append(oclawModels, map[string]any{"id": m.ID, "name": name})
	}
	if err := config.UpdateOpenClawModels(configPath, "anthropic", oclawModels); err != nil {
		return
	}
	s.Broadcast("models_updated", map[string]any{"provider": "anthropic", "count": len(infos)})
}

// discoverOpenClawMiniMax updates the minimax-portal models in the OpenClaw
// config. Tries live discovery if an apiKey is present in the minimax-portal
// provider section; falls back to the adapter's static model list. Only runs
// if minimax-portal is present in the OpenClaw config.
func (s *Server) discoverOpenClawMiniMax(configPath string) {
	// Only proceed if minimax-portal is configured in OpenClaw.
	hasPortal := false
	for _, n := range config.ReadOpenClawProviderNames(configPath) {
		if n == "minimax-portal" {
			hasPortal = true
			break
		}
	}
	if !hasPortal {
		return
	}

	p, ok := s.registry.Get("minimax")
	if !ok {
		return
	}

	var infos []providers.ModelInfo

	// Try live discovery when an API key is stored in the OpenClaw config.
	if mmKey := config.ReadOpenClawProviderAPIKey(configPath, "minimax-portal"); mmKey != "" {
		if disc, ok := p.(providers.ModelDiscoverer); ok {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if discovered, err := disc.DiscoverModels(ctx, func() string { return mmKey }); err == nil && len(discovered) > 0 {
				infos = discovered
			}
		}
	}

	// Fall back to the adapter's static model list.
	if len(infos) == 0 {
		for _, id := range p.SupportedModels() {
			infos = append(infos, providers.ModelInfo{ID: id, DisplayName: id})
		}
	}
	if len(infos) == 0 {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	dbModels := make([]storage.DiscoveredModel, 0, len(infos))
	for _, m := range infos {
		dbModels = append(dbModels, storage.DiscoveredModel{
			Provider:    "minimax",
			ModelID:     m.ID,
			DisplayName: m.DisplayName,
		})
	}
	if err := s.store.SaveDiscoveredModels(ctx, "minimax", dbModels); err != nil {
		return
	}
	s.applyModelsToRegistry("minimax", dbModels)

	oclawModels := make([]map[string]any, 0, len(infos))
	for _, m := range infos {
		name := m.DisplayName
		if name == "" {
			name = m.ID
		}
		oclawModels = append(oclawModels, map[string]any{"id": m.ID, "name": name})
	}
	if err := config.UpdateOpenClawModels(configPath, "minimax-portal", oclawModels); err != nil {
		return
	}
	s.Broadcast("models_updated", map[string]any{"provider": "minimax-portal", "count": len(infos)})
}

// discoverProviderModels runs model discovery for an OpenAI-compatible provider.
// The API key is resolved from inherited_endpoints (preferred, populated by
// agentscan from the user's AI agent config) or, failing that, from
// settings.ProviderKeys (dashboard override). Only the DB is updated; no
// agent config is written. Errors are silently ignored.
func (s *Server) discoverProviderModels(ctx context.Context, providerName string) {
	if s.store == nil || s.registry == nil {
		return
	}
	p, ok := s.registry.Get(providerName)
	if !ok {
		return
	}
	disc, ok := p.(providers.ModelDiscoverer)
	if !ok {
		return
	}
	key := s.resolveProviderKey(ctx, providerName)
	if key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	infos, err := disc.DiscoverModels(ctx, func() string { return key })
	if err != nil {
		return
	}
	dbModels := make([]storage.DiscoveredModel, 0, len(infos))
	for _, m := range infos {
		dbModels = append(dbModels, storage.DiscoveredModel{
			Provider:    providerName,
			ModelID:     m.ID,
			DisplayName: m.DisplayName,
		})
	}
	if err := s.store.SaveDiscoveredModels(ctx, providerName, dbModels); err != nil {
		return
	}
	s.applyModelsToRegistry(providerName, dbModels)
}

// applyModelsToRegistry pushes a provider's discovered model IDs into its
// adapter's model list, which the routing engine reads via SupportedModels().
// This is the authoritative availability source — LiteLLM provides pricing
// only (see spec/04). No-op when the provider has no adapter or the adapter
// does not support runtime model updates.
func (s *Server) applyModelsToRegistry(provider string, models []storage.DiscoveredModel) {
	if s.registry == nil {
		return
	}
	p, ok := s.registry.Get(provider)
	if !ok {
		return
	}
	ms, ok := p.(providers.ModelSetter)
	if !ok {
		return
	}
	ids := make([]string, 0, len(models))
	for _, m := range models {
		ids = append(ids, m.ModelID)
	}
	ms.SetModels(ids)
}

// DiscoverIfStale lazily discovers a provider's live /v1/models list using a key
// taken from a proxied request (not from config), when the cached list is
// missing or older than 24h. Deduplicated per provider so a burst of requests
// triggers at most one in-flight discovery. Used by the proxy's model observer
// to cover agents whose key krouter cannot read from config (Cursor keychain,
// Claude Code env). Safe to call on every request; returns fast in the common
// (fresh-cache) case.
func (s *Server) DiscoverIfStale(ctx context.Context, provider, key string) {
	if s.store == nil || s.registry == nil || provider == "" || key == "" {
		return
	}
	if models, fetchedAt, err := s.store.GetDiscoveredModels(ctx, provider); err == nil &&
		len(models) > 0 && time.Since(fetchedAt) < 24*time.Hour {
		return
	}
	if _, busy := s.discoveryInflight.LoadOrStore(provider, struct{}{}); busy {
		return
	}
	defer s.discoveryInflight.Delete(provider)

	p, ok := s.registry.Get(provider)
	if !ok {
		return
	}
	disc, ok := p.(providers.ModelDiscoverer)
	if !ok {
		return
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	infos, err := disc.DiscoverModels(cctx, func() string { return key })
	if err != nil || len(infos) == 0 {
		return
	}
	dbModels := make([]storage.DiscoveredModel, 0, len(infos))
	for _, m := range infos {
		dbModels = append(dbModels, storage.DiscoveredModel{
			Provider:    provider,
			ModelID:     m.ID,
			DisplayName: m.DisplayName,
		})
	}
	if err := s.store.SaveDiscoveredModels(cctx, provider, dbModels); err != nil {
		return
	}
	s.applyModelsToRegistry(provider, dbModels)
}

// ApplyDiscoveredModelsToRegistry loads every provider's cached /v1/models list
// from the DB and pushes it into the registry. Called once at startup so the
// routing engine has accurate availability before any fresh discovery runs
// (RefreshModelsIfStale only re-discovers stale providers).
func (s *Server) ApplyDiscoveredModelsToRegistry(ctx context.Context) {
	if s.store == nil || s.registry == nil {
		return
	}
	all, err := s.store.GetAllDiscoveredModels(ctx)
	if err != nil {
		return
	}
	for provider, models := range all {
		s.applyModelsToRegistry(provider, models)
	}
}

// RefreshModelsIfStale re-discovers models for any provider with a credential
// (inherited from an enabled agent, or set manually in the dashboard) when
// its cached model list is empty or older than 24 h. Called once at daemon
// startup, after a brief delay.
func (s *Server) RefreshModelsIfStale(ctx context.Context) {
	if s.store == nil || s.registry == nil {
		return
	}
	all, err := s.store.GetAllDiscoveredModels(ctx)
	if err != nil {
		return
	}
	staleCutoff := time.Now().Add(-24 * time.Hour)

	for _, providerName := range s.providersWithCredentials(ctx) {
		p, ok := s.registry.Get(providerName)
		if !ok {
			continue
		}
		if _, ok := p.(providers.ModelDiscoverer); !ok {
			continue
		}
		cached := all[providerName]
		needsRefresh := len(cached) == 0
		if !needsRefresh {
			for _, m := range cached {
				if m.FetchedAt.Before(staleCutoff) {
					needsRefresh = true
					break
				}
			}
		}
		if needsRefresh {
			s.discoverProviderModels(ctx, providerName)
		}
	}

	// OpenClaw legacy path: when the user is on an old binary that hasn't run
	// the inheritance flow yet, fall back to scanning openclaw.json directly
	// for the anthropic key. Once Step 3a/3b are deployed and the user has
	// re-enabled OpenClaw in the wizard, anthropic will appear in
	// inherited_endpoints and the loop above handles it.
	if _, found := all["anthropic"]; !found {
		for _, a := range config.DetectInstalledApps() {
			if a.Name != "openclaw" {
				continue
			}
			s.discoverOpenClawModels(a.ConfigPath)
			break
		}
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

// generateToken creates a 32-byte cryptographically random token encoded as base64.
func generateToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(b), nil
}

// writeInternalToken writes the token to ~/.kinthai/internal-token with 0600 perms.
func writeInternalToken(token string) error {
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".kinthai")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	path := filepath.Join(dir, "internal-token")
	return os.WriteFile(path, []byte(strings.TrimSpace(token)), 0600)
}

// handleDebugSSECapture handles GET /internal/debug/last-sse-capture.
// Returns the raw bytes of the most recently captured Anthropic SSE response
// as plain text. Used to diagnose token-parsing issues (Bug F).
func (s *Server) handleDebugSSECapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	if s.sseDebugFn == nil {
		http.Error(w, "SSE debug not available (proxy not wired)", http.StatusServiceUnavailable)
		return
	}
	data := s.sseDebugFn()
	if len(data) == 0 {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = fmt.Fprintln(w, "(no SSE capture yet — send a streaming request first)")
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("X-SSE-Capture-Bytes", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}
