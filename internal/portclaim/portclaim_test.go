package portclaim

import "testing"

func TestHeldByOther(t *testing.T) {
	old := sources
	t.Cleanup(func() { sources = old })
	sources = []func() []Claim{
		func() []Claim { return []Claim{{OpenVPN, "tcp", 1194}} },
		func() []Claim { return []Claim{{SSLH, "tcp", 443}, {Proxy("ws"), "tcp", 8080}} },
		func() []Claim { return nil },
	}
	cases := []struct {
		self, proto string
		port        int
		want        bool
	}{
		{OpenVPN, "tcp", 443, true},   // SSLH holds it: OpenVPN must not close it
		{SSLH, "tcp", 443, false},     // its own port
		{OpenVPN, "tcp", 1194, false}, // its own port
		{SSLH, "tcp", 1194, true},     // OpenVPN's
		{OpenVPN, "udp", 443, false},  // other protocol
		{Proxy("ws"), "tcp", 8080, false},
		{Proxy("ssh"), "tcp", 8080, true},
		{SSLTunnel, "tcp", 9999, false}, // nobody
	}
	for _, c := range cases {
		if got := HeldByOther(c.self)(c.proto, c.port); got != c.want {
			t.Errorf("HeldByOther(%q)(%s, %d) = %v, want %v", c.self, c.proto, c.port, got, c.want)
		}
	}
}

func TestOwnerNames(t *testing.T) {
	if OpenVPNExtra(3) != "openvpn-3" || Proxy("ws-8080") != "proxy-ws-8080" {
		t.Error("owner names changed; ovpninstance builds \"openvpn-<id>\" itself")
	}
}
