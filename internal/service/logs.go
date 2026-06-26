// logs.go: surface a service's systemd-journal logs.
//
// We shell out to journalctl rather than parsing the journal binary
// format directly. The format is documented and stable but the parser
// would add a few hundred lines for no real benefit - journalctl
// already handles filtering, follow, and tail in ways the user
// expects.

package service

import (
	"errors"
	"os"
	"os/exec"
	"strconv"
)

// LogsOptions controls the `hexplus logs` invocation.
type LogsOptions struct {
	// Follow streams new entries as they arrive (-f).
	Follow bool
	// Tail caps the number of past entries shown (-n N). Zero means
	// don't pass -n at all; journalctl's default is the full journal.
	Tail int
}

// StreamLogs runs `journalctl -u <unit>` with the given options and
// connects its stdout/stderr to the calling process. The function
// blocks until journalctl exits (immediately for one-shot, on signal
// for --follow).
//
// Returns errJournalctlMissing when journalctl isn't installed so the
// CLI can render a focused message instead of "exec: not found".
func StreamLogs(svc Service, opts LogsOptions) error {
	args := []string{"-u", svc.UnitName, "--no-pager"}
	if opts.Tail > 0 {
		args = append(args, "-n", strconv.Itoa(opts.Tail))
	}
	if opts.Follow {
		args = append(args, "-f")
	}
	cmd := exec.Command("journalctl", args...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errJournalctlMissing
		}
		return err
	}
	return nil
}

var errJournalctlMissing = errors.New("journalctl not found in PATH; is systemd-journald installed?")
