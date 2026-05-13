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
// See spec/01-proxy-layer.md §4 for the full endpoint list.
package api

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/kinthaiofficial/krouter/internal/pricing"
	"github.com/kinthaiofficial/krouter/internal/providers"
	"github.com/kinthaiofficial/krouter/internal/remote"
	"github.com/kinthaiofficial/krouter/internal/storage"
	"github.com/kinthaiofficial/krouter/internal/upgrade"
)

const defaultPreset = "balanced"

var validPresets = map[string]bool{
	"saver":    true,
	"balanced": true,
	"quality":  true,
}

// Server is the management API server.
type Server struct {
	token    string
	store    *storage.Store
	pricing  *pricing.Service
	upgrade  *upgrade.Service
	remote   *remote.Service
	registry *providers.Registry
	startAt  time.Time
	version  string
	ports    struct{ proxy, mgmt int }
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

// SetPricing wires in the pricing service for cost/savings computation.
func (s *Server) SetPricing(p *pricing.Service) { s.pricing = p }

// SetUpgrade wires in the upgrade service for update status.
func (s *Server) SetUpgrade(u *upgrade.Service) { s.upgrade = u }

// SetRemote wires in the remote-access service.
func (s *Server) SetRemote(r *remote.Service) { s.remote = r }

// SetRegistry wires in the provider registry for the /internal/providers endpoint.
func (s *Server) SetRegistry(r *providers.Registry) { s.registry = r }

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

// AuthMiddleware validates the Bearer token.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth := r.Header.Get("Authorization")
		expected := "Bearer " + s.token
		if auth != expected {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// Handler returns the authenticated mux (used in tests without Serve).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.Handle("/internal/status", s.AuthMiddleware(http.HandlerFunc(s.handleStatus)))
	mux.Handle("/internal/logs", s.AuthMiddleware(http.HandlerFunc(s.handleLogs)))
	mux.Handle("/internal/preset", s.AuthMiddleware(http.HandlerFunc(s.handlePreset)))
	mux.Handle("/internal/usage", s.AuthMiddleware(http.HandlerFunc(s.handleUsage)))
	mux.Handle("/internal/announcements/read", s.AuthMiddleware(http.HandlerFunc(s.handleAnnouncementRead)))
	mux.Handle("/internal/announcements/dismiss", s.AuthMiddleware(http.HandlerFunc(s.handleAnnouncementDismiss)))
	mux.Handle("/internal/announcements/count", s.AuthMiddleware(http.HandlerFunc(s.handleAnnouncementsCount)))
	mux.Handle("/internal/announcements", s.AuthMiddleware(http.HandlerFunc(s.handleAnnouncements)))
	mux.Handle("/internal/update-status", s.AuthMiddleware(http.HandlerFunc(s.handleUpdateStatus)))
	mux.Handle("/internal/remote/enable", s.AuthMiddleware(http.HandlerFunc(s.handleRemoteEnable)))
	mux.Handle("/internal/remote/disable", s.AuthMiddleware(http.HandlerFunc(s.handleRemoteDisable)))
	mux.Handle("/internal/remote/status", s.AuthMiddleware(http.HandlerFunc(s.handleRemoteStatus)))
	mux.Handle("/internal/pairing/exchange", s.AuthMiddleware(http.HandlerFunc(s.handlePairingExchange)))
	mux.Handle("/internal/devices", s.AuthMiddleware(http.HandlerFunc(s.handleDevices)))
	mux.Handle("/internal/devices/", s.AuthMiddleware(http.HandlerFunc(s.handleDeviceDelete)))
	mux.Handle("/internal/providers", s.AuthMiddleware(http.HandlerFunc(s.handleProviders)))
	mux.Handle("/internal/quota", s.AuthMiddleware(http.HandlerFunc(s.handleQuota)))
	mux.Handle("/internal/update-apply", s.AuthMiddleware(http.HandlerFunc(s.handleUpdateApply)))
	return mux
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

	if s.store == nil {
		writeJSON(w, []any{})
		return
	}

	records, err := s.store.ListRequests(r.Context(), n)
	if err != nil {
		http.Error(w, `{"error":"database error"}`, http.StatusInternalServerError)
		return
	}

	type row struct {
		ID             string  `json:"id"`
		Timestamp      string  `json:"ts"`
		Agent          string  `json:"agent,omitempty"`
		Protocol       string  `json:"protocol"`
		RequestedModel string  `json:"requested_model,omitempty"`
		Provider       string  `json:"provider"`
		Model          string  `json:"model"`
		InputTokens    int     `json:"input_tokens"`
		OutputTokens   int     `json:"output_tokens"`
		CostMicroUSD   int64   `json:"cost_micro_usd"`
		CostUSD        float64 `json:"cost_usd"`
		LatencyMS      int64   `json:"latency_ms"`
		StatusCode     int     `json:"status_code"`
		ErrorMessage   string  `json:"error_message,omitempty"`
	}

	out := make([]row, 0, len(records))
	for _, rec := range records {
		out = append(out, row{
			ID:             rec.ID,
			Timestamp:      rec.Timestamp.Format(time.RFC3339),
			Agent:          rec.Agent,
			Protocol:       rec.Protocol,
			RequestedModel: rec.RequestedModel,
			Provider:       rec.Provider,
			Model:          rec.Model,
			InputTokens:    rec.InputTokens,
			OutputTokens:   rec.OutputTokens,
			CostMicroUSD:   rec.CostMicroUSD,
			CostUSD:        float64(rec.CostMicroUSD) / 1_000_000,
			LatencyMS:      rec.LatencyMS,
			StatusCode:     rec.StatusCode,
			ErrorMessage:   rec.ErrorMessage,
		})
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
		http.Error(w, `{"error":"preset must be one of: saver, balanced, quality"}`, http.StatusBadRequest)
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
					baseline := s.pricing.BaselineCostFor(rec.RequestedModel, rec.InputTokens, rec.OutputTokens)
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
	// Apply runs in background; binary replacement terminates the process.
	go func() {
		_ = s.upgrade.Apply(context.Background(), nil)
	}()
	writeJSON(w, map[string]string{"status": "applying"})
}

// handleProviders handles GET /internal/providers.
// Returns the list of registered providers with health data.
func (s *Server) handleProviders(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	type providerInfo struct {
		Name                string  `json:"name"`
		Protocol            string  `json:"protocol"`
		Available           bool    `json:"available"`
		ConsecutiveFailures int     `json:"consecutive_failures"`
		SuccessRate         float64 `json:"success_rate"`
		LastErrorCode       int     `json:"last_error_code,omitempty"`
	}

	var out []providerInfo

	if s.registry != nil {
		for _, p := range s.registry.All() {
			info := providerInfo{
				Name:        p.Name(),
				Protocol:    string(p.Protocol()),
				Available:   true,
				SuccessRate: 1.0,
			}
			if s.store != nil {
				if ps, err := s.store.GetProviderStatus(r.Context(), p.Name()); err == nil && ps != nil {
					info.ConsecutiveFailures = ps.ConsecutiveFailures
					info.SuccessRate = ps.RollingSuccessRate
					info.LastErrorCode = ps.LastErrorCode
				}
			}
			out = append(out, info)
		}
	}

	if out == nil {
		out = []providerInfo{}
	}
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
