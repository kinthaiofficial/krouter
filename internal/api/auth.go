package api

import (
	"net/http"
)

// allowedOrigin is the only browser origin permitted to call management API
// endpoints. Requests from a different Origin (cross-site) are rejected with
// 403 to prevent CSRF. Requests with no Origin header (curl, CLI) are always
// allowed — they carry no CSRF risk because a browser cannot make a
// cross-origin request without an Origin header.
const allowedOrigin = "http://127.0.0.1:8403"

// originCheck is a middleware that blocks requests where the Origin header is
// present but does not match allowedOrigin.
//
// Security model:
//   - Browser same-origin JS  → Origin: http://127.0.0.1:8403  → allowed
//   - Browser cross-origin JS → Origin: https://evil.com        → 403
//   - curl / CLI              → no Origin header               → allowed
//
// The Origin header is set automatically by browsers and cannot be spoofed
// by JS running on a different origin (W3C CORS spec).
func originCheck(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" && origin != allowedOrigin {
			http.Error(w, `{"error":"cross-origin denied"}`, http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// authMiddleware is the single auth + CSRF guard for all /internal/* endpoints.
//
// Decision order:
//  1. Valid Bearer token → allow unconditionally (CLI / remote programmatic access).
//  2. Cross-origin request (Origin present but ≠ allowedOrigin) → 403 Forbidden.
//  3. No Origin or correct Origin → allow (browser dashboard or curl).
//
// This replaces the previous session-cookie + ticket-exchange scheme. The Origin
// header (enforced by browsers, unforgeable by cross-origin JS) provides equivalent
// CSRF protection without any server-side session state.
func (s *Server) authMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Bearer token path — valid token overrides all other checks.
		if r.Header.Get("Authorization") == "Bearer "+s.token {
			next.ServeHTTP(w, r)
			return
		}
		// CSRF guard: block requests that carry a foreign Origin.
		if origin := r.Header.Get("Origin"); origin != "" && origin != allowedOrigin {
			http.Error(w, `{"error":"cross-origin denied"}`, http.StatusForbidden)
			return
		}
		// No Origin (curl / CLI) or our own Origin (browser dashboard) → allow.
		next.ServeHTTP(w, r)
	})
}

// AuthMiddleware is the exported alias used by tests that call Handler() directly.
func (s *Server) AuthMiddleware(next http.Handler) http.Handler {
	return s.authMiddleware(next)
}
