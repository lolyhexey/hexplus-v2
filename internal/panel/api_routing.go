package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_routing.go: routing_rules + outbounds CRUD. Every mutation
// regenerates xray config + reloads.
//
// The frontend edits routing rules as a flat list ordered by priority;
// this file translates that shape to xray's routing.rules[] array in
// generate.go.

// ---------- outbounds ----------

type OutboundView struct {
	ID        int64           `json:"id"`
	Tag       string          `json:"tag"`
	Protocol  string          `json:"protocol"`
	Settings  json.RawMessage `json:"settings"`
	Stream    json.RawMessage `json:"stream"`
	Remark    string          `json:"remark"`
	Enabled   bool            `json:"enabled"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

type OutboundInput struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Settings json.RawMessage `json:"settings"`
	Stream   json.RawMessage `json:"stream"`
	Remark   string          `json:"remark"`
	Enabled  *bool           `json:"enabled"`
}

func (s *Server) registerRoutingRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/outbounds", s.auth.RequireSession(s.handleOutboundList))
	mux.HandleFunc("POST /api/outbounds", s.auth.RequireSession(s.handleOutboundCreate))
	mux.HandleFunc("PUT /api/outbounds/{id}", s.auth.RequireSession(s.handleOutboundUpdate))
	mux.HandleFunc("DELETE /api/outbounds/{id}", s.auth.RequireSession(s.handleOutboundDelete))

	mux.HandleFunc("GET /api/routing/rules", s.auth.RequireSession(s.handleRuleList))
	mux.HandleFunc("POST /api/routing/rules", s.auth.RequireSession(s.handleRuleCreate))
	mux.HandleFunc("PUT /api/routing/rules/{id}", s.auth.RequireSession(s.handleRuleUpdate))
	mux.HandleFunc("DELETE /api/routing/rules/{id}", s.auth.RequireSession(s.handleRuleDelete))
	mux.HandleFunc("POST /api/routing/warp", s.auth.RequireSession(s.handleProvisionWARP))
}

func (s *Server) handleOutboundList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, tag, protocol, settings, stream, remark, enabled, created_at, updated_at FROM outbounds ORDER BY id`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()
	var out []OutboundView
	for rows.Next() {
		var (
			v      OutboundView
			set, str string
			enInt  int
		)
		if err := rows.Scan(&v.ID, &v.Tag, &v.Protocol, &set, &str, &v.Remark, &enInt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err))
			return
		}
		v.Settings = json.RawMessage(set)
		v.Stream = json.RawMessage(str)
		v.Enabled = enInt != 0
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleOutboundCreate(w http.ResponseWriter, r *http.Request) {
	var in OutboundInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if in.Tag == "" || in.Protocol == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("tag + protocol required")))
		return
	}
	if len(in.Settings) == 0 {
		in.Settings = json.RawMessage(`{}`)
	}
	if len(in.Stream) == 0 {
		in.Stream = json.RawMessage(`{}`)
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO outbounds (node_id, tag, protocol, settings, stream, remark, enabled, created_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Tag, in.Protocol, string(in.Settings), string(in.Stream), in.Remark, enabled, now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	id, _ := res.LastInsertId()
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleOutboundUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var in OutboundInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	if _, err := s.db.Exec(`
		UPDATE outbounds SET tag=?, protocol=?, settings=?, stream=?, remark=?, enabled=?, updated_at=?
		WHERE id = ?`, in.Tag, in.Protocol, string(in.Settings), string(in.Stream), in.Remark, enabled, now, id); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleOutboundDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := s.db.Exec(`DELETE FROM outbounds WHERE id = ?`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// ---------- routing rules ----------

type RuleView struct {
	ID          int64    `json:"id"`
	Priority    int      `json:"priority"`
	OutboundTag string   `json:"outbound_tag"`
	InboundTag  string   `json:"inbound_tag"`
	Domains     []string `json:"domains"`
	IPs         []string `json:"ips"`
	Protocols   []string `json:"protocols"`
	PortRange   string   `json:"port_range"`
	Remark      string   `json:"remark"`
	Enabled     bool     `json:"enabled"`
	CreatedAt   int64    `json:"created_at"`
	UpdatedAt   int64    `json:"updated_at"`
}

type RuleInput struct {
	Priority    int      `json:"priority"`
	OutboundTag string   `json:"outbound_tag"`
	InboundTag  string   `json:"inbound_tag"`
	Domains     []string `json:"domains"`
	IPs         []string `json:"ips"`
	Protocols   []string `json:"protocols"`
	PortRange   string   `json:"port_range"`
	Remark      string   `json:"remark"`
	Enabled     *bool    `json:"enabled"`
}

func (s *Server) handleRuleList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, priority, outbound_tag, inbound_tag, domains, ips, protocols, port_range, remark, enabled, created_at, updated_at FROM routing_rules ORDER BY priority, id`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()
	var out []RuleView
	for rows.Next() {
		var (
			v         RuleView
			domains, ips, protos string
			enInt     int
		)
		if err := rows.Scan(&v.ID, &v.Priority, &v.OutboundTag, &v.InboundTag, &domains, &ips, &protos, &v.PortRange, &v.Remark, &enInt, &v.CreatedAt, &v.UpdatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err))
			return
		}
		v.Domains = splitLines(domains)
		v.IPs = splitLines(ips)
		v.Protocols = splitLines(protos)
		v.Enabled = enInt != 0
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleRuleCreate(w http.ResponseWriter, r *http.Request) {
	var in RuleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if in.OutboundTag == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("outbound_tag required")))
		return
	}
	if in.Priority == 0 {
		in.Priority = 100
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO routing_rules (priority, outbound_tag, inbound_tag, domains, ips, protocols, port_range, remark, enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		in.Priority, in.OutboundTag, in.InboundTag,
		strings.Join(in.Domains, "\n"), strings.Join(in.IPs, "\n"), strings.Join(in.Protocols, "\n"),
		in.PortRange, in.Remark, enabled, now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	id, _ := res.LastInsertId()
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleRuleUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var in RuleInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	if _, err := s.db.Exec(`
		UPDATE routing_rules SET priority=?, outbound_tag=?, inbound_tag=?, domains=?, ips=?, protocols=?, port_range=?, remark=?, enabled=?, updated_at=?
		WHERE id = ?`,
		in.Priority, in.OutboundTag, in.InboundTag,
		strings.Join(in.Domains, "\n"), strings.Join(in.IPs, "\n"), strings.Join(in.Protocols, "\n"),
		in.PortRange, in.Remark, enabled, now, id); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleRuleDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := s.db.Exec(`DELETE FROM routing_rules WHERE id = ?`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleProvisionWARP creates a Cloudflare WARP outbound by generating
// a fresh WireGuard key pair, registering it with Cloudflare's WARP
// API, and inserting an outbound row that points at the WARP endpoint.
// Delegated to xray.ProvisionWARP so the network I/O sits under
// internal/xray with the other daemon-side code.
func (s *Server) handleProvisionWARP(w http.ResponseWriter, r *http.Request) {
	tag := "warp"
	if q := r.URL.Query().Get("tag"); q != "" {
		tag = q
	}
	settings, err := xray.ProvisionWARP()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err))
		return
	}
	raw, _ := json.Marshal(settings)
	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO outbounds (node_id, tag, protocol, settings, stream, remark, enabled, created_at, updated_at)
		VALUES (1, ?, 'wireguard', ?, '{}', 'Cloudflare WARP', 1, ?, ?)
		ON CONFLICT(tag) DO UPDATE SET
			settings=excluded.settings, remark=excluded.remark, updated_at=excluded.updated_at
	`, tag, string(raw), now, now)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	id, _ := res.LastInsertId()
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{"id": id, "reload_error": err.Error()})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "tag": tag})
}

// splitLines strips the newline-joined DB representation into a slice.
// Empty input → empty slice (never nil, so JSON encodes as [] not null).
func splitLines(s string) []string {
	if s == "" {
		return []string{}
	}
	out := []string{}
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}
