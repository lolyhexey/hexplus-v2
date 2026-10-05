package firewall

import (
	"fmt"
	"os"
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

// InputRCLocalPrefix starts every line InputRCLocalLine writes.
const InputRCLocalPrefix = "iptables -w 5 -I INPUT 1 -p "

// InputRCLocalLine recreates the rule at boot. Without it a host whose
// INPUT policy is DROP closes the VPN port again after a reboot.
func InputRCLocalLine(proto string, port int) string {
	return "iptables -w 5 -I INPUT 1 " + strings.Join(InputSpec(proto, port), " ")
}

// ParseInputRCLocalLine is the inverse of InputRCLocalLine.
func ParseInputRCLocalLine(line string) (proto string, port int, ok bool) {
	f := strings.Fields(line)
	if len(f) != 12 || !strings.HasPrefix(strings.TrimSpace(line), InputRCLocalPrefix) ||
		f[8] != "--dport" || f[10] != "-j" || f[11] != "ACCEPT" {
		return "", 0, false
	}
	port, err := strconv.Atoi(f[9])
	if err != nil || validInput(f[7], port) != nil || InputRCLocalLine(f[7], port) != strings.TrimSpace(line) {
		return "", 0, false
	}
	return f[7], port, true
}

// MoveInput follows a port change: it removes the rule and rc.local line for
// oldPort and installs and persists them for newPort. Without it the old
// port stayed open at every boot and, on a host whose INPUT policy is DROP,
// the new port was unreachable.
func MoveInput(proto string, oldPort, newPort int, rcLocal string) error {
	if err := validInput(proto, newPort); err != nil {
		return err
	}
	if oldPort != newPort && validInput(proto, oldPort) == nil {
		RemoveInput(proto, oldPort)
		old := InputRCLocalLine(proto, oldPort)
		if err := RemoveRCLocalLines(rcLocal, func(l string) bool { return l == old }); err != nil {
			return err
		}
	}
	if err := ApplyInput(proto, newPort); err != nil {
		return err
	}
	return AddRCLocalLine(rcLocal, InputRCLocalLine(proto, newPort))
}

// RemovePersistedInput deletes, live and from rc.local, every INPUT rule an
// rc.local line written by InputRCLocalLine names. Uninstall uses it after
// the extra instances are gone, so what is left belongs to the primary
// instance, including lines for ports it used before a port change.
func RemovePersistedInput(rcLocal string) error {
	raw, err := os.ReadFile(rcLocal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if proto, port, ok := ParseInputRCLocalLine(l); ok {
			RemoveInput(proto, port)
		}
	}
	return RemoveRCLocalLines(rcLocal, func(l string) bool {
		_, _, ok := ParseInputRCLocalLine(l)
		return ok
	})
}
