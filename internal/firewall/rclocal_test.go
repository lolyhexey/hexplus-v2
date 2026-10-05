package firewall

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestAddRCLocalLine(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"empty file gets the skeleton", "", "#!/bin/sh -e\nL\nexit 0\n"},
		{"before the final exit 0", "#!/bin/sh -e\nA\nexit 0\n", "#!/bin/sh -e\nA\nL\nexit 0\n"},
		{"no exit 0: appended", "#!/bin/sh\nA\n", "#!/bin/sh\nA\nL\n"},
		{"already present: unchanged", "#!/bin/sh -e\nL\nexit 0\n", "#!/bin/sh -e\nL\nexit 0\n"},
		{"last exit 0 wins", "#!/bin/sh -e\nif x; then exit 0; fi\nexit 0\n", "#!/bin/sh -e\nif x; then exit 0; fi\nL\nexit 0\n"},
	}
	for _, c := range cases {
		if got := addRCLocalLine(c.in, "L"); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestParseInputRCLocalLine(t *testing.T) {
	for _, tc := range []struct {
		proto string
		port  int
	}{{"tcp", 443}, {"udp", 1194}, {"tcp", 65535}} {
		proto, port, ok := ParseInputRCLocalLine(InputRCLocalLine(tc.proto, tc.port))
		if !ok || proto != tc.proto || port != tc.port {
			t.Errorf("round trip %s/%d: got %q %d %v", tc.proto, tc.port, proto, port, ok)
		}
	}
	for _, bad := range []string{
		"",
		"iptables -A INPUT -p tcp --dport 25 -j DROP",
		"iptables -w 5 -I INPUT 1 -p tcp --dport 443 -j DROP",
		"iptables -w 5 -I INPUT 1 -p icmp --dport 443 -j ACCEPT",
		"iptables -w 5 -I INPUT 1 -p tcp --dport 99999 -j ACCEPT",
		"iptables -w 5 -I FORWARD 1 -i tun+ -o eth0 -j ACCEPT",
	} {
		if _, _, ok := ParseInputRCLocalLine(bad); ok {
			t.Errorf("parsed a line that is not ours: %q", bad)
		}
	}
}

func writeRC(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "rc.local")
	if err := os.WriteFile(path, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func readRC(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// Regression guard: the port-change menu rewrote server.conf only, so the
// old port stayed open (live and at every boot) and the new one was never
// opened in INPUT.
func TestMoveInput(t *testing.T) {
	c := useChain(t, "-p udp --dport 1194 -j ACCEPT", "-j REJECT")
	rc := writeRC(t, "#!/bin/sh -e\n"+InputRCLocalLine("udp", 1194)+"\nexit 0\n")

	if err := MoveInput("udp", 1194, 1195, rc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p udp --dport 1195 -j ACCEPT", "-j REJECT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
	got := readRC(t, rc)
	if strings.Contains(got, "--dport 1194") || !strings.Contains(got, InputRCLocalLine("udp", 1195)) {
		t.Errorf("rc.local after the move:\n%s", got)
	}
}

func TestMoveInputSamePortAndFreshHost(t *testing.T) {
	c := useChain(t)
	rc := filepath.Join(t.TempDir(), "rc.local") // does not exist yet
	if err := MoveInput("tcp", 443, 443, rc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p tcp --dport 443 -j ACCEPT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
	if got := readRC(t, rc); !strings.Contains(got, InputRCLocalLine("tcp", 443)) || !strings.HasSuffix(got, "exit 0\n") {
		t.Errorf("rc.local:\n%s", got)
	}
	if err := MoveInput("tcp", 443, 0, rc); err == nil {
		t.Error("an invalid new port was accepted")
	}
}

// Uninstall must also remove lines a pre-fix port change left behind.
func TestRemovePersistedInput(t *testing.T) {
	c := useChain(t, "-p udp --dport 1194 -j ACCEPT", "-p udp --dport 1300 -j ACCEPT", "-p tcp --dport 22 -j ACCEPT")
	rc := writeRC(t, "#!/bin/sh -e\n"+
		"iptables -A INPUT -p tcp --dport 25 -j DROP\n"+
		InputRCLocalLine("udp", 1194)+"\n"+
		InputRCLocalLine("udp", 1300)+"\n"+
		"exit 0\n")

	if err := RemovePersistedInput(rc); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p tcp --dport 22 -j ACCEPT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
	if got, want := readRC(t, rc), "#!/bin/sh -e\niptables -A INPUT -p tcp --dport 25 -j DROP\nexit 0\n"; got != want {
		t.Errorf("rc.local:\n got %q\nwant %q", got, want)
	}
}
