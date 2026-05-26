// Package proxy implements the HTTP reverse proxy (port 8402, agent-facing).
//
// Accepts LLM requests from local AI agents and forwards them to upstream
// providers. This is the hot path — every agent request goes through here.
//
// Endpoints (spec/01-proxy-layer.md §2):
//
//	POST /v1/messages         Anthropic Messages API
//	POST /v1/chat/completions OpenAI Chat Completions (M1.3+)
//	GET  /v1/models           Model discovery
//	GET  /health              Daemon health check
//
// All endpoints bind to 127.0.0.1:8402. No authentication — process-level
// isolation is the security boundary (see DECISIONS.md D-011).
//
// See spec/01-proxy-layer.md for the full specification.
package proxy

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/providers/minimax"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

// decompressForParsing returns a decompressed copy of data when the response
// headers indicate Content-Encoding: gzip. Used by non-streaming paths to
// parse usage tokens while forwarding the original (possibly compressed) bytes
// to the client unchanged — the client sees the real Content-Encoding header
// and handles decompression itself.
// Falls back to returning data as-is on any decompression error.
func decompressForParsing(header http.Header, data []byte) []byte {
	if !strings.EqualFold(header.Get("Content-Encoding"), "gzip") {
		return data
	}
	gr, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return data
	}
	defer gr.Close()
	out, err := io.ReadAll(gr)
	if err != nil {
		return data
	}
	return out
}

// hopByHopHeaders are headers that must not be forwarded to upstream.
var hopByHopHeaders = map[string]bool{
	"connection":          true,
	"keep-alive":          true,
	"proxy-authenticate":  true,
	"proxy-authorization": true,
	"te":                  true,
	"trailers":            true,
	"transfer-encoding":   true,
	"upgrade":             true,
}

// openAIUsageRE extracts token counts from OpenAI SSE data.
var (
	promptTokensRE        = regexp.MustCompile(`"prompt_tokens"\s*:\s*(\d+)`)
	completionTokensRE    = regexp.MustCompile(`"completion_tokens"\s*:\s*(\d+)`)
	cachedTokensRE        = regexp.MustCompile(`"cached_tokens"\s*:\s*(\d+)`)
	promptCacheHitTokensRE = regexp.MustCompile(`"prompt_cache_hit_tokens"\s*:\s*(\d+)`)
)

// Server is the agent-facing HTTP reverse proxy (always 127.0.0.1:8402).
type Server struct {
	logger       logging.Logger
	httpClient   *http.Client
	anthropicURL string // legacy: used when engine == nil (test mode)

	engine     *routing.Engine
	registry   *providers.Registry
	store      *storage.Store
	pricing    *pricing.Service
	onComplete func(storage.RequestRecord) // optional; called after every logged request

	// modelObserver, when set, is called in a goroutine on each routed request
	// with (requestedModel, apiKey) so the daemon can lazily discover the live
	// /v1/models list for the request's provider using the key from the request
	// itself — covering agents whose key krouter cannot read from config (Cursor
	// keychain, Claude Code env). Never blocks the request path.
	modelObserver func(requestedModel, apiKey string)

	// knownApps is the set of application ids krouter connects (e.g. "openclaw",
	// "claude-code"). Used to validate the /a/<appid> request-path prefix that
	// connect bakes into each app's base URL.
	knownApps map[string]bool

	// sessionStore, when set, receives token-bucket updates after each successful
	// response. Phase 2 shadow mode — does not affect routing decisions.
	sessionStore routing.SessionSource

	// lastSSECaptureMu guards lastSSECapture for the debug endpoint.
	lastSSECaptureMu sync.RWMutex
	lastSSECapture   []byte
}

// GetLastSSECapture returns a copy of the most recently captured Anthropic SSE
// buffer (up to 4 KB). Used by the /internal/debug/last-sse-capture endpoint.
func (s *Server) GetLastSSECapture() []byte {
	s.lastSSECaptureMu.RLock()
	defer s.lastSSECaptureMu.RUnlock()
	out := make([]byte, len(s.lastSSECapture))
	copy(out, s.lastSSECapture)
	return out
}

// Option configures a Server.
type Option func(*Server)

// WithLogger sets the logger.
func WithLogger(l logging.Logger) Option {
	return func(s *Server) { s.logger = l }
}

// WithAnthropicURL overrides the Anthropic base URL.
// Intended for testing; when engine is nil this URL is used directly.
func WithAnthropicURL(url string) Option {
	return func(s *Server) { s.anthropicURL = strings.TrimRight(url, "/") }
}

// WithEngine sets the routing engine (enables routing mode).
func WithEngine(e *routing.Engine) Option {
	return func(s *Server) { s.engine = e }
}

// WithRegistry sets the provider registry (required when engine is set).
func WithRegistry(r *providers.Registry) Option {
	return func(s *Server) { s.registry = r }
}

// WithStore sets the SQLite store for request logging.
func WithStore(st *storage.Store) Option {
	return func(s *Server) { s.store = st }
}

// WithPricing sets the pricing service for cost computation.
func WithPricing(p *pricing.Service) Option {
	return func(s *Server) { s.pricing = p }
}

// SetOnComplete registers a callback invoked (in a goroutine) after each
// request record is written to the store. Used to broadcast SSE events.
func (s *Server) SetOnComplete(fn func(storage.RequestRecord)) { s.onComplete = fn }

// SetModelObserver registers a callback invoked (in a goroutine) on each routed
// request with the requested model and the request's API key. Used to trigger
// lazy /v1/models discovery from live traffic.
func (s *Server) SetModelObserver(fn func(requestedModel, apiKey string)) { s.modelObserver = fn }

