// Package extract writes embedded asset files onto disk on first run.
//
// Design rules:
//   - Atomic per-file: write to <target>.tmp then rename, so a crash mid-extract
//     can be recovered by re-running the binary.
//   - Idempotent: if the target file exists AND its size matches the embedded
//     copy, skip. This is "good enough" for our binaries; full content compare
//     would force us to read the embedded blob just to no-op.
//   - Preserves +x on entries that look like ELF binaries or scripts.
//   - Never touches files outside the destination dir.
package extract

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Result describes the outcome of one extraction pass.
type Result struct {
	Written []string
	Skipped []string
}

// All copies every regular file in src into destDir, preserving the relative tree.
// destDir is created (with parents) if missing.
func All(src fs.FS, destDir string) (Result, error) {
	var res Result
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return res, fmt.Errorf("mkdir %s: %w", destDir, err)
	}
	err := fs.WalkDir(src, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		// Skip the placeholder marker file - it exists only so embed.FS has
		// something to walk during Phase 0.
		if d.Name() == ".gitkeep" || strings.HasSuffix(d.Name(), ".placeholder") {
			return nil
		}
		target := filepath.Join(destDir, path)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		written, err := extractOne(src, path, target)
		if err != nil {
			return err
		}
		if written {
			res.Written = append(res.Written, target)
		} else {
			res.Skipped = append(res.Skipped, target)
		}
		return nil
	})
	return res, err
}

// extractOne writes one file from src to target.
// Returns (true, nil) if a write happened, (false, nil) if skipped as up-to-date.
func extractOne(src fs.FS, srcPath, target string) (bool, error) {
	in, err := src.Open(srcPath)
	if err != nil {
		return false, fmt.Errorf("open embedded %s: %w", srcPath, err)
	}
	defer in.Close()

	stat, err := in.(fs.File).Stat()
	if err != nil {
		return false, fmt.Errorf("stat embedded %s: %w", srcPath, err)
	}

	// Skip if target already matches by size. Cheap idempotency check; we accept
	// the (small) risk that someone swapped the file with one of identical size.
	if existing, err := os.Stat(target); err == nil && !existing.IsDir() && existing.Size() == stat.Size() {
		return false, nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return false, fmt.Errorf("stat target %s: %w", target, err)
	}

	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		return false, fmt.Errorf("mkdir parent of %s: %w", target, err)
	}

	tmp := target + ".tmp"
	mode := pickMode(srcPath)
	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return false, fmt.Errorf("open %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		os.Remove(tmp)
		return false, fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := out.Close(); err != nil {
		os.Remove(tmp)
		return false, fmt.Errorf("close %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		os.Remove(tmp)
		return false, fmt.Errorf("rename %s -> %s: %w", tmp, target, err)
	}
	return true, nil
}

// pickMode decides the on-disk perms for an extracted file.
// We mark everything under bin/ executable; scripts (.sh, no extension) likewise.
// Plain config / README files stay 0644.
func pickMode(srcPath string) os.FileMode {
	ext := strings.ToLower(filepath.Ext(srcPath))
	switch ext {
	case ".sh", "":
		return 0o755
	case ".md", ".txt", ".conf", ".cnf", ".yaml", ".yml", ".json":
		return 0o644
	}
	return 0o755
}
