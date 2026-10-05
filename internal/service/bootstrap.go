// bootstrap.go: file helpers for the per-service lazy install.
//
// Scope decisions:
//   - Support files are written only if they're missing; an existing file
//     belongs to the operator, not to us.
//   - No squid.conf is laid down here. The menu's squidInstall writes the
//     one real config (buildSquidConf); a second, starter ACL set in this
//     package drifted from it once and shipped an open CONNECT relay. With
//     no squid.conf on disk, squid refuses to start, which fails closed.
//   - Dropbear's host keys are deliberately NOT generated here; the unit
//     passes `-R` to dropbear so it creates them lazily on first start.
//   - OpenVPN needs a CA + server cert + DH params, which is a separate
//     decision the operator makes via `hexplus pki init`. On a fresh box
//     without that, openvpn refuses to start with a clear "ca.crt not
//     found" - that's intentional, not a bug.

package service

import (
	"fmt"
	"os"
	"path/filepath"
)

// writeIfMissing writes data to dest with mode mode IFF the file doesn't
// exist yet. Returns true when a write happened, false when the existing
// file was preserved. Atomic via tmp+rename, like writeAtomic.
func writeIfMissing(dest string, data []byte, mode os.FileMode) (bool, error) {
	if _, err := os.Stat(dest); err == nil {
		return false, nil
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return false, fmt.Errorf("mkdir parent of %s: %w", dest, err)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return false, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return false, fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	return true, nil
}
