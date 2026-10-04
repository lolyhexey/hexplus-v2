package menu

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

type fakeFileInfo struct {
	mode fs.FileMode
}

func (f fakeFileInfo) Name() string       { return "x" }
func (f fakeFileInfo) Size() int64        { return 1 }
func (f fakeFileInfo) Mode() fs.FileMode  { return f.mode }
func (f fakeFileInfo) ModTime() time.Time { return time.Time{} }
func (f fakeFileInfo) IsDir() bool        { return f.mode.IsDir() }
func (f fakeFileInfo) Sys() any           { return nil }

// fakeTools makes lookPath find onPath and statFile find the given sbin
// files (path -> mode).
func fakeTools(t *testing.T, onPath map[string]bool, files map[string]fs.FileMode) {
	t.Helper()
	oldLook, oldStat := lookPath, statFile
	lookPath = func(name string) (string, error) {
		if onPath[name] {
			return "/usr/bin/" + name, nil
		}
		return "", errors.New("not found")
	}
	statFile = func(name string) (os.FileInfo, error) {
		if m, ok := files[name]; ok {
			return fakeFileInfo{mode: m}, nil
		}
		return nil, os.ErrNotExist
	}
	t.Cleanup(func() { lookPath, statFile = oldLook, oldStat })
}

func TestMissingTools(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	fakeTools(t, map[string]bool{"iptables": true}, nil)
	if got, want := missingTools(openvpnInstallTools), []string{"ip", "openssl"}; !reflect.DeepEqual(got, want) {
		t.Errorf("missingTools = %v, want %v", got, want)
	}

	fakeTools(t, map[string]bool{"ip": true, "iptables": true, "openssl": true}, nil)
	if got := missingTools(openvpnInstallTools); len(got) != 0 {
		t.Errorf("all present: missingTools = %v", got)
	}
}

// `su` without "-" on Debian keeps a PATH without /usr/sbin, where iptables
// and ip live. The tools are installed: they must not be reported missing,
// and PATH must gain the directory so the installer's own calls work.
func TestMissingToolsFindsSbinOffPath(t *testing.T) {
	t.Setenv("PATH", "/usr/bin"+string(os.PathListSeparator)+"/bin")
	sbinIptables := filepath.Join("/usr/sbin", "iptables")
	sbinIp := filepath.Join("/usr/sbin", "ip")
	fakeTools(t, map[string]bool{"openssl": true}, map[string]fs.FileMode{
		sbinIptables: 0o755,
		sbinIp:       0o755,
	})
	if got := missingTools(openvpnInstallTools); len(got) != 0 {
		t.Fatalf("installed tools reported missing: %v", got)
	}
	found := false
	for _, p := range filepath.SplitList(os.Getenv("PATH")) {
		if p == "/usr/sbin" {
			found = true
		}
	}
	if !found {
		t.Errorf("PATH = %q, want /usr/sbin appended", os.Getenv("PATH"))
	}
	// Appending twice must not duplicate the entry.
	before := os.Getenv("PATH")
	missingTools(openvpnInstallTools)
	if os.Getenv("PATH") != before {
		t.Errorf("PATH grew on a second call: %q -> %q", before, os.Getenv("PATH"))
	}
}

func TestMissingToolsIgnoresNonExecutablesAndDirectories(t *testing.T) {
	t.Setenv("PATH", "/usr/bin")
	fakeTools(t, map[string]bool{"openssl": true}, map[string]fs.FileMode{
		filepath.Join("/usr/sbin", "iptables"): 0o644,
		filepath.Join("/usr/sbin", "ip"):       fs.ModeDir | 0o755,
	})
	if got, want := missingTools(openvpnInstallTools), []string{"ip", "iptables"}; !reflect.DeepEqual(got, want) {
		t.Errorf("missingTools = %v, want %v", got, want)
	}
}

// The auth script needs openssl, FORWARD needs ip and iptables: an install
// without any of them looked successful and left clients unable to log in
// or reach the internet.
func TestOpenVPNInstallRequiresTheToolsItUses(t *testing.T) {
	want := map[string]bool{"ip": true, "iptables": true, "openssl": true}
	for _, tool := range openvpnInstallTools {
		delete(want, tool)
	}
	if len(want) != 0 {
		t.Errorf("openvpnInstallTools misses %v", want)
	}
}