// WithKnownApps sets the application ids the proxy recognises in the /a/<appid>
// request-path prefix. Sourced from the agent scanner registry so it stays in
// sync with the apps krouter can connect.
func WithKnownApps(ids []string) Option {
	return func(s *Server) {
		s.knownApps = make(map[string]bool, len(ids))
		for _, id := range ids {
			s.knownApps[id] = true
		}
	}
}

// WithSessionStore attaches an in-memory session store for Phase 2 shadow-mode
// cache hit rate tracking. Does not affect routing decisions.
func WithSessionStore(ss routing.SessionSource) Option {
	return func(s *Server) { s.sessionStore = ss }
}

// New creates a proxy server with the given options.
func New(opts ...Option) *Server {
	s := &Server{
		logger:       logging.New("info"),
		anthropicURL: "https://api.anthropic.com",
		httpClient: &http.Client{
			// No timeout — streaming responses can be arbitrarily long.
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
				ForceAttemptHTTP2:   true,
			},
		},
	}
	for _, o := range opts {
		o(s)
	}
	return s
}

// Serve starts the proxy HTTP server on host:port and blocks until ctx is
// cancelled or a fatal error occurs. Graceful shutdown waits up to 5 seconds.
func (s *Server) Serve(ctx context.Context, host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	srv := &http.Server{
		Addr:              addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 30 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		s.logger.Info("proxy listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- fmt.Errorf("proxy server: %w", err)
		}
		close(errCh)
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			s.logger.Error("graceful shutdown error", "err", err)
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// Handler returns an http.Handler for the proxy (used in tests without Serve).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/v1/messages", s.handleAnthropicMessages)
	mux.HandleFunc("/v1/models", s.handleModels)
	mux.HandleFunc("/v1/chat/completions", s.handleOpenAICompletions)
	// Application-prefixed routes (/a/<appid>/...) carry connect-time attribution.
	mux.HandleFunc("/a/", s.handlePrefixed)
	return mux
}

// handlePrefixed serves requests whose path carries the krouter application
// prefix /a/<appid>/... that connect bakes into each app's base URL. It records
// the application id for deterministic attribution (spec/12 §6.3), strips the
// prefix, and dispatches to the protocol handler by path suffix — the client
// always appends a canonical sub-path (/messages, /chat/completions, /models)
// regardless of any provider-specific base path that precedes it.
func (s *Server) handlePrefixed(w http.ResponseWriter, r *http.Request) {
	rest := strings.TrimPrefix(r.URL.Path, "/a/")
	i := strings.IndexByte(rest, '/')
	if i <= 0 {
		http.NotFound(w, r)
		return
	}
	appID := rest[:i]
	if !s.knownApps[appID] {
		http.NotFound(w, r)
		return
	}
	stripped := rest[i:] // leading slash retained, e.g. /api/paas/v4/chat/completions
	r = r.WithContext(context.WithValue(r.Context(), appIDCtxKey{}, appID))
	r.URL.Path = stripped

	switch {
	case strings.HasSuffix(stripped, "/messages"):
		s.handleAnthropicMessages(w, r)
	case strings.HasSuffix(stripped, "/chat/completions"):
		s.handleOpenAICompletions(w, r)
	case strings.HasSuffix(stripped, "/models"):
		s.handleModels(w, r)
	default:
		http.NotFound(w, r)
	}
}

// apiKeyFromHeaders extracts the caller's API key, trying the Anthropic
// x-api-key header first, then a Bearer Authorization header.
func apiKeyFromHeaders(h http.Header) string {
	if k := h.Get("x-api-key"); k != "" {
		return k
	}
	if a := h.Get("Authorization"); a != "" {
		return strings.TrimPrefix(a, "Bearer ")
	}
	return ""
}

// handleHealth handles GET /health.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(`{"status":"ok"}`))
}

// handleAnthropicMessages handles POST /v1/messages.
// When engine is set (production mode), routing decisions are made before forwarding.
// When engine is nil (test mode), requests go directly to anthropicURL.
func (s *Server) handleAnthropicMessages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.logger.Error("failed to read request body", "err", err)
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var parsed struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
		Tools  []any  `json:"tools"`
	}
	_ = json.Unmarshal(body, &parsed)

	start := time.Now()

	if s.engine != nil {
		preset := s.currentPreset(r.Context())
		s.handleAnthropicWithRouting(w, r, body, parsed.Model, parsed.Stream, len(parsed.Tools) > 0, start, preset)
		return
	}

	// Legacy path: direct forward to anthropicURL (used in tests without engine).
	s.forwardToUpstream(w, r, body, parsed.Model, parsed.Stream, s.anthropicURL+"/v1/messages")
}

