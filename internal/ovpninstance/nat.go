package ovpninstance

import (
	"fmt"
	"os/exec"
	"strings"
)

// iptablesRun runs iptables and returns its combined output. Tests replace it.
var iptablesRun = func(args ...string) ([]byte, error) {
	return exec.Command("iptables", args...).CombinedOutput()
}

// maxMasqueradeCopies bounds DeleteMasquerade, so a rule that cannot be
// deleted cannot loop forever.
const maxMasqueradeCopies = 32

func masqueradeSpec(subnet string) []string {
	return []string{"POSTROUTING", "-s", subnet, "-j", "MASQUERADE"}
}

// EnsureMasquerade adds the nat MASQUERADE rule for subnet unless it is
// already there, so installing again does not stack a second copy.
func EnsureMasquerade(subnet string) error {
	spec := masqueradeSpec(subnet)
	if _, err := iptablesRun(append([]string{"-t", "nat", "-C"}, spec...)...); err == nil {
		return nil
	}
	out, err := iptablesRun(append([]string{"-t", "nat", "-A"}, spec...)...)
	if err != nil {
		return fmt.Errorf("%w %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteMasquerade removes every copy of the MASQUERADE rule for subnet (an
// older release could stack several; one -D removes only one) and returns
// how many it deleted.
func DeleteMasquerade(subnet string) int {
	spec := masqueradeSpec(subnet)
	n := 0
	for ; n < maxMasqueradeCopies; n++ {
		if _, err := iptablesRun(append([]string{"-t", "nat", "-D"}, spec...)...); err != nil {
			break
		}
	}
	return n
}
