package panel

import (
	"archive/tar"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// backup.go: tar+gzip the panel + xray state into a single archive
// the operator can move between boxes. Contents:
//   panel/panel.yaml
//   panel/panel.db
//   xray/config.json
//   xray/certs/**
//
// We intentionally DO NOT include the extracted xray binary — the
// hexplus binary will re-extract it on install anywhere the tarball
// lands, so shipping it just doubles the archive size.

// BackupPaths is what a caller can inspect after MakeBackup to know
// which files ended up in the archive (mostly useful for debug output).
type BackupPaths struct {
	Included []string
}

// MakeBackup writes a hexplus-panel-<ts>.tar.gz to destDir and returns
// its full path. Missing sources are silently skipped — a fresh
// install with no xray config yet is still backup-able.
func MakeBackup(destDir string) (string, BackupPaths, error) {
	if err := os.MkdirAll(destDir, 0o750); err != nil {
		return "", BackupPaths{}, err
	}
	ts := time.Now().UTC().Format("20060102-150405")
	dest := filepath.Join(destDir, "hexplus-panel-"+ts+".tar.gz")
	tmp := dest + ".tmp"

	f, err := os.Create(tmp)
	if err != nil {
		return "", BackupPaths{}, err
	}
	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	sources := []struct{ localPath, tarPath string }{
		{ConfigPath, "panel/panel.yaml"},
		{paths.PanelDBPath, "panel/panel.db"},
		{paths.XrayConfigPath, "xray/config.json"},
	}
	included := []string{}
	for _, s := range sources {
		if added, err := addFileToTar(tw, s.localPath, s.tarPath); err == nil && added {
			included = append(included, s.tarPath)
		} else if err != nil {
			_ = tw.Close()
			_ = gz.Close()
			_ = f.Close()
			_ = os.Remove(tmp)
			return "", BackupPaths{}, fmt.Errorf("add %s: %w", s.localPath, err)
		}
	}
	// Certs directory — walk recursively; every domain subfolder holds
	// two PEM files.
	certsDir := filepath.Join(paths.XrayStateDir, "certs")
	if list, err := walkAll(tw, certsDir, "xray/certs"); err == nil {
		included = append(included, list...)
	} else if !errors.Is(err, os.ErrNotExist) {
		_ = tw.Close()
		_ = gz.Close()
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", BackupPaths{}, err
	}

	if err := tw.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", BackupPaths{}, err
	}
	if err := gz.Close(); err != nil {
		_ = f.Close()
		_ = os.Remove(tmp)
		return "", BackupPaths{}, err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return "", BackupPaths{}, err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return "", BackupPaths{}, err
	}
	return dest, BackupPaths{Included: included}, nil
}

// Restore expands an archive on top of the current state. The panel
// daemon and xray daemon MUST be stopped before calling; we don't
// enforce that ourselves because the menu / CLI do it explicitly.
//
// The archive is validated first (no absolute paths, no "..") to keep
// a hostile tarball from writing outside the state dirs.
func Restore(archive string) error {
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()

	gz, err := gzip.NewReader(f)
	if err != nil {
		return fmt.Errorf("gzip: %w", err)
	}
	defer gz.Close()

	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("tar next: %w", err)
		}
		dst, err := resolveRestorePath(hdr.Name)
		if err != nil {
			return err
		}
		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dst, 0o750); err != nil {
				return err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
				return err
			}
			out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777)
			if err != nil {
				return err
			}
			if _, err := io.Copy(out, tr); err != nil {
				_ = out.Close()
				return err
			}
			_ = out.Close()
		default:
			// Skip symlinks / block devices — a legitimate backup has
			// none, and refusing to touch them keeps a malicious
			// archive from planting one.
		}
	}
	return nil
}

// resolveRestorePath maps a tar entry name (panel/panel.yaml,
// xray/config.json, xray/certs/example.com/fullchain.pem) back to its
// absolute location under paths.StateDir. Rejects any entry that tries
// to escape (absolute, ".." components).
func resolveRestorePath(name string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(name))
	if strings.HasPrefix(clean, "/") || strings.Contains(clean, "..") {
		return "", fmt.Errorf("backup entry outside state dir: %q", name)
	}
	switch {
	case strings.HasPrefix(clean, "panel/"):
		return filepath.Join(paths.PanelStateDir, strings.TrimPrefix(clean, "panel/")), nil
	case strings.HasPrefix(clean, "xray/"):
		return filepath.Join(paths.XrayStateDir, strings.TrimPrefix(clean, "xray/")), nil
	default:
		return "", fmt.Errorf("unknown backup section: %q", name)
	}
}

// addFileToTar writes one file's bytes into the tar, or skips silently
// when the source is missing. Returns whether the file was added.
func addFileToTar(tw *tar.Writer, src, tarPath string) (bool, error) {
	st, err := os.Stat(src)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	f, err := os.Open(src)
	if err != nil {
		return false, err
	}
	defer f.Close()
	if err := tw.WriteHeader(&tar.Header{
		Name:    tarPath,
		Mode:    int64(st.Mode() & 0o777),
		Size:    st.Size(),
		ModTime: st.ModTime(),
	}); err != nil {
		return false, err
	}
	if _, err := io.Copy(tw, f); err != nil {
		return false, err
	}
	return true, nil
}

// walkAll recursively adds every regular file under localDir into
// tarPrefix. Missing localDir is fine (empty state before first cert
// was ever issued).
func walkAll(tw *tar.Writer, localDir, tarPrefix string) ([]string, error) {
	included := []string{}
	err := filepath.Walk(localDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(localDir, path)
		if err != nil {
			return err
		}
		tarPath := filepath.ToSlash(filepath.Join(tarPrefix, rel))
		if added, err := addFileToTar(tw, path, tarPath); err == nil && added {
			included = append(included, tarPath)
		} else if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return included, err
	}
	return included, nil
}
