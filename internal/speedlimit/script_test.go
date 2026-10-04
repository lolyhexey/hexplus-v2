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
