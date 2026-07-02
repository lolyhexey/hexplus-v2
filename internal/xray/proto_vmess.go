package xray

// proto_vmess.go: VMess inbound settings.
//
// VMess is VLESS's older sibling: has its own encryption but heavier
// per-packet overhead. Client identity is a UUID + alterID (deprecated;
// modern configs use 0). AEAD mode is on by default in xray-core 1.8+.

// VMessClient is one client entry in a VMess inbound's settings.clients[].
type VMessClient struct {
	ID      string `json:"id"`
	Email   string `json:"email,omitempty"`
	Level   int    `json:"level"`
	AlterID int    `json:"alterId"`
}

// VMessInboundSettings is the top-level settings for a VMess inbound.
type VMessInboundSettings struct {
	Clients []VMessClient `json:"clients"`
}

func buildVMessSettings(clients []Client) VMessInboundSettings {
	out := VMessInboundSettings{
		Clients: make([]VMessClient, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		out.Clients = append(out.Clients, VMessClient{
			ID:      c.UUID,
			Email:   c.Email,
			AlterID: 0,
		})
	}
	return out
}
