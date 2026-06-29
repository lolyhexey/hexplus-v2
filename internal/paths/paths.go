// Package paths centralizes every absolute filesystem path hexplus owns.
//
// Why a separate package: install and service both need these constants,
// and importing install from service would create a cycle (install drives
// the service writer at end-of-install). One small package upstream of
// both breaks that and gives us one obvious place to audit/change paths.
package paths

const (
	// BinDir holds the executable the user invokes from PATH. We copy
	// the running hexplus binary here during install.
	BinDir = "/usr/local/bin"

	// SelfPath is the installed location of hexplus itself.
	SelfPath = BinDir + "/hexplus"

	// MenuShortcut is a symlink → SelfPath created at install time. Gives
	// the operator a shorter command (`menu`) without renaming the canonical
	// binary path that every systemd unit and self-install path bakes in.
	MenuShortcut = BinDir + "/menu"

	// LibDir is where the embedded static binaries land. Picked under
	// /usr/local so distro-managed updates of openvpn / squid / openssh
	// don't fight ours; LFHS says /usr/local/lib is for site software.
	LibDir = "/usr/local/lib/hexplus"

	// StateDir holds state that hexplus itself owns (install marker,
	// later: PKI working dir, runtime caches). Service-specific state
	// stays under /etc/openvpn etc. so distro tooling can still find it.
	StateDir = "/var/lib/hexplus"

	// MarkerFile is the install marker; presence == "install ran cleanly".
	MarkerFile = StateDir + "/installed"

	// LogDir is reserved for our own log surface (P2.5). Services that
	// integrate with systemd still go through the journal.
	LogDir = "/var/log/hexplus"
)
