// fileutil.go: tiny on-disk helper shared by the TUI views. Kept here
// instead of leaking through main.go so the TUI doesn't pick up
// extract/install package imports for a single write call.

package tui

import (
	"os"
	"path/filepath"
)

// writeFileOnce writes data atomically via tmp+rename. Used by the
// users view to drop the generated .ovpn next to the user's row.
func writeFileOnce(dest string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