// handleAnthropicWithRouting uses the routing engine and provider registry.
func (s *Server) handleAnthropicWithRouting(
	w http.ResponseWriter, r *http.Request,
	body []byte, requestedModel string, stream bool, hasTools bool,
	start time.Time, preset string,
) {
	hasImages, systemPrompt := extractAnthropicMeta(body)
	sessionKey := computeSessionKey(r.Header, body)
	req := routing.Request{
		Protocol:       "anthropic",
		RequestedModel: requestedModel,
		InputTokenEst:  len(body) / 4,
		HasImages:      hasImages,
		HasTools:       hasTools,
		SystemPrompt:   systemPrompt,
		AppID:          requestAppID(r),
		SessionKey:     sessionKey,
	}

	upstreamResp, dec, err := s.tryWithFallback(r.Context(), r.Header, body, req, preset, "/v1/messages")
	if err != nil {
		if errors.Is(err, routing.ErrBudgetExceeded) {
			// Record the blocked request so it still appears on the dashboards
			// (#66). Nothing was forwarded upstream, so provider/model are empty.
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:             s.storeNewULID(),
				Timestamp:      start,
				Agent:          requestAppID(r),
				Protocol:       "anthropic",
				RequestedModel: req.RequestedModel,
				StatusCode:     http.StatusTooManyRequests,
				LatencyMS:      time.Since(start).Milliseconds(),
				ErrorMessage:   err.Error(),
				RoutingPreset:  preset,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"type":"error","error":{"type":"rate_limit_error","message":"Daily budget limit exceeded. Adjust your limit in KRouter Settings."}}`))
			return
		}
		if r.Context().Err() != nil {
			s.logger.Debug("client disconnected before upstream responded")
			return
		}
		s.logger.Error("provider forward failed (all fallbacks exhausted)", "err", err)
		// Write a durable log row so the failed request still appears on the
		// Router/Logs dashboards (and in per-provider stats) instead of being
		// silently dropped — issue #52. dec carries the last attempted
		// provider/model (may be empty if nothing was attempted).
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:             s.storeNewULID(),
			Timestamp:      start,
			Agent:          requestAppID(r),
			Protocol:       "anthropic",
			RequestedModel: req.RequestedModel,
			Provider:       dec.Provider,
			Model:          dec.Model,
			StatusCode:     http.StatusBadGateway,
			LatencyMS:      time.Since(start).Milliseconds(),
			ErrorMessage:   err.Error(),
			RoutingPreset:  preset,
		})
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	s.logger.Debug("routing decision",
		"provider", dec.Provider,
		"model", dec.Model,
		"reason", dec.Reason,
	)

	// Cache MiniMax OAuth token for the quota poller (never persisted to disk).
	if dec.Provider == "minimax" {
		minimax.CacheOAuthToken(r.Header.Get("Authorization"))
	}

	statusCode := upstreamResp.StatusCode
	for k, vs := range upstreamResp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if stream && statusCode == http.StatusOK {
		s.streamSSEWithCapture(w, r, upstreamResp.Body, func(captured []byte) {
			// Store raw capture for /internal/debug/last-sse-capture diagnosis.
			s.lastSSECaptureMu.Lock()
			s.lastSSECapture = make([]byte, len(captured))
			copy(s.lastSSECapture, captured)
			s.lastSSECaptureMu.Unlock()

			in, out, cached, cacheWrite := parseAnthropicSSEUsage(captured)
			if in == 0 && out == 0 && cached == 0 && cacheWrite == 0 {
				// Log the first 512 bytes of the captured buffer at debug level
				// so operators can diagnose SSE parsing failures.
				preview := captured
				if len(preview) > 512 {
					preview = preview[:512]
				}
				s.logger.Debug("anthropic SSE token parse returned 0/0 — captured buffer preview",
					"bytes_total", len(captured),
					"preview", string(preview),
				)
			}
			cost := s.computeCost(dec.Provider, dec.Model, in, out, cached, cacheWrite)
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:               s.storeNewULID(),
				Timestamp:        start,
				Agent:            requestAppID(r),
				Protocol:         "anthropic",
				RequestedModel:   requestedModel,
				Provider:         dec.Provider,
				Model:            dec.Model,
				InputTokens:      in,
				OutputTokens:     out,
				CachedTokens:     cached,
				CacheWriteTokens: cacheWrite,
				CostMicroUSD:     cost,
				LatencyMS:        time.Since(start).Milliseconds(),
				StatusCode:       statusCode,
				RoutingPreset:    preset,
			})
			s.updateSessionFromResponse(sessionKey, dec.Provider, dec.Model, in, out, cached, cacheWrite)
		})
	} else {
		// Non-streaming: forward original bytes to client (preserves Content-Encoding),
		// then decompress a copy for token parsing so the client sees the real response.
		respData, _ := io.ReadAll(upstreamResp.Body)
		w.WriteHeader(statusCode)
		_, _ = w.Write(respData)
		latencyMS := time.Since(start).Milliseconds()
		var in, out, cached, cacheWrite int
		if statusCode == http.StatusOK {
			in, out, cached, cacheWrite = parseAnthropicJSONUsage(
				decompressForParsing(upstreamResp.Header, respData))
		}
		cost := s.computeCost(dec.Provider, dec.Model, in, out, cached, cacheWrite)
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:               s.storeNewULID(),
			Timestamp:        start,
			Agent:            requestAppID(r),
			Protocol:         "anthropic",
			RequestedModel:   requestedModel,
			Provider:         dec.Provider,
			Model:            dec.Model,
			InputTokens:      in,
			OutputTokens:     out,
			CachedTokens:     cached,
			CacheWriteTokens: cacheWrite,
			CostMicroUSD:     cost,
			LatencyMS:        latencyMS,
			StatusCode:       statusCode,
			RoutingPreset:    preset,
		})
		s.updateSessionFromResponse(sessionKey, dec.Provider, dec.Model, in, out, cached, cacheWrite)
	}
}

// tryWithFallback forwards the request to the provider selected by the routing engine,
// retrying with a downgraded provider/model on 5xx or network errors (up to 2 retries).
// Returns the first successful response or the last 5xx response if no fallback is available.
// 4xx responses are returned immediately without retrying.
// Caller is responsible for closing resp.Body.
func (s *Server) tryWithFallback(
	ctx context.Context,
	headers http.Header,
	body []byte,
	req routing.Request,
	preset string,
	path string,
) (*http.Response, routing.Decision, error) {
	dec := s.engine.Decide(req, preset)
	if dec.BudgetExceeded {
		return nil, dec, routing.ErrBudgetExceeded
	}

	// Lazily learn the request's provider model list from its own key. Fired
	// async so it never adds latency; the observer dedups and stale-checks.
	if s.modelObserver != nil {
		if key := apiKeyFromHeaders(headers); key != "" {
			go s.modelObserver(req.RequestedModel, key)
		}
	}

	tried := make(map[string]bool)

	var lastErrBody []byte
	var lastErrStatus int

	for attempt := 0; attempt < 3; attempt++ {
		key := dec.Provider + "/" + dec.Model
		if tried[key] {
			break
		}
		tried[key] = true

		provider, ok := s.registry.Get(dec.Provider)
		if !ok {
			return nil, dec, fmt.Errorf("provider %q not in registry", dec.Provider)
		}

		// Rewrite model in body if engine chose a different model.
		reqBody := body
		if dec.Model != req.RequestedModel {
			reqBody = rewriteModel(body, dec.Model)
		}

		upstreamReq, err := http.NewRequestWithContext(ctx, http.MethodPost,
			"http://placeholder"+path, bytes.NewReader(reqBody))
		if err != nil {
			return nil, dec, err
		}
		copyRequestHeaders(upstreamReq.Header, headers)

		resp, err := provider.Forward(ctx, upstreamReq)
		if err != nil {
			if ctx.Err() != nil {
				return nil, dec, err
			}
			s.recordProviderHealth(dec.Provider, 0)
			fb := s.engine.FallbackDecide(req, preset, tried)
			if fb.Provider == "" {
				return nil, dec, fmt.Errorf("forward failed and no fallback available: %w", err)
			}
			dec = fb
			continue
		}

		// 4xx: return immediately, no retry.
		if resp.StatusCode >= 400 && resp.StatusCode < 500 {
			s.recordProviderHealth(dec.Provider, resp.StatusCode)
			// Mark the provider exhausted for free-credit routing when
			// the upstream rejects auth (401/403), reports the user has
			// no remaining credit (402), or rate-limits (429). The
			// routing engine's free-first path will skip this provider
			// until the TTL expires; expiry depends on status:
			//   401/403 → 1 h  (often a stale key the user will fix)
			//   402     → 24 h (credit usually resets monthly,
			//                   but daily quotas reset overnight)
			//   429     → 5 min (short-term burst)
			//
			// We mark all of these unconditionally — if the provider is
			// NOT a free-credit one, the routing engine just doesn't
			// consult provider_exhausted_until for it, so marking is a
			// cheap no-op for paid providers.
			s.markIfThrottle(dec.Provider, resp.StatusCode)
			return resp, dec, nil
		}

		// 5xx: try fallback.
		if resp.StatusCode >= 500 {
			lastErrStatus = resp.StatusCode
			lastErrBody, _ = io.ReadAll(io.LimitReader(resp.Body, 64*1024))
			_ = resp.Body.Close()
			s.recordProviderHealth(dec.Provider, resp.StatusCode)
			fb := s.engine.FallbackDecide(req, preset, tried)
			if fb.Provider == "" {
				break // no more fallbacks — return the last 5xx below
			}
			dec = fb
			continue
		}

		// 2xx/3xx: success.
		s.recordProviderHealth(dec.Provider, resp.StatusCode)
		return resp, dec, nil
	}

	// Reconstruct a response from the last 5xx body so the client sees the error.
	if lastErrStatus > 0 {
		return &http.Response{
			StatusCode: lastErrStatus,
			Status:     fmt.Sprintf("%d %s", lastErrStatus, http.StatusText(lastErrStatus)),
			Header:     make(http.Header),
			Body:       io.NopCloser(bytes.NewReader(lastErrBody)),
		}, dec, nil
	}
	return nil, dec, fmt.Errorf("all providers failed")
}

// forwardToUpstream is the legacy direct-forward path (used when engine is nil).
func (s *Server) forwardToUpstream(
	w http.ResponseWriter, r *http.Request,
	body []byte, model string, stream bool, upstreamURL string,
) {
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, upstreamURL, bytes.NewReader(body))
	if err != nil {
		s.logger.Error("failed to build upstream request", "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(upstreamReq.Header, r.Header)

	s.logger.Debug("forwarding to anthropic", "model", model, "stream", stream, "upstream", upstreamURL)

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		if r.Context().Err() != nil {
			s.logger.Debug("client disconnected before upstream responded")
			return
		}
		s.logger.Error("upstream request failed", "err", err)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vs := range resp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	if stream && resp.StatusCode == http.StatusOK {
		s.streamSSE(w, r, resp.Body)
	} else {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// streamSSE streams an upstream SSE response to the client (legacy path).
func (s *Server) streamSSE(w http.ResponseWriter, r *http.Request, body io.Reader) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error("response writer does not support flushing")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				s.logger.Debug("client disconnected during stream", "err", writeErr)
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if r.Context().Err() != nil {
				s.logger.Debug("stream cancelled by client")
			} else {
				s.logger.Error("upstream read error during stream", "err", err)
			}
			return
		}
	}
}

// streamSSEWithCapture streams SSE to the client while tee-ing into two buffers
// for usage extraction:
//   - head: first 64 KB (contains message_start with input_tokens)
//   - tail: last 4 KB  (contains message_delta with final output_tokens)
//
// Using both ensures usage is captured even for responses > 64 KB.
// Calls done(combined) after the stream ends or the client disconnects.
func (s *Server) streamSSEWithCapture(
	w http.ResponseWriter, r *http.Request,
	body io.Reader,
	done func(captured []byte),
) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		s.logger.Error("response writer does not support flushing")
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)

	const maxHead = 64 * 1024
	const maxTail = 4 * 1024
	var headBuf bytes.Buffer
	tailBuf := make([]byte, 0, maxTail)

	flush := func() {
		// Concatenate head + tail, dropping the overlapping region when the stream
		// is short enough to fit entirely in head.
		if headBuf.Len() < maxHead {
			done(headBuf.Bytes())
			return
		}
		done(append(headBuf.Bytes(), tailBuf...))
	}

	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			chunk := buf[:n]
			// Capture head (first maxHead bytes).
			if headBuf.Len() < maxHead {
				remaining := maxHead - headBuf.Len()
				if n <= remaining {
					headBuf.Write(chunk)
				} else {
					headBuf.Write(chunk[:remaining])
				}
			}
			// Always keep last maxTail bytes in tailBuf.
			if len(tailBuf)+n <= maxTail {
				tailBuf = append(tailBuf, chunk...)
			} else if n >= maxTail {
				tailBuf = append(tailBuf[:0], chunk[n-maxTail:]...)
			} else {
				keep := maxTail - n
				tailBuf = append(tailBuf[:0], tailBuf[len(tailBuf)-keep:]...)
				tailBuf = append(tailBuf, chunk...)
			}

			if _, writeErr := w.Write(chunk); writeErr != nil {
				s.logger.Debug("client disconnected during stream", "err", writeErr)
				flush()
				return
			}
			flusher.Flush()
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if r.Context().Err() != nil {
				s.logger.Debug("stream cancelled by client")
			} else {
				s.logger.Error("upstream read error during stream", "err", err)
			}
			flush()
			return
		}
	}
	flush()
}

// handleModels handles GET /v1/models by forwarding to the upstream provider.
func (s *Server) handleModels(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	upstreamURL := s.anthropicURL + "/v1/models"
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodGet, upstreamURL, nil)
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(upstreamReq.Header, r.Header)

	resp, err := s.httpClient.Do(upstreamReq)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = resp.Body.Close() }()

	for k, vs := range resp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// handleOpenAICompletions handles POST /v1/chat/completions.
func (s *Server) handleOpenAICompletions(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	if s.engine == nil {
		w.Header().Set("Content-Type", "application/json")
		http.Error(w, `{"error":"openai-protocol routing requires engine"}`, http.StatusNotImplemented)
		return
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "failed to read request body", http.StatusBadRequest)
		return
	}

	var parsed struct {
		Stream bool   `json:"stream"`
		Model  string `json:"model"`
		Tools  []any  `json:"tools"`
	}
	_ = json.Unmarshal(body, &parsed)

	start := time.Now()
	preset := s.currentPreset(r.Context())

	hasImages, systemPrompt := extractOpenAIMeta(body)
	sessionKey := computeSessionKey(r.Header, body)
	req := routing.Request{
		Protocol:       "openai",
		RequestedModel: parsed.Model,
		InputTokenEst:  len(body) / 4,
		HasImages:      hasImages,
		HasTools:       len(parsed.Tools) > 0,
		SystemPrompt:   systemPrompt,
		AppID:          requestAppID(r),
		SessionKey:     sessionKey,
	}

	upstreamResp, dec, err := s.tryWithFallback(r.Context(), r.Header, body, req, preset, "/v1/chat/completions")
	if err != nil {
		if errors.Is(err, routing.ErrBudgetExceeded) {
			// Record the blocked request so it still appears on the dashboards (#66).
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:             s.storeNewULID(),
				Timestamp:      start,
				Agent:          requestAppID(r),
				Protocol:       "openai",
				RequestedModel: req.RequestedModel,
				StatusCode:     http.StatusTooManyRequests,
				LatencyMS:      time.Since(start).Milliseconds(),
				ErrorMessage:   err.Error(),
				RoutingPreset:  preset,
			})
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"type":"insufficient_quota","message":"Daily budget limit exceeded. Adjust your limit in KRouter Settings."}}`))
			return
		}
		if r.Context().Err() != nil {
			return
		}
		s.logger.Error("provider forward failed (all fallbacks exhausted)", "err", err)
		// Durable log row on the error path too — issue #52 (see Anthropic handler).
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:             s.storeNewULID(),
			Timestamp:      start,
			Agent:          requestAppID(r),
			Protocol:       "openai",
			RequestedModel: req.RequestedModel,
			Provider:       dec.Provider,
			Model:          dec.Model,
			StatusCode:     http.StatusBadGateway,
			LatencyMS:      time.Since(start).Milliseconds(),
			ErrorMessage:   err.Error(),
			RoutingPreset:  preset,
		})
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	s.logger.Debug("routing decision",
		"provider", dec.Provider,
		"model", dec.Model,
		"reason", dec.Reason,
	)

	statusCode := upstreamResp.StatusCode
	for k, vs := range upstreamResp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	if parsed.Stream && statusCode == http.StatusOK {
		s.streamSSEWithCapture(w, r, upstreamResp.Body, func(captured []byte) {
			in, out, cached, cacheWrite := parseOpenAISSEUsage(captured)
			cost := s.computeCost(dec.Provider, dec.Model, in, out, cached, cacheWrite)
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:               s.storeNewULID(),
				Timestamp:        start,
				Agent:            requestAppID(r),
				Protocol:         "openai",
				RequestedModel:   parsed.Model,
				Provider:         dec.Provider,
				Model:            dec.Model,
				InputTokens:      in,
				OutputTokens:     out,
				CachedTokens:     cached,
				CacheWriteTokens: cacheWrite,
				CostMicroUSD:     cost,
				LatencyMS:        time.Since(start).Milliseconds(),
				StatusCode:       statusCode,
				RoutingPreset:    preset,
			})
			s.updateSessionFromResponse(sessionKey, dec.Provider, dec.Model, in, out, cached, cacheWrite)
		})
	} else {
		respData, _ := io.ReadAll(upstreamResp.Body)
		w.WriteHeader(statusCode)
		_, _ = w.Write(respData)
		latencyMS := time.Since(start).Milliseconds()
		var in, out, cached, cacheWrite int
		if statusCode == http.StatusOK {
			in, out, cached, cacheWrite = parseOpenAIJSONUsage(
				decompressForParsing(upstreamResp.Header, respData))
		}
		cost := s.computeCost(dec.Provider, dec.Model, in, out, cached, cacheWrite)
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:               s.storeNewULID(),
			Timestamp:        start,
			Agent:            requestAppID(r),
			Protocol:         "openai",
			RequestedModel:   parsed.Model,
			Provider:         dec.Provider,
			Model:            dec.Model,
			InputTokens:      in,
			OutputTokens:     out,
			CachedTokens:     cached,
			CacheWriteTokens: cacheWrite,
			CostMicroUSD:     cost,
			LatencyMS:        latencyMS,
			StatusCode:       statusCode,
			RoutingPreset:    preset,
		})
		s.updateSessionFromResponse(sessionKey, dec.Provider, dec.Model, in, out, cached, cacheWrite)
	}
}

