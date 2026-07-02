// Package panel wires the V2Ray/Xray web panel: HTTP server, auth,
// session store, SQLite-backed inbound/client management, subscription
// endpoint. See internal/panel/db for the schema and internal/xray for
// the xray-core wrapper the panel talks to.
package panel

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// ConfigPath is where the panel's yaml config file lives. Written once
// during `hexplus panel install` and read on every `panel serve` start.
var ConfigPath = filepath.Join(paths.PanelStateDir, "panel.yaml")

// Config is the runtime configuration for the panel. Kept intentionally
// small: everything mutable (protocols, clients, inbounds, admin
// password) lives in the SQLite DB. This file only exists to bootstrap
// the process before the DB is open.
type Config struct {
	// ListenAddr is the bind address for the HTTP server. Default 0.0.0.0.
	ListenAddr string `yaml:"listen_addr"`

	// Port is the TCP port the panel listens on. Chosen at install time
	// (default 2053) and mutable via the hexplus install menu.
	Port int `yaml:"port"`

	// URLPrefix is the random path segment that gates the panel behind
	// a "who told you the URL" barrier. Rendered as a leading path
	// component (e.g. "/xY9kQ2"). Empty means root path.
	URLPrefix string `yaml:"url_prefix"`

	// SessionSecret is 32 random bytes hex-encoded — the HMAC key used
	// to sign session cookies. Rotating it (via re-install) logs
	// everyone out; we treat that as acceptable for a rare event.
	SessionSecret string `yaml:"session_secret"`

	// SubscriptionEnabled toggles the /sub/{token} endpoint. On by
	// default; a user who never issues subscription tokens is fine
	// leaving it on because handlers 404 without a valid token anyway.
	SubscriptionEnabled bool `yaml:"subscription_enabled"`
}

// DefaultPort is the port picked when no override is passed. Matches
// 3x-ui's default so users migrating in feel at home.
const DefaultPort = 2053

// Load reads ConfigPath and returns the parsed Config. Missing file is
// an error — the caller should have run `hexplus panel install` first.
func Load() (Config, error) {
	data, err := os.ReadFile(ConfigPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, fmt.Errorf("panel config missing at %s (run: hexplus panel install)", ConfigPath)
		}
		return Config{}, fmt.Errorf("read %s: %w", ConfigPath, err)
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, fmt.Errorf("parse %s: %w", ConfigPath, err)
	}
	if err := cfg.validate(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes cfg to ConfigPath atomically (temp+rename) so a crash
// mid-write can't leave the panel unable to boot.
func Save(cfg Config) error {
	if err := os.MkdirAll(filepath.Dir(ConfigPath), 0o750); err != nil {
		return fmt.Errorf("mkdir panel state: %w", err)
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}
	tmp := ConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return err
	}
	if err := os.Rename(tmp, ConfigPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// NewDefault builds a Config with a freshly-generated URL prefix and
// session secret, ready to be Save()d during install.
func NewDefault(port int) (Config, error) {
	if port <= 0 {
		port = DefaultPort
	}
	prefix, err := randHex(4) // 8 hex chars = ~48 bits of entropy, plenty for obscurity
	if err != nil {
		return Config{}, fmt.Errorf("gen url prefix: %w", err)
	}
	secret, err := randHex(32) // 64 hex chars = 256-bit HMAC key
	if err != nil {
		return Config{}, fmt.Errorf("gen session secret: %w", err)
	}
	return Config{
		ListenAddr:          "0.0.0.0",
		Port:                port,
		URLPrefix:           "/" + prefix,
		SessionSecret:       secret,
		SubscriptionEnabled: true,
	}, nil
}

func (c Config) validate() error {
	if c.Port <= 0 || c.Port > 65535 {
		return fmt.Errorf("panel port %d out of range 1-65535", c.Port)
	}
	if c.SessionSecret == "" {
		return errors.New("session_secret empty (regenerate config with 'hexplus panel install --force')")
	}
	return nil
}

// randHex returns 2*n hex chars from crypto/rand.
func randHex(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
