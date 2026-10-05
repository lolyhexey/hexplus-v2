//go:build !windows

package ovpnguard

import (
	"os"
	"path/filepath"
	"syscall"
)

// lockRunDir serialises management access between hexplus processes (the
// guard loop, a menu kick, `hexplus ovpnguard once`). OpenVPN 2.5 serves one
// management client at a time: a second connection waits in a 1-deep
// backlog while OpenVPN spins a CPU core, and a third is refused at once.
// Queuing on a lock file instead keeps the instance idle and the kick from
// being dropped. If the lock cannot be taken the caller proceeds anyway.
func lockRunDir(logf func(string, ...any)) func() {
	f, err := os.OpenFile(filepath.Join(RunDir, ".lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		if !os.IsNotExist(err) {
			logf("ovpnguard: lock: %v", err)
		}
		return func() {}
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		logf("ovpnguard: lock: %v", err)
		f.Close()
		return func() {}
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		f.Close()
	}
}
