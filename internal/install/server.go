package install

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/kinthaiofficial/krouter/internal/config"
)

// Server is the HTTP API backend for the browser-based install wizard.
// It runs on :8404 (or a nearby port) and is served only from krouter-installer.
type Server struct {
	token        string // single-use install token
	finalized    atomic.Bool
	orch         *Orchestrator
	shutdownOnce sync.Once
	shutdownCh   chan struct{}

	// Override for tests.
	waitForDaemonFn func() // polls :8403/health; injectable for tests
	uiAssets        fs.FS  // if non-nil, served at /
}

// NewServer creates a Server with the given install token and orchestrator.
func NewServer(token string, orch *Orchestrator) *Server {
	return &Server{
		token:           token,
		orch:            orch,
		shutdownCh:      make(chan struct{}),
		waitForDaemonFn: defaultWaitForDaemon,
	}
}

// ShutdownCh returns a channel that is closed when the install wizard has
// finished and the browser has been redirected to the dashboard. The caller
// (krouter-installer main) can block on this channel and exit cleanly.
func (s *Server) ShutdownCh() <-chan struct{} {
	return s.shutdownCh
}

// SetUIAssets sets the embedded filesystem used to serve the install wizard frontend.
func (s *Server) SetUIAssets(assets fs.FS) {
	s.uiAssets = assets
}

// Handler returns the http.Handler for the install server.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// Static frontend — served at /
	if s.uiAssets != nil {
		mux.Handle("/", http.FileServer(http.FS(s.uiAssets)))
	}

	// Install API endpoints (all require ?token= or Authorization: Bearer).
	mux.HandleFunc("/api/install/detect-agents", s.withAuth(s.handleDetectAgents))
	mux.HandleFunc("/api/install/copy-binary", s.withAuth(s.handleCopyBinary))
	mux.HandleFunc("/api/install/register-service", s.withAuth(s.handleRegisterService))
	mux.HandleFunc("/api/install/shell-integration", s.withAuth(s.handleShellIntegration))
	mux.HandleFunc("/api/install/connect-agent", s.withAuth(s.handleConnectAgent))
	mux.HandleFunc("/api/install/set-budget", s.withAuth(s.handleSetBudget))
	mux.HandleFunc("/api/install/finalize", s.withAuth(s.handleFinalize))
	mux.HandleFunc("/api/install/daemon-ready", s.withAuth(s.handleDaemonReady))

	// Agent inheritance endpoints — feed the wizard's "Agent Paths" step
	// (spec/04 §4). Selections land in pending-agents.json; the daemon
	// imports them at startup.
	mux.HandleFunc("/api/install/agents/supported", s.withAuth(s.handleAgentsSupported))
	mux.HandleFunc("/api/install/agents/preview", s.withAuth(s.handleAgentsPreview))
	mux.HandleFunc("/api/install/agents/select", s.withAuth(s.handleAgentsSelect))

	// Health — no auth needed.
	mux.HandleFunc("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	return mux
}

// Listen starts the server on the first available port from startPort onwards.
// Returns the actual address it is listening on.
func Listen(startPort int, h http.Handler) (net.Listener, string, error) {
	for port := startPort; port < startPort+5; port++ {
		addr := fmt.Sprintf("127.0.0.1:%d", port)
		ln, err := net.Listen("tcp", addr)
		if err == nil {
			go func() {
				_ = http.Serve(ln, h)
			}()
			return ln, addr, nil
		}
	}
	return nil, "", fmt.Errorf("install: no free port in range %d-%d", startPort, startPort+4)
}

