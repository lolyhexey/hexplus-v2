// Package firewall owns the iptables FORWARD rules that let OpenVPN
// clients reach the internet.
//
// The rules are scoped by device, not by client subnet: every OpenVPN
// instance hexplus creates uses a tun device (tun0 for the primary,
// tun<id> for extra instances), so "tun+" covers all of them without a
// per-instance rule. Forwarding is only allowed out of the host's egress
// interface, so a VPN client cannot route to Docker bridges or any other
// local network the host happens to be attached to.
//
// The rules are inserted at the top of FORWARD, not appended: hosts that
// restore a ruleset ending in "-j REJECT" (Oracle Cloud images,
// iptables-persistent) would never reach an appended rule. Because they sit
// first they are all scoped to tun+, so they cannot shadow anyone else's
// traffic, and they carry their own SMTP/POP3 block so that block still
// wins over the ACCEPT.
package firewall

import (
	"fmt"
	"os/exec"
	"regexp"
	"strings"
)

// tunMatch is the iptables wildcard for every hexplus OpenVPN device.
const tunMatch = "tun+"

// ifaceRe bounds what we accept as an interface name. The name is written
// into /etc/rc.local, which is a shell script, so it must never carry
// shell metacharacters.
var ifaceRe = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,15}$`)

// lockWait makes iptables wait for the xtables lock instead of failing
// when Docker (or another tool) holds it, which happens at boot when
// rc.local runs while docker.service is still programming its chains.
var lockWait = []string{"-w", "5"}

// iptables runs the iptables binary. A variable so tests can fake it.
var iptables = func(args ...string) ([]byte, error) {
	return exec.Command("iptables", append(append([]string{}, lockWait...), args...)...).CombinedOutput()
}

// ipRouteGet runs `ip -4 route get`. A variable so tests can fake it.
var ipRouteGet = func(dst string) ([]byte, error) {
	return exec.Command("ip", "-4", "route", "get", dst).Output()
}

// DetectEgress returns the interface the kernel would use to reach the
// public internet.
func DetectEgress() (string, error) {
	out, err := ipRouteGet("1.1.1.1")
	if err != nil {
		return "", fmt.Errorf("ip route get 1.1.1.1: %w", err)
	}
	return parseEgress(string(out))
}

// parseEgress extracts the device from `ip route get` output such as
// "1.1.1.1 via 10.0.0.1 dev eth0 src 10.0.0.5 uid 0".
func parseEgress(out string) (string, error) {
	f := strings.Fields(out)
	for i, tok := range f {
		if tok == "dev" && i+1 < len(f) {
			if !ifaceRe.MatchString(f[i+1]) {
				return "", fmt.Errorf("unusable egress interface name %q", f[i+1])
			}
			return f[i+1], nil
		}
	}
	return "", fmt.Errorf("no egress device in %q", strings.TrimSpace(out))
}

// ForwardSpecs returns the FORWARD rule specifications (everything after
// "FORWARD") in the order they must end up in the chain, first match first:
//
//  1. VPN clients may not send to SMTP (25) or POP3 (110); VPS providers
//     penalise spam relays
//  2. VPN client traffic leaving through the egress interface
//  3. replies coming back to a tun device, established flows only
//
// The block is written the way `iptables -S` prints it, so RemoveForward
// can recognise the rules by comparing tokens.
func ForwardSpecs(egress string) [][]string {
	return [][]string{
		{"-i", tunMatch, "-p", "tcp", "-m", "tcp", "--dport", "25", "-j", "DROP"},
		{"-i", tunMatch, "-p", "tcp", "-m", "tcp", "--dport", "110", "-j", "DROP"},
		{"-i", tunMatch, "-o", egress, "-j", "ACCEPT"},
		replySpec(),
	}
}

func replySpec() []string {
	return []string{"-o", tunMatch, "-m", "conntrack", "--ctstate", "RELATED,ESTABLISHED", "-j", "ACCEPT"}
}

// RCLocalLines returns the rc.local lines that recreate ForwardSpecs on
// boot. Docker (and any other firewall manager) rebuilds FORWARD at
// startup, so rules that are not persisted vanish on reboot. Each rule is
// inserted at position 1, so the lines are emitted in reverse: the last
// one executed ends up first in the chain.
func RCLocalLines(egress string) []string {
	specs := ForwardSpecs(egress)
	var lines []string
	for i := len(specs) - 1; i >= 0; i-- {
		lines = append(lines, "iptables -w 5 -I FORWARD 1 "+strings.Join(specs[i], " "))
	}
	return lines
}

// RCLocalPrefixes matches the lines RCLocalLines wrote regardless of which
// egress interface they name; uninstall uses it to strip them.
var RCLocalPrefixes = []string{
	"iptables -w 5 -I FORWARD 1 -i " + tunMatch + " ",
	"iptables -w 5 -I FORWARD 1 -o " + tunMatch + " -m conntrack",
}

// DropStaleRCLocal removes rc.local lines that RCLocalPrefixes recognises
// but that are not in keep, e.g. the "-o <old iface>" line left behind
// after the egress interface changed. Lines it does not own are untouched.
func DropStaleRCLocal(content string, keep []string) string {
	kept := map[string]bool{}
	for _, k := range keep {
		kept[k] = true
	}
	var out []string
	for _, line := range strings.Split(content, "\n") {
		stale := false
		if !kept[strings.TrimSpace(line)] {
			for _, p := range RCLocalPrefixes {
				if strings.HasPrefix(strings.TrimSpace(line), p) {
					stale = true
				}
			}
		}
		if !stale {
			out = append(out, line)
		}
	}
	return strings.Join(out, "\n")
}

// legacyBroadAccept is the rule older hexplus versions inserted at the top
// of FORWARD for the primary subnet: any destination, ahead of Docker's
// isolation chains.
var legacyBroadAccept = []string{"-s", "10.8.0.0/16", "-j", "ACCEPT"}

// ApplyForward installs the rules in the running kernel and removes the
// legacy broad rule. It is idempotent: a rule that already exists is left
// alone.
//
// The legacy rule is only removed once the new rules are in place, so a
// failure cannot leave VPN clients with no forwarding at all.
func ApplyForward(egress string) error {
	if !ifaceRe.MatchString(egress) {
		return fmt.Errorf("unusable egress interface name %q", egress)
	}
	specs := ForwardSpecs(egress)
	// Insert last-to-first so the final order matches ForwardSpecs.
	for i := len(specs) - 1; i >= 0; i-- {
		spec := specs[i]
		if _, err := iptables(append([]string{"-C", "FORWARD"}, spec...)...); err == nil {
			continue
		}
		if out, err := iptables(append([]string{"-I", "FORWARD", "1"}, spec...)...); err != nil {
			return fmt.Errorf("iptables -I FORWARD 1 %s: %w: %s",
				strings.Join(spec, " "), err, strings.TrimSpace(string(out)))
		}
	}
	// Repeated installs inserted one copy each; delete until none is left.
	for i := 0; i < 32; i++ {
		if _, err := iptables(append([]string{"-D", "FORWARD"}, legacyBroadAccept...)...); err != nil {
			break
		}
	}
	return nil
}

// RemoveForward deletes every FORWARD rule ApplyForward (or the legacy
// installer) created, whatever egress interface it names.
func RemoveForward() {
	out, err := iptables("-S", "FORWARD")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(out), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "-A" || f[1] != "FORWARD" {
			continue
		}
		if isOurForwardRule(f[2:]) {
			_, _ = iptables(append([]string{"-D", "FORWARD"}, f[2:]...)...)
		}
	}
}

// isOurForwardRule reports whether spec (the part after "-A FORWARD") is a
// rule hexplus owns.
func isOurForwardRule(spec []string) bool {
	if equal(spec, legacyBroadAccept) || equal(spec, replySpec()) {
		return true
	}
	specs := ForwardSpecs("x")
	if equal(spec, specs[0]) || equal(spec, specs[1]) {
		return true
	}
	// -i tun+ -o <iface> -j ACCEPT, for whichever egress it was created with
	return len(spec) == 6 && spec[0] == "-i" && spec[1] == tunMatch && spec[2] == "-o" &&
		ifaceRe.MatchString(spec[3]) && spec[4] == "-j" && spec[5] == "ACCEPT"
}

func equal(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
