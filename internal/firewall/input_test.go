package firewall

import (
	"reflect"
	"strings"
	"testing"
)

func TestApplyInputIsIdempotentAndFirst(t *testing.T) {
	c := useChain(t, "-j REJECT --reject-with icmp-host-prohibited")
	// useChain fakes one chain; INPUT calls land in the same fake.
	if err := ApplyInput("tcp", 443); err != nil {
		t.Fatal(err)
	}
	if err := ApplyInput("tcp", 443); err != nil {
		t.Fatal(err)
	}
	want := []string{"-p tcp --dport 443 -j ACCEPT", "-j REJECT --reject-with icmp-host-prohibited"}
	if !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q (one copy, ahead of the REJECT)", c.rules, want)
	}
}

func TestRemoveInputRemovesEveryCopyOnly(t *testing.T) {
	ours := "-p udp --dport 1194 -j ACCEPT"
	c := useChain(t, ours, "-p tcp --dport 22 -j ACCEPT", ours)
	RemoveInput("udp", 1194)
	if want := []string{"-p tcp --dport 22 -j ACCEPT"}; !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain = %q, want %q", c.rules, want)
	}
}

func TestInputRejectsBadArguments(t *testing.T) {
	c := useChain(t)
	for _, tc := range []struct {
		proto string
		port  int
	}{{"icmp", 443}, {"tcp", 0}, {"udp", 70000}, {"tcp; reboot", 443}} {
		if err := ApplyInput(tc.proto, tc.port); err == nil {
			t.Errorf("ApplyInput(%q, %d) accepted", tc.proto, tc.port)
		}
		RemoveInput(tc.proto, tc.port)
	}
	if len(c.calls) != 0 {
		t.Errorf("iptables was called with bad arguments: %q", c.calls)
	}
}

// The boot line must recreate exactly the live rule.
func TestInputRCLocalLineReplaysTheLiveRule(t *testing.T) {
	live := useChain(t)
	if err := ApplyInput("tcp", 8443); err != nil {
		t.Fatal(err)
	}
	boot := &fakeChain{}
	line := InputRCLocalLine("tcp", 8443)
	if !strings.HasPrefix(line, "iptables -w 5 ") {
		t.Fatalf("rc.local line %q does not wait for the xtables lock", line)
	}
	fields := strings.Fields(strings.TrimPrefix(line, "iptables -w 5 "))
	if _, err := boot.run(fields...); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(boot.rules, live.rules) {
		t.Errorf("boot %q != live %q", boot.rules, live.rules)
	}
}