// withAuth wraps a handler to require the install token.
func (s *Server) withAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := r.URL.Query().Get("token")
		if tok == "" {
			if auth := r.Header.Get("Authorization"); len(auth) > 7 && auth[:7] == "Bearer " {
				tok = auth[7:]
			}
		}
		if tok != s.token {
			http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// handleDetectAgents returns detected AI agents.
func (s *Server) handleDetectAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	agents := s.orch.detectAgentsFn()
	type agentResp struct {
		Name       string `json:"name"`
		ConfigPath string `json:"config_path,omitempty"`
		CLIPath    string `json:"cli_path,omitempty"`
	}
	out := make([]agentResp, len(agents))
	for i, a := range agents {
		out[i] = agentResp{Name: a.Name, ConfigPath: a.ConfigPath, CLIPath: a.CLIPath}
	}
	writeJSON(w, out)
}

// handleCopyBinary copies the binary using the orchestrator.
func (s *Server) handleCopyBinary(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.orch.CopyBinary(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleRegisterService registers the OS service.
func (s *Server) handleRegisterService(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.orch.RegisterService(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleShellIntegration writes the shell RC block.
func (s *Server) handleShellIntegration(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if err := s.orch.ShellIntegration(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleConnectAgent connects a single agent by name.
func (s *Server) handleConnectAgent(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		Agent      string `json:"agent"`
		ConfigPath string `json:"config_path"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if body.Agent == "" {
		writeError(w, http.StatusBadRequest, "agent name required")
		return
	}
	info := config.AgentInfo{Name: body.Agent, ConfigPath: body.ConfigPath}
	if err := s.orch.connectAgent(info); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleSetBudget writes the daily budget limit to settings.json so the daemon
// honours it from first launch. Called by the install wizard's BudgetStep.
func (s *Server) handleSetBudget(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	var body struct {
		DailyLimitUSD float64 `json:"daily_limit_usd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	mgr := config.New("")
	current := mgr.Get()
	if current.BudgetWarnings == nil {
		current.BudgetWarnings = make(map[string]float64)
	}
	current.BudgetWarnings["daily"] = body.DailyLimitUSD
	if err := mgr.Set(current); err != nil {
		writeError(w, http.StatusInternalServerError, "failed to save settings")
		return
	}
	writeJSON(w, map[string]bool{"ok": true})
}

// handleFinalize marks the install complete and returns a redirect URL
// to the main daemon UI (:8403/krouter/).
func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}
	if s.finalized.Load() {
		writeError(w, http.StatusGone, "already finalized")
		return
	}

	if err := s.orch.MarkInstalled(); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.finalized.Store(true)
	// Return immediately — the client polls /api/install/daemon-ready to wait
	// for the daemon and obtain a session ticket once it is up.
	writeJSON(w, map[string]string{"status": "ok"})
}

// handleDaemonReady checks whether the daemon is reachable and, if so, mints
// a one-time session ticket so the browser can open the dashboard authenticated.
// The client polls this endpoint every ~1.5 s while showing a "starting" screen.
func (s *Server) handleDaemonReady(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeError(w, http.StatusMethodNotAllowed, "method not allowed")
		return
	}

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://127.0.0.1:8403/health")
	if err != nil || resp.StatusCode != http.StatusOK {
		writeJSON(w, map[string]any{"ready": false})
		return
	}
	_ = resp.Body.Close()

	// No ticket/session needed — the browser dashboard uses Origin-check auth.
	// Direct redirect to the dashboard; browser's same-origin Origin header is
	// sufficient for the management API to accept subsequent requests.
	writeJSON(w, map[string]any{"ready": true, "redirect_url": "http://127.0.0.1:8403/krouter/"})

	// Signal main to exit after the browser has had time to process the redirect.
	go func() {
		time.Sleep(500 * time.Millisecond)
		s.shutdownOnce.Do(func() { close(s.shutdownCh) })
	}()
}

// defaultWaitForDaemon polls :8403/health until the daemon responds or 10 s elapses.
func defaultWaitForDaemon() {
	client := &http.Client{Timeout: time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := client.Get("http://127.0.0.1:8403/health")
		if err == nil {
			_ = resp.Body.Close()
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

