// Package atomicfile replaces files without ever exposing a partly
// written or rewritten version.
package atomicfile

import (
	"os"
	"path/filepath"
)

// Write writes data to a temporary file in path's directory and renames it
// over path. Anything that already has the old file open, such as a bash
// that is executing an older hook script, keeps reading the old inode;
// rewriting in place made bash continue at its old byte offset inside the
// new text, which can fail or, with an unlucky layout, run the wrong
// command. A symlink at path is replaced, not followed.
func Write(path string, data []byte, mode os.FileMode) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	fail := func(err error) error {
		tmp.Close()
		os.Remove(name)
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		return fail(err)
	}
	if err := tmp.Chmod(mode); err != nil {
		return fail(err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		os.Remove(name)
		return err
	}
	return nil
}