// currentPreset reads the active preset from the store. Returns "balanced" on error or when store is nil.
func (s *Server) currentPreset(ctx context.Context) string {
	if s.store == nil {
		return routing.PresetBalanced
	}
	v, ok, err := s.store.GetSetting(ctx, "preset")
	if err != nil || !ok || v == "" {
		return routing.PresetBalanced
	}
	return v
}

// computeCost returns micro-USD cost via the pricing service, or 0 if not available.
func (s *Server) computeCost(provider, model string, inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) int64 {
	if s.pricing == nil {
		return 0
	}
	return s.pricing.CostFor(provider, model, inputTokens, outputTokens, cachedTokens, cacheWriteTokens)
}

// copyRequestHeaders copies safe request headers from src to dst.
func copyRequestHeaders(dst, src http.Header) {
	for k, vs := range src {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		if strings.ToLower(k) == "host" {
			continue
		}
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}

// rewriteModel replaces the "model" field in a JSON body.
func rewriteModel(body []byte, newModel string) []byte {
	var m map[string]json.RawMessage
	if err := json.Unmarshal(body, &m); err != nil {
		return body
	}
	modelJSON, err := json.Marshal(newModel)
	if err != nil {
		return body
	}
	m["model"] = modelJSON
	out, err := json.Marshal(m)
	if err != nil {
		return body
	}
	return out
}

// extractAnthropicMeta extracts HasImages and SystemPrompt from an Anthropic
// Messages API request body. Both values are used for routing decisions.
//
// system field: may be a string or []{"type":"text","text":"..."}
// messages[i].content: may be a string or []{"type":"image"|"text"|...}
func extractAnthropicMeta(body []byte) (hasImages bool, systemPrompt string) {
	var req struct {
		System   json.RawMessage `json:"system"`
		Messages []struct {
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}

	// Parse system prompt (string or content-block array).
	if len(req.System) > 0 {
		var s string
		if err := json.Unmarshal(req.System, &s); err == nil {
			systemPrompt = truncate(s, 300)
		} else {
			var blocks []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}
			if err := json.Unmarshal(req.System, &blocks); err == nil {
				for _, b := range blocks {
					if b.Type == "text" {
						systemPrompt = truncate(b.Text, 300)
						break
					}
				}
			}
		}
	}

	// Detect image content blocks in messages.
	for _, msg := range req.Messages {
		if len(msg.Content) == 0 {
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "image" {
				hasImages = true
				return
			}
		}
	}
	return
}

// extractOpenAIMeta extracts HasImages and SystemPrompt from an OpenAI Chat
// Completions request body.
//
// role=="system" message → systemPrompt
// content[].type=="image_url" → hasImages
func extractOpenAIMeta(body []byte) (hasImages bool, systemPrompt string) {
	var req struct {
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return
	}
	for _, msg := range req.Messages {
		if msg.Role == "system" && systemPrompt == "" {
			var s string
			if err := json.Unmarshal(msg.Content, &s); err == nil {
				systemPrompt = truncate(s, 300)
			}
			continue
		}
		var blocks []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(msg.Content, &blocks); err != nil {
			continue
		}
		for _, b := range blocks {
			if b.Type == "image_url" {
				hasImages = true
			}
		}
	}
	return
}

// truncate returns at most n runes from s.
func truncate(s string, n int) string {
	runes := []rune(s)
	if len(runes) <= n {
		return s
	}
	return string(runes[:n])
}

// computeSessionKey derives a stable 16-char hex key from the request.
// It hashes the API key, system prompt, tool names, and first user message —
// the fields that are stable across turns in the same agent conversation and
// that LLM providers use to key their prompt cache.
func computeSessionKey(headers http.Header, body []byte) string {
	h := sha256.New()

	// API key — differentiates users; never logged.
	if auth := headers.Get("Authorization"); auth != "" {
		h.Write([]byte(auth))
	} else if key := headers.Get("X-Api-Key"); key != "" {
		h.Write([]byte(key))
	}
	h.Write([]byte{0})

	// Extract structured fields from the body in a single unmarshal.
	var req struct {
		System   json.RawMessage `json:"system"`
		Tools    []struct {
			Name string `json:"name"`
		} `json:"tools"`
		Messages []struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		} `json:"messages"`
	}
	_ = json.Unmarshal(body, &req)

	// System prompt bytes.
	h.Write(req.System)
	h.Write([]byte{0})

	// Tool names (stable across turns; order matters for cache keying).
	for _, t := range req.Tools {
		h.Write([]byte(t.Name))
		h.Write([]byte{0})
	}
	h.Write([]byte{0})

	// First user message — the conversation anchor.
	for _, msg := range req.Messages {
		if msg.Role == "user" || msg.Role == "human" {
			h.Write(msg.Content)
			break
		}
	}

	return hex.EncodeToString(h.Sum(nil))[:16]
}

