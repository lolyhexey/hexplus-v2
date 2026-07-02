package xray

// proto_hysteria2.go: Hysteria2 inbound (UDP, congestion-controlled).
//
// Hysteria2 replaces the older Hysteria (v1) as a UDP transport that
// fakes an HTTP/3 handshake for DPI resistance. Uses TLS 1.3 always.
//
// Note: xray-core's hysteria2 inbound support arrived in 1.8.24+; we
// pin the recent xray tag in Dockerfile.xray so this is available.

type Hy2Client struct {
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
}

type Hy2InboundSettings struct {
	Password  string      `json:"password,omitempty"` // single-user shorthand
	Clients   []Hy2Client `json:"users,omitempty"`
	Obfs      Hy2Obfs     `json:"obfs,omitempty"`
	IgnoreCC  bool        `json:"ignore_client_bandwidth,omitempty"`
}

type Hy2Obfs struct {
	Type     string `json:"type,omitempty"`     // "salamander" is the current one
	Password string `json:"password,omitempty"`
}

func buildHysteria2Settings(clients []Client) Hy2InboundSettings {
	out := Hy2InboundSettings{
		Clients: make([]Hy2Client, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		out.Clients = append(out.Clients, Hy2Client{
			Password: c.Password,
			Email:    c.Email,
		})
	}
	return out
}
