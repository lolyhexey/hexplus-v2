package xray

// proto_vless.go: VLESS inbound settings.
//
// VLESS is the "no encryption, rely on the transport" evolution of
// VMess. Client identity is a UUID. When paired with Reality, no cert
// or domain is needed — xray "borrows" a legitimate TLS handshake from
// another site (dest).

// VLESSClient is one client entry in a VLESS inbound's settings.clients[].
type VLESSClient struct {
	ID    string `json:"id"`
	Flow  string `json:"flow,omitempty"`
	Email string `json:"email,omitempty"`
	Level int    `json:"level"`
}

// VLESSInboundSettings is the top-level settings for a VLESS inbound.
type VLESSInboundSettings struct {
	Clients    []VLESSClient `json:"clients"`
	Decryption string        `json:"decryption"` // "none" always for now
	Fallbacks  []Fallback    `json:"fallbacks,omitempty"`
}

// Fallback wires a fallback route when a request doesn't match VLESS
// (used to serve a decoy site alongside VLESS on 443).
type Fallback struct {
	Name string `json:"name,omitempty"`
	Alpn string `json:"alpn,omitempty"`
	Path string `json:"path,omitempty"`
	Dest any    `json:"dest"` // string ("127.0.0.1:80") or int (port)
	Xver int    `json:"xver,omitempty"`
}

// buildVLESSSettings turns the DB-backed clients into xray's
// settings block. flow = "xtls-rprx-vision" for Reality; empty for
// non-Reality VLESS.
func buildVLESSSettings(clients []Client, flow string) VLESSInboundSettings {
	out := VLESSInboundSettings{
		Decryption: "none",
		Clients:    make([]VLESSClient, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		out.Clients = append(out.Clients, VLESSClient{
			ID:    c.UUID,
			Flow:  flow,
			Email: c.Email,
		})
	}
	return out
}