// updateSessionFromResponse records token-bucket counts into the session store.
// Called after every successful response (both streaming and non-streaming).
// No-op when sessionStore is nil or key is empty.
func (s *Server) updateSessionFromResponse(key, provider, model string, in, out, cached, cacheWrite int) {
	if s.sessionStore == nil || key == "" {
		return
	}
	s.sessionStore.Update(key, func(st *routing.SessionState) {
		if st.RequestCount == 0 {
			// Bind provider and model on the first observed request only.
			// These are the sticky targets for Phase 3 cache-aware routing.
			st.BoundProvider = provider
			st.BoundModel = model
		}
		st.RequestCount++
		st.FreshInputTokens += in
		st.CachedTokens += cached
		st.OutputTokens += out
		st.CacheWriteTokens += cacheWrite
		// Last-request snapshot for hit-rate prediction (see session.go).
		st.LastInputTokens = in
		st.LastCacheRead = cached
		st.LastCacheWrite = cacheWrite
	})
}

// appIDCtxKey carries the application id parsed from a /a/<appid> request path
// (set by handlePrefixed) through to the routing handlers.
type appIDCtxKey struct{}

// requestAppID resolves the application that sent the request. The authoritative
// source is the /a/<appid> path prefix krouter baked into the app's config at
// connect time (carried in the request context); only when that is absent — a
// source krouter never connected — do we fall back to the legacy header sniff.
func requestAppID(r *http.Request) string {
	if v, ok := r.Context().Value(appIDCtxKey{}).(string); ok && v != "" {
		return v
	}
	return sniffAppID(r)
}

