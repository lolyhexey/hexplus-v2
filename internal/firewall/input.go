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
//
// keep, when not nil, reports ports another hexplus service still listens
// on: the old port then stays open for it.
func MoveInput(proto string, oldPort, newPort int, rcLocal string, keep func(proto string, port int) bool) error {
	if err := validInput(proto, newPort); err != nil {
		return err
	}
	if oldPort != newPort && validInput(proto, oldPort) == nil && (keep == nil || !keep(proto, oldPort)) {
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
// rc.local line written by InputRCLocalLine names, except the ports keep
// reports (another hexplus service such as SSLH or SSL TUNNEL listens
// there). OpenVPN's uninstall uses it after the extra instances are gone,
// so what is left of OpenVPN's belongs to the primary, including lines for
// ports it used before a port change.
func RemovePersistedInput(rcLocal string, keep func(proto string, port int) bool) error {
	raw, err := os.ReadFile(rcLocal)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	ours := func(l string) (string, int, bool) {
		proto, port, ok := ParseInputRCLocalLine(l)
		return proto, port, ok && (keep == nil || !keep(proto, port))
	}
	for _, l := range strings.Split(string(raw), "\n") {
		if proto, port, ok := ours(l); ok {
			RemoveInput(proto, port)
		}
	}
	return RemoveRCLocalLines(rcLocal, func(l string) bool {
		_, _, ok := ours(l)
		return ok
	})
}

// OpenPort opens proto/port in INPUT and persists it in rc.local, for a
// service that listens there (SSLH, SSL TUNNEL, a proxy). On a host whose
// INPUT policy is DROP these were unreachable: only OpenVPN opened its port.
func OpenPort(proto string, port int, rcLocal string) error {
	if err := ApplyInput(proto, port); err != nil {
		return err
	}
	return AddRCLocalLine(rcLocal, InputRCLocalLine(proto, port))
}

// ClosePort undoes OpenPort, unless keep reports that another hexplus
// service still listens on the port.
func ClosePort(proto string, port int, rcLocal string, keep func(proto string, port int) bool) error {
	if err := validInput(proto, port); err != nil {
		return err
	}
	if keep != nil && keep(proto, port) {
		return nil
	}
	RemoveInput(proto, port)
	line := InputRCLocalLine(proto, port)
	return RemoveRCLocalLines(rcLocal, func(l string) bool { return l == line })
}
