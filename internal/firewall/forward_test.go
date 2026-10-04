package firewall

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestParseEgress(t *testing.T) {
	cases := []struct {
		name, out, want string
		wantErr         bool
	}{
		{"via gateway", "1.1.1.1 via 10.0.0.1 dev eth0 src 10.0.0.5 uid 0\n    cache\n", "eth0", false},
		{"on-link", "1.1.1.1 dev ens3 src 203.0.113.9 uid 0", "ens3", false},
		{"vlan style name", "1.1.1.1 via 10.0.0.1 dev eth0.100 src 10.0.0.5", "eth0.100", false},
		{"no dev", "RTNETLINK answers: Network is unreachable", "", true},
		{"dev at end", "1.1.1.1 dev", "", true},
		{"shell metacharacters", "1.1.1.1 dev eth0;reboot src 1.2.3.4", "", true},
		{"too long", "1.1.1.1 dev abcdefghijklmnop src 1.2.3.4", "", true},
		{"empty", "", "", true},
	}
	for _, c := range cases {
		got, err := parseEgress(c.out)
		if (err != nil) != c.wantErr || got != c.want {
			t.Errorf("%s: parseEgress = %q, %v; want %q (err=%v)", c.name, got, err, c.want, c.wantErr)
		}
	}
}

// Regression guard for the old installer, which inserted
// "FORWARD -s 10.8.0.0/16 -j ACCEPT" at the top of the chain: any
// destination, ahead of Docker's isolation. Every rule we install must be
// pinned to a tun device, and client traffic to a single egress interface.
func TestForwardSpecsAreScoped(t *testing.T) {
	specs := ForwardSpecs("eth0")
	if len(specs) != 4 {
		t.Fatalf("got %d specs, want 4", len(specs))
	}
	for _, s := range specs {
		if equal(s, legacyBroadAccept) {
			t.Errorf("spec %v is the legacy broad rule", s)
		}
		joined := " " + strings.Join(s, " ") + " "
		if !strings.Contains(joined, " -i tun+ ") && !strings.Contains(joined, " -o tun+ ") {
			t.Errorf("spec %v is not pinned to a tun device", s)
		}
	}
	// Order is part of the contract: the SMTP/POP3 DROPs must come before
	// the ACCEPT that would otherwise let that traffic through.
	last := -1
	for i, s := range specs {
		if s[len(s)-1] == "DROP" {
			if i > last {
				last = i
			}
		}
	}
	for i, s := range specs {
		if s[len(s)-1] == "ACCEPT" && i < last {
			t.Errorf("ACCEPT spec %v precedes a DROP spec", s)
		}
	}
	if want := []string{"-i", "tun+", "-o", "eth0", "-j", "ACCEPT"}; !reflect.DeepEqual(specs[2], want) {
		t.Errorf("outbound spec = %v, want %v", specs[2], want)
	}
	// Replies must be limited to established flows, or the last rule
	// would let anything reach a client.
	if !strings.Contains(strings.Join(specs[3], " "), "--ctstate RELATED,ESTABLISHED") {
		t.Errorf("reply spec %v does not require an established connection", specs[3])
	}
}

func TestRCLocalLinesMatchCleanupPrefixes(t *testing.T) {
	for _, egress := range []string{"eth0", "ens3", "enp1s0f0"} {
		lines := RCLocalLines(egress)
		if len(lines) != 4 {
			t.Fatalf("got %d lines, want 4", len(lines))
		}
		for _, line := range lines {
			ok := false
			for _, p := range RCLocalPrefixes {
				if strings.HasPrefix(line, p) {
					ok = true
				}
			}
			if !ok {
				t.Errorf("rc.local line %q is not stripped by RCLocalPrefixes", line)
			}
			// Without -w a boot-time xtables lock race fails the line,
			// and rc.local runs under sh -e.
			if !strings.HasPrefix(line, "iptables -w 5 ") {
				t.Errorf("rc.local line %q does not wait for the xtables lock", line)
			}
		}
	}
}

func TestDropStaleRCLocal(t *testing.T) {
	old := strings.Join(RCLocalLines("ens3"), "\n")
	cur := RCLocalLines("eth0")
	var curEgress string
	for _, l := range cur {
		if strings.Contains(l, "-o eth0 ") {
			curEgress = l
		}
	}
	if curEgress == "" {
		t.Fatal("no egress-specific line in RCLocalLines")
	}
	content := "#!/bin/sh -e\niptables -A INPUT -p tcp --dport 25 -j DROP\n" + old + "\n" + curEgress + "\nexit 0\n"
	got := DropStaleRCLocal(content, cur)

	if strings.Contains(got, "-o ens3") {
		t.Errorf("stale egress line survived:\n%s", got)
	}
	for _, keep := range []string{"-o eth0 -j ACCEPT", "iptables -A INPUT -p tcp --dport 25 -j DROP", "exit 0"} {
		if !strings.Contains(got, keep) {
			t.Errorf("lost %q:\n%s", keep, got)
		}
	}
	// The SMTP/POP3 and reply lines are not egress-specific: they are in
	// keep, so they must not be dropped even though a prefix matches them.
	for _, l := range cur {
		if !strings.Contains(got, l) {
			t.Errorf("dropped current line %q:\n%s", l, got)
		}
	}
}

// fakeChain is an in-memory FORWARD chain that answers the handful of
// iptables calls this package makes, so ordering can be asserted for real.
type fakeChain struct {
	rules []string // each rule is its argument tokens joined by spaces
	calls []string
}

var errNo = errors.New("exit status 1")

