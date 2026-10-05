// Package portclaim lists the ports hexplus services listen on, so that
// closing a port in the firewall for one service does not close it under
// another. Two hexplus services cannot listen on one port at the same time
// (every install checks), but a port is still claimed by a stopped service,
// and the INPUT rule and rc.local line are the same whoever opened them.
package portclaim

import (
	"strconv"

	"github.com/lolyhexey/hexplus/internal/ovpninstance"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/sslhmux"
	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// Claim is one service's port.
type Claim struct {
	Owner string // "openvpn", "openvpn-<id>", "sslhmux", "ssltunnel", "proxy-<name>"
	Proto string
	Port  int
}

// Owner names for the services that open their own port.
const (
	OpenVPN   = "openvpn"
	SSLH      = "sslhmux"
	SSLTunnel = "ssltunnel"
)

// OpenVPNExtra is the owner name of extra OpenVPN instance id.
func OpenVPNExtra(id int) string { return "openvpn-" + strconv.Itoa(id) }

// Proxy is the owner name of the proxy called name.
func Proxy(name string) string { return "proxy-" + name }

// sources read each service's configuration; tests replace them.
var sources = []func() []Claim{
	func() []Claim {
		proto, port, err := pki.ReadServerListen(pki.ServerConfPath)
		if err != nil {
			return nil
		}
		return []Claim{{OpenVPN, proto, port}}
	},
	func() []Claim {
		insts, _ := ovpninstance.List()
		var out []Claim
		for _, i := range ovpninstance.Extras(insts) { // workers open no port
			out = append(out, Claim{OpenVPNExtra(i.ID), i.Proto, i.Port})
		}
		return out
	},
	func() []Claim {
		if cfg, err := sslhmux.Load(); err == nil && cfg.Port > 0 {
			return []Claim{{SSLH, "tcp", cfg.Port}}
		}
		return nil
	},
	func() []Claim {
		if cfg, err := ssltunnel.Load(); err == nil && cfg.Port > 0 {
			return []Claim{{SSLTunnel, "tcp", cfg.Port}}
		}
		return nil
	},
	func() []Claim {
		db, err := proxy.Load()
		if err != nil {
			return nil
		}
		var out []Claim
		for _, c := range db.All() {
			if c.Port > 0 {
				out = append(out, Claim{Proxy(c.Name), "tcp", c.Port})
			}
		}
		return out
	},
}

// All returns every claim found now.
func All() []Claim {
	var out []Claim
	for _, src := range sources {
		out = append(out, src()...)
	}
	return out
}

// HeldByOther returns a check for the firewall helpers' keep argument:
// whether a service other than self listens on proto/port.
func HeldByOther(self string) func(proto string, port int) bool {
	return func(proto string, port int) bool {
		for _, c := range All() {
			if c.Owner != self && c.Proto == proto && c.Port == port {
				return true
			}
		}
		return false
	}
}

func init() {
	// ovpninstance sits below this package; it asks through a hook.
	ovpninstance.InputHeldByOther = HeldByOther
}
