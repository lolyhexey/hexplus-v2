package panel

import (
	"database/sql"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// subscription.go: GET /sub/{token} serves the share link(s) for one
// client. Only reachable if panel.Config.SubscriptionEnabled is true
// AND the client's row has a matching sub_token AND the client is
// still enabled and unexpired.
//
// Output shapes controlled by ?type=:
//   type=v2ray (default): base64 of newline-joined share URIs
//   type=plain:           raw newline-joined share URIs (no base64)
//   type=clash / sing-box: reserved — not implemented yet; return 501
//
// Auth: no session cookie required. The 24-char hex token is the
// credential. Any client with the token has full access to the
// underlying share link, so it must be treated like a password.

// handleSubscription is registered on the OUTSIDE of the URL prefix in
// server.go so end-user apps don't need to know the admin path.
func (s *Server) handleSubscription(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.SubscriptionEnabled {
		http.NotFound(w, r)
		return
	}
	token := strings.TrimPrefix(r.URL.Path, "/sub/")
	if token == "" || strings.Contains(token, "/") {
		http.NotFound(w, r)
		return
	}

	client, inbound, streamJSON, err := s.subscriptionLookup(token)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	host := subscriptionHost(r)
	format := strings.ToLower(r.URL.Query().Get("type"))

	// Clash / Sing-box branch: build structured config from the same
	// (inbound, client) pair. Not every protocol is expressible in
	// those formats — we return 200 with an empty proxies block so
	// the client app doesn't retry-storm.
	if format == "clash" || format == "clash-meta" {
		w.Header().Set("Content-Type", "text/yaml; charset=utf-8")
		w.Header().Set("Profile-Title", "HEXPLUS Panel")
		w.Header().Set("Profile-Update-Interval", "24")
		var proxies []clashProxy
		if p := buildClashProxy(inbound, streamJSON, client, host); p != nil {
			proxies = []clashProxy{p}
		}
		_, _ = w.Write([]byte(renderClashYAML(proxies, "HEXPLUS Panel — "+client.Email)))
		return
	}
	if format == "sing-box" || format == "singbox" {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		var outbounds []map[string]any
		if o := buildSingBoxOutbound(inbound, streamJSON, client, host); o != nil {
			outbounds = []map[string]any{o}
		}
		_, _ = w.Write([]byte(renderSingBoxJSON(outbounds)))
		return
	}

	link, err := xray.ShareForClient(inbound, streamJSON, client, host, "")
	if err != nil || link == "" {
		// Nothing shareable (e.g. WireGuard) — still return 200 so the
		// client app doesn't retry-storm, just serve an empty body.
		writePlainSub(w, r, []string{})
		return
	}
	writePlainSub(w, r, []string{link})
}

// subscriptionLookup reads the client + inbound + stream JSON for a
// given sub_token. Returns sql.ErrNoRows if the token doesn't match a
// live, enabled, unexpired row so callers can 404 uniformly.
func (s *Server) subscriptionLookup(token string) (xray.Client, xray.InboundFromDB, []byte, error) {
	var (
		client       xray.Client
		inbound      xray.InboundFromDB
		streamStr    string
		settingsStr  string
		expUnix      int64
		enInt        int
		crtUnix      int64
	)
	row := s.db.QueryRow(`
		SELECT c.id, c.inbound_id, c.email, c.protocol, c.uuid, c.password, c.shared_key,
		       c.quota_bytes, c.used_bytes, c.ip_limit, c.expires_at, c.enabled, c.created_at,
		       i.id, i.tag, i.protocol, i.port, i.settings, i.stream
		FROM clients c
		JOIN inbounds i ON i.id = c.inbound_id
		WHERE c.sub_token = ? AND c.enabled = 1 AND i.enabled = 1
	`, token)
	if err := row.Scan(&client.ID, &client.InboundID, &client.Email, &client.Protocol,
		&client.UUID, &client.Password, &client.Key,
		&client.QuotaBytes, &client.UsedBytes, &client.IPLimit, &expUnix, &enInt, &crtUnix,
		&inbound.ID, &inbound.Tag, &inbound.Protocol, &inbound.Port, &settingsStr, &streamStr); err != nil {
		return client, inbound, nil, err
	}
	if expUnix > 0 && time.Now().Unix() > expUnix {
		return client, inbound, nil, sql.ErrNoRows
	}
	if client.QuotaBytes > 0 && client.UsedBytes >= client.QuotaBytes {
		return client, inbound, nil, sql.ErrNoRows
	}
	if expUnix > 0 {
		client.ExpiresAt = time.Unix(expUnix, 0)
	}
	client.CreatedAt = time.Unix(crtUnix, 0)
	client.Enabled = enInt != 0
	inbound.Settings = []byte(settingsStr)
	return client, inbound, []byte(streamStr), nil
}

// writePlainSub emits the link list in the shape ?type= asked for.
// Default is base64 (the v2ray/v2rayN convention); plain returns the
// raw newline list. clash/sing-box render structured formats via
// helpers in sub_formats.go.
func writePlainSub(w http.ResponseWriter, r *http.Request, links []string) {
	body := strings.Join(links, "\n")
	switch strings.ToLower(r.URL.Query().Get("type")) {
	case "", "v2ray":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		w.Header().Set("Subscription-Userinfo", "") // clients ignore an empty header safely
		w.Header().Set("Profile-Update-Interval", "24")
		w.Header().Set("Profile-Title", "HEXPLUS Panel")
		_, _ = w.Write([]byte(base64.StdEncoding.EncodeToString([]byte(body))))
	case "plain":
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte(body))
	default:
		http.Error(w, "type not implemented", http.StatusNotImplemented)
	}
}

// subscriptionHost picks the server address that goes into the share
// link's host field. Preference order: X-Forwarded-Host header (behind
// reverse proxy), Host header (direct hit), stripped RemoteAddr as last
// resort.
func subscriptionHost(r *http.Request) string {
	if v := r.Header.Get("X-Forwarded-Host"); v != "" {
		if host, _, ok := strings.Cut(v, ":"); ok {
			return host
		}
		return v
	}
	if r.Host != "" {
		if host, _, ok := strings.Cut(r.Host, ":"); ok {
			return host
		}
		return r.Host
	}
	if host, _, ok := strings.Cut(r.RemoteAddr, ":"); ok {
		return host
	}
	return r.RemoteAddr
}

// Unused error sentinel kept so a future "token expired" 410 response
// can be added without an import churn.
var _ = errors.New
