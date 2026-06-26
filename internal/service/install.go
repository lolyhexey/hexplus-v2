// install.go: per-service lazy install. v1-style: nothing is extracted
// until the operator explicitly picks "ติดตั้ง" inside the conexao
// menu, at which point only THAT binary lands on disk + only THAT unit
// is written.
//
// Bulk install (the old behavior) is gone - install.Install() now just
// sets up the wrapper. A box that only ever uses Squid never pays for
// the OpenVPN extract.

package service

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/paths"
)

// InstallResult is what InstallService reports back to the menu.
type InstallResult struct {
	Extracted      []string
	UnitsWritten   []string
	ConfigsWritten []string
}

// UninstallResult mirrors InstallResult for the reverse path.
type UninstallResult struct {
	Removed []string
}

// InstallService extracts the embedded binary for svc, writes its
// systemd unit, and bootstraps its default config. Idempotent: every
// step is a no-op if the target file is already present.
//
// The function is the one place that knows about the per-service binary
// names inside the embed tree, so adding a service is one switch case
// here plus a row in service.All().
func InstallService(svc Service) (InstallResult, error) {
	var res InstallResult

	if os.Geteuid() != 0 {
		return res, errors.New("ต้องรันด้วยสิทธิ์ root (ใช้ sudo)")
	}
	if err := os.MkdirAll(paths.LibDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir %s: %w", paths.LibDir, err)
	}

	binaries, err := embeddedBinariesForService(svc)
	if err != nil {
		return res, err
	}
	for _, b := range binaries {
		dest := filepath.Join(paths.LibDir, b.destName)
		if written, err := extractEmbedded(b.srcName, dest, 0o755); err != nil {
			return res, err
		} else if written {
			res.Extracted = append(res.Extracted, dest)
		}
	}

	// Dropbear's multi-binary needs the argv[0] symlinks so the systemd
	// ExecStart picks the right dispatch. Other services don't.
	if svc.Name == "dropbear" {
		if err := CreateDropbearSymlinks(); err != nil {
			return res, fmt.Errorf("symlink dropbear helpers: %w", err)
		}
		res.Extracted = append(res.Extracted,
			paths.LibDir+"/dropbear",
			paths.LibDir+"/dropbearkey",
			paths.LibDir+"/scp",
		)
	}

	// systemd unit just for this service. WriteUnits() handles all
	// services; we filter the report to only the one we touched.
	if SystemdAvailable() {
		wr, err := WriteUnitsFor([]Service{svc})
		if err != nil {
			return res, err
		}
		res.UnitsWritten = wr.Written
	}

	// Config bootstrap only for the service we're installing.
	if cfg, err := bootstrapConfigFor(svc); err != nil {
		return res, err
	} else if cfg != "" {
		res.ConfigsWritten = append(res.ConfigsWritten, cfg)
	}

	return res, nil
}

// UninstallService reverses InstallService. Configs under /etc are
// preserved on purpose - reinstalling shouldn't wipe a hand-edited
// squid.conf.
func UninstallService(svc Service) (UninstallResult, error) {
	var res UninstallResult
	if os.Geteuid() != 0 {
		return res, errors.New("ต้องรันด้วยสิทธิ์ root (ใช้ sudo)")
	}

	// Stop + disable through systemctl first so a running service
	// doesn't hold the binary open after we delete it.
	_ = Stop(svc)
	_ = Disable(svc)

	// Remove the unit, then the binary(s).
	if SystemdAvailable() {
		if removed, _ := RemoveUnitsFor([]Service{svc}); len(removed) > 0 {
			res.Removed = append(res.Removed, removed...)
		}
	}
	binaries, err := embeddedBinariesForService(svc)
	if err != nil {
		return res, err
	}
	for _, b := range binaries {
		path := filepath.Join(paths.LibDir, b.destName)
		if err := os.Remove(path); err == nil {
			res.Removed = append(res.Removed, path)
		} else if !errors.Is(err, os.ErrNotExist) {
			return res, fmt.Errorf("remove %s: %w", path, err)
		}
	}
	if svc.Name == "dropbear" {
		for _, name := range []string{"dropbear", "dropbearkey", "scp"} {
			path := filepath.Join(paths.LibDir, name)
			if err := os.Remove(path); err == nil {
				res.Removed = append(res.Removed, path)
			}
		}
	}
	return res, nil
}

