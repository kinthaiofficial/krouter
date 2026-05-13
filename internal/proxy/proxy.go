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
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/logging"
	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/routing"
	"github.com/kinthaiofficial/krouter/internal/storage"
)

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

// usageRE extracts the last occurrence of input_tokens / output_tokens from Anthropic SSE data.
var (
	inputTokensRE  = regexp.MustCompile(`"input_tokens"\s*:\s*(\d+)`)
	outputTokensRE = regexp.MustCompile(`"output_tokens"\s*:\s*(\d+)`)
)

// openAIUsageRE extracts prompt_tokens / completion_tokens from OpenAI SSE data.
var (
	promptTokensRE     = regexp.MustCompile(`"prompt_tokens"\s*:\s*(\d+)`)
	completionTokensRE = regexp.MustCompile(`"completion_tokens"\s*:\s*(\d+)`)
)

// Server is the agent-facing HTTP reverse proxy (always 127.0.0.1:8402).
type Server struct {
	logger       logging.Logger
	httpClient   *http.Client
	anthropicURL string // legacy: used when engine == nil (test mode)

	engine   *routing.Engine
	registry *providers.Registry
	store    *storage.Store
	pricing  *pricing.Service
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
	return mux
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
	req := routing.Request{
		Protocol:       "anthropic",
		RequestedModel: requestedModel,
		InputTokenEst:  len(body) / 4,
		HasTools:       hasTools,
		AgentName:      agentName(r),
	}

	dec := s.engine.Decide(req, preset)
	s.logger.Debug("routing decision",
		"provider", dec.Provider,
		"model", dec.Model,
		"reason", dec.Reason,
	)

	// Rewrite model in body if the engine chose a different one.
	if dec.Model != requestedModel {
		body = rewriteModel(body, dec.Model)
	}

	provider, ok := s.registry.Get(dec.Provider)
	if !ok {
		s.logger.Error("provider not found in registry", "provider", dec.Provider)
		http.Error(w, "internal error: provider unavailable", http.StatusBadGateway)
		return
	}

	// Build request for the provider (copy headers, set body).
	// URL host is intentionally "placeholder" — the adapter rewrites it.
	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"http://placeholder/v1/messages", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(upstreamReq.Header, r.Header)

	upstreamResp, err := provider.Forward(r.Context(), upstreamReq)
	if err != nil {
		if r.Context().Err() != nil {
			s.logger.Debug("client disconnected before upstream responded")
			return
		}
		s.logger.Error("provider forward failed", "err", err)
		s.recordProviderHealth(dec.Provider, 0)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	// Forward safe response headers.
	statusCode := upstreamResp.StatusCode
	s.recordProviderHealth(dec.Provider, statusCode)
	for k, vs := range upstreamResp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	w.Header().Set("X-Krouter-Provider", dec.Provider)
	w.Header().Set("X-Krouter-Model", dec.Model)

	if stream && statusCode == http.StatusOK {
		s.streamSSEWithCapture(w, r, upstreamResp.Body, func(captured []byte, latencyMS int64) {
			in, out := parseAnthropicSSEUsage(captured)
			cost := s.computeCost(dec.Provider, dec.Model, in, out, 0)
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:             s.storeNewULID(),
				Timestamp:      start,
				Agent:          agentName(r),
				Protocol:       "anthropic",
				RequestedModel: requestedModel,
				Provider:       dec.Provider,
				Model:          dec.Model,
				InputTokens:    in,
				OutputTokens:   out,
				CostMicroUSD:   cost,
				LatencyMS:      latencyMS,
				StatusCode:     statusCode,
			})
		})
	} else {
		// Non-streaming or error: read full body, parse usage if success, then write.
		respData, _ := io.ReadAll(upstreamResp.Body)
		w.WriteHeader(statusCode)
		_, _ = w.Write(respData)
		latencyMS := time.Since(start).Milliseconds()
		var in, out int
		if statusCode == http.StatusOK {
			in, out = parseAnthropicJSONUsage(respData)
		}
		cost := s.computeCost(dec.Provider, dec.Model, in, out, 0)
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:             s.storeNewULID(),
			Timestamp:      start,
			Agent:          agentName(r),
			Protocol:       "anthropic",
			RequestedModel: requestedModel,
			Provider:       dec.Provider,
			Model:          dec.Model,
			InputTokens:    in,
			OutputTokens:   out,
			CostMicroUSD:   cost,
			LatencyMS:      latencyMS,
			StatusCode:     statusCode,
		})
	}
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
		s.streamSSE(w, r, resp)
	} else {
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}
}

