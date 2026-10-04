//go:build integration && linux

package firewall

import (
	"os"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

// This test drives the real iptables binary against the FORWARD chain of the
// network namespace it runs in. It FLUSHES that chain, so it refuses to run
// unless HEXPLUS_FIREWALL_IT=1 and is meant for a throwaway container:
//
//	go test -c -tags integration -o fw.test ./internal/firewall
//	docker run --rm -i --cap-add NET_ADMIN -e HEXPLUS_FIREWALL_IT=1 \
//	    ubuntu:22.04 sh -c 'apt-get update -qq && apt-get install -y -qq iptables >/dev/null && cat >/fw.test && chmod +x /fw.test && /fw.test -test.v' < fw.test
func TestRealIptables(t *testing.T) {
	if os.Getenv("HEXPLUS_FIREWALL_IT") != "1" {
		t.Skip("set HEXPLUS_FIREWALL_IT=1 in a throwaway network namespace to run")
	}
	if os.Geteuid() != 0 {
		t.Skip("needs root")
	}
	if _, err := exec.LookPath("iptables"); err != nil {
		t.Skip("iptables not installed")
	}

	sh := func(cmd string) string {
		t.Helper()
		out, err := exec.Command("sh", "-c", cmd).CombinedOutput()
		if err != nil {
			t.Fatalf("%s: %v\n%s", cmd, err, out)
		}
		return strings.TrimSpace(string(out))
	}
	// chain returns the FORWARD rules as `iptables -S` prints them, without
	// the "-A FORWARD " prefix.
	chain := func() []string {
		t.Helper()
		var rules []string
		for _, l := range strings.Split(sh("iptables -S FORWARD"), "\n") {
			if strings.HasPrefix(l, "-A FORWARD ") {
				rules = append(rules, strings.TrimPrefix(l, "-A FORWARD "))
			}
		}
		return rules
	}
	// dockerLikeHost resets FORWARD to a Docker host that also restored a
	// ruleset ending in a catch-all REJECT, plus two copies of the legacy rule
	// the old installer left behind.
	dockerLikeHost := func() {
		t.Helper()
		sh("iptables -F FORWARD; iptables -P FORWARD DROP")
		sh("iptables -N FAKE-DOCKER 2>/dev/null || iptables -F FAKE-DOCKER")
		sh("iptables -A FORWARD -j FAKE-DOCKER")
		sh("iptables -A FORWARD -j REJECT --reject-with icmp-host-prohibited")
		sh("iptables -I FORWARD -s 10.8.0.0/16 -j ACCEPT")
		sh("iptables -I FORWARD -s 10.8.0.0/16 -j ACCEPT")
	}
	foreign := []string{"-j FAKE-DOCKER", "-j REJECT --reject-with icmp-host-prohibited"}
	want := append(specStrings("eth0"), foreign...)
	t.Cleanup(func() { sh("iptables -F FORWARD; iptables -P FORWARD ACCEPT") })

	// 1. live install: new rules first in contract order, legacy gone,
	// foreign rules untouched and still in their original order.
	dockerLikeHost()
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	live := chain()
	if !reflect.DeepEqual(live, want) {
		t.Fatalf("after ApplyForward:\n got %q\nwant %q", live, want)
	}

	// 2. idempotent
	if err := ApplyForward("eth0"); err != nil {
		t.Fatal(err)
	}
	if got := chain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("second ApplyForward changed the chain:\n got %q\nwant %q", got, want)
	}

	// 3. what rc.local replays at boot, executed by a real shell on a fresh
	// Docker-like chain, must give the same chain
	dockerLikeHost()
	sh("iptables -D FORWARD -s 10.8.0.0/16 -j ACCEPT; iptables -D FORWARD -s 10.8.0.0/16 -j ACCEPT")
	for _, line := range RCLocalLines("eth0") {
		sh(line)
	}
	if got := chain(); !reflect.DeepEqual(got, want) {
		t.Fatalf("rc.local replay differs from the live install:\n got %q\nwant %q", got, want)
	}

	// 4. uninstall removes ours, even for another egress name, and only ours
	sh("iptables -I FORWARD 1 -i tun+ -o ens3 -j ACCEPT") // stale rule from an old egress
	RemoveForward()
	if got := chain(); !reflect.DeepEqual(got, foreign) {
		t.Fatalf("after RemoveForward:\n got %q\nwant %q", got, foreign)
	}
}
