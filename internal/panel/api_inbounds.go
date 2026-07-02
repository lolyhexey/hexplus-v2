package panel

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_inbounds.go: CRUD REST for /api/inbounds.
//
// Every mutation (create/update/delete) regenerates xray's config.json
// and reloads the daemon so the change is live by the time the HTTP
// response returns.

// InboundView is the JSON shape returned to the frontend. Settings
// and Stream stay as opaque strings from the panel's POV — the xray
// package interprets them.
type InboundView struct {
	ID        int64           `json:"id"`
	Tag       string          `json:"tag"`
	Protocol  string          `json:"protocol"`
	Listen    string          `json:"listen"`
	Port      int             `json:"port"`
	Settings  json.RawMessage `json:"settings"`
	Stream    json.RawMessage `json:"stream"`
	Sniffing  bool            `json:"sniffing"`
	Enabled   bool            `json:"enabled"`
	Remark    string          `json:"remark"`
	TotalUp   int64           `json:"total_up"`
	TotalDown int64           `json:"total_down"`
	CreatedAt int64           `json:"created_at"`
	UpdatedAt int64           `json:"updated_at"`
}

// InboundInput is the shape POST /api/inbounds and PUT /api/inbounds/{id}
// accept from the frontend.
type InboundInput struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Listen   string          `json:"listen"`
	Port     int             `json:"port"`
	Settings json.RawMessage `json:"settings"`
	Stream   json.RawMessage `json:"stream"`
	Sniffing *bool           `json:"sniffing"`
	Enabled  *bool           `json:"enabled"`
	Remark   string          `json:"remark"`
}

func (s *Server) registerInboundRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inbounds", s.auth.RequireSession(s.handleInboundList))
	mux.HandleFunc("POST /api/inbounds", s.auth.RequireSession(s.handleInboundCreate))
	mux.HandleFunc("GET /api/inbounds/{id}", s.auth.RequireSession(s.handleInboundGet))
	mux.HandleFunc("PUT /api/inbounds/{id}", s.auth.RequireSession(s.handleInboundUpdate))
	mux.HandleFunc("DELETE /api/inbounds/{id}", s.auth.RequireSession(s.handleInboundDelete))
}

func (s *Server) handleInboundList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, tag, protocol, listen, port, settings, stream,
		       sniffing, enabled, remark, total_up, total_down,
		       created_at, updated_at
		FROM inbounds ORDER BY id
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()

	out := []InboundView{}
	for rows.Next() {
		v, err := scanInbound(rows)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err))
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleInboundGet(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	row := s.db.QueryRow(`
		SELECT id, tag, protocol, listen, port, settings, stream,
		       sniffing, enabled, remark, total_up, total_down,
		       created_at, updated_at
		FROM inbounds WHERE id = ?
	`, id)
	v, err := scanInbound(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody(err))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) handleInboundCreate(w http.ResponseWriter, r *http.Request) {
	var in InboundInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if err := validateInbound(in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if len(in.Settings) == 0 {
		in.Settings = json.RawMessage(`{}`)
	}
	if len(in.Stream) == 0 {
		in.Stream = json.RawMessage(`{}`)
	}
	sniff := true
	if in.Sniffing != nil {
		sniff = *in.Sniffing
	}
	enabled := true
	if in.Enabled != nil {
		enabled = *in.Enabled
	}
	if in.Listen == "" {
		in.Listen = "0.0.0.0"
	}

	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO inbounds (node_id, tag, protocol, listen, port, settings, stream,
		                     sniffing, enabled, remark, created_at, updated_at)
		VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, in.Tag, in.Protocol, in.Listen, in.Port,
		string(in.Settings), string(in.Stream),
		boolInt(sniff), boolInt(enabled), in.Remark, now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	id, _ := res.LastInsertId()
	if _, err := xray.Reload(s.db); err != nil {
		// Persisted but xray reload failed — surface via 202 so the
		// frontend can show a warning without treating the CRUD as
		// failed. The row IS in DB either way.
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": id, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id})
}

func (s *Server) handleInboundUpdate(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var in InboundInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if err := validateInbound(in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	sniff := 1
	if in.Sniffing != nil && !*in.Sniffing {
		sniff = 0
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	if _, err := s.db.Exec(`
		UPDATE inbounds SET
			tag=?, protocol=?, listen=?, port=?, settings=?, stream=?,
			sniffing=?, enabled=?, remark=?, updated_at=?
		WHERE id = ?
	`, in.Tag, in.Protocol, orDefault(in.Listen, "0.0.0.0"), in.Port,
		string(in.Settings), string(in.Stream),
		sniff, enabled, in.Remark, now, id); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": id, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

func (s *Server) handleInboundDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := s.db.Exec(`DELETE FROM inbounds WHERE id = ?`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": id, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// scanInbound is the shared column reader used by both list-loop
// (*sql.Rows) and single-row (*sql.Row) query paths.
func scanInbound(r rowScanner) (InboundView, error) {
	var (
		v              InboundView
		settings, str  string
		sniffInt, enInt int
	)
	if err := r.Scan(&v.ID, &v.Tag, &v.Protocol, &v.Listen, &v.Port,
		&settings, &str, &sniffInt, &enInt, &v.Remark,
		&v.TotalUp, &v.TotalDown, &v.CreatedAt, &v.UpdatedAt); err != nil {
		return v, err
	}
	v.Settings = json.RawMessage(settings)
	v.Stream = json.RawMessage(str)
	v.Sniffing = sniffInt != 0
	v.Enabled = enInt != 0
	return v, nil
}

// rowScanner is the common interface of *sql.Row and *sql.Rows for the
// Scan method — lets scanInbound serve both call sites.
type rowScanner interface {
	Scan(dest ...any) error
}

func validateInbound(in InboundInput) error {
	if in.Tag == "" {
		return errors.New("tag required")
	}
	if in.Protocol == "" {
		return errors.New("protocol required")
	}
	if in.Port <= 0 || in.Port > 65535 {
		return fmt.Errorf("port %d out of range", in.Port)
	}
	switch in.Protocol {
	case "vless", "vmess", "trojan", "shadowsocks", "hysteria2",
		"wireguard", "http", "socks", "dokodemo-door":
		// ok
	default:
		return fmt.Errorf("unsupported protocol %q", in.Protocol)
	}
	return nil
}

func pathID(r *http.Request, name string) (int64, error) {
	raw := r.PathValue(name)
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id <= 0 {
		return 0, fmt.Errorf("bad %s: %q", name, raw)
	}
	return id, nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func orDefault(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func errBody(err error) map[string]string {
	return map[string]string{"error": err.Error()}
}
