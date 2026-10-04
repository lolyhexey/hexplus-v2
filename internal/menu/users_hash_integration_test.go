//go:build integration && linux

package menu

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"

	"github.com/lolyhexey/hexplus/internal/user"
)

// This test creates and deletes real system users and rewrites their
// /etc/shadow entries, so it refuses to run unless HEXPLUS_USER_IT=1 and is
// meant for a throwaway container on a distro whose PAM defaults to
// yescrypt (Ubuntu 22.04+, Debian 11+):
//
//	go test -c -tags integration -o menu.test ./internal/menu
//	docker run --rm -i -e HEXPLUS_USER_IT=1 ubuntu:22.04 sh -c \
//	    'cat >/menu.test && chmod +x /menu.test && /menu.test -test.v -test.run TestRealYescryptRepair' < menu.test
func TestRealYescryptRepair(t *testing.T) {
	if os.Getenv("HEXPLUS_USER_IT") != "1" {
		t.Skip("set HEXPLUS_USER_IT=1 in a throwaway container to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	sh := func(cmd string) string {
		t.Helper()
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", cmd, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	prefix := func(name string) string {
		return sh("getent shadow " + name + " | cut -d: -f2 | cut -c1-3")
	}
	for _, n := range []string{"hxalice", "hxbob", "hxcarol"} {
		sh("useradd -M " + n)
		n := n
		t.Cleanup(func() { _ = exec.Command("userdel", n).Run() })
	}
	// What hexplus did before the fix: bare chpasswd (PAM picks the algorithm).
	sh(`echo 'hxalice:pw one' | chpasswd`)
	sh(`echo 'hxbob:pw two' | chpasswd`)
	if prefix("hxalice") != "$y$" {
		t.Skipf("this host's PAM does not default to yescrypt (got %s); nothing to test", prefix("hxalice"))
	}
	// What hexplus does now.
	sh(`echo 'hxcarol:pw three' | chpasswd -c SHA512`)

	writeSenha(t, map[string]string{"hxalice": "pw one\n"}) // hxbob has no stored password

	names := []string{"hxalice", "hxbob", "hxcarol"}
	bad, err := user.ReadUnverifiableHashUsers(names)
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"hxalice", "hxbob"}; !reflect.DeepEqual(bad, want) {
		t.Fatalf("detected %v, want %v", bad, want)
	}

	fixed, noStored, failed := repairUnverifiableHashes(bad) // real user.SetPassword -> chpasswd -c SHA512
	if !reflect.DeepEqual(fixed, []string{"hxalice"}) || !reflect.DeepEqual(noStored, []string{"hxbob"}) || len(failed) != 0 {
		t.Fatalf("fixed=%v noStored=%v failed=%v", fixed, noStored, failed)
	}

	if got := prefix("hxalice"); got != "$6$" {
		t.Errorf("hxalice hash prefix after repair = %s, want $6$", got)
	}
	// The repaired hash is exactly what the auth script recomputes.
	if _, err := exec.LookPath("openssl"); err == nil {
		hash := sh("getent shadow hxalice | cut -d: -f2")
		salt := strings.Split(hash, "$")[2]
		if got := sh("openssl passwd -6 -salt " + salt + " 'pw one'"); got != hash {
			t.Errorf("openssl passwd -6 gives %q, shadow holds %q", got, hash)
		}
	}
	if prefix("hxbob") != "$y$" {
		t.Errorf("hxbob has no stored password and must be left untouched, got %s", prefix("hxbob"))
	}
	after, _ := user.ReadUnverifiableHashUsers(names)
	if want := []string{"hxbob"}; !reflect.DeepEqual(after, want) {
		t.Errorf("after repair detected %v, want %v", after, want)
	}
}

// With SHA_CRYPT_MIN_ROUNDS/MAX_ROUNDS in /etc/login.defs, chpasswd -c SHA512
// writes "$6$rounds=N$salt$hash". hexplus-auth.sh splits on '$' and takes
// "rounds=N" as the salt, so it rejects that hash. The detector must flag it,
// and a repair must not claim success, because chpasswd writes the same shape
// again. Same guards and container advice as TestRealYescryptRepair; this one
// also edits /etc/login.defs.
func TestRealRoundsHashIsFlaggedAndNotReportedFixed(t *testing.T) {
	if os.Getenv("HEXPLUS_USER_IT") != "1" {
		t.Skip("set HEXPLUS_USER_IT=1 in a throwaway container to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	sh := func(cmd string) string {
		t.Helper()
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", cmd, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	orig, err := os.ReadFile("/etc/login.defs")
	if err != nil {
		t.Skip("no /etc/login.defs")
	}
	t.Cleanup(func() { _ = os.WriteFile("/etc/login.defs", orig, 0o644) })
	sh("printf 'SHA_CRYPT_MIN_ROUNDS 6000\nSHA_CRYPT_MAX_ROUNDS 6000\n' >> /etc/login.defs")

	sh("useradd -M hxrounds")
	t.Cleanup(func() { _ = exec.Command("userdel", "hxrounds").Run() })
	sh(`echo 'hxrounds:pw rounds' | chpasswd -c SHA512`)
	if hash := sh("getent shadow hxrounds | cut -d: -f2"); !strings.HasPrefix(hash, "$6$rounds=6000$") {
		t.Skipf("chpasswd did not write a rounds= hash on this host: %.20s", hash)
	}

	bad, err := user.ReadUnverifiableHashUsers([]string{"hxrounds"})
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(bad, []string{"hxrounds"}) {
		t.Fatalf("a rounds= hash must be flagged, got %v", bad)
	}

	writeSenha(t, map[string]string{"hxrounds": "pw rounds\n"})
	fixed, noStored, failed := repairUnverifiableHashes(bad)
	if len(fixed) != 0 || len(noStored) != 0 || failed["hxrounds"] == nil {
		t.Errorf("repair must report failure, got fixed=%v noStored=%v failed=%v", fixed, noStored, failed)
	}
}
