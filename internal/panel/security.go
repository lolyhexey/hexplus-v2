package panel

import (
	"net/http"
	"strings"
	"sync"
	"time"
)

// security.go: login rate-limit + IP bans + CSRF protection.
//
// Rate limiting and IP ban state lives in-memory only; a panel
// restart clears bans on purpose so a locked-out admin who fixed
// their password by other means (menu 04) can immediately log in.
// For persistent bans use fail2ban with the panel's login audit log
// (rows written by handleLogin via the auditLogin helper).

// LoginRateLimit describes the failure budget before an IP is banned.
type LoginRateLimit struct {
	Failures    int
	Window      time.Duration
	BanDuration time.Duration
}

// DefaultLoginRateLimit — 5 failures in 10 minutes = 1-hour ban. In
// line with 3x-ui's fail2ban preset so admins moving in feel at home.
var DefaultLoginRateLimit = LoginRateLimit{
	Failures:    5,
	Window:      10 * time.Minute,
	BanDuration: time.Hour,
}

// ipTracker holds the per-IP recent-failure log + active bans. Both
// maps are pruned on every check so long-running processes don't leak.
type ipTracker struct {
	mu       sync.Mutex
	failures map[string][]time.Time
	banned   map[string]time.Time
	limit    LoginRateLimit
}

func newIPTracker(limit LoginRateLimit) *ipTracker {
	return &ipTracker{
		failures: map[string][]time.Time{},
		banned:   map[string]time.Time{},
		limit:    limit,
	}
}

// banned returns true if the IP is currently under a ban. Cleans up
// expired ban entries in-line to avoid a background sweeper.
func (t *ipTracker) isBanned(ip string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if until, ok := t.banned[ip]; ok {
		if time.Now().Before(until) {
			return true
		}
		delete(t.banned, ip)
	}
	return false
}

// recordFailure appends now to the IP's failure log and, if the window
// is exceeded, adds a ban. Returns whether the IP just got banned so
// the caller can log the transition.
func (t *ipTracker) recordFailure(ip string) (nowBanned bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	cut := now.Add(-t.limit.Window)
	trimmed := []time.Time{}
	for _, at := range t.failures[ip] {
		if at.After(cut) {
			trimmed = append(trimmed, at)
		}
	}
	trimmed = append(trimmed, now)
	t.failures[ip] = trimmed
	if len(trimmed) >= t.limit.Failures {
		t.banned[ip] = now.Add(t.limit.BanDuration)
		delete(t.failures, ip) // start fresh after the ban ends
		return true
	}
	return false
}

// clearFailures wipes an IP's failure log on successful login.
func (t *ipTracker) clearFailures(ip string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	delete(t.failures, ip)
}

// wrapLoginRateLimit is middleware that gates handleLogin — sends
// 429 with a Retry-After header when the IP is currently banned.
func (t *ipTracker) wrapLoginRateLimit(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ip := remoteIP(r)
		if t.isBanned(ip) {
			w.Header().Set("Retry-After", "60")
			writeJSON(w, http.StatusTooManyRequests, map[string]string{
				"error": "too many failed logins — try again later",
			})
			return
		}
		next.ServeHTTP(w, r)
	}
}

// CSRF token flow: cookie + header combo (submit-cookie pattern).
//
// The frontend reads the CSRF cookie (readable JS) and echoes its
// value back in the X-CSRF-Token header for every mutating request.
// Since same-origin JS can read the cookie but a cross-origin attacker
// can't, matching cookie and header proves the request came from our
// own UI.
//
// We accept token-less GETs so read endpoints (list inbounds, etc.)
// don't need frontend cooperation to work.

const CSRFCookieName = "hexplus_csrf"
const CSRFHeaderName = "X-CSRF-Token"

// wrapCSRF is the middleware installed on the whole admin sub-mux. It:
//  1. Ensures a CSRF cookie exists (issues on first hit).
//  2. On mutating verbs, checks the cookie value equals the header.
func wrapCSRF(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token := ""
		if c, err := r.Cookie(CSRFCookieName); err == nil {
			token = c.Value
		}
		if token == "" {
			token = newCSRFToken()
			http.SetCookie(w, &http.Cookie{
				Name:     CSRFCookieName,
				Value:    token,
				Path:     "/",
				SameSite: http.SameSiteStrictMode,
				MaxAge:   int(24 * time.Hour.Seconds()),
			})
		}
		if isMutation(r.Method) && !csrfExempt(r.URL.Path) {
			got := r.Header.Get(CSRFHeaderName)
			if got == "" || got != token {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "bad csrf token"})
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// newCSRFToken reuses the session-cookie hex helper but with random
// bytes only (no HMAC — the cookie IS the token).
func newCSRFToken() string {
	b := make([]byte, 24)
	// panic-on-error is fine here: crypto/rand.Read only fails when the
	// OS entropy source is missing, in which case the whole panel is
	// unsafe to run anyway.
	if _, err := randRead(b); err != nil {
		panic(err)
	}
	return hexEncode(b)
}

// isMutation reports whether the HTTP method changes server state.
func isMutation(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	}
	return false
}

// csrfExempt bypasses the check for endpoints that legitimately can't
// carry the header — the login form (no session yet, so no cookie
// either) and the subscription download (already token-authed).
func csrfExempt(path string) bool {
	return path == "/login" || strings.HasPrefix(path, "/sub/")
}