func (c *fakeChain) run(args ...string) ([]byte, error) {
	c.calls = append(c.calls, strings.Join(args, " "))
	switch args[0] {
	case "-C":
		if c.index(strings.Join(args[2:], " ")) >= 0 {
			return nil, nil
		}
		return nil, errNo
	case "-I": // -I FORWARD 1 <spec...>
		c.rules = append([]string{strings.Join(args[3:], " ")}, c.rules...)
		return nil, nil
	case "-D":
		if i := c.index(strings.Join(args[2:], " ")); i >= 0 {
			c.rules = append(c.rules[:i], c.rules[i+1:]...)
			return nil, nil
		}
		return nil, errNo
	case "-S":
		out := "-P FORWARD DROP\n"
		for _, r := range c.rules {
			out += "-A FORWARD " + r + "\n"
		}
		return []byte(out), nil
	}
	return nil, errors.New("unexpected iptables call: " + strings.Join(args, " "))
}

func (c *fakeChain) index(rule string) int {
	for i, r := range c.rules {
		if r == rule {
			return i
		}
	}
	return -1
}

func useChain(t *testing.T, initial ...string) *fakeChain {
	t.Helper()
	c := &fakeChain{rules: initial}
	old := iptables
	iptables = c.run
	t.Cleanup(func() { iptables = old })
	return c
}

func specStrings(egress string) []string {
	var out []string
	for _, s := range ForwardSpecs(egress) {
		out = append(out, strings.Join(s, " "))
	}
	return out
}

// A host that restored a ruleset ending in a catch-all REJECT (Oracle Cloud
// images, iptables-persistent). Appended rules would sit behind it and never
// match, so the rules must land ahead of it, in contract order.
var rejectTailHost = []string{
	"-j DOCKER-USER",
	"-j DOCKER-FORWARD",
	"-p tcp -m tcp --dport 25 -j DROP", // v1 installer's global rule, not ours
	"-j REJECT --reject-with icmp-host-prohibited",
}

func TestApplyForwardInsertsAheadOfTrailingReject(t *testing.T) {
	c := useChain(t, rejectTailHost...)
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	want := append(specStrings("eth0"), rejectTailHost...)
	if !reflect.DeepEqual(c.rules, want) {
		t.Errorf("chain:\n got %q\nwant %q", c.rules, want)
	}
}

// What rc.local replays at boot must produce the very same chain as the
// live call, or the first reboot changes the behaviour.
func TestRCLocalReplayMatchesLiveOrder(t *testing.T) {
	live := useChain(t, rejectTailHost...)
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}

	boot := &fakeChain{rules: append([]string{}, rejectTailHost...)}
	for _, line := range RCLocalLines("eth0") {
		fields := strings.Fields(strings.TrimPrefix(line, "iptables -w 5 "))
		if _, err := boot.run(fields...); err != nil {
			t.Fatalf("replaying %q: %v", line, err)
		}
	}
	if !reflect.DeepEqual(boot.rules, live.rules) {
		t.Errorf("boot chain differs from live chain:\n boot %q\n live %q", boot.rules, live.rules)
	}
}

func TestApplyForwardIsIdempotent(t *testing.T) {
	c := useChain(t, rejectTailHost...)
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	first := append([]string{}, c.rules...)
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(c.rules, first) {
		t.Errorf("second ApplyForward changed the chain:\n before %q\n after  %q", first, c.rules)
	}
}

func TestApplyForwardRemovesEveryLegacyCopy(t *testing.T) {
	legacy := strings.Join(legacyBroadAccept, " ")
	c := useChain(t, legacy, legacy, "-j DOCKER-USER", legacy)
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	if c.index(legacy) >= 0 {
		t.Errorf("a legacy copy is left: %q", c.rules)
	}
	if c.index("-j DOCKER-USER") < 0 {
		t.Errorf("removed a rule that is not ours: %q", c.rules)
	}
}

func TestApplyForwardKeepsLegacyRuleWhenInsertFails(t *testing.T) {
	var calls []string
	old := iptables
	iptables = func(args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if args[0] == "-C" || args[0] == "-I" {
			return []byte("iptables: Permission denied"), errNo
		}
		return nil, nil
	}
	t.Cleanup(func() { iptables = old })

	if err := ApplyForward("eth0"); err == nil {
		t.Fatal("expected an error when -I fails")
	}
	for _, c := range calls {
		if strings.HasPrefix(c, "-D ") {
			t.Errorf("removed the legacy rule although the new rules were not installed: %s", c)
		}
	}
}

func TestApplyForwardRejectsBadInterface(t *testing.T) {
	c := useChain(t)
	if err := ApplyForward("eth0; reboot"); err == nil {
		t.Fatal("expected an error")
	}
	if len(c.calls) != 0 {
		t.Errorf("iptables was called with a bad interface name: %q", c.calls)
	}
}

// RemoveForward must delete only hexplus's own rules, whichever egress they
// were created with. Docker's chains, the v1 installer's global SMTP drop
// and an admin's own tun rule live in the same FORWARD chain.
func TestRemoveForwardLeavesForeignRules(t *testing.T) {
	foreign := []string{
		"-j DOCKER-USER",
		"-j DOCKER-FORWARD",
		"-i docker0 -j ACCEPT",
		"-o docker0 -m conntrack --ctstate RELATED,ESTABLISHED -j ACCEPT",
		"-p tcp -m tcp --dport 25 -j DROP",
		"-i tun+ -j ACCEPT", // unscoped: an admin's rule, not ours
		"-j REJECT --reject-with icmp-host-prohibited",
	}
	ours := append(specStrings("ens3"), strings.Join(legacyBroadAccept, " "))
	c := useChain(t, append(append([]string{}, ours...), foreign...)...)

	RemoveForward()

	if !reflect.DeepEqual(c.rules, foreign) {
		t.Errorf("chain after RemoveForward:\n got %q\nwant %q", c.rules, foreign)
	}
}
