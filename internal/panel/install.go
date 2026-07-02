package panel

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/lolyhexey/hexplus/internal/panel/db"
	"github.com/lolyhexey/hexplus/internal/paths"
	"github.com/lolyhexey/hexplus/internal/service"
)

// InstallOptions drives panel install from the menu / CLI. Port and
// AdminUsername default when zero/empty; AdminPassword is generated
// when Empty and returned in InstallResult so the menu can display it.
type InstallOptions struct {
	Port          int
	AdminUsername string
	AdminPassword string
	// KeepDB, when true, skips wiping /var/lib/hexplus/panel/panel.db
	// on reinstall. Ignored on first install (there's nothing to keep).
	KeepDB bool
}

// InstallResult is what the caller renders back to the user.
type InstallResult struct {
	Port          int
	URLPrefix     string
	AdminUsername string
	AdminPassword string // generated on empty input; otherwise passthrough
	WroteConfig   bool
	WroteUnits    bool
}

// Install bootstraps the panel: writes panel.yaml with a fresh URL
// prefix + session secret, opens (creating if needed) the DB, seeds
// the admin account, and asks the service package to write the systemd
// units. Does NOT enable/start the units — the caller (menu) decides.
func Install(opts InstallOptions) (InstallResult, error) {
	if opts.Port <= 0 {
		opts.Port = DefaultPort
	}
	if opts.AdminUsername == "" {
		opts.AdminUsername = "admin"
	}
	if opts.AdminPassword == "" {
		pw, err := randomPassword(18)
		if err != nil {
			return InstallResult{}, fmt.Errorf("gen admin password: %w", err)
		}
		opts.AdminPassword = pw
	}

	// Wipe DB unless caller asked to keep it. Missing file → no error.
	if !opts.KeepDB {
		if err := os.Remove(paths.PanelDBPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return InstallResult{}, fmt.Errorf("wipe %s: %w", paths.PanelDBPath, err)
		}
	}

	cfg, err := NewDefault(opts.Port)
	if err != nil {
		return InstallResult{}, err
	}
	if err := Save(cfg); err != nil {
		return InstallResult{}, fmt.Errorf("save config: %w", err)
	}

	// Ensure the state dirs exist with tight perms before we let the
	// server touch them.
	for _, d := range []string{paths.PanelStateDir, paths.XrayStateDir} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			return InstallResult{}, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}

	sqldb, err := db.Open()
	if err != nil {
		return InstallResult{}, fmt.Errorf("open db: %w", err)
	}
	if err := SetAdminPassword(sqldb, opts.AdminUsername, opts.AdminPassword); err != nil {
		_ = sqldb.Close()
		return InstallResult{}, fmt.Errorf("seed admin: %w", err)
	}
	_ = sqldb.Close()

	// Install both services: extract the xray-core binary (panel has
	// no separate binary) and write both systemd units. Idempotent —
	// re-running on a healthy install is a no-op.
	panelSvc, ok := service.ByName("panel")
	if !ok {
		return InstallResult{}, errors.New("service registry missing 'panel'")
	}
	xraySvc, ok := service.ByName("xray")
	if !ok {
		return InstallResult{}, errors.New("service registry missing 'xray'")
	}
	if _, err := service.InstallService(xraySvc); err != nil {
		return InstallResult{}, fmt.Errorf("install xray: %w", err)
	}
	if _, err := service.InstallService(panelSvc); err != nil {
		return InstallResult{}, fmt.Errorf("install panel: %w", err)
	}

	return InstallResult{
		Port:          cfg.Port,
		URLPrefix:     cfg.URLPrefix,
		AdminUsername: opts.AdminUsername,
		AdminPassword: opts.AdminPassword,
		WroteConfig:   true,
		WroteUnits:    true,
	}, nil
}

// Uninstall reverses Install. Stops the units (best-effort), removes
// them, deletes panel.yaml, and (when wipeDB=true) drops the SQLite DB.
// Xray extracted binary at paths.LibDir/xray is left in place — it was
// planted by the main install, not by panel install.
func Uninstall(wipeDB bool) error {
	// Stop first so an in-flight write doesn't race the file remove.
	for _, name := range []string{"panel", "xray"} {
		if svc, ok := service.ByName(name); ok {
			_ = service.Stop(svc)
			_ = service.Disable(svc)
			_ = service.RemoveUnitFor(svc)
		}
	}
	if err := os.Remove(ConfigPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", ConfigPath, err)
	}
	if wipeDB {
		if err := os.Remove(paths.PanelDBPath); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", paths.PanelDBPath, err)
		}
		// Best-effort clear the empty state dir.
		_ = os.Remove(paths.PanelStateDir)
	}
	// Xray state (config.json + reality keys) is scoped to the panel; wipe
	// unconditionally on uninstall so a reinstall starts clean without
	// dragging along stale inbound configs.
	_ = os.RemoveAll(paths.XrayStateDir)
	return nil
}

// IsInstalled reports whether panel.yaml exists on disk.
func IsInstalled() bool {
	_, err := os.Stat(ConfigPath)
	return err == nil
}

// ShowInfo returns the port + URL prefix + admin username currently on
// disk. The admin password is not stored in plaintext; menu code must
// call ResetAdminPassword to change it.
func ShowInfo() (Config, string, error) {
	cfg, err := Load()
	if err != nil {
		return Config{}, "", err
	}
	sqldb, err := db.Open()
	if err != nil {
		return cfg, "", err
	}
	defer sqldb.Close()
	var username string
	err = sqldb.QueryRow(`SELECT username FROM admin_users ORDER BY id LIMIT 1`).Scan(&username)
	if err != nil {
		// No admin yet — install must not have finished.
		return cfg, "", nil
	}
	return cfg, username, nil
}

// ResetAdminPassword changes the admin password. Menu action. Returns
// the new password so the caller can display it. When plaintext is
// empty a random one is generated.
func ResetAdminPassword(username, plaintext string) (string, error) {
	if username == "" {
		username = "admin"
	}
	if plaintext == "" {
		pw, err := randomPassword(18)
		if err != nil {
			return "", err
		}
		plaintext = pw
	}
	sqldb, err := db.Open()
	if err != nil {
		return "", err
	}
	defer sqldb.Close()
	if err := SetAdminPassword(sqldb, username, plaintext); err != nil {
		return "", err
	}
	return plaintext, nil
}

// ChangePort rewrites panel.yaml with a new port. Caller must restart
// the panel unit for the change to take effect.
func ChangePort(newPort int) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if newPort <= 0 || newPort > 65535 {
		return fmt.Errorf("port %d out of range", newPort)
	}
	cfg.Port = newPort
	return Save(cfg)
}

// randomPassword generates n bytes of entropy and returns URL-safe
// base64 without padding — human-copyable, ~1.33*n chars.
func randomPassword(n int) (string, error) {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}

// ConfigDir is the directory containing ConfigPath. Handy for menu code
// showing the user where files live.
func ConfigDir() string {
	return filepath.Dir(ConfigPath)
}
