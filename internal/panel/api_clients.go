package panel

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// api_clients.go: CRUD REST for /api/inbounds/{id}/clients plus the
// share-link + QR endpoints under each client.
//
// Convention: per-protocol identity (uuid / password / shared_key) is
// generated server-side when the caller sends an empty string. That
// keeps the frontend simple — it doesn't have to know which protocol
// wants which credential shape.

type ClientView struct {
	ID         int64  `json:"id"`
	InboundID  int64  `json:"inbound_id"`
	Email      string `json:"email"`
	Protocol   string `json:"protocol"`
	UUID       string `json:"uuid,omitempty"`
	Password   string `json:"password,omitempty"`
	Key        string `json:"key,omitempty"`
	QuotaBytes int64  `json:"quota_bytes"`
	UsedBytes  int64  `json:"used_bytes"`
	IPLimit    int    `json:"ip_limit"`
	ExpiresAt  int64  `json:"expires_at"`
	Enabled    bool   `json:"enabled"`
	SubToken   string `json:"sub_token,omitempty"`
	CreatedAt  int64  `json:"created_at"`
}

type ClientInput struct {
	Email      string `json:"email"`
	UUID       string `json:"uuid"`
	Password   string `json:"password"`
	Key        string `json:"key"`
	QuotaBytes int64  `json:"quota_bytes"`
	IPLimit    int    `json:"ip_limit"`
	ExpiresAt  int64  `json:"expires_at"`
	Enabled    *bool  `json:"enabled"`
}

func (s *Server) registerClientRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/inbounds/{id}/clients", s.auth.RequireSession(s.handleClientList))
	mux.HandleFunc("POST /api/inbounds/{id}/clients", s.auth.RequireSession(s.handleClientCreate))
	mux.HandleFunc("PUT /api/clients/{cid}", s.auth.RequireSession(s.handleClientUpdate))
	mux.HandleFunc("DELETE /api/clients/{cid}", s.auth.RequireSession(s.handleClientDelete))
	mux.HandleFunc("GET /api/clients/{cid}/link", s.auth.RequireSession(s.handleClientLink))
	mux.HandleFunc("GET /api/clients/{cid}/qr", s.auth.RequireSession(s.handleClientQR))
}

func (s *Server) handleClientList(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	rows, err := s.db.Query(`
		SELECT id, inbound_id, email, protocol, uuid, password, shared_key,
		       quota_bytes, used_bytes, ip_limit, expires_at, enabled,
		       sub_token, created_at
		FROM clients WHERE inbound_id = ? ORDER BY id
	`, id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	defer rows.Close()

	out := []ClientView{}
	for rows.Next() {
		var (
			v     ClientView
			enInt int
		)
		if err := rows.Scan(&v.ID, &v.InboundID, &v.Email, &v.Protocol,
			&v.UUID, &v.Password, &v.Key,
			&v.QuotaBytes, &v.UsedBytes, &v.IPLimit, &v.ExpiresAt,
			&enInt, &v.SubToken, &v.CreatedAt); err != nil {
			writeJSON(w, http.StatusInternalServerError, errBody(err))
			return
		}
		v.Enabled = enInt != 0
		out = append(out, v)
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) handleClientCreate(w http.ResponseWriter, r *http.Request) {
	inboundID, err := pathID(r, "id")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	// Learn the inbound's protocol so we know which identity field to fill.
	var protocol string
	if err := s.db.QueryRow(`SELECT protocol FROM inbounds WHERE id = ?`, inboundID).Scan(&protocol); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody(errors.New("inbound not found")))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}

	var in ClientInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if in.Email == "" {
		writeJSON(w, http.StatusBadRequest, errBody(errors.New("email required")))
		return
	}
	if err := fillIdentity(&in, protocol); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	subToken, err := newSubToken()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	enabled := 1
	if in.Enabled != nil && !*in.Enabled {
		enabled = 0
	}
	now := time.Now().Unix()
	res, err := s.db.Exec(`
		INSERT INTO clients (inbound_id, node_id, email, protocol, uuid, password, shared_key,
		                    quota_bytes, ip_limit, expires_at, enabled, sub_token,
		                    created_at, updated_at)
		VALUES (?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`, inboundID, in.Email, protocol, in.UUID, in.Password, in.Key,
		in.QuotaBytes, in.IPLimit, in.ExpiresAt, enabled, subToken, now, now)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	cid, _ := res.LastInsertId()
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": cid, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"id": cid})
}

