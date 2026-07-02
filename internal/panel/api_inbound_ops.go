package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_inbound_ops.go: reset-traffic + export + import for inbounds.
// Kept separate from the CRUD file so future bulk operations have an
// obvious home.

func (s *Server) registerInboundOpRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/inbounds/{id}/reset-traffic", s.auth.RequireSession(s.handleInboundReset))
	mux.HandleFunc("POST /api/inbounds/reset-all-traffic", s.auth.RequireSession(s.handleInboundResetAll))
	mux.HandleFunc("GET /api/inbounds/export", s.auth.RequireSession(s.handleInboundExport))
	mux.HandleFunc("POST /api/inbounds/import", s.auth.RequireSession(s.handleInboundImport))
}

// handleInboundReset — zeros the inbound's cumulative counters AND
// every attached client's used_bytes, and (best-effort) resets each
// client's xray-side counter so the next stats poll doesn't re-inflate.
func (s *Server) handleInboundReset(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if err := resetInboundTraffic(s, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "ok": true})
}

// handleInboundResetAll — same but iterated over every inbound. Kept
// atomic-per-row (not one big transaction) so a partial failure still
// resets everything else.
func (s *Server) handleInboundResetAll(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`SELECT id FROM inbounds`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err == nil {
			ids = append(ids, id)
		}
	}
	rows.Close()

	failed := map[string]string{}
	for _, id := range ids {
		if err := resetInboundTraffic(s, id); err != nil {
			failed[itoa64(id)] = err.Error()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"count":  len(ids) - len(failed),
		"failed": failed,
	})
}

func resetInboundTraffic(s *Server, id int64) error {
	// Pull every client email so we can also reset xray counters.
	rows, err := s.db.Query(`SELECT email FROM clients WHERE inbound_id = ?`, id)
	if err != nil {
		return err
	}
	var emails []string
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err == nil && e != "" {
			emails = append(emails, e)
		}
	}
	rows.Close()

	now := time.Now().Unix()
	if _, err := s.db.Exec(
		`UPDATE inbounds SET total_up = 0, total_down = 0, updated_at = ? WHERE id = ?`,
		now, id); err != nil {
		return err
	}
	if _, err := s.db.Exec(
		`UPDATE clients SET used_bytes = 0, updated_at = ? WHERE inbound_id = ?`,
		now, id); err != nil {
		return err
	}
	for _, e := range emails {
		_ = xray.ResetUserCounter(e)
	}
	return nil
}

// ── Export / Import ────────────────────────────────────────────────

// ExportEnvelope is the shape returned to the caller. Version + panel
// let a future format bump be detected without breaking older exports.
type ExportEnvelope struct {
	Version     int             `json:"version"`
	Panel       string          `json:"panel"`
	GeneratedAt int64           `json:"generated_at"`
	Inbounds    []ExportInbound `json:"inbounds"`
}

type ExportInbound struct {
	Tag      string          `json:"tag"`
	Protocol string          `json:"protocol"`
	Listen   string          `json:"listen"`
	Port     int             `json:"port"`
	Settings json.RawMessage `json:"settings"`
	Stream   json.RawMessage `json:"stream"`
	Sniffing bool            `json:"sniffing"`
	Enabled  bool            `json:"enabled"`
	Remark   string          `json:"remark"`
	Clients  []ExportClient  `json:"clients"`
}

type ExportClient struct {
	Email      string `json:"email"`
	Protocol   string `json:"protocol"`
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
	Key        string `json:"key,omitempty"`
	QuotaBytes int64  `json:"quota_bytes"`
	IPLimit    int    `json:"ip_limit"`
	ExpiresAt  int64  `json:"expires_at"`
	Enabled    bool   `json:"enabled"`
}

