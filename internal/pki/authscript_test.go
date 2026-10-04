package pki

import (
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

// fixedDay is the "today" the fake date command reports, in days since
// 1970-01-01 (2025-10-05). The script computes the day as $(date +%s)/86400.
const fixedDay = 20366

type authHarness struct {
	t       *testing.T
	dir     string
	bin     string
	script  string
	argvLog string
	openssl string
}

// newAuthHarness runs the real hexplus-auth.sh under bash with a fake getent
// (serving one shadow line) and a fake date, and wraps the real openssl so
// every argv it receives is logged.
func newAuthHarness(t *testing.T) *authHarness {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs bash")
	}
	for _, tool := range []string{"bash", "openssl", "awk", "cut", "sed"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s not available", tool)
		}
	}
	realOpenssl, _ := exec.LookPath("openssl")
	dir := t.TempDir()
	h := &authHarness{t: t, dir: dir, bin: filepath.Join(dir, "bin"), script: filepath.Join(dir, "hexplus-auth.sh"),
		argvLog: filepath.Join(dir, "openssl-argv"), openssl: realOpenssl}
	if err := os.Mkdir(h.bin, 0o755); err != nil {
		t.Fatal(err)
	}
	h.write(h.script, authScript)
	h.write(filepath.Join(h.bin, "getent"), "#!/bin/sh\n[ \"$1\" = shadow ] && [ -n \"$SHADOW_LINE\" ] && printf '%s\\n' \"$SHADOW_LINE\"\n")
	h.write(filepath.Join(h.bin, "date"), "#!/bin/sh\n[ -n \"$DATE_BROKEN\" ] && exit 1\necho "+strconv.Itoa(fixedDay*86400+3600)+"\n")
	h.write(filepath.Join(h.bin, "openssl"), "#!/bin/sh\nprintf '%s\\n' \"$*\" >> \""+h.argvLog+"\"\nexec \""+realOpenssl+"\" \"$@\"\n")
	return h
}

func (h *authHarness) write(path, body string) {
	h.t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		h.t.Fatal(err)
	}
}

// hash makes a real crypt hash with the real openssl (argv here is test-only).
func (h *authHarness) hash(alg, salt, pw string) string {
	h.t.Helper()
	// "--" so a password that looks like an option ("-stdin") is hashed as
	// the password, independently of the script's stdin path.
	out, err := exec.Command(h.openssl, "passwd", "-"+alg, "-salt", salt, "--", pw).Output()
	if err != nil {
		h.t.Fatalf("openssl passwd -%s: %v", alg, err)
	}
	return strings.TrimSpace(string(out))
}

// login runs the script the way OpenVPN does (via-file) and reports whether
// it accepted the credentials.
func (h *authHarness) login(shadowLine, user, pw string, env ...string) bool {
	h.t.Helper()
	cred := filepath.Join(h.dir, "cred")
	if err := os.WriteFile(cred, []byte(user+"\n"+pw+"\n"), 0o600); err != nil {
		h.t.Fatal(err)
	}
	cmd := exec.Command("bash", h.script, cred)
	cmd.Env = append([]string{"PATH=" + h.bin + ":/usr/local/bin:/usr/bin:/bin", "SHADOW_LINE=" + shadowLine}, env...)
	err := cmd.Run()
	if err == nil {
		return true
	}
	if _, ok := err.(*exec.ExitError); !ok {
		h.t.Fatalf("running the script: %v", err)
	}
	return false
}

func shadowLine(user, hash, expire string) string {
	return user + ":" + hash + ":20000:0:99999:7::" + expire + ":"
}

// Regression guard: the script only compared the hash, so an account
// expired with `chage -E` (or created with `useradd -e` and past its date)
// still logged in to OpenVPN. pam_unix rejects it once today >= field 8.
func TestAuthScriptEnforcesAccountExpiry(t *testing.T) {
	h := newAuthHarness(t)
	hash := h.hash("6", "saltsalt", "pw one")
	day := func(d int) string { return strconv.Itoa(d) }
	cases := []struct {
		name, expire string
		want         bool
	}{
		{"no expiry", "", true},
		{"-1 means never", "-1", true},
		{"expires tomorrow", day(fixedDay + 1), true},
		{"expires today (pam_unix: expired)", day(fixedDay), false},
		{"expired yesterday", day(fixedDay - 1), false},
		{"expired long ago", "1", false},
		{"malformed fails closed", "abc", false},
		{"leading zeros are still that day", "0" + day(fixedDay), false},
		{"more than 9 digits is never", "99999999999", true},
	}
	for _, c := range cases {
		if got := h.login(shadowLine("alice", hash, c.expire), "alice", "pw one"); got != c.want {
			t.Errorf("%s (field 8 = %q): accepted = %v, want %v", c.name, c.expire, got, c.want)
		}
	}
}

