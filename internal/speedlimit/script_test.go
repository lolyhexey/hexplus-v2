package speedlimit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// runLearnAddress runs learnAddressScript under sh with fake `ip` and `tc`
// binaries first in PATH and returns the tc calls it made. env is the
// COMPLETE environment besides PATH/LOG: OpenVPN runs the 'delete' operation
// with no environment at all, which is the case that used to be wrong.
func runLearnAddress(t *testing.T, env []string, args ...string) []string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	write := func(path, body string) {
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	// `ip -o route get A` answers like the kernel would for the two tun
	// subnets, and via the default interface for anything else.
	write(filepath.Join(bin, "ip"), `#!/bin/sh
if [ "$1 $2 $3" = "-o route get" ]; then
    case "$4" in
        10.8.*) echo "$4 dev tun0 src 10.8.0.1 uid 0" ;;
        10.9.*) echo "$4 dev tun2 src 10.9.0.1 uid 0" ;;
        *)      echo "$4 via 192.0.2.1 dev eth0 src 192.0.2.9 uid 0" ;;
    esac
    exit 0
fi
echo "ip $*" >> "$LOG"
`)
	write(filepath.Join(bin, "tc"), "#!/bin/sh\necho \"tc $*\" >> \"$LOG\"\n")
	script := filepath.Join(dir, "learn-address.sh")
	write(script, learnAddressScript)

	cmd := exec.Command("sh", append([]string{script}, args...)...)
	cmd.Env = append([]string{"PATH=" + bin + ":/usr/bin:/bin", "LOG=" + log}, env...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("script must always exit 0 (OpenVPN disconnects the client otherwise): %v\n%s", err, out)
	}
	raw, _ := os.ReadFile(log)
	var calls []string
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.HasPrefix(l, "tc ") {
			calls = append(calls, l)
		}
	}
	return calls
}

func devicesTouched(calls []string) map[string]bool {
	seen := map[string]bool{}
	for _, c := range calls {
		f := strings.Fields(c)
		for i, w := range f {
			if w == "dev" && i+1 < len(f) {
				seen[f[i+1]] = true
			}
		}
	}
	return seen
}

// Regression guard: 'delete' arrives with an empty environment, so $dev is
// unset. The script used to default to tun0 and delete the class with the
// same id (last two octets) on the primary instance, while leaving the real
// instance's class behind.
func TestDeleteResolvesDeviceFromRouteWhenEnvIsEmpty(t *testing.T) {
	cases := []struct {
		name, ip       string
		wantTun, wantI string
	}{
		{"extra instance", "10.9.0.5", "tun2", "ifb2"},
		{"primary instance", "10.8.0.5", "tun0", "ifb0"},
	}
	for _, c := range cases {
		calls := runLearnAddress(t, nil, "delete", c.ip, "")
		if len(calls) == 0 {
			t.Fatalf("%s: no tc calls", c.name)
		}
		got := devicesTouched(calls)
		if len(got) != 2 || !got[c.wantTun] || !got[c.wantI] {
			t.Errorf("%s: touched %v, want exactly {%s %s}\ncalls: %q", c.name, got, c.wantTun, c.wantI, calls)
		}
		for _, call := range calls {
			if !strings.Contains(call, "0005") && !strings.Contains(call, "prio 5") {
				t.Errorf("%s: call %q is not about the client's class/prio", c.name, call)
			}
		}
	}
}

func TestDeleteUsesDevFromEnvWhenPresent(t *testing.T) {
	calls := runLearnAddress(t, []string{"dev=tun3"}, "delete", "10.9.0.5", "")
	got := devicesTouched(calls)
	if len(got) != 2 || !got["tun3"] || !got["ifb3"] {
		t.Errorf("touched %v, want exactly {tun3 ifb3}\ncalls: %q", got, calls)
	}
}

// If the instance is already gone, the route to the client lands on the
// default interface. tc deletes there would hit unrelated traffic.
func TestDeleteNeverTouchesNonTunDevices(t *testing.T) {
	if calls := runLearnAddress(t, nil, "delete", "192.0.2.7", ""); len(calls) != 0 {
		t.Errorf("route via eth0: expected no tc calls, got %q", calls)
	}
	if calls := runLearnAddress(t, []string{"dev=eth0"}, "delete", "10.9.0.5", ""); len(calls) != 0 {
		t.Errorf("dev=eth0: expected no tc calls, got %q", calls)
	}
	if calls := runLearnAddress(t, []string{"dev=tunnel"}, "delete", "10.9.0.5", ""); len(calls) != 0 {
		t.Errorf("dev=tunnel: expected no tc calls, got %q", calls)
	}
}

