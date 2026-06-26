// Package user owns the HEXPLUS-side user lifecycle: create the system
// account, sign a client cert against the OpenVPN PKI, persist per-user
// metadata (limit, expiry, created-at) that doesn't live in /etc/passwd,
// and produce the .ovpn export the seller hands to their customer.
//
// Storage layout:
//
//   /etc/passwd, /etc/shadow             - system user, owned by useradd
//   /etc/openvpn/pki/clients/<name>.crt  - signed client cert (issued by CA)
//   /etc/openvpn/pki/clients/<name>.key  - client private key (chmod 600)
//   /var/lib/hexplus/users.json          - HEXPLUS metadata DB
//   /root/<name>.ovpn                    - the export drop (default)
//
// HEXPLUS v1 stored the plaintext password in /etc/SSHPlus/senha/<name>
// so the operator could read it back later. We deliberately drop that
// behavior: the password is shown once at create time, then only the
// hash lives in /etc/shadow. If the customer forgets, run 'hexplus user
// passwd <name>'.

package user

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// DBPath is where the metadata file lives.
const DBPath = paths.StateDir + "/users.json"

// Record is one row in users.json. Subset of what `hexplus user add`
// stores; the system bits (UID, GECOS) come straight from /etc/passwd.
type Record struct {
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at,omitzero"`
	// Limit caps simultaneous connections. Zero means "no enforced cap";
	// HEXPLUS v1 stored this for a verifatt cron to act on - v2 will
	// surface it the same way once we port that flow.
	Limit int `json:"limit,omitempty"`
}

// DB is the on-disk metadata. JSON for forward-compat: easy to add
// fields without versioning headaches.
type DB struct {
	Users map[string]Record `json:"users"`
}

// Load reads users.json. Missing file returns an empty DB (no error)
// so first-run callers don't need to special-case the file's absence.
func Load() (*DB, error) {
	data, err := os.ReadFile(DBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DB{Users: map[string]Record{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", DBPath, err)
	}
	db := &DB{}
	if err := json.Unmarshal(data, db); err != nil {
		return nil, fmt.Errorf("parse %s: %w", DBPath, err)
	}
	if db.Users == nil {
		db.Users = map[string]Record{}
	}
	return db, nil
}

// Save persists the DB atomically. Parent dir is created if missing.
func (d *DB) Save() error {
	if err := os.MkdirAll(filepath.Dir(DBPath), 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", filepath.Dir(DBPath), err)
	}
	data, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := DBPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, DBPath); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, DBPath, err)
	}
	return nil
}

// All returns users sorted by name. Stable order makes `hexplus user list`
// reproducible across runs.
func (d *DB) All() []Record {
	out := make([]Record, 0, len(d.Users))
	for _, r := range d.Users {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}
