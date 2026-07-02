package panel

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
)

// SessionCookieName is the cookie the panel issues on login. HttpOnly
// so JS can't touch it; Path=/ so requests under URLPrefix still send it.
const SessionCookieName = "hexplus_panel_session"

// SessionLifetime is the 24h sliding expiry from Phase 0. Every
// authenticated request bumps expires_at to now+SessionLifetime; idle
// tabs get logged out at the 24h mark.
const SessionLifetime = 24 * time.Hour

// Auth bundles the DB handle and HMAC key needed for session signing.
// Constructed once by Serve and passed to the router.
type Auth struct {
	db     *sql.DB
	secret []byte
}

// NewAuth builds an Auth with the session_secret from the panel config.
// The secret is hex-decoded once here so cookie signing is a cheap HMAC
// per request.
func NewAuth(sqldb *sql.DB, hexSecret string) *Auth {
	key, err := hex.DecodeString(hexSecret)
	if err != nil {
		// Bad secret shape is a config-time error caught by validate();
		// falling back to the raw bytes here at least fails closed
		// (signatures won't match anything an attacker forges).
		key = []byte(hexSecret)
	}
	return &Auth{db: sqldb, secret: key}
}

// ctxKey is the private key type used for context.Value storage.
type ctxKey int

const (
	ctxAdminID ctxKey = iota
)

// AdminIDFromContext returns the authenticated admin's ID, or 0 when
// the request wasn't authenticated (defensive; RequireSession bails
// early so handlers behind it can trust the ID is nonzero).
func AdminIDFromContext(ctx context.Context) int64 {
	v, _ := ctx.Value(ctxAdminID).(int64)
	return v
}

// RequireSession is middleware. It reads the session cookie, verifies
// its HMAC, looks up the row in `sessions`, checks expires_at, bumps it,
// and injects the admin ID into the request context. Anything wrong
// returns 401 with a JSON error body — the frontend redirects to /login.
func (a *Auth) RequireSession(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		adminID, ok := a.checkSession(r)
		if !ok {
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthenticated"})
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdminID, adminID)
		next.ServeHTTP(w, r.WithContext(ctx))
	}
}

// checkSession returns the authenticated admin ID and true, or 0/false
// if anything about the session is invalid. Also bumps expires_at on
// success so active tabs stay logged in indefinitely.
func (a *Auth) checkSession(r *http.Request) (int64, bool) {
	c, err := r.Cookie(SessionCookieName)
	if err != nil || c.Value == "" {
		return 0, false
	}
	token, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return 0, false
	}
	if !hmacEqualHex(a.secret, token, sig) {
		return 0, false
	}
	var (
		adminID   int64
		expiresAt int64
	)
	row := a.db.QueryRow(`SELECT admin_id, expires_at FROM sessions WHERE token = ?`, token)
	if err := row.Scan(&adminID, &expiresAt); err != nil {
		return 0, false
	}
	now := time.Now().Unix()
	if expiresAt <= now {
		_, _ = a.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
		return 0, false
	}
	// Sliding expiry: bump only if we've burned at least 10% of the
	// window so we don't do a write on every single request.
	if expiresAt-now < int64(SessionLifetime.Seconds()*0.9) {
		_, _ = a.db.Exec(`UPDATE sessions SET expires_at = ? WHERE token = ?`,
			now+int64(SessionLifetime.Seconds()), token)
	}
	return adminID, true
}

// issueSession creates a new sessions row for adminID and returns the
// cookie value ("<token>.<hmac>") the caller sets on the response.
func (a *Auth) issueSession(adminID int64, remoteIP string) (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", err
	}
	token := hex.EncodeToString(tokenBytes)
	now := time.Now().Unix()
	_, err := a.db.Exec(
		`INSERT INTO sessions (token, admin_id, created_at, expires_at, remote_ip) VALUES (?, ?, ?, ?, ?)`,
		token, adminID, now, now+int64(SessionLifetime.Seconds()), remoteIP)
	if err != nil {
		return "", err
	}
	return token + "." + hmacHex(a.secret, token), nil
}

// handleLogin: POST /login with JSON body {username, password}.
// Success sets the session cookie and returns 200 with the admin ID.
// Failure returns 401 without disclosing which of user/pass was wrong.
func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	var in struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad json"})
		return
	}
	var (
		adminID int64
		hash    string
	)
	row := s.db.QueryRow(`SELECT id, password_hash FROM admin_users WHERE username = ?`, in.Username)
	if err := row.Scan(&adminID, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			// Same 401 as a bad password so the response doesn't tell
			// an attacker which usernames exist.
			writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "db"})
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.Password)); err != nil {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid credentials"})
		return
	}
	cookieVal, err := s.auth.issueSession(adminID, remoteIP(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "session"})
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    cookieVal,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(SessionLifetime.Seconds()),
	})
	_, _ = s.db.Exec(`UPDATE admin_users SET last_login_at = ? WHERE id = ?`, time.Now().Unix(), adminID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "admin_id": adminID})
}

// handleLogout: POST /logout — drops the session row and clears the
// cookie. Idempotent: a stale cookie or no cookie still returns 200.
func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie(SessionCookieName); err == nil {
		if token, _, ok := strings.Cut(c.Value, "."); ok {
			_, _ = s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
		}
	}
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
	})
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

// SetAdminPassword upserts the given admin user with a bcrypt hash of
// the plaintext password. Used by `hexplus panel install` (initial
// admin) and the "change admin password" menu action.
func SetAdminPassword(sqldb *sql.DB, username, plaintext string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(plaintext), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	_, err = sqldb.Exec(`
		INSERT INTO admin_users (username, password_hash, created_at)
		VALUES (?, ?, ?)
		ON CONFLICT(username) DO UPDATE SET password_hash = excluded.password_hash
	`, username, string(hash), now)
	return err
}

// hmacHex signs msg with key and returns the lowercase hex digest.
func hmacHex(key []byte, msg string) string {
	mac := hmac.New(sha256.New, key)
	mac.Write([]byte(msg))
	return hex.EncodeToString(mac.Sum(nil))
}

// hmacEqualHex checks whether wantHex matches HMAC(key, msg). Constant
// time; safe against timing attacks that would leak the secret.
func hmacEqualHex(key []byte, msg, wantHex string) bool {
	got := hmacHex(key, msg)
	return hmac.Equal([]byte(got), []byte(wantHex))
}

// writeJSON emits status + JSON body with the standard content type.
// Errors on the write are logged only; response is best-effort.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// remoteIP returns the client IP for audit logging. Trusts X-Forwarded-For
// only if the panel is behind a reverse proxy — the frontier config toggle
// for that lands later; for now we prefer RemoteAddr's host part.
func remoteIP(r *http.Request) string {
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}
