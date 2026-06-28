// Package service owns the three first-party services HEXPLUS installs
// (openvpn, squid, dropbear), the systemd units that drive them, and the
// subcommand surface for starting/stopping/inspecting them.
//
// Naming: every unit is hexplus-<name>.service so HEXPLUS can never
// clash with a distro-packaged openvpn/squid/ssh unit on the same box.
// Binaries live under paths.LibDir (/usr/local/lib/hexplus/) and the
// dropbear symlinks are created at install time because dropbearmulti
// dispatches on argv[0] - systemd can't override that without a wrapper.
package service

import (
	"github.com/lolyhexey/hexplus/internal/paths"
)

// Service is the static descriptor for one of HEXPLUS's three units.
// All fields are read-only after construction; All() returns the
// canonical set.
type Service struct {
	// Name is the short identifier the user types: openvpn, squid, dropbear.
	Name string

	// DisplayName goes into the unit Description= and human-facing output.
	DisplayName string

	// UnitName is the full systemd unit name, including the .service suffix.
	UnitName string

	// Binary is the absolute path to the executable systemd ExecStart's.
	// For dropbear this points at the symlink we create on install
	// (LibDir/dropbear -> LibDir/dropbearmulti), not the multi binary
	// directly, so argv[0] picks the right dispatch inside dropbearmulti.
	Binary string

	// Args are the static arguments. Config paths are baked in; the
	// install bootstrap (P2.3) writes the configs themselves if they
	// don't already exist.
	Args []string

	// Port and PortProto describe where the service is expected to listen,
	// used by the status command to verify the unit is actually up.
	Port      int
	PortProto string // "tcp" or "udp"

	// After lists systemd unit dependencies for the [Unit] After= line.
	After []string

	// AllowHome disables ProtectHome=true in the unit — needed for services
	// that must read from /root/.
	AllowHome bool
}

// OVPNDir is the directory served by the built-in file server.
const OVPNDir = "/root/openvpn"

// All returns the canonical service set in stable order. Iteration order
// matches install / status / logs displays.
func All() []Service {
	return []Service{
		{
			Name:        "openvpn",
			DisplayName: "HEXPLUS OpenVPN server",
			UnitName:    "hexplus-openvpn.service",
			Binary:      paths.LibDir + "/openvpn",
			Args:        []string{"--config", "/etc/openvpn/server.conf"},
			Port:        1194,
			PortProto:   "udp",
			After:       []string{"network-online.target"},
		},
		{
			Name:        "squid",
			DisplayName: "HEXPLUS Squid HTTP proxy",
			UnitName:    "hexplus-squid.service",
			Binary:      paths.LibDir + "/squid",
			// -N: don't daemonize; systemd is our parent.
			// -f: explicit config path so we don't drift to /etc/squid/squid.conf
			//     surprises from a distro squid install.
			Args:      []string{"-N", "-f", "/etc/squid/squid.conf"},
			Port:      3128,
			PortProto: "tcp",
			After:     []string{"network-online.target"},
		},
		{
			Name:        "fileserver",
			DisplayName: "HEXPLUS OVPN file server",
			UnitName:    "hexplus-fileserver.service",
			Binary:      "/usr/local/bin/hexplus",
			Args:        []string{"fileserver"},
			Port:        82,
			PortProto:   "tcp",
			After:       []string{"network-online.target"},
			AllowHome:   true,
		},
		{
			Name:        "dropbear",
			DisplayName: "HEXPLUS Dropbear SSH server",
			UnitName:    "hexplus-dropbear.service",
			// We symlink LibDir/dropbear -> LibDir/dropbearmulti on install
			// so argv[0]="dropbear" dispatches the server inside the multi
			// binary. systemd can't rewrite argv[0] otherwise.
			Binary: paths.LibDir + "/dropbear",
			// -F: foreground (no daemonize) so systemd Type=simple sees PID.
			// -R: generate host keys lazily if missing.
			// -p: listen on this port (overridden via drop-in if user changes it).
			// Note: no -E flag — built without syslog so stderr is already default.
			Args:      []string{"-F", "-R", "-p", "22"},
			Port:      22,
			PortProto: "tcp",
			After:     []string{"network-online.target"},
		},
	}
}

// ByName looks up one service from All() by short name. Returns the zero
// value and ok=false when the name doesn't match.
func ByName(name string) (Service, bool) {
	for _, s := range All() {
		if s.Name == name {
			return s, true
		}
	}
	return Service{}, false
}

// Names returns the short names in stable order; convenience for listings
// and validation.
func Names() []string {
	all := All()
	out := make([]string, len(all))
	for i, s := range all {
		out[i] = s.Name
	}
	return out
}