// OpenVPN gives the hook no PATH. bash-as-sh (RHEL family) then looks only in
// /usr/local/bin:/usr/bin, where neither ip nor tc live, while dash and
// busybox invent a default that includes the sbin directories - so running
// with PATH unset would pass on Debian even without the fix. The test pins a
// PATH that has no sbin and checks what the script turns it into (probe: the
// script cut off right after its PATH setup).
func TestScriptSuppliesSbinDirectories(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "learn-address.sh")
	probe := strings.Replace(learnAddressScript, "\nCONF=", "\necho \"PATH=$PATH\" >&2; exit 0\nCONF=", 1)
	if probe == learnAddressScript {
		t.Fatal("could not insert the probe: CONF= marker moved")
	}
	if err := os.WriteFile(script, []byte(probe), 0o755); err != nil {
		t.Fatal(err)
	}
	pathOf := func(env string) string {
		cmd := exec.Command("sh", script, "delete", "10.9.0.5", "")
		cmd.Env = []string{env}
		out, _ := cmd.CombinedOutput()
		return strings.TrimSpace(string(out))
	}

	// A PATH without sbin keeps its own entries first and gains the sbin ones.
	got := pathOf("PATH=/usr/bin:/bin")
	if !strings.HasPrefix(got, "PATH=/usr/bin:/bin:") {
		t.Errorf("existing PATH entries must stay first, got %q", got)
	}
	for _, want := range []string{"/usr/sbin", "/sbin"} {
		if !strings.Contains(got, want) {
			t.Errorf("PATH %q lacks %s", got, want)
		}
	}

	// An empty PATH must not become a leading colon, which would put the
	// current directory on the search path.
	got = pathOf("PATH=")
	if strings.HasPrefix(got, "PATH=:") {
		t.Errorf("empty PATH produced a leading colon: %q", got)
	}
	if !strings.Contains(got, "/usr/sbin") {
		t.Errorf("empty PATH lacks /usr/sbin: %q", got)
	}
}

// If ip cannot be run the device stays unknown, and the hook must still
// exit 0 without touching tc.
func TestDeleteWithBrokenIpExitsZeroWithoutTc(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("needs a POSIX sh")
	}
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skip("sh not available")
	}
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	if err := os.Mkdir(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	log := filepath.Join(dir, "log")
	for name, body := range map[string]string{
		"ip": "#!/bin/sh\nexit 1\n",
		"tc": "#!/bin/sh\necho \"tc $*\" >> \"$LOG\"\n",
	} {
		if err := os.WriteFile(filepath.Join(bin, name), []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	script := filepath.Join(dir, "learn-address.sh")
	if err := os.WriteFile(script, []byte(learnAddressScript), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", script, "delete", "10.9.0.5", "")
	cmd.Env = []string{"PATH=" + bin + ":/usr/bin:/bin", "LOG=" + log}
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("must exit 0 when ip fails: %v\n%s", err, out)
	}
	if raw, _ := os.ReadFile(log); len(raw) != 0 {
		t.Errorf("tc was called although the device is unknown: %q", raw)
	}
}

func TestEnsureScriptAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hexplus-learn-address")

	// No hook installed (no speed cap was ever set): leave the host alone.
	if err := ensureScriptAt(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("EnsureScript created the hook on a host that never had one: %v", err)
	}

	// An older release's script is replaced by the embedded one, executable.
	if err := os.WriteFile(path, []byte("#!/bin/sh\n# old release\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := ensureScriptAt(path); err != nil {
		t.Fatal(err)
	}
	got, _ := os.ReadFile(path)
	if string(got) != learnAddressScript {
		t.Errorf("old script was not replaced")
	}
	if runtime.GOOS != "windows" {
		if st, _ := os.Stat(path); st.Mode().Perm()&0o111 == 0 {
			t.Errorf("rewritten hook is not executable: %v", st.Mode())
		}
	}

	// Already current: no rewrite (mtime would change under busy systems' caches).
	before, _ := os.Stat(path)
	if err := ensureScriptAt(path); err != nil {
		t.Fatal(err)
	}
	after, _ := os.Stat(path)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Errorf("identical script was rewritten")
	}
}
