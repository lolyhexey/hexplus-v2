package menu

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// fakeSetPassword records calls instead of running chpasswd.
func fakeSetPassword(t *testing.T, fail map[string]error) *[][2]string {
	t.Helper()
	var calls [][2]string
	old := setUserPassword
	setUserPassword = func(name, pw string) error {
		calls = append(calls, [2]string{name, pw})
		return fail[name]
	}
	t.Cleanup(func() { setUserPassword = old })
	return &calls
}

func writeSenha(t *testing.T, files map[string]string) {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	old := senhaDir
	senhaDir = dir
	t.Cleanup(func() { senhaDir = old })
}

func TestRepairUnverifiableHashes(t *testing.T) {
	writeSenha(t, map[string]string{
		"alice": "Secret 1!\n", // spaces are part of the password; only the line ending goes
		"bob":   "pw2\r\n",
		"empty": "\n",
		"gina":  "pw4\n",
	})
	calls := fakeSetPassword(t, map[string]error{"gina": errors.New("chpasswd: boom")})

	fixed, noStored, failed := repairUnverifiableHashes([]string{"alice", "bob", "carol", "empty", "gina"})

	if want := []string{"alice", "bob"}; !reflect.DeepEqual(fixed, want) {
		t.Errorf("fixed = %v, want %v", fixed, want)
	}
	if want := []string{"carol", "empty"}; !reflect.DeepEqual(noStored, want) {
		t.Errorf("noStored = %v, want %v (missing file and empty file)", noStored, want)
	}
	if len(failed) != 1 || failed["gina"] == nil {
		t.Errorf("failed = %v, want only gina", failed)
	}
	wantCalls := [][2]string{{"alice", "Secret 1!"}, {"bob", "pw2"}, {"gina", "pw4"}}
	if !reflect.DeepEqual(*calls, wantCalls) {
		t.Errorf("SetPassword calls = %v, want %v", *calls, wantCalls)
	}
}

// Rewriting someone's password is only ever done after an explicit y.
func TestOfferHashRepairDefaultsToNo(t *testing.T) {
	writeSenha(t, map[string]string{"alice": "pw\n"})
	for _, answer := range []string{"", "n", "N", "yes please", " "} {
		calls := fakeSetPassword(t, nil)
		offerHashRepair(func(string) (string, error) { return answer, nil }, []string{"alice"})
		if len(*calls) != 0 {
			t.Errorf("answer %q changed a password: %v", answer, *calls)
		}
	}
	calls := fakeSetPassword(t, nil)
	offerHashRepair(func(string) (string, error) { return "", errors.New("EOF") }, []string{"alice"})
	if len(*calls) != 0 {
		t.Errorf("a read error changed a password: %v", *calls)
	}
}

func TestOfferHashRepairAcceptsY(t *testing.T) {
	writeSenha(t, map[string]string{"alice": "pw\n"})
	for _, answer := range []string{"y", "Y"} {
		calls := fakeSetPassword(t, nil)
		offerHashRepair(func(string) (string, error) { return answer, nil }, []string{"alice"})
		if want := [][2]string{{"alice", "pw"}}; !reflect.DeepEqual(*calls, want) {
			t.Errorf("answer %q: calls = %v, want %v", answer, *calls, want)
		}
	}
}
