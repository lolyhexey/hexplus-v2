package firewall

import (
	"fmt"
	"strconv"
	"strings"
)

// InputSpec is the INPUT rule that lets clients reach an OpenVPN port.
func InputSpec(proto string, port int) []string {
	return []string{"-p", proto, "--dport", strconv.Itoa(port), "-j", "ACCEPT"}
}

func validInput(proto string, port int) error {
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("proto %q is not tcp or udp", proto)
	}
	if port < 1 || port > 65535 {
		return fmt.Errorf("port %d out of range", port)
	}
	return nil
}

// ApplyInput opens proto/port in INPUT, ahead of any DROP or REJECT the host
// already has. It is idempotent: an existing rule is left alone, so a
// reinstall does not stack copies.
func ApplyInput(proto string, port int) error {
	if err := validInput(proto, port); err != nil {
		return err
	}
	spec := InputSpec(proto, port)
	if _, err := iptables(append([]string{"-C", "INPUT"}, spec...)...); err == nil {
		return nil
	}
	if out, err := iptables(append([]string{"-I", "INPUT", "1"}, spec...)...); err != nil {
		return fmt.Errorf("iptables -I INPUT 1 %s: %w: %s", strings.Join(spec, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// RemoveInput deletes every copy of the rule ApplyInput (or an older
// installer, which inserted the same rule without checking) created.
func RemoveInput(proto string, port int) {
	if validInput(proto, port) != nil {
		return
	}
	spec := InputSpec(proto, port)
	for i := 0; i < 32; i++ {
		if _, err := iptables(append([]string{"-D", "INPUT"}, spec...)...); err != nil {
			return
		}
	}
}

// InputRCLocalLine recreates the rule at boot. Without it a host whose
// INPUT policy is DROP closes the VPN port again after a reboot.
func InputRCLocalLine(proto string, port int) string {
	return "iptables -w 5 -I INPUT 1 " + strings.Join(InputSpec(proto, port), " ")
}
