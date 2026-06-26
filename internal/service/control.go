// control.go: wrap systemctl for the user-facing service subcommand.
//
// Why shell out instead of speaking dbus directly: systemctl handles all
// the corner cases (failed -> active transitions, --no-block, unit
// reloads, etc.) and is the lingua franca every linux sysadmin already
// debugs against. We get robust behavior for ~50 LOC and the user can
// always reach for systemctl manually when our wrapper isn't enough.

package service

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// State summarizes one service's current systemd state. Empty fields mean
// systemd didn't report that property (e.g. MainPID is 0 when inactive).
type State struct {
	Service     Service // descriptor copy for the caller
	UnitExists  bool    // false when /etc/systemd/system/<unit> is missing
	ActiveState string  // active, inactive, failed, activating, ...
	SubState    string  // running, dead, exited, ...
	MainPID     string  // string so 0/unset can be distinguished
	Enabled     bool    // true if `systemctl is-enabled` says enabled or alias
}

// Start calls `systemctl start hexplus-<name>`. Returns nil on success.
func Start(svc Service) error     { return run("start", svc.UnitName) }
func Stop(svc Service) error      { return run("stop", svc.UnitName) }
func Restart(svc Service) error   { return run("restart", svc.UnitName) }
func Enable(svc Service) error    { return run("enable", svc.UnitName) }
func Disable(svc Service) error   { return run("disable", svc.UnitName) }
func TryReload(svc Service) error { return run("try-reload-or-restart", svc.UnitName) }

// Status queries systemctl for one service's current state. UnitExists
// will be false (and no err returned) if the unit file isn't installed.
// Caller can distinguish "not installed" from "installed but inactive"
// via that flag.
//
// In containers/chroots where dbus isn't reachable, systemctl show exits
// non-zero. We fall back to checking the unit file on disk so the user
// still gets a meaningful answer ("installed but state unknown") instead
// of an opaque exit-1.
func Status(svc Service) (State, error) {
	st := State{Service: svc}

	// `systemctl show` returns key=value lines for every requested property.
	// Robust even when the unit doesn't exist: missing units get empty values
	// but the command still exits 0 - except when dbus is unavailable, in
	// which case the whole call exits 1.
	out, err := exec.Command(
		"systemctl",
		"show",
		"-p", "LoadState",
		"-p", "ActiveState",
		"-p", "SubState",
		"-p", "MainPID",
		"--value",
		"--no-pager",
		svc.UnitName,
	).Output()
	if err != nil {
		// Either systemctl is missing (no systemd box) or dbus is
		// unreachable (typical in containers). In both cases fall
		// back to disk inspection: the unit file at SystemdUnitDir
		// tells us whether install ever ran here.
		if _, statErr := os.Stat(filepath.Join(SystemdUnitDir, svc.UnitName)); statErr == nil {
			st.UnitExists = true
			st.ActiveState = "unknown"
			if errors.Is(err, exec.ErrNotFound) {
				st.SubState = "no-systemctl"
			} else {
				st.SubState = "no-dbus"
			}
		}
		return st, nil
	}

	// --value emits each property on its own line in the request order.
	lines := strings.Split(strings.TrimSpace(string(out)), "\n")
	for len(lines) < 4 {
		lines = append(lines, "")
	}
	loadState := strings.TrimSpace(lines[0])
	st.ActiveState = strings.TrimSpace(lines[1])
	st.SubState = strings.TrimSpace(lines[2])
	st.MainPID = strings.TrimSpace(lines[3])
	st.UnitExists = loadState != "" && loadState != "not-found" && loadState != "masked"

	// is-enabled returns non-zero for disabled units but the stdout is
	// still meaningful ("disabled", "static", "enabled", "alias").
	enOut, _ := exec.Command("systemctl", "is-enabled", svc.UnitName).Output()
	enabled := strings.TrimSpace(string(enOut))
	st.Enabled = enabled == "enabled" || enabled == "alias" || enabled == "enabled-runtime"

	return st, nil
}

// StatusAll runs Status for every Service in All() in stable order.
// Returns the first hard error encountered (e.g. systemctl missing);
// individual UnitExists/ActiveState fields on each State carry the
// per-service result.
func StatusAll() ([]State, error) {
	all := All()
	out := make([]State, 0, len(all))
	for _, s := range all {
		st, err := Status(s)
		if err != nil {
			return out, err
		}
		out = append(out, st)
	}
	return out, nil
}

// errSystemctlMissing is the canonical error for "this box has no
// systemctl in PATH"; callers can unwrap and present a focused message.
var errSystemctlMissing = errors.New("systemctl not found in PATH; is systemd installed?")

func run(verb, unit string) error {
	cmd := exec.Command("systemctl", verb, unit)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errSystemctlMissing
		}
		// systemctl's stderr is the most useful thing to surface; we
		// collapse stdout+stderr via CombinedOutput so a failure path
		// like "Unit hexplus-openvpn.service not found" reaches the user.
		return fmt.Errorf("systemctl %s %s: %w: %s",
			verb, unit, err, strings.TrimSpace(string(out)))
	}
	return nil
}
