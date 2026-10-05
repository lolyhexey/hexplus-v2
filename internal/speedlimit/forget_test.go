package speedlimit

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// limitsHost points the speed-limit files at a temp dir: the cap file with
// the given content and one server config per name, each holding a single
// "dev" line.
func limitsHost(t *testing.T, conf string, servers ...string) (dir string) {
	t.Helper()
	dir = t.TempDir()
	oldConf, oldScript, oldGlob := confPath, scriptPath, serverConfGlob
	confPath = filepath.Join(dir, "hexplus-speedlimit.conf")
	scriptPath = filepath.Join(dir, "hexplus-learn-address")
	serverConfGlob = filepath.Join(dir, "server*.conf")
	t.Cleanup(func() { confPath, scriptPath, serverConfGlob = oldConf, oldScript, oldGlob })
	if conf != "" {
		if err := os.WriteFile(confPath, []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	for _, s := range servers {
		if err := os.WriteFile(filepath.Join(dir, s), []byte("dev tun\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func hasHook(t *testing.T, path string) bool {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return strings.Contains(string(b), learnAddressLine)
}

// A removed instance's cap stayed in the file and applied to whichever
// instance took its ID next.
func TestForgetDropsOnlyThatKey(t *testing.T) {
	dir := limitsHost(t, "main=10\n2=20\n3=30\n", "server.conf", "server3.conf")
	if err := Forget("2"); err != nil {
		t.Fatal(err)
	}
	if got, want := LoadAll(), (Limits{"main": 10, "3": 30}); !reflect.DeepEqual(got, want) {
		t.Errorf("limits = %v, want %v", got, want)
	}
	// Other caps remain, so the hook stays in the configs.
	for _, s := range []string{"server.conf", "server3.conf"} {
		if !hasHook(t, filepath.Join(dir, s)) {
			t.Errorf("%s lost the learn-address hook while caps remain", s)
		}
	}
}

func TestForgetLastKeyRemovesFileAndHook(t *testing.T) {
	dir := limitsHost(t, "2=20\n", "server.conf", "server2.conf")
	if err := InjectServerConf(); err != nil {
		t.Fatal(err)
	}
	if !hasHook(t, filepath.Join(dir, "server2.conf")) {
		t.Fatal("setup: hook not injected")
	}
	if err := Forget("2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("cap file still exists: %v", err)
	}
	for _, s := range []string{"server.conf", "server2.conf"} {
		if hasHook(t, filepath.Join(dir, s)) {
			t.Errorf("%s still has the learn-address hook", s)
		}
	}
}

// An instance without a cap must not cause any write, or create the file.
func TestForgetWithoutCapChangesNothing(t *testing.T) {
	dir := limitsHost(t, "main=10\n", "server.conf")
	before, _ := os.ReadFile(confPath)
	if err := Forget("2"); err != nil {
		t.Fatal(err)
	}
	after, _ := os.ReadFile(confPath)
	if string(before) != string(after) {
		t.Errorf("cap file rewritten: %q -> %q", before, after)
	}
	if hasHook(t, filepath.Join(dir, "server.conf")) {
		t.Error("Forget injected the hook for an instance without a cap")
	}

	os.Remove(confPath)
	if err := Forget("2"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(confPath); !os.IsNotExist(err) {
		t.Errorf("Forget created the cap file: %v", err)
	}
}
