package atomicfile

import (
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteCreatesAndReplaces(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hook.sh")

	if err := Write(path, []byte("one"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := Write(path, []byte("two"), 0o755); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != "two" {
		t.Errorf("content = %q, want two", got)
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm() != 0o755 {
			t.Errorf("mode = %v, want 0755", st.Mode().Perm())
		}
	}
	if left, _ := filepath.Glob(filepath.Join(dir, ".hook.sh.*")); len(left) != 0 {
		t.Errorf("temporary files left behind: %v", left)
	}
}

// A process that already has the file open (bash running an old hook)
// keeps reading the old content: the replacement is a new inode.
func TestWriteLeavesOpenReadersOnTheOldFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename over an open file is a Unix semantic")
	}
	path := filepath.Join(t.TempDir(), "hook.sh")
	if err := os.WriteFile(path, []byte("old"), 0o700); err != nil {
		t.Fatal(err)
	}
	running, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()

	if err := Write(path, []byte("new"), 0o700); err != nil {
		t.Fatal(err)
	}
	if seen, _ := io.ReadAll(running); string(seen) != "old" {
		t.Errorf("open reader saw %q, want old", seen)
	}
}

func TestWriteFailsCleanlyWhenDirectoryIsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-such-dir", "hook.sh")
	if err := Write(path, []byte("x"), 0o700); err == nil {
		t.Error("expected an error for a missing directory")
	}
}
