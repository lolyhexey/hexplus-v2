// Package install owns the first-run setup that turns a downloaded hexplus
// binary into a running system installation.
//
// Layout HEXPLUS v2 lays down on disk:
//
//	/usr/local/bin/hexplus            symlink (or copy) of the running binary
//	/usr/local/lib/hexplus/openvpn    extracted from the embed tree
//	/usr/local/lib/hexplus/squid      extracted from the embed tree
//	/usr/local/lib/hexplus/dropbearmulti
//	/var/lib/hexplus/installed        marker file (presence => first-run done)
//
// Design rules:
//   - install() is idempotent: calling it twice on the same machine produces
//     the same end state without re-extracting binaries whose size already
//     matches.
//   - Every path is rooted at the absolute constants below so 'sudo'd /
//     suid-y invocations can't surprise us with a different cwd.
//   - We do NOT touch /etc/openvpn, /etc/squid, etc. Those are state dirs
//     owned by the services themselves; the menu code that wires services
//     up writes them.
package install

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/extract"
	"github.com/lolyhexey/hexplus/internal/paths"
	"github.com/lolyhexey/hexplus/internal/service"
)

// Re-export the legacy names so callers that already use install.LibDir etc.
// keep compiling while we migrate them to the paths package directly.
const (
	BinDir     = paths.BinDir
	LibDir     = paths.LibDir
	StateDir   = paths.StateDir
	MarkerFile = paths.MarkerFile
	SelfPath   = paths.SelfPath
)

// Result is what Install reports back to the CLI for human consumption.
type Result struct {
	BinariesWritten    []string
	BinariesSkipped    []string
	SymlinksCreated    []string
	UnitsWritten       []string
	UnitsSkipped       []string
	UnitsReloadWarning error // non-fatal; surfaced in the CLI output
	ConfigsWritten     []string
	ConfigsSkipped     []string
	SelfCopied         bool
	MarkerWritten      bool
}

// IsInstalled returns true if the install marker exists. Cheap check used
// by main() to decide whether to run Install at startup.
func IsInstalled() bool {
	_, err := os.Stat(MarkerFile)
	return err == nil
}

// Install lays down the binaries, copies self to /usr/local/bin/hexplus,
// and drops the marker file. Returns details for the caller to print.
func Install() (Result, error) {
	var res Result

	if os.Geteuid() != 0 {
		return res, errors.New("install requires root; rerun under sudo")
	}

	if err := os.MkdirAll(LibDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir %s: %w", LibDir, err)
	}
	if err := os.MkdirAll(StateDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir %s: %w", StateDir, err)
	}

	exres, err := extract.All(assets.Binaries(), LibDir)
	if err != nil {
		return res, fmt.Errorf("extract: %w", err)
	}
	res.BinariesWritten = exres.Written
	res.BinariesSkipped = exres.Skipped

	// dropbearmulti dispatches on argv[0]; systemd ExecStart picks up the
	// basename of the binary path. Symlink dropbear / dropbearkey / scp
	// against the multi binary so the matching subcommand runs.
	if err := service.CreateDropbearSymlinks(); err != nil {
		return res, fmt.Errorf("symlink dropbear helpers: %w", err)
	}
	res.SymlinksCreated = []string{
		LibDir + "/dropbear",
		LibDir + "/dropbearkey",
		LibDir + "/scp",
	}

	// systemd units. Best-effort on non-systemd boxes: write the files,
	// skip daemon-reload if systemctl isn't there. The Install itself
	// still succeeds so the binaries are usable manually.
	if service.SystemdAvailable() {
		wr, err := service.WriteUnits()
		if err != nil {
			return res, fmt.Errorf("write systemd units: %w", err)
		}
		res.UnitsWritten = wr.Written
		res.UnitsSkipped = wr.Skipped
		res.UnitsReloadWarning = wr.ReloadWarning
	}

	// Default configs: write only if missing so we never clobber the
	// operator's edits. /etc/openvpn is mkdir'd but no server.conf is
	// written - that needs PKI material (P2.x `hexplus pki init`).
	br, err := service.BootstrapConfigs()
	if err != nil {
		return res, fmt.Errorf("bootstrap configs: %w", err)
	}
	res.ConfigsWritten = br.Written
	res.ConfigsSkipped = br.Skipped

	if err := installSelf(); err != nil {
		return res, fmt.Errorf("install self: %w", err)
	}
	res.SelfCopied = true

	if err := os.WriteFile(MarkerFile, []byte("ok\n"), 0o644); err != nil {
		return res, fmt.Errorf("write marker: %w", err)
	}
	res.MarkerWritten = true

	return res, nil
}

// Uninstall reverses Install: removes /usr/local/lib/hexplus, the marker,
// and (if it points to our embedded build) the self copy. Service state
// dirs are left intact - users may want to keep configs/users across
// a reinstall.
func Uninstall() error {
	if os.Geteuid() != 0 {
		return errors.New("uninstall requires root; rerun under sudo")
	}

	// systemd units first - if anything is currently running off them, we
	// want systemd to forget the unit before the binary disappears so a
	// stopped service doesn't keep referencing a deleted path.
	if _, err := service.RemoveUnits(); err != nil {
		return fmt.Errorf("remove systemd units: %w", err)
	}

	// Self copy first so a partial uninstall still leaves the cwd binary
	// runnable for retries.
	if err := os.Remove(SelfPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", SelfPath, err)
	}
	if err := os.RemoveAll(LibDir); err != nil {
		return fmt.Errorf("remove %s: %w", LibDir, err)
	}
	if err := os.Remove(MarkerFile); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove %s: %w", MarkerFile, err)
	}
	return nil
}

// installSelf copies the currently-running executable to /usr/local/bin/hexplus.
// If the path we read from /proc/self/exe is already the destination, we
// skip the copy so 'install' run from /usr/local/bin doesn't fight itself.
func installSelf() error {
	src, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	// Resolve symlinks so a wrapper symlink doesn't confuse the equality check.
	if resolved, err := filepath.EvalSymlinks(src); err == nil {
		src = resolved
	}
	if src == SelfPath {
		return nil
	}

	in, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("open self %s: %w", src, err)
	}
	defer in.Close()

	tmp := SelfPath + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return fmt.Errorf("copy: %w", err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, SelfPath); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, SelfPath, err)
	}
	return nil
}
