package api

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

const (
	ticketTTL  = 30 * time.Second
	sessionTTL = 8 * time.Hour
	cookieName = "krouter_session"
)

type sessionStore struct {
	mu   sync.RWMutex
	data map[string]time.Time // sid → expiry
}

func newSessionStore() *sessionStore {
	return &sessionStore{data: make(map[string]time.Time)}
}

func (s *sessionStore) create() string {
	sid := randomHex(32)
	s.mu.Lock()
	s.data[sid] = time.Now().Add(sessionTTL)
	s.mu.Unlock()
	return sid
}

func (s *sessionStore) valid(sid string) bool {
	s.mu.RLock()
	exp, ok := s.data[sid]
	s.mu.RUnlock()
	return ok && time.Now().Before(exp)
}

// ticketStore uses sync.Map so LoadAndDelete is atomic (prevents replay).
type ticketStore struct {
	m sync.Map // ticket string → time.Time (expiry)
}

func (t *ticketStore) mint() string {
	ticket := randomHex(32)
	t.m.Store(ticket, time.Now().Add(ticketTTL))
	return ticket
}

// consume atomically deletes and validates the ticket.
// Returns true only once for a valid, non-expired ticket.
func (t *ticketStore) consume(ticket string) bool {
	v, loaded := t.m.LoadAndDelete(ticket)
	if !loaded {
		return false
	}
	exp, ok := v.(time.Time)
	return ok && time.Now().Before(exp)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// authMiddleware validates Bearer token OR session cookie.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer token path (CLI / existing API clients).
		if auth := r.Header.Get("Authorization"); auth == "Bearer "+s.token {
			next.ServeHTTP(w, r)
			return
		}
		// Session cookie path (browser).
		if c, err := r.Cookie(cookieName); err == nil && s.sessions.valid(c.Value) {
			next.ServeHTTP(w, r)
			return
		}
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
	})
}

// handleMintTicket handles POST /internal/auth/ticket.
// Requires Bearer auth; returns a single-use 30s ticket.
func (s *Server) handleMintTicket(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// Only Bearer token is accepted here (browser hasn't got a cookie yet).
	if r.Header.Get("Authorization") != "Bearer "+s.token {
		http.Error(w, `{"error":"unauthorized"}`, http.StatusUnauthorized)
		return
	}
	ticket := s.tickets.mint()
	writeJSON(w, map[string]string{"ticket": ticket})
}

// handleExchangeTicket handles GET /internal/auth/exchange?ticket=...&redirect=...
// Consumes the ticket atomically, sets a session cookie, and redirects.
func (s *Server) handleExchangeTicket(w http.ResponseWriter, r *http.Request) {
	ticket := r.URL.Query().Get("ticket")
	if ticket == "" || !s.tickets.consume(ticket) {
		http.Error(w, `{"error":"invalid or expired ticket"}`, http.StatusUnauthorized)
		return
	}

	sid := s.sessions.create()
	http.SetCookie(w, &http.Cookie{
		Name:     cookieName,
		Value:    sid,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(sessionTTL.Seconds()),
	})

	redirect := r.URL.Query().Get("redirect")
	if redirect == "" {
		redirect = "/ui/"
	}
	http.Redirect(w, r, redirect, http.StatusFound)
}