func TestAuthScriptVerifiesPasswords(t *testing.T) {
	h := newAuthHarness(t)
	for _, alg := range []string{"6", "5", "1"} {
		hash := h.hash(alg, "saltsalt", "pw one")
		if !h.login(shadowLine("alice", hash, ""), "alice", "pw one") {
			t.Errorf("$%s$: correct password rejected", alg)
		}
		if h.login(shadowLine("alice", hash, ""), "alice", "pw two") {
			t.Errorf("$%s$: wrong password accepted", alg)
		}
	}
	// A password that looks like an option used to reach openssl as argv.
	dash := h.hash("6", "saltsalt", "-stdin")
	if !h.login(shadowLine("bob", dash, ""), "bob", "-stdin") {
		t.Error("a password starting with '-' was rejected")
	}
	for _, line := range []string{shadowLine("carol", "!", ""), shadowLine("carol", "*", ""), shadowLine("carol", "!!", ""), ""} {
		if h.login(line, "carol", "anything") {
			t.Errorf("shadow line %q was accepted", line)
		}
	}
}

// Regression guard: the password was passed on openssl's command line,
// readable by every local user in /proc/<pid>/cmdline while it ran.
func TestAuthScriptKeepsPasswordOffTheCommandLine(t *testing.T) {
	h := newAuthHarness(t)
	const secret = "Sup3r-Secret-Pw"
	hash := h.hash("6", "saltsalt", secret)
	_ = os.Remove(h.argvLog)
	if !h.login(shadowLine("alice", hash, ""), "alice", secret) {
		t.Fatal("correct password rejected")
	}
	argv, err := os.ReadFile(h.argvLog)
	if err != nil {
		t.Fatalf("openssl was never called: %v", err)
	}
	if strings.Contains(string(argv), secret) {
		t.Errorf("the password appeared in openssl's argv: %q", argv)
	}
}

func TestEnsureAuthScriptAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "hexplus-auth.sh")

	// OpenVPN never installed: leave the host alone.
	if err := ensureAuthScriptAt(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("created the script on a host that never had it: %v", err)
	}

	// An older release's script is replaced.
	if err := os.WriteFile(path, []byte("#!/bin/bash\n# old\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := ensureAuthScriptAt(path); err != nil {
		t.Fatal(err)
	}
	if got, _ := os.ReadFile(path); string(got) != authScript {
		t.Error("old script was not replaced")
	}

	// Already current: not rewritten.
	before, _ := os.Stat(path)
	if err := ensureAuthScriptAt(path); err != nil {
		t.Fatal(err)
	}
	if after, _ := os.Stat(path); !after.ModTime().Equal(before.ModTime()) {
		t.Error("identical script was rewritten")
	}
}

// If the clock cannot be read, an account that has an expiry date cannot be
// judged and must be rejected; one without an expiry is unaffected. The
// first version let the expired user in (the arithmetic failed and the
// "exit 1" never ran).
func TestAuthScriptFailsClosedWithoutAClock(t *testing.T) {
	h := newAuthHarness(t)
	hash := h.hash("6", "saltsalt", "pw one")
	if h.login(shadowLine("alice", hash, "1"), "alice", "pw one", "DATE_BROKEN=1") {
		t.Error("expired account accepted while date was broken")
	}
	if h.login(shadowLine("alice", hash, strconv.Itoa(fixedDay+30)), "alice", "pw one", "DATE_BROKEN=1") {
		t.Error("account with an expiry date accepted although the clock could not be read")
	}
	if !h.login(shadowLine("alice", hash, ""), "alice", "pw one", "DATE_BROKEN=1") {
		t.Error("account without an expiry was rejected because date was broken")
	}
}

// A login that is already running the old script keeps reading it: the
// replacement must be a new file renamed into place, not a rewrite of the
// same inode (bash reads scripts incrementally).
func TestEnsureAuthScriptReplacesAtomically(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("rename over an open file is a Unix semantic")
	}
	path := filepath.Join(t.TempDir(), "hexplus-auth.sh")
	const old = "#!/bin/bash\n# old release\nexit 1\n"
	if err := os.WriteFile(path, []byte(old), 0o700); err != nil {
		t.Fatal(err)
	}
	running, err := os.Open(path) // what a bash already executing it holds
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()

	if err := ensureAuthScriptAt(path); err != nil {
		t.Fatal(err)
	}
	seen, _ := io.ReadAll(running)
	if string(seen) != old {
		t.Errorf("the running reader saw %q, want the old script untouched", seen)
	}
	if got, _ := os.ReadFile(path); string(got) != authScript {
		t.Error("path does not hold the new script")
	}
	if st, _ := os.Stat(path); st.Mode().Perm() != 0o700 {
		t.Errorf("mode = %v, want 0700", st.Mode().Perm())
	}
	if matches, _ := filepath.Glob(filepath.Join(filepath.Dir(path), ".*")); len(matches) != 0 {
		t.Errorf("temporary files left behind: %v", matches)
	}
}
