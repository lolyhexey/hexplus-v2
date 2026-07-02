package xray

// daemon.go: control the hexplus-xray.service unit and coordinate
// config.json regen + reload.
//
// systemd owns the process lifecycle; this file provides the panel
// with a thin API. Callers hand us the DB and we:
//   1. Regenerate config.json from live DB state (generate.go)
//   2. Ask systemd to reload-or-restart the unit
//
// Reload path: xray-core honors SIGHUP for `routing` changes but
// inbound/outbound changes require a full restart. We always take the
// restart path via `systemctl try-reload-or-restart` — cheap enough
// (~sub-second) and correct for every kind of change.

import (
	"database/sql"
	"errors"
	"os/exec"
	"strings"
)

// Reload regenerates config.json from the DB, then asks systemd to
// bring xray up to that config. Returns the number of enabled inbounds
// written (0 is not an error — a config with only the internal API
// inbound is legal and lets stats polling keep working).
func Reload(sqldb *sql.DB) (int, error) {
	count, err := GenerateAndWrite(sqldb)
	if err != nil {
		return 0, err
	}
	if err := systemctlReload("hexplus-xray.service"); err != nil {
		return count, err
	}
	return count, nil
}

// systemctlReload prefers try-reload-or-restart so systemd falls back
// to a full restart when the unit doesn't declare a reload command
// (ours doesn't; we let it restart).
func systemctlReload(unit string) error {
	cmd := exec.Command("systemctl", "try-reload-or-restart", unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("systemctl not found in PATH")
		}
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return errors.New(msg)
	}
	return nil
}