// binaryDef is what we need to copy: (name inside embed, name on disk).
// For dropbear, "dropbearmulti" extracts as itself; symlinks created
// separately by CreateDropbearSymlinks.
type binaryDef struct {
	srcName  string
	destName string
}

func embeddedBinariesForService(svc Service) ([]binaryDef, error) {
	switch svc.Name {
	case "openvpn":
		return []binaryDef{{"openvpn", "openvpn"}}, nil
	case "squid":
		return []binaryDef{{"squid", "squid"}}, nil
	case "dropbear":
		return []binaryDef{{"dropbearmulti", "dropbearmulti"}}, nil
	default:
		return nil, fmt.Errorf("no embed mapping for service %q", svc.Name)
	}
}

// extractEmbedded copies one file from the embed tree to dest. Returns
// (true, nil) when a write happened, (false, nil) when the file was
// already present with the same size.
func extractEmbedded(srcName, dest string, mode os.FileMode) (bool, error) {
	fs := assets.Binaries()
	in, err := fs.Open(srcName)
	if err != nil {
		return false, fmt.Errorf("open embedded %s: %w", srcName, err)
	}
	defer in.Close()

	stat, err := in.Stat()
	if err != nil {
		return false, err
	}
	if existing, err := os.Stat(dest); err == nil && existing.Size() == stat.Size() {
		return false, nil
	}
	tmp := dest + ".tmp"
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return false, err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return false, err
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return false, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		os.Remove(tmp)
		return false, err
	}
	return true, nil
}

// bootstrapConfigFor lays down the default config for one service.
// Returns the path of the file that was written, or "" if no config
// applies (dropbear lazily generates host keys via -R) or the file
// already existed.
func bootstrapConfigFor(svc Service) (string, error) {
	if err := os.MkdirAll("/etc/squid", 0o755); err != nil {
		return "", err
	}
	if err := os.MkdirAll("/etc/dropbear", 0o700); err != nil {
		return "", err
	}
	if err := os.MkdirAll("/etc/openvpn", 0o755); err != nil {
		return "", err
	}
	switch svc.Name {
	case "squid":
		wrote, err := writeIfMissing("/etc/squid/squid.conf", []byte(defaultSquidConf), 0o644)
		if err != nil {
			return "", err
		}
		if wrote {
			return "/etc/squid/squid.conf", nil
		}
	}
	return "", nil
}

// WriteUnitsFor renders units for a subset of services and runs
// daemon-reload. Same shape as WriteUnits() but with an explicit list -
// the lazy install path calls this with one service at a time.
func WriteUnitsFor(svcs []Service) (WriteResult, error) {
	var res WriteResult
	written, skipped, err := writeUnitFilesFor(svcs)
	if err != nil {
		return WriteResult{Written: written, Skipped: skipped}, err
	}
	res.Written = written
	res.Skipped = skipped
	if len(written) > 0 {
		if rerr := daemonReload(); rerr != nil {
			res.ReloadWarning = rerr
		}
	}
	return res, nil
}

// RemoveUnitsFor removes units for the named services.
func RemoveUnitsFor(svcs []Service) ([]string, error) {
	var removed []string
	for _, svc := range svcs {
		dest := filepath.Join(SystemdUnitDir, svc.UnitName)
		if err := os.Remove(dest); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return removed, err
		}
		removed = append(removed, dest)
	}
	if len(removed) > 0 {
		_ = daemonReload()
	}
	return removed, nil
}
