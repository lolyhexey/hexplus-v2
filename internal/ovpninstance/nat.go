package ovpninstance

import (
	"errors"
	"fmt"
	"os/exec"
	"strings"
)

// iptablesRun runs iptables and returns its combined output. Tests replace it.
// -w 5 waits for the xtables lock (Docker hosts hold it often), like the
// firewall package does.
var iptablesRun = func(args ...string) ([]byte, error) {
	return exec.Command("iptables", append([]string{"-w", "5"}, args...)...).CombinedOutput()
}

// ruleAbsent reports whether a -C or -D failed because the rule is not
// there (exit status 1). Anything else, such as the xtables lock (exit 4),
// is a real failure and must not be read as "absent".
func ruleAbsent(err error) bool {
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return ee.ExitCode() == 1
	}
	return err != nil && err.Error() == "exit status 1"
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
	out, err := iptablesRun(append([]string{"-t", "nat", "-C"}, spec...)...)
	if err == nil {
		return nil
	}
	if !ruleAbsent(err) {
		return fmt.Errorf("%w %s", err, strings.TrimSpace(string(out)))
	}
	out, err = iptablesRun(append([]string{"-t", "nat", "-A"}, spec...)...)
	if err != nil {
		return fmt.Errorf("%w %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteMasquerade removes every copy of the MASQUERADE rule for subnet (an
// older release could stack several; one -D removes only one) and returns
// how many it deleted. A failure other than "no more copies" is returned.
func DeleteMasquerade(subnet string) (int, error) {
	spec := masqueradeSpec(subnet)
	for n := 0; n < maxMasqueradeCopies; n++ {
		out, err := iptablesRun(append([]string{"-t", "nat", "-D"}, spec...)...)
		if err == nil {
			continue
		}
		if ruleAbsent(err) {
			return n, nil
		}
		return n, fmt.Errorf("%w %s", err, strings.TrimSpace(string(out)))
	}
	return maxMasqueradeCopies, nil
}
