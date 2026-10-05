package pki

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The embedded OpenVPN is built with --disable-plugins, so any "plugin"
// directive stops the daemon at startup. Authentication must go through the
// script no matter what exists on the host.
func TestAuthDirectivesUseScriptNotPlugin(t *testing.T) {
	got := authDirectives()

	for _, want := range []string{
		"script-security 2",
		"verify-client-cert none",
		"username-as-common-name",
		"auth-user-pass-verify " + AuthScriptPath + " via-file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "plugin") {
			t.Errorf("emits a plugin directive the embedded binary cannot parse: %q", line)
		}
	}
}

// The auth block releases up to v2.1.5 wrote when the host had
// openvpn-plugin-auth-pam.so: no script-security, no auth-user-pass-verify.
const oldPluginConf = "port 1194\nproto udp\ndev tun\n\nclient-to-client\n" +
	"verify-client-cert none\nusername-as-common-name\n" +
	"plugin /usr/lib/x86_64-linux-gnu/openvpn/plugins/openvpn-plugin-auth-pam.so login\n" +
	"duplicate-cn\n\nverb 3\n"

func TestRepairPluginConf(t *testing.T) {
	got, changed := repairPluginConf(oldPluginConf)
	if !changed {
		t.Fatal("the old plugin line was not recognised")
	}
	want := strings.Replace(oldPluginConf,
		"plugin /usr/lib/x86_64-linux-gnu/openvpn/plugins/openvpn-plugin-auth-pam.so login\n",
		"script-security 2\nauth-user-pass-verify "+AuthScriptPath+" via-file\n", 1)
	if got != want {
		t.Errorf("repaired conf:\n%s\nwant:\n%s", got, want)
	}
	if again, changed := repairPluginConf(got); changed || again != got {
		t.Error("repairing twice changed the config again")
	}

	// Directives already present are not duplicated.
	withScript := strings.Replace(oldPluginConf, "client-to-client\n", "client-to-client\nscript-security 2\n", 1)
	got, _ = repairPluginConf(withScript)
	if n := strings.Count(got, "script-security"); n != 1 {
		t.Errorf("script-security appears %d times", n)
	}

	// Anything else is the operator's.
	for _, conf := range []string{
		authDirectives() + "\n",
		"plugin /opt/custom/my-plugin.so arg\n",
		"# plugin /usr/lib/openvpn/openvpn-plugin-auth-pam.so login\n",
	} {
		if _, changed := repairPluginConf(conf); changed {
			t.Errorf("changed a config it does not own: %q", conf)
		}
	}
}

func TestRepairPluginConfsIn(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "hexplus-auth.sh")
	write := func(name, body string) {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("server.conf", oldPluginConf)
	write("server2.conf", oldPluginConf)
	write("server3.conf", "port 9000\n"+authDirectives()+"\n")
	write("client.conf", oldPluginConf) // not a server config

	fixed, err := repairPluginConfsIn(dir, script)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(dir, "server.conf"), filepath.Join(dir, "server2.conf")}
	if strings.Join(fixed, ",") != strings.Join(want, ",") {
		t.Errorf("fixed = %v, want %v", fixed, want)
	}
	if b, _ := os.ReadFile(script); string(b) != authScript {
		t.Error("the auth script was not written; old plugin installs never had one")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "client.conf")); string(b) != oldPluginConf {
		t.Error("client.conf was modified")
	}
	if fixed, _ := repairPluginConfsIn(dir, script); len(fixed) != 0 {
		t.Errorf("second run fixed %v again", fixed)
	}
}

func TestSetDuplicateCN(t *testing.T) {
	path := filepath.Join(t.TempDir(), "server2.conf")
	if err := os.WriteFile(path, []byte("port 1\nduplicate-cn\nverb 3\nduplicate-cn\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !HasDuplicateCN(path) {
		t.Fatal("not detected")
	}
	if err := SetDuplicateCN(path, false); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); string(b) != "port 1\nverb 3\n" || HasDuplicateCN(path) {
		t.Errorf("after off: %q", b)
	}
	for i := 0; i < 2; i++ {
		if err := SetDuplicateCN(path, true); err != nil {
			t.Fatal(err)
		}
	}
	if b, _ := os.ReadFile(path); string(b) != "port 1\nverb 3\nduplicate-cn\n" {
		t.Errorf("after on twice: %q", b)
	}
	if HasDuplicateCN(filepath.Join(t.TempDir(), "missing.conf")) {
		t.Error("a missing file reads as on")
	}
}