func (s *Server) handleClientUpdate(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	var in ClientInput
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
		UPDATE clients SET
			email=?, uuid=?, password=?, shared_key=?,
			quota_bytes=?, ip_limit=?, expires_at=?, enabled=?, updated_at=?
		WHERE id = ?
	`, in.Email, in.UUID, in.Password, in.Key,
		in.QuotaBytes, in.IPLimit, in.ExpiresAt, enabled, now, cid); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": cid, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": cid})
}

func (s *Server) handleClientDelete(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if _, err := s.db.Exec(`DELETE FROM clients WHERE id = ?`, cid); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	if _, err := xray.Reload(s.db); err != nil {
		writeJSON(w, http.StatusAccepted, map[string]any{
			"id": cid, "reload_error": err.Error(),
		})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": cid})
}

func (s *Server) handleClientLink(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	link, err := s.buildShareLink(cid, r.URL.Query().Get("address"))
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, errBody(err))
			return
		}
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"link": link})
}

func (s *Server) handleClientQR(w http.ResponseWriter, r *http.Request) {
	cid, err := pathID(r, "cid")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	link, err := s.buildShareLink(cid, r.URL.Query().Get("address"))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	png, err := xray.QRPNG(link, 320)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(png)
}

// buildShareLink is the shared code path for /link and /qr — reads the
// client + inbound rows and hands off to xray.ShareForClient.
func (s *Server) buildShareLink(cid int64, addressOverride string) (string, error) {
	var (
		client  xray.Client
		expUnix int64
		enInt   int
		crtUnix int64
	)
	row := s.db.QueryRow(`
		SELECT id, inbound_id, email, protocol, uuid, password, shared_key,
		       quota_bytes, used_bytes, ip_limit, expires_at, enabled, created_at
		FROM clients WHERE id = ?
	`, cid)
	if err := row.Scan(&client.ID, &client.InboundID, &client.Email, &client.Protocol,
		&client.UUID, &client.Password, &client.Key,
		&client.QuotaBytes, &client.UsedBytes, &client.IPLimit, &expUnix,
		&enInt, &crtUnix); err != nil {
		return "", err
	}
	if expUnix > 0 {
		client.ExpiresAt = time.Unix(expUnix, 0)
	}
	client.CreatedAt = time.Unix(crtUnix, 0)
	client.Enabled = enInt != 0

	var inbound xray.InboundFromDB
	var streamStr, settingsStr string
	row = s.db.QueryRow(`
		SELECT id, tag, protocol, port, settings, stream
		FROM inbounds WHERE id = ?
	`, client.InboundID)
	if err := row.Scan(&inbound.ID, &inbound.Tag, &inbound.Protocol,
		&inbound.Port, &settingsStr, &streamStr); err != nil {
		return "", err
	}
	inbound.Settings = []byte(settingsStr)

	address := addressOverride
	if address == "" {
		address = "your-server-address"
	}
	return xray.ShareForClient(inbound, []byte(streamStr), client, address, "")
}

// fillIdentity generates whichever credential the protocol needs when
// the caller left it blank. Callers can force a specific value by
// providing one in the input.
func fillIdentity(in *ClientInput, protocol string) error {
	switch protocol {
	case "vless", "vmess":
		if in.UUID == "" {
			in.UUID = xray.NewUUID()
		}
	case "trojan":
		if in.Password == "" {
			p, err := xray.NewTrojanPassword()
			if err != nil {
				return err
			}
			in.Password = p
		}
	case "shadowsocks":
		if in.Key == "" {
			k, err := xray.NewSSKey("aes-256-gcm")
			if err != nil {
				return err
			}
			in.Key = k
		}
	case "hysteria2":
		if in.Password == "" {
			p, err := xray.NewTrojanPassword() // same shape works
			if err != nil {
				return err
			}
			in.Password = p
		}
	case "wireguard":
		if in.Key == "" {
			return errors.New("wireguard: client public key (key) required")
		}
	case "http", "socks":
		if in.Password == "" {
			p, err := xray.NewTrojanPassword()
			if err != nil {
				return err
			}
			in.Password = p
		}
	case "dokodemo-door":
		// no credential needed
	default:
		return fmt.Errorf("unsupported protocol %q", protocol)
	}
	return nil
}

func newSubToken() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
