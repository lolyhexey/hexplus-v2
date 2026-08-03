package panel

import (
	"encoding/json"
	"errors"
	"net/http"

	"golang.org/x/crypto/bcrypt"
)

// api_settings.go: endpoints backing the Settings page.
//
// The panel's mutable config (port, URL prefix, subscription toggle)
// and the admin credential are the two things an operator wants to
// change from the browser without SSH-ing back into the box. Anything
// heavier (backup, restart, uninstall) still lives in the CLI menu
// since it interacts with systemd.

func (s *Server) registerSettingsRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/settings", s.auth.RequireSession(s.handleSettingsGet))
	mux.HandleFunc("PUT /api/settings", s.auth.RequireSession(s.handleSettingsPut))
	mux.HandleFunc("POST /api/settings/password", s.auth.RequireSession(s.handleSettingsPassword))
	mux.HandleFunc("POST /api/settings/url-prefix/rotate", s.auth.RequireSession(s.handleRotatePrefix))
}

// SettingsView is the shape returned to the frontend. Session secret
// is intentionally NOT surfaced — no operator flow needs to see it,
// and exposing it defeats the "rotate on suspicion" use case.
type SettingsView struct {
	ListenAddr          string `json:"listen_addr"`
	Port                int    `json:"port"`
	URLPrefix           string `json:"url_prefix"`
	SubscriptionEnabled bool   `json:"subscription_enabled"`
	AdminUsername       string `json:"admin_username"`
	XrayVersion         string `json:"xray_version"`
	PanelVersion        string `json:"panel_version"`
}

type SettingsInput struct {
	ListenAddr          *string `json:"listen_addr"`
	Port                *int    `json:"port"`
	SubscriptionEnabled *bool   `json:"subscription_enabled"`
}

type PasswordInput struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
	NewUsername     string `json:"new_username"`
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, _ *http.Request) {
	var username string
	_ = s.db.QueryRow(`SELECT username FROM admin_users ORDER BY id LIMIT 1`).Scan(&username)
	writeJSON(w, http.StatusOK, SettingsView{
		ListenAddr:          s.cfg.ListenAddr,
		Port:                s.cfg.Port,
		URLPrefix:           s.cfg.URLPrefix,
		SubscriptionEnabled: s.cfg.SubscriptionEnabled,
		AdminUsername:       username,
		// Xray/panel version lookup would need a version package
		// exposed from Go; for MVP the frontend labels these as
		// "installed" and shows a link out to the release notes.
		XrayVersion:  "installed",
		PanelVersion: "installed",
	})
}

// handleSettingsPut writes the panel.yaml on disk. Port change requires
// a systemd restart to actually rebind — that's out-of-band and
// documented in the UI copy.
func (s *Server) handleSettingsPut(w http.ResponseWriter, r *http.Request) {
	var in SettingsInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	cfg, err := Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if in.ListenAddr != nil {
		cfg.ListenAddr = *in.ListenAddr
	}
	if in.Port != nil {
		if *in.Port < 1 || *in.Port > 65535 {
			writeJSON(w, http.StatusBadRequest, errBody(errors.New("port out of range")))
			return
		}
		cfg.Port = *in.Port
	}
	if in.SubscriptionEnabled != nil {
		cfg.SubscriptionEnabled = *in.SubscriptionEnabled
	}
	if err := Save(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	// Also update the server's in-memory copy so subscription toggles
	// take effect on the current process without a restart.
	s.cfg.SubscriptionEnabled = cfg.SubscriptionEnabled
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"requires_restart": in.Port != nil || in.ListenAddr != nil,
	})
}

// handleSettingsPassword — change username + password. Verifies the
// current password so a stolen session cookie can't just take over
// the admin credential silently.
func (s *Server) handleSettingsPassword(w http.ResponseWriter, r *http.Request) {
	var in PasswordInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if len(in.NewPassword) < 6 {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("new password too short (min 6 chars)")))
		return
	}
	adminID := AdminIDFromContext(r.Context())
	if adminID == 0 {
		writeJSON(w, http.StatusUnauthorized, errBody(errors.New("no session")))
		return
	}
	var (
		username string
		hash     string
	)
	if err := s.db.QueryRow(
		`SELECT username, password_hash FROM admin_users WHERE id = ?`, adminID,
	).Scan(&username, &hash); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(in.CurrentPassword)); err != nil {
		writeJSON(w, http.StatusUnauthorized, errBody(errors.New("current password is wrong")))
		return
	}
	newHash, err := bcrypt.GenerateFromPassword([]byte(in.NewPassword), bcrypt.DefaultCost)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	newName := username
	if in.NewUsername != "" {
		newName = in.NewUsername
	}
	if _, err := s.db.Exec(
		`UPDATE admin_users SET username = ?, password_hash = ? WHERE id = ?`,
		newName, string(newHash), adminID,
	); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	// Drop every OTHER session for this admin so a stolen cookie is
	// immediately invalidated; keep the current one so the UI doesn't
	// bounce the user to login.
	if c, err := r.Cookie(SessionCookieName); err == nil {
		if tok, _, ok := cookieToken(c.Value); ok {
			_, _ = s.db.Exec(
				`DELETE FROM sessions WHERE admin_id = ? AND token != ?`,
				adminID, tok)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "username": newName})
}

// handleRotatePrefix mints a new random URL prefix and writes panel.yaml.
// The current session survives (cookie is Path=/), but future links
// under the old prefix will 404. Frontend must reload with the new
// value; we return it so it can redirect.
func (s *Server) handleRotatePrefix(w http.ResponseWriter, _ *http.Request) {
	cfg, err := Load()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	next, err := randHex(4)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	cfg.URLPrefix = "/" + next
	if err := Save(cfg); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"url_prefix":       cfg.URLPrefix,
		"requires_restart": true,
	})
}

// cookieToken splits "<token>.<hmac>" back into its parts.
func cookieToken(v string) (token, sig string, ok bool) {
	for i := 0; i < len(v); i++ {
		if v[i] == '.' {
			return v[:i], v[i+1:], true
		}
	}
	return "", "", false
}
