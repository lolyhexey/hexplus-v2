package xray

// proto_shadowsocks.go: Shadowsocks inbound settings (classic + SS 2022).
//
// SS 2022 = new AEAD methods (2022-blake3-aes-128-gcm etc) with
// per-user keys managed by the "clients" array. Classic SS supports
// a single method+password per inbound; when clients > 1 we upgrade
// silently to the 2022 clients-array shape.

// SSClient is one client in a Shadowsocks 2022 inbound.
type SSClient struct {
	Method   string `json:"method,omitempty"` // usually inherited from top-level
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
	Level    int    `json:"level"`
}

// SSInboundSettings drives xray's Shadowsocks settings block. When the
// method starts with "2022-" the clients[] shape is required; older
// methods (aes-128-gcm, chacha20-ietf-poly1305, none) use top-level
// method+password only.
type SSInboundSettings struct {
	Method   string     `json:"method"`
	Password string     `json:"password,omitempty"`
	Network  string     `json:"network,omitempty"` // "tcp,udp" recommended
	Clients  []SSClient `json:"clients,omitempty"`
}

func buildSSSettings(method string, clients []Client) SSInboundSettings {
	out := SSInboundSettings{
		Method:  method,
		Network: "tcp,udp",
	}
	enabled := make([]Client, 0, len(clients))
	for _, c := range clients {
		if c.Enabled {
			enabled = append(enabled, c)
		}
	}
	// SS 2022 → clients array; classic → single top-level password.
	if len(enabled) > 0 && isSS2022(method) {
		out.Clients = make([]SSClient, 0, len(enabled))
		for _, c := range enabled {
			out.Clients = append(out.Clients, SSClient{
				Password: c.Key,
				Email:    c.Email,
			})
		}
		return out
	}
	if len(enabled) > 0 {
		// Classic: use the first enabled client's key.
		out.Password = enabled[0].Key
	}
	return out
}

func isSS2022(method string) bool {
	return len(method) >= 5 && method[:5] == "2022-"
}
