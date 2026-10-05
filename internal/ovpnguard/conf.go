package ovpnguard

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/lolyhexey/hexplus/internal/atomicfile"
	"github.com/lolyhexey/hexplus/internal/paths"
	"github.com/lolyhexey/hexplus/internal/service"
)

// RunDir holds one management socket per OpenVPN instance. OpenVPN creates
// the sockets world-writable (0777), and anyone who can connect can kill
// sessions, so the directory itself is root-only (0700); a non-root user
// was verified to get "Permission denied".
const RunDir = paths.StateDir + "/run"

// EnabledPath marks the guard as switched on by the operator.
const EnabledPath = paths.StateDir + "/ovpnguard.enabled"

// SocketPath is the management socket of an instance ("main" or an id).
func SocketPath(instance string) string {
	return RunDir + "/ovpn-" + instance + ".sock"
}

var instanceConfRe = regexp.MustCompile(`^server(\d*)\.conf$`)

// InstanceOfConf maps /etc/openvpn/server.conf to "main" and
// server<N>.conf to "N".
func InstanceOfConf(confPath string) (string, bool) {
	m := instanceConfRe.FindStringSubmatch(filepath.Base(confPath))
	if m == nil {
		return "", false
	}
	if m[1] == "" {
		return "main", true
	}
	return m[1], true
}

func confLines(instance string) []string {
	return []string{
		"management " + SocketPath(instance) + " unix",
		"management-client-user root",
	}
}

// InjectConf adds the management socket lines to an OpenVPN server config.
// Idempotent. OpenVPN must be restarted to pick them up.
func InjectConf(confPath string) error {
	instance, ok := InstanceOfConf(confPath)
	if !ok {
		return errors.New("not a hexplus OpenVPN server config: " + confPath)
	}
	if err := os.MkdirAll(RunDir, 0o700); err != nil {
		return err
	}
	if err := os.Chmod(RunDir, 0o700); err != nil {
		return err
	}
	raw, err := os.ReadFile(confPath)
	if err != nil {
		return err
	}
	// OpenVPN keeps only the last management address: ours would silently
	// replace one the operator configured (and inherit its password, so the
	// guard could never log in). Refuse instead.
	if foreign := foreignManagement(string(raw), confLines(instance)); foreign != "" {
		return errors.New("มีการตั้ง management ไว้เองอยู่แล้ว (" + foreign + ") ใน " + confPath)
	}
	out := injectLines(string(raw), confLines(instance))
	if out == string(raw) {
		return nil
	}
	return atomicfile.Write(confPath, []byte(out), 0o644)
}

// StripConf removes exactly the lines InjectConf added, leaving any
// management directive the operator wrote by hand.
func StripConf(confPath string) error {
	instance, ok := InstanceOfConf(confPath)
	if !ok {
		return nil
	}
	raw, err := os.ReadFile(confPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	out := stripLines(string(raw), confLines(instance))
	if out == string(raw) {
		return nil
	}
	return atomicfile.Write(confPath, []byte(out), 0o644)
}

// foreignManagement returns the first management directive in content that
// is not one of ours, or "".
func foreignManagement(content string, ours []string) string {
	own := map[string]bool{}
	for _, l := range ours {
		own[l] = true
	}
	for _, l := range strings.Split(content, "\n") {
		t := strings.TrimSpace(l)
		f := strings.Fields(t)
		if len(f) == 0 || own[t] {
			continue
		}
		if f[0] == "management" || strings.HasPrefix(f[0], "management-") {
			return t
		}
	}
	return ""
}

func injectLines(content string, lines []string) string {
	have := map[string]bool{}
	for _, l := range strings.Split(content, "\n") {
		have[strings.TrimSpace(l)] = true
	}
	var add []string
	for _, l := range lines {
		if !have[l] {
			add = append(add, l)
		}
	}
	if len(add) == 0 {
		return content
	}
	return strings.TrimRight(content, "\n") + "\n" + strings.Join(add, "\n") + "\n"
}

func stripLines(content string, lines []string) string {
	drop := map[string]bool{}
	for _, l := range lines {
		drop[l] = true
	}
	var out []string
	for _, l := range strings.Split(content, "\n") {
		if !drop[strings.TrimSpace(l)] {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// ServerConfs lists every hexplus OpenVPN server config on the host.
func ServerConfs() []string {
	matches, _ := filepath.Glob("/etc/openvpn/server*.conf")
	var out []string
	for _, m := range matches {
		if _, ok := InstanceOfConf(m); ok {
			out = append(out, m)
		}
	}
	return out
}

// Enabled reports whether the operator switched the guard on.
func Enabled() bool {
	_, err := os.Stat(EnabledPath)
	return err == nil
}

// SetEnabled records the operator's choice.
func SetEnabled(on bool) error {
	if !on {
		if err := os.Remove(EnabledPath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(EnabledPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(EnabledPath, []byte("1\n"), 0o644)
}

// Service is the systemd unit that runs the guard loop. AllowHome, because
// v1 limits live in /root/usuarios.db and ProtectHome=true hides /root: the
// guard would then silently enforce nothing for test users and v1 users.
func Service() service.Service {
	return service.Service{
		Name:        "ovpnguard",
		DisplayName: "HEXPLUS OpenVPN device limit guard",
		UnitName:    "hexplus-ovpnguard.service",
		Binary:      paths.SelfPath,
		Args:        []string{"ovpnguard", "run"},
		After:       []string{"network-online.target"},
		AllowHome:   true,
	}
}

// Teardown stops and removes the guard's unit, clears the enabled marker and
// deletes any socket left in RunDir. It does not touch OpenVPN configs.
func Teardown() {
	svc := Service()
	_ = service.Stop(svc)
	_ = service.Disable(svc)
	_ = service.RemoveUnitFor(svc)
	_ = SetEnabled(false)
	socks, _ := filepath.Glob(filepath.Join(RunDir, "ovpn-*.sock"))
	for _, s := range socks {
		_ = os.Remove(s)
	}
}