func (s *Server) handleInboundExport(w http.ResponseWriter, _ *http.Request) {
	rows, err := s.db.Query(`
		SELECT id, tag, protocol, listen, port, settings, stream,
		       sniffing, enabled, remark FROM inbounds ORDER BY id
	`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()

	env := ExportEnvelope{
		Version:     1,
		Panel:       "hexplus",
		GeneratedAt: time.Now().Unix(),
	}
	for rows.Next() {
		var (
			id                                 int64
			tag, protocol, listen, remark      string
			port                               int
			settings, stream                   string
			sniffInt, enabledInt               int
		)
		if err := rows.Scan(&id, &tag, &protocol, &listen, &port,
			&settings, &stream, &sniffInt, &enabledInt, &remark); err != nil {
			continue
		}
		ex := ExportInbound{
			Tag: tag, Protocol: protocol, Listen: listen, Port: port,
			Settings: json.RawMessage(settings), Stream: json.RawMessage(stream),
			Sniffing: sniffInt != 0, Enabled: enabledInt != 0, Remark: remark,
		}
		// Attach clients.
		crows, err := s.db.Query(`
			SELECT email, protocol, uuid, password, shared_key,
			       quota_bytes, ip_limit, expires_at, enabled
			FROM clients WHERE inbound_id = ? ORDER BY id
		`, id)
		if err == nil {
			for crows.Next() {
				var c ExportClient
				var enInt int
				if err := crows.Scan(&c.Email, &c.Protocol, &c.UUID, &c.Password, &c.Key,
					&c.QuotaBytes, &c.IPLimit, &c.ExpiresAt, &enInt); err == nil {
					c.Enabled = enInt != 0
					ex.Clients = append(ex.Clients, c)
				}
			}
			crows.Close()
		}
		env.Inbounds = append(env.Inbounds, ex)
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", `attachment; filename="hexplus-inbounds.json"`)
	_ = json.NewEncoder(w).Encode(env)
}

// handleInboundImport accepts an ExportEnvelope + a "mode" query:
//
//	?mode=merge (default) — skip inbounds whose tag already exists.
//	?mode=replace         — delete existing inbound with same tag, then insert.
//	?mode=skip            — same as merge; explicit.
//
// Every mutation is best-effort per-row so a broken entry in the middle
// doesn't tank the whole batch. Xray is reloaded once at the end.
func (s *Server) handleInboundImport(w http.ResponseWriter, r *http.Request) {
	mode := r.URL.Query().Get("mode")
	if mode == "" {
		mode = "merge"
	}
	if mode != "merge" && mode != "replace" && mode != "skip" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New(`mode must be "merge" | "replace" | "skip"`)))
		return
	}
	var env ExportEnvelope
	if err := json.NewDecoder(r.Body).Decode(&env); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	inserted, skipped, failed := 0, 0, map[string]string{}
	now := time.Now().Unix()

	for _, in := range env.Inbounds {
		var existingID int64
		_ = s.db.QueryRow(`SELECT id FROM inbounds WHERE tag = ?`, in.Tag).Scan(&existingID)
		if existingID != 0 {
			if mode == "replace" {
				if _, err := s.db.Exec(`DELETE FROM inbounds WHERE id = ?`, existingID); err != nil {
					failed[in.Tag] = err.Error()
					continue
				}
			} else {
				skipped++
				continue
			}
		}
		res, err := s.db.Exec(`
			INSERT INTO inbounds (node_id, tag, protocol, listen, port, settings, stream,
			                     sniffing, enabled, remark, created_at, updated_at)
			VALUES (1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		`, in.Tag, in.Protocol, orString(in.Listen, "0.0.0.0"), in.Port,
			jsonOr(in.Settings, "{}"), jsonOr(in.Stream, "{}"),
			boolInt(in.Sniffing), boolInt(in.Enabled), in.Remark, now, now)
		if err != nil {
			failed[in.Tag] = err.Error()
			continue
		}
		id, _ := res.LastInsertId()
		for _, c := range in.Clients {
			enInt := boolInt(c.Enabled)
			if _, err := s.db.Exec(`
				INSERT INTO clients (inbound_id, node_id, email, protocol,
				                   uuid, password, shared_key, quota_bytes,
				                   ip_limit, expires_at, enabled, sub_token,
				                   created_at, updated_at)
				VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, '', ?, ?)
			`, id, c.Email, c.Protocol, c.UUID, c.Password, c.Key,
				c.QuotaBytes, c.IPLimit, c.ExpiresAt, enInt, now, now); err != nil {
				// per-client failures are logged but don't sink the inbound
				failed[in.Tag+"/"+c.Email] = err.Error()
			}
		}
		inserted++
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"inserted": inserted, "skipped": skipped, "failed": failed,
			"reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"inserted": inserted, "skipped": skipped, "failed": failed,
	})
}

func itoa64(n int64) string {
	if n == 0 { return "0" }
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func orString(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

func jsonOr(v json.RawMessage, fallback string) string {
	if len(v) == 0 || string(v) == "null" {
		return fallback
	}
	return string(v)
}
