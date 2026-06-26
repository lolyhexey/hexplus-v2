// config.go: per-proxy configuration and the JSON-backed store under
// /var/lib/hexplus/proxies.json.
//
// Why a separate DB instead of one file per proxy: there are usually
// 3-5 proxies on a real deployment and listing them needs one read.
// JSON also gives us forward-compat for adding new fields without a
// schema migration step.

package proxy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// DBPath is where the proxies.json lives.
const DBPath = paths.StateDir + "/proxies.json"

// Config is one proxy instance's settings.
type Config struct {
	// Name is the short identifier (ssh, ws, openvpn, custom-foo).
	// Maps directly to the systemd unit name: hexplus-proxy-<name>.service.
	Name string `json:"name"`

	// Port the proxy listens on. Validated by NewHandler.
	Port int `json:"port"`

	// DefaultHost is what the proxy tunnels to when the client doesn't
	// send X-Real-Host. Format: "host:port".
	DefaultHost string `json:"default_host"`

	// StatusCode is the digits we send in the spoof status line
	// (101, 200, 400, 520, ...). Stored as string to allow non-standard
	// codes operators have used in the wild.
	StatusCode string `json:"status_code"`

	// StatusMsg is the text after the status code. Stores literal '\r\n'
	// for multi-header responses; the handler expands at runtime.
	StatusMsg string `json:"status_msg"`

	// AllowedHosts is the prefix whitelist for X-Real-Host values.
	// Empty -> default set ('127.0.0.1', '0.0.0.0', 'localhost').
	AllowedHosts []string `json:"allowed_hosts,omitempty"`
}

// DB is the on-disk store.
type DB struct {
	Proxies map[string]Config `json:"proxies"`
}

// nameRE rejects names that would either be invalid systemd unit
// fragments or collide with our existing units. systemd allows
// [a-zA-Z0-9:_.-]+ in unit names; we additionally bar leading dots
// and the reserved 'openvpn'/'squid'/'dropbear' suffixes that map
// to the other hexplus units.
var nameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// reservedNames are short names already taken by the service package.
// Adding a proxy with one of these would produce a hexplus-proxy-openvpn
// unit that's confusingly close to hexplus-openvpn.
var reservedNames = map[string]bool{
	"openvpn":  true,
	"squid":    true,
	"dropbear": true,
}

// ValidateName enforces the rules above so the CLI can fail before any
// file gets written.
func ValidateName(name string) error {
	if len(name) < 2 || len(name) > 32 {
		return errors.New("name must be 2-32 chars")
	}
	if !nameRE.MatchString(name) {
		return errors.New("name must start with a letter, then letters/digits/_/-")
	}
	if reservedNames[name] {
		return fmt.Errorf("name %q clashes with a built-in service", name)
	}
	return nil
}

// Load reads proxies.json. Missing file is not an error - we hand back
// an empty DB so callers don't need to special-case first-run.
func Load() (*DB, error) {
	data, err := os.ReadFile(DBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &DB{Proxies: map[string]Config{}}, nil
		}
		return nil, fmt.Errorf("read %s: %w", DBPath, err)
	}
	db := &DB{}
	if err := json.Unmarshal(data, db); err != nil {
		return nil, fmt.Errorf("parse %s: %w", DBPath, err)
	}
	if db.Proxies == nil {
		db.Proxies = map[string]Config{}
	}
	return db, nil
}

// Save writes proxies.json atomically.
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

// All returns the proxies sorted by name for stable CLI rendering.
func (d *DB) All() []Config {
	out := make([]Config, 0, len(d.Proxies))
	for _, c := range d.Proxies {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// UnitName is the systemd unit name a Config maps to.
func (c Config) UnitName() string {
	return "hexplus-proxy-" + c.Name + ".service"
}
