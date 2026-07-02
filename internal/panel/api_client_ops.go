package panel

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_client_ops.go: bulk-friendly client operations beyond CRUD —
// toggle enabled, reset used_bytes, extend expiry. Also the bulk
// endpoint that runs one of those against a list of client IDs.
//
// Every write path here regenerates xray's config (state visible to
// xray changed) and reloads the daemon. Errors from the reload land in
// a 202 Accepted body so the frontend can render a partial-success
// notice without treating the DB write as lost.

func (s *Server) registerClientOpRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/clients/{cid}/toggle", s.auth.RequireSession(s.handleClientToggle))
	mux.HandleFunc("POST /api/clients/{cid}/reset", s.auth.RequireSession(s.handleClientReset))
	mux.HandleFunc("POST /api/clients/{cid}/extend", s.auth.RequireSession(s.handleClientExtend))
	mux.HandleFunc("POST /api/clients/bulk", s.auth.RequireSession(s.handleClientBulk))
}

// handleClientToggle: POST /api/clients/{cid}/toggle → flip enabled.
// Idempotent given the current state; response returns the new value.
func (s *Server) handleClientToggle(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	res, err := s.db.Exec(`UPDATE clients SET enabled = 1 - enabled, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), cid)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if n, _ := res.RowsAffected(); n == 0 {
		writeJSON(w, http.StatusNotFound, errBody(errors.New("client not found")))
		return
	}
	var enabled int
	_ = s.db.QueryRow(`SELECT enabled FROM clients WHERE id = ?`, cid).Scan(&enabled)
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": cid, "enabled": enabled != 0, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": cid, "enabled": enabled != 0})
}

// handleClientReset: POST /api/clients/{cid}/reset → used_bytes back
// to zero. Xray's own counter also needs a reset — sent through the
// stats API subprocess so ongoing polls don't re-inflate the counter.
func (s *Server) handleClientReset(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var email string
	if err := s.db.QueryRow(`SELECT email FROM clients WHERE id = ?`, cid).Scan(&email); err != nil {
		writeJSON(w, http.StatusNotFound, errBody(errors.New("client not found")))
		return
	}
	if _, err := s.db.Exec(`UPDATE clients SET used_bytes = 0, updated_at = ? WHERE id = ?`,
		time.Now().Unix(), cid); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	// Best-effort reset the xray-side counter too. Failure here is
	// tolerated — worst case the next poll re-adds what we just
	// cleared; a subsequent reset will catch up.
	_ = xray.ResetUserCounter(email)
	writeJSON(w, http.StatusOK, map[string]any{"id": cid, "used_bytes": 0})
}

// handleClientExtend: POST /api/clients/{cid}/extend?days=N
// Adds N days to expires_at. If the current expiry is in the past (or
// zero), the extension is anchored to "now" so a lapsed account gets a
// fresh N-day window rather than an already-expired one.
func (s *Server) handleClientExtend(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	daysStr := r.URL.Query().Get("days")
	days, err := strconv.Atoi(daysStr)
	if err != nil || days == 0 {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("days must be a non-zero integer")))
		return
	}
	if err := extendClientExpiry(s, cid, days); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	var expiresAt int64
	_ = s.db.QueryRow(`SELECT expires_at FROM clients WHERE id = ?`, cid).Scan(&expiresAt)
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": cid, "expires_at": expiresAt, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": cid, "expires_at": expiresAt})
}

// extendClientExpiry is factored out so both the /extend endpoint and
// bulk operations share the "extend anchored to now if expired" logic.
func extendClientExpiry(s *Server, cid int64, days int) error {
	var current int64
	if err := s.db.QueryRow(`SELECT expires_at FROM clients WHERE id = ?`, cid).Scan(&current); err != nil {
		return err
	}
	now := time.Now().Unix()
	base := current
	if base < now {
		base = now
	}
	next := base + int64(days)*86400
	_, err := s.db.Exec(`UPDATE clients SET expires_at = ?, updated_at = ? WHERE id = ?`,
		next, now, cid)
	return err
}

// BulkOp is the POST /api/clients/bulk body shape.
type BulkOp struct {
	Op      string  `json:"op"`      // "toggle" | "reset" | "extend" | "delete"
	IDs     []int64 `json:"ids"`
	Days    int     `json:"days"`    // for op=extend
	Enabled *bool   `json:"enabled"` // for op=toggle when caller wants absolute set instead of flip
}

// handleClientBulk applies one operation to every ID. Partial failures
// are per-row and don't halt the batch; the response lists successes
// and errors so the frontend can render a summary.
func (s *Server) handleClientBulk(w http.ResponseWriter, r *http.Request) {
	var op BulkOp
	if err := json.NewDecoder(r.Body).Decode(&op); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if len(op.IDs) == 0 {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("ids required")))
		return
	}
	if op.Op == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("op required")))
		return
	}
	success := []int64{}
	failed := map[string]string{}
	now := time.Now().Unix()

	for _, id := range op.IDs {
		var err error
		switch op.Op {
		case "toggle":
			if op.Enabled != nil {
				v := 0
				if *op.Enabled {
					v = 1
				}
				_, err = s.db.Exec(`UPDATE clients SET enabled = ?, updated_at = ? WHERE id = ?`, v, now, id)
			} else {
				_, err = s.db.Exec(`UPDATE clients SET enabled = 1 - enabled, updated_at = ? WHERE id = ?`, now, id)
			}
		case "reset":
			var email string
			if e := s.db.QueryRow(`SELECT email FROM clients WHERE id = ?`, id).Scan(&email); e != nil {
				err = e
				break
			}
			_, err = s.db.Exec(`UPDATE clients SET used_bytes = 0, updated_at = ? WHERE id = ?`, now, id)
			if err == nil {
				_ = xray.ResetUserCounter(email)
			}
		case "extend":
			if op.Days == 0 {
				err = errors.New("days required for op=extend")
				break
			}
			err = extendClientExpiry(s, id, op.Days)
		case "delete":
			_, err = s.db.Exec(`DELETE FROM clients WHERE id = ?`, id)
		default:
			err = fmt.Errorf("unknown op %q", op.Op)
		}
		if err != nil {
			failed[strconv.FormatInt(id, 10)] = err.Error()
			continue
		}
		success = append(success, id)
	}
	// Reload once at the end so a bulk of 100 clients doesn't trigger
	// 100 separate systemctl calls.
	reloadErr := ""
	if _, err := xray.Reload(s.db); err != nil {
		reloadErr = err.Error()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":      success,
		"failed":       failed,
		"reload_error": reloadErr,
	})
}