// sniffAppID is the best-effort header-based fallback for requests that carry no
// /a/<appid> prefix (e.g. a source krouter didn't connect).
//
// OpenClaw uses the Anthropic TypeScript SDK which sends "Anthropic/JS X.Y.Z" as
// User-Agent — the string "openclaw" does NOT appear. The
// "anthropic-dangerous-direct-browser-access" header is set by OpenClaw's SDK
// client in every Anthropic-provider request and is absent from CLI tools like
// Claude Code, making it the reliable secondary signal.
func sniffAppID(r *http.Request) string {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	switch {
	case strings.Contains(ua, "openclaw"):
		return "openclaw"
	case r.Header.Get("anthropic-dangerous-direct-browser-access") == "true":
		return "openclaw"
	case strings.Contains(ua, "claude"):
		return "claude-code"
	case strings.Contains(ua, "cursor"):
		return "cursor"
	default:
		return "unknown"
	}
}

// parseOpenAIJSONUsage extracts token counts from a non-streaming OpenAI response.
// Handles OpenAI standard (prompt_tokens_details.cached_tokens) and
// DeepSeek's prompt_cache_hit_tokens field.
//
// OpenAI/DeepSeek do not expose a cache write field — cache is managed
// automatically and the first write is charged at full prompt_tokens price.
// cacheWriteTokens is therefore always 0 for this protocol.
func parseOpenAIJSONUsage(data []byte) (inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) {
	var resp struct {
		Usage struct {
			PromptTokens        int `json:"prompt_tokens"`
			CompletionTokens    int `json:"completion_tokens"`
			PromptTokensDetails struct {
				CachedTokens int `json:"cached_tokens"`
			} `json:"prompt_tokens_details"`
			PromptCacheHitTokens int `json:"prompt_cache_hit_tokens"` // DeepSeek
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &resp)
	cachedTokens = resp.Usage.PromptTokensDetails.CachedTokens
	if cachedTokens == 0 {
		cachedTokens = resp.Usage.PromptCacheHitTokens
	}
	inputTokens = resp.Usage.PromptTokens - cachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	outputTokens = resp.Usage.CompletionTokens
	return
}

// parseOpenAISSEUsage extracts the last token counts from OpenAI SSE stream bytes.
// Handles OpenAI cached_tokens and DeepSeek prompt_cache_hit_tokens.
func parseOpenAISSEUsage(data []byte) (inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) {
	if m := promptTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &inputTokens)
	}
	if m := completionTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &outputTokens)
	}
	if m := cachedTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &cachedTokens)
	}
	if cachedTokens == 0 {
		if m := promptCacheHitTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
			last := m[len(m)-1]
			_, _ = fmt.Sscanf(string(last[1]), "%d", &cachedTokens)
		}
	}
	inputTokens -= cachedTokens
	if inputTokens < 0 {
		inputTokens = 0
	}
	return
}

