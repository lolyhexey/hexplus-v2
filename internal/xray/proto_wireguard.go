package xray

// proto_wireguard.go: WireGuard inbound.
//
// Xray's WireGuard support treats the daemon as a userspace WG
// endpoint; peers are the "clients" here. Keys are the standard
// x25519 pair per peer.

type WGPeer struct {
	PublicKey    string   `json:"publicKey"`
	PreSharedKey string   `json:"preSharedKey,omitempty"`
	AllowedIPs   []string `json:"allowedIPs,omitempty"`
	Endpoint     string   `json:"endpoint,omitempty"`
	KeepAlive    int      `json:"keepAlive,omitempty"`
}

type WGInboundSettings struct {
	SecretKey string   `json:"secretKey"` // server-side x25519 private, base64
	Peers     []WGPeer `json:"peers"`
	MTU       int      `json:"mtu,omitempty"`
	KernelMode bool    `json:"kernelMode,omitempty"`
}

func buildWireGuardSettings(secretKey string, clients []Client) WGInboundSettings {
	out := WGInboundSettings{
		SecretKey: secretKey,
		Peers:     make([]WGPeer, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled {
			continue
		}
		out.Peers = append(out.Peers, WGPeer{
			PublicKey:  c.Key, // client's public key
			AllowedIPs: []string{"0.0.0.0/0"},
		})
	}
	return out
}
