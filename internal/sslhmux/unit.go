// unit.go: writes and removes the systemd unit for hexplus-sslhmux.service.

package sslhmux

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/template"

	"github.com/lolyhexey/hexplus/internal/paths"
)

const systemdUnitDir = "/etc/systemd/system"

const unitTemplate = `[Unit]
Description=HEXPLUS SSLH Multiplex (port {{.Port}})
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
ExecStart={{.SelfPath}} sslhmux run
Restart=on-failure
RestartSec=5s
LimitNOFILE=65536
ProtectSystem=full
ProtectHome=true
NoNewPrivileges=true

[Install]
WantedBy=multi-user.target
`

// WriteUnit renders the systemd unit for cfg and calls daemon-reload.
// Uses an atomic write (tmp → rename) to avoid partial unit files.
func WriteUnit(cfg Config) error {
	tmpl, err := template.New("sslhmux-unit").Parse(unitTemplate)
	if err != nil {
		return fmt.Errorf("parse template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, struct {
		Port     int
		SelfPath string
	}{
		Port:     cfg.Port,
		SelfPath: paths.SelfPath,
	}); err != nil {
		return fmt.Errorf("render template: %w", err)
	}

	if err := os.MkdirAll(systemdUnitDir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", systemdUnitDir, err)
	}
	dest := filepath.Join(systemdUnitDir, UnitName)
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s → %s: %w", tmp, dest, err)
	}
	if err := muxDaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload: %w", err)
	}
	return nil
}

// RemoveUnit removes the unit file and calls daemon-reload.
// A missing unit file is tolerated (idempotent).
func RemoveUnit() error {
	dest := filepath.Join(systemdUnitDir, UnitName)
	if err := os.Remove(dest); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", dest, err)
	}
	_ = muxDaemonReload()
	return nil
}

func muxDaemonReload() error {
	out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}
