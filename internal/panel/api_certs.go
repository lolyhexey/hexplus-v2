package panel

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_certs.go: TLS cert lifecycle for xray inbounds.
//
// Two flows:
//   POST /api/certs/manual   — upload cert + key PEM bodies
//   POST /api/certs/acme     — trigger Let's Encrypt HTTP-01 issuance
//
// The DB row lives in certs (added in migration v2). The panel doesn't
// auto-rewrite inbound.settings.certificates — that stays the frontend's
// responsibility, which lets the operator choose which inbound uses
// which cert.

type CertView struct {
	ID            int64  `json:"id"`
	Domain        string `json:"domain"`
	Source        string `json:"source"`
	CertPath      string `json:"cert_path"`
	KeyPath       string `json:"key_path"`
	NotAfter      int64  `json:"not_after"`
	LastRenewedAt int64  `json:"last_renewed_at"`
	Remark        string `json:"remark"`
	CreatedAt     int64  `json:"created_at"`
}

func (s *Server) registerCertRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/certs", s.auth.RequireSession(s.handleCertList))
	mux.HandleFunc("POST /api/certs/manual", s.auth.RequireSession(s.handleCertManual))
	mux.HandleFunc("POST /api/certs/acme", s.auth.RequireSession(s.handleCertACME))
	mux.HandleFunc("DELETE /api/certs/{id}", s.auth.RequireSession(s.handleCertDelete))
	mux.HandleFunc("POST /api/certs/{id}/renew", s.auth.RequireSession(s.handleCertRenew))
}

func (s *Server) handleCertList(w http.ResponseWriter, r *http.Request) {
	rows, err := s.db.Query(`SELECT id, domain, source, cert_path, key_path, not_after, last_renewed_at, remark, created_at FROM certs ORDER BY id`)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()
	out := []CertView{}
	for rows.Next() {
		var v CertView
		if err := rows.Scan(&v.ID, &v.Domain, &v.Source, &v.CertPath, &v.KeyPath, &v.NotAfter, &v.LastRenewedAt, &v.Remark, &v.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err))
			return
		}
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

// ManualCertInput carries base64-encoded PEM bytes. base64 keeps the
// JSON body a single line even for multiline PEMs — cheaper than
// escaping newlines client-side.
type ManualCertInput struct {
	Domain  string `json:"domain"`
	CertPEM string `json:"cert_pem"` // base64
	KeyPEM  string `json:"key_pem"`  // base64
	Remark  string `json:"remark"`
}

func (s *Server) handleCertManual(w http.ResponseWriter, r *http.Request) {
	var in ManualCertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if in.Domain == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("domain required")))
		return
	}
	certPEM, err := decodePEMField(in.CertPEM)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	keyPEM, err := decodePEMField(in.KeyPEM)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	paths, notAfter, err := xray.SaveManualCert(in.Domain, certPEM, keyPEM)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	id, err := s.upsertCert(in.Domain, "manual", paths, notAfter, in.Remark)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "not_after": notAfter.Unix()})
}

// ACMECertInput drives the Let's Encrypt flow. Email is used as the
// registration contact so LE can mail expiry reminders.
type ACMECertInput struct {
	Domain       string `json:"domain"`
	ContactEmail string `json:"contact_email"`
	Remark       string `json:"remark"`
}

func (s *Server) handleCertACME(w http.ResponseWriter, r *http.Request) {
	var in ACMECertInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if in.Domain == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("domain required")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	paths, notAfter, err := xray.AcquireACMECert(ctx, in.Domain, in.ContactEmail)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err))
		return
	}
	id, err := s.upsertCert(in.Domain, "acme", paths, notAfter, in.Remark)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": id, "not_after": notAfter.Unix()})
}

func (s *Server) handleCertDelete(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var domain string
	if err := s.db.QueryRow(`SELECT domain FROM certs WHERE id = ?`, id).Scan(&domain); err != nil {
		writeJSON(w, http.StatusNotFound, errBody(err))
		return
	}
	_ = xray.RemoveCert(domain)
	if _, err := s.db.Exec(`DELETE FROM certs WHERE id = ?`, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id})
}

// handleCertRenew re-runs the ACME flow for an existing "acme"-sourced
// cert. Manual certs 400 — an operator wanting to renew a manual cert
// re-uploads via /certs/manual.
func (s *Server) handleCertRenew(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var (
		domain string
		source string
	)
	if err := s.db.QueryRow(`SELECT domain, source FROM certs WHERE id = ?`, id).Scan(&domain, &source); err != nil {
		writeJSON(w, http.StatusNotFound, errBody(err))
		return
	}
	if source != "acme" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("renew only supported for source=acme")))
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 90*time.Second)
	defer cancel()
	paths, notAfter, err := xray.AcquireACMECert(ctx, domain, "")
	if err != nil {
		writeJSON(w, http.StatusBadGateway, errBody(err))
		return
	}
	now := time.Now().Unix()
	if _, err := s.db.Exec(`UPDATE certs SET cert_path=?, key_path=?, not_after=?, last_renewed_at=?, updated_at=? WHERE id=?`,
		paths.CertFile, paths.KeyFile, notAfter.Unix(), now, now, id); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "not_after": notAfter.Unix()})
}

// upsertCert inserts (or updates on-conflict) the certs row after a
// successful cert acquisition. Returns the row id.
func (s *Server) upsertCert(domain, source string, paths xray.CertPaths, notAfter time.Time, remark string) (int64, error) {
	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO certs (domain, source, cert_path, key_path, not_after, last_renewed_at, remark, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(domain) DO UPDATE SET
			source          = excluded.source,
			cert_path       = excluded.cert_path,
			key_path        = excluded.key_path,
			not_after       = excluded.not_after,
			last_renewed_at = excluded.last_renewed_at,
			remark          = excluded.remark,
			updated_at      = excluded.updated_at
	`, domain, source, paths.CertFile, paths.KeyFile, notAfter.Unix(), now, remark, now, now)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	if id == 0 {
		// ON CONFLICT UPDATE doesn't populate LastInsertId; look up.
		_ = s.db.QueryRow(`SELECT id FROM certs WHERE domain = ?`, domain).Scan(&id)
	}
	return id, nil
}

// decodePEMField accepts either raw PEM (starts with "-----BEGIN") or
// base64-wrapped PEM. Keeps the API convenient for both curl testing
// and browser uploads.
func decodePEMField(field string) ([]byte, error) {
	if field == "" {
		return nil, errors.New("empty PEM field")
	}
	if len(field) >= 5 && field[:5] == "-----" {
		return []byte(field), nil
	}
	raw, err := base64Decode(field)
	if err != nil {
		return nil, err
	}
	return raw, nil
}
