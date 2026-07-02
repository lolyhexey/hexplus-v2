package xray

// proto_trojan.go: Trojan inbound settings.
//
// Trojan uses a per-client plaintext password checked against SHA-224
// over the wire; TLS is what makes it look like plain HTTPS traffic.
// Almost always paired with TLS+TCP or TLS+WS+CDN.

// TrojanClient is one client entry in a Trojan inbound.
type TrojanClient struct {
	Password string `json:"password"`
	Email    string `json:"email,omitempty"`
	Level    int    `json:"level"`
	Flow     string `json:"flow,omitempty"` // "xtls-rprx-vision" for XTLS variant
}

// TrojanInboundSettings drives xray's Trojan settings block.
type TrojanInboundSettings struct {
	Clients   []TrojanClient `json:"clients"`
	Fallbacks []Fallback     `json:"fallbacks,omitempty"`
}

func buildTrojanSettings(clients []Client, flow string) TrojanInboundSettings {
	out := TrojanInboundSettings{
		Clients: make([]TrojanClient, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		out.Clients = append(out.Clients, TrojanClient{
			Password: c.Password,
			Email:    c.Email,
			Flow:     flow,
		})
	}
	return out
}
