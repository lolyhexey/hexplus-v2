package firewall

import (
	"reflect"
	"strings"
	"testing"
)

// keep443 says another hexplus service (SSLH, say) listens on tcp/443.
func keep443(proto string, port int) bool { return proto == "tcp" && port == 443 }

// SSLH, SSL TUNNEL and the proxies never opened their port, so on a host
// whose INPUT policy is DROP they were unreachable.
func TestOpenPortClosePortRoundTrip(t *testing.T) {
	c := useChain(t, "-j REJECT")
	rc := writeRC(t, "#!/bin/sh -e\nexit 0\n")
	for i := 0; i < 2; i++ {
		if err := OpenPort("tcp", 8443, rc); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{"-p tcp --dport 8443 -j ACCEPT", "-j REJECT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain after open = %q, want %q", c.rules, want)
	}
	if got := readRC(t, rc); strings.Count(got, InputRCLocalLine("tcp", 8443)) != 1 {
		t.Errorf("rc.local after open:\n%s", got)
	}
	if err := ClosePort("tcp", 8443, rc, nil); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-j REJECT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain after close = %q, want %q", c.rules, want)
	}
	if got := readRC(t, rc); got != "#!/bin/sh -e\nexit 0\n" {
		t.Errorf("rc.local after close: %q", got)
	}
}

// A port another hexplus service still listens on stays open.
func TestClosePortKeepsAPortStillInUse(t *testing.T) {
	c := useChain(t, "-p tcp --dport 443 -j ACCEPT")
	rc := writeRC(t, "#!/bin/sh -e\n"+InputRCLocalLine("tcp", 443)+"\nexit 0\n")
	if err := ClosePort("tcp", 443, rc, keep443); err != nil {
		t.Fatal(err)
	}
	if len(c.rules) != 1 || !strings.Contains(readRC(t, rc), InputRCLocalLine("tcp", 443)) {
		t.Errorf("a port still in use was closed: chain %q", c.rules)
	}
}

// Moving OpenVPN off 443 used to delete the 443 rule even when SSLH had
// taken that port over.
func TestMoveInputKeepsTheOldPortForAnotherService(t *testing.T) {
	c := useChain(t, "-p tcp --dport 443 -j ACCEPT")
	rc := writeRC(t, "#!/bin/sh -e\n"+InputRCLocalLine("tcp", 443)+"\nexit 0\n")
	if err := MoveInput("tcp", 443, 1194, rc, keep443); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p tcp --dport 1194 -j ACCEPT", "-p tcp --dport 443 -j ACCEPT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
	got := readRC(t, rc)
	if !strings.Contains(got, InputRCLocalLine("tcp", 443)) || !strings.Contains(got, InputRCLocalLine("tcp", 1194)) {
		t.Errorf("rc.local:\n%s", got)
	}
}

// OpenVPN's uninstall removed every persisted INPUT line, SSLH's and SSL
// TUNNEL's included.
func TestRemovePersistedInputKeepsOtherServicesPorts(t *testing.T) {
	c := useChain(t, "-p tcp --dport 443 -j ACCEPT", "-p udp --dport 1194 -j ACCEPT")
	rc := writeRC(t, "#!/bin/sh -e\n"+InputRCLocalLine("tcp", 443)+"\n"+InputRCLocalLine("udp", 1194)+"\nexit 0\n")
	if err := RemovePersistedInput(rc, keep443); err != nil {
		t.Fatal(err)
	}
	if want := []string{"-p tcp --dport 443 -j ACCEPT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
	if got, want := readRC(t, rc), "#!/bin/sh -e\n"+InputRCLocalLine("tcp", 443)+"\nexit 0\n"; got != want {
		t.Errorf("rc.local:\n got %q\nwant %q", got, want)
	}
}