// streamSSE streams an upstream SSE response to the client (legacy path).
func (s *Server) streamSSE(w http.ResponseWriter, r *http.Request, resp *http.Response) {
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
		n, err := resp.Body.Read(buf)
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

// streamSSEWithCapture streams SSE while tee-ing up to 256KB into a buffer
// for usage extraction. Calls done(captured, latencyMS) after stream ends.
func (s *Server) streamSSEWithCapture(
	w http.ResponseWriter, r *http.Request,
	body io.Reader,
	done func(captured []byte, latencyMS int64),
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

	const maxCapture = 256 * 1024
	var captureBuf bytes.Buffer
	streamStart := time.Now()

	buf := make([]byte, 4096)
	for {
		n, err := body.Read(buf)
		if n > 0 {
			if captureBuf.Len() < maxCapture {
				remaining := maxCapture - captureBuf.Len()
				if n < remaining {
					captureBuf.Write(buf[:n])
				} else {
					captureBuf.Write(buf[:remaining])
				}
			}
			if _, writeErr := w.Write(buf[:n]); writeErr != nil {
				s.logger.Debug("client disconnected during stream", "err", writeErr)
				done(captureBuf.Bytes(), time.Since(streamStart).Milliseconds())
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
			done(captureBuf.Bytes(), time.Since(streamStart).Milliseconds())
			return
		}
	}
	done(captureBuf.Bytes(), time.Since(streamStart).Milliseconds())
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

	req := routing.Request{
		Protocol:       "openai",
		RequestedModel: parsed.Model,
		InputTokenEst:  len(body) / 4,
		HasTools:       len(parsed.Tools) > 0,
		AgentName:      agentName(r),
	}
	dec := s.engine.Decide(req, preset)
	s.logger.Debug("routing decision",
		"provider", dec.Provider,
		"model", dec.Model,
		"reason", dec.Reason,
	)

	if dec.Model != parsed.Model {
		body = rewriteModel(body, dec.Model)
	}

	provider, ok := s.registry.Get(dec.Provider)
	if !ok {
		s.logger.Error("provider not found in registry", "provider", dec.Provider)
		http.Error(w, "internal error: provider unavailable", http.StatusBadGateway)
		return
	}

	upstreamReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost,
		"http://placeholder/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	copyRequestHeaders(upstreamReq.Header, r.Header)

	upstreamResp, err := provider.Forward(r.Context(), upstreamReq)
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		s.logger.Error("provider forward failed", "err", err)
		s.recordProviderHealth(dec.Provider, 0)
		http.Error(w, "bad gateway", http.StatusBadGateway)
		return
	}
	defer func() { _ = upstreamResp.Body.Close() }()

	statusCode := upstreamResp.StatusCode
	s.recordProviderHealth(dec.Provider, statusCode)
	for k, vs := range upstreamResp.Header {
		if hopByHopHeaders[strings.ToLower(k)] {
			continue
		}
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}

	if parsed.Stream && statusCode == http.StatusOK {
		s.streamSSEWithCapture(w, r, upstreamResp.Body, func(captured []byte, latencyMS int64) {
			in, out := parseOpenAISSEUsage(captured)
			cost := s.computeCost(dec.Provider, dec.Model, in, out, 0)
			s.logRequest(r.Context(), storage.RequestRecord{
				ID:             s.storeNewULID(),
				Timestamp:      start,
				Agent:          agentName(r),
				Protocol:       "openai",
				RequestedModel: parsed.Model,
				Provider:       dec.Provider,
				Model:          dec.Model,
				InputTokens:    in,
				OutputTokens:   out,
				CostMicroUSD:   cost,
				LatencyMS:      latencyMS,
				StatusCode:     statusCode,
			})
		})
	} else {
		respData, _ := io.ReadAll(upstreamResp.Body)
		w.WriteHeader(statusCode)
		_, _ = w.Write(respData)
		latencyMS := time.Since(start).Milliseconds()
		var in, out int
		if statusCode == http.StatusOK {
			in, out = parseOpenAIJSONUsage(respData)
		}
		cost := s.computeCost(dec.Provider, dec.Model, in, out, 0)
		s.logRequest(r.Context(), storage.RequestRecord{
			ID:             s.storeNewULID(),
			Timestamp:      start,
			Agent:          agentName(r),
			Protocol:       "openai",
			RequestedModel: parsed.Model,
			Provider:       dec.Provider,
			Model:          dec.Model,
			InputTokens:    in,
			OutputTokens:   out,
			CostMicroUSD:   cost,
			LatencyMS:      latencyMS,
			StatusCode:     statusCode,
		})
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
func (s *Server) computeCost(provider, model string, inputTokens, outputTokens, cachedTokens int) int64 {
	if s.pricing == nil {
		return 0
	}
	return s.pricing.CostFor(provider, model, inputTokens, outputTokens, cachedTokens)
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

// agentName extracts a best-effort agent identifier from the request.
func agentName(r *http.Request) string {
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	switch {
	case strings.Contains(ua, "openclaw"):
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
func parseOpenAIJSONUsage(data []byte) (inputTokens, outputTokens int) {
	var resp struct {
		Usage struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Usage.PromptTokens, resp.Usage.CompletionTokens
}

// parseOpenAISSEUsage extracts the last token counts from OpenAI SSE stream bytes.
func parseOpenAISSEUsage(data []byte) (inputTokens, outputTokens int) {
	if m := promptTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &inputTokens)
	}
	if m := completionTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &outputTokens)
	}
	return
}

// parseAnthropicJSONUsage extracts token counts from a non-streaming Anthropic response.
func parseAnthropicJSONUsage(data []byte) (inputTokens, outputTokens int) {
	var resp struct {
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	}
	_ = json.Unmarshal(data, &resp)
	return resp.Usage.InputTokens, resp.Usage.OutputTokens
}

// parseAnthropicSSEUsage extracts the last token counts from SSE stream bytes.
func parseAnthropicSSEUsage(data []byte) (inputTokens, outputTokens int) {
	if m := inputTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &inputTokens)
	}
	if m := outputTokensRE.FindAllSubmatch(data, -1); len(m) > 0 {
		last := m[len(m)-1]
		_, _ = fmt.Sscanf(string(last[1]), "%d", &outputTokens)
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
func (s *Server) logRequest(ctx context.Context, rec storage.RequestRecord) {
	if s.store == nil {
		return
	}
	go func() {
		bg := context.Background()
		if err := s.store.InsertRequest(bg, rec); err != nil {
			s.logger.Error("failed to log request", "err", err)
		}
		if rec.StatusCode >= 200 && rec.StatusCode < 300 {
			_ = s.store.RecordSuccess(bg, rec.Provider)
			if rec.Provider == "anthropic" {
				total := rec.InputTokens + rec.OutputTokens
				if total > 0 {
					_ = s.store.IncrementQuota(bg, "5h", int64(total))
					_ = s.store.IncrementQuota(bg, "weekly", int64(total))
				}
			}
		} else if rec.StatusCode > 0 {
			_ = s.store.RecordFailure(bg, rec.Provider, rec.StatusCode)
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
