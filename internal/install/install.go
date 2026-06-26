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

// Result is what Install reports back to the CLI. Wrapper-only since v2.1:
// per-service install/uninstall lives in the service package and reports
// its own InstallResult.
type Result struct {
	SelfCopied    bool
	MarkerWritten bool
}

// IsInstalled returns true if the install marker exists. Cheap check used
// by main() to decide whether to run Install at startup.
func IsInstalled() bool {
	_, err := os.Stat(MarkerFile)
	return err == nil
}

// Install registers hexplus on this box without extracting any service
// binaries. The wrapper-only install matches HEXPLUS v1's mental model:
// `hexplus install` makes the menu available; individual services
// (openvpn / squid / dropbear) extract only when the operator picks
// "ติดตั้ง" inside the menu. The storage cost on a box that only ever
// uses Squid is ~16 MB (the wrapper) instead of ~25 MB (wrapper + all
// three binaries).
//
// Steps:
//  1. Create the lib + state dirs.
//  2. Copy self to /usr/local/bin/hexplus.
//  3. Drop the install marker.
//
// Per-service extract + systemd unit + bootstrap config are owned by
// service.InstallService(svc).
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

// Uninstall reverses Install. Per-service binaries get cleaned up
// through service.UninstallService(svc) loops; this function owns
// only the wrapper-side state. Configs under /etc are preserved on
// purpose so the operator can reinstall without losing their
// squid.conf / server.conf edits.
func Uninstall() error {
	if os.Geteuid() != 0 {
		return errors.New("uninstall requires root; rerun under sudo")
	}

	// Walk known services and best-effort uninstall each one. Failures
	// are logged but don't stop the overall uninstall - we want the
	// wrapper to come down even if a half-broken systemd setup blocks
	// individual service teardown.
	for _, svc := range service.All() {
		_, _ = service.UninstallService(svc)
	}

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