// parseAnthropicJSONUsage extracts token counts from a non-streaming Anthropic response.
// Returns four mutually-exclusive buckets:
//
//	inputTokens:      input_tokens (fresh, neither cached nor written to cache)
//	outputTokens:     output_tokens
//	cachedTokens:     cache_read_input_tokens (billed at ~10% of input price)
//	cacheWriteTokens: cache_creation_input_tokens (billed at 1.25× input price, 5m TTL)
func parseAnthropicJSONUsage(data []byte) (inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) {
	var resp struct {
		Usage struct {
			InputTokens              int `json:"input_tokens"`
			CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
			CacheReadInputTokens     int `json:"cache_read_input_tokens"`
			OutputTokens             int `json:"output_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &resp)
	u := resp.Usage
	return u.InputTokens, u.OutputTokens, u.CacheReadInputTokens, u.CacheCreationInputTokens
}

// parseAnthropicSSEUsage extracts token counts from Anthropic SSE stream bytes.
// Accumulates across message_start events (rare multi-message responses).
// Returns four mutually-exclusive buckets matching parseAnthropicJSONUsage.
func parseAnthropicSSEUsage(data []byte) (inputTokens, outputTokens, cachedTokens, cacheWriteTokens int) {
	type usageFields struct {
		InputTokens              int `json:"input_tokens"`
		CacheCreationInputTokens int `json:"cache_creation_input_tokens"`
		CacheReadInputTokens     int `json:"cache_read_input_tokens"`
		OutputTokens             int `json:"output_tokens"`
	}
	type msgStart struct {
		Usage usageFields `json:"usage"`
	}
	type sseEvent struct {
		Type    string      `json:"type"`
		Message msgStart    `json:"message"` // message_start
		Usage   usageFields `json:"usage"`   // message_delta
	}

	for _, line := range bytes.Split(data, []byte("\n")) {
		line = bytes.TrimSpace(line)
		if !bytes.HasPrefix(line, []byte("data: ")) {
			continue
		}
		payload := line[6:]
		if bytes.Equal(payload, []byte("[DONE]")) {
			continue
		}
		var ev sseEvent
		if err := json.Unmarshal(payload, &ev); err != nil {
			continue
		}
		switch ev.Type {
		case "message_start":
			u := ev.Message.Usage
			inputTokens += u.InputTokens
			cachedTokens += u.CacheReadInputTokens
			cacheWriteTokens += u.CacheCreationInputTokens
		case "message_delta":
			outputTokens += ev.Usage.OutputTokens
		}
	}
	return
}

// storeNewULID returns a new ULID from the store, or a time-based fallback.
func (s *Server) storeNewULID() string {
	if s.store != nil {
		return s.store.NewULID()
	}
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

// logRequest writes a request record, provider health update, and (for Anthropic)
// quota increments to SQLite in a single goroutine to avoid write contention.
// After the insert it calls onComplete (if set) so callers can broadcast SSE events.
func (s *Server) logRequest(ctx context.Context, rec storage.RequestRecord) {
	if s.store == nil && s.onComplete == nil {
		return
	}
	go func() {
		bg := context.Background()
		if s.store != nil {
			if err := s.store.InsertRequest(bg, rec); err != nil {
				s.logger.Error("failed to log request", "err", err)
			}
			if rec.StatusCode >= 200 && rec.StatusCode < 300 {
				_ = s.store.RecordSuccess(bg, rec.Provider)
				if rec.Provider == "anthropic" {
					// Count all token buckets against quota to match Anthropic's billing.
					total := rec.InputTokens + rec.CachedTokens + rec.CacheWriteTokens + rec.OutputTokens
					if total > 0 {
						_ = s.store.IncrementQuota(bg, "5h", int64(total))
						_ = s.store.IncrementQuota(bg, "weekly", int64(total))
					}
					if strings.HasPrefix(rec.Model, "claude-opus") && total > 0 {
						_ = s.store.IncrementQuota(bg, "opus", int64(total))
					}
				}
			} else if rec.StatusCode > 0 && rec.Provider != "" {
				// Guard the empty provider: a fallback-exhaustion row (#52) may
				// carry no provider when nothing was attempted; don't write a
				// provider_status entry keyed by "".
				_ = s.store.RecordFailure(bg, rec.Provider, rec.StatusCode)
			}
		}
		if s.onComplete != nil {
			s.onComplete(rec)
		}
	}()
}

// recordProviderHealth records a network-level failure (no HTTP response received).
// For HTTP-level failures (4xx/5xx), health is recorded inside logRequest.
func (s *Server) recordProviderHealth(providerName string, statusCode int) {
	if s.store == nil || statusCode > 0 {
		// HTTP responses are handled by logRequest; only handle network errors here.
		return
	}
	go func() {
		_ = s.store.RecordFailure(context.Background(), providerName, 0)
	}()
}

// markIfThrottle records a provider_exhausted_until entry when the
// upstream returned an auth / quota / rate-limit status. The routing
// engine's free-first path consults this table and skips marked
// providers until their TTL expires; for paid providers (the majority),
// the row is written but never read, which is fine — a few extra rows
// in a tiny table is cheaper than per-provider feature flags.
//
// Status-code → TTL map:
//
//	401, 403 → 1 hour   (likely a fixable key)
//	402      → 24 hours (paid credit exhausted; daily quotas reset overnight)
//	429      → 5 minutes (short-term burst)
//	other 4xx → no mark (probably a request-format issue, not a provider problem)
//
// Reason text records the status code so the dashboard can surface
// "DeepSeek exhausted: HTTP 402 from upstream".
func (s *Server) markIfThrottle(providerName string, statusCode int) {
	if s.store == nil || providerName == "" {
		return
	}
	var ttl time.Duration
	var reason string
	switch statusCode {
	case 401, 403:
		ttl = 1 * time.Hour
		reason = fmt.Sprintf("HTTP %d unauthorized — key may need rotation", statusCode)
	case 402:
		ttl = 24 * time.Hour
		reason = "HTTP 402 — provider reports no credit remaining"
	case 429:
		ttl = 5 * time.Minute
		reason = "HTTP 429 — rate limited"
	default:
		return
	}
	go func() {
		_ = s.store.MarkProviderExhausted(
			context.Background(), providerName,
			time.Now().UTC().Add(ttl), statusCode, reason,
		)
	}()
}
