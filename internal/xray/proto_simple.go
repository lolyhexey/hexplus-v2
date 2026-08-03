package xray

// proto_simple.go: HTTP, SOCKS, and Dokodemo-door inbounds — the
// "plain proxy" shapes xray offers that don't need protocol-specific
// client metadata. Kept in one file because each is a dozen lines.

// HTTPAccount is one HTTP-proxy credential.
type HTTPAccount struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// HTTPInboundSettings is xray's http inbound settings.
type HTTPInboundSettings struct {
	Accounts         []HTTPAccount `json:"accounts,omitempty"`
	AllowTransparent bool          `json:"allowTransparent,omitempty"`
	UserLevel        int           `json:"userLevel,omitempty"`
}

func buildHTTPSettings(clients []Client) HTTPInboundSettings {
	out := HTTPInboundSettings{
		Accounts: make([]HTTPAccount, 0, len(clients)),
	}
	for _, c := range clients {
		if !c.Enabled || c.Email == "" || c.Password == "" {
			continue
		}
		out.Accounts = append(out.Accounts, HTTPAccount{User: c.Email, Pass: c.Password})
	}
	return out
}

// SOCKSAccount is one SOCKS credential (identical shape to HTTP, kept
// separate so the JSON tag differences stay honest per xray schema).
type SOCKSAccount struct {
	User string `json:"user"`
	Pass string `json:"pass"`
}

// SOCKSInboundSettings drives xray's socks inbound.
type SOCKSInboundSettings struct {
	Auth      string         `json:"auth"` // "noauth" | "password"
	Accounts  []SOCKSAccount `json:"accounts,omitempty"`
	UDP       bool           `json:"udp"`
	IP        string         `json:"ip,omitempty"`
	UserLevel int            `json:"userLevel,omitempty"`
}

func buildSOCKSSettings(clients []Client) SOCKSInboundSettings {
	auth := "noauth"
	accounts := make([]SOCKSAccount, 0, len(clients))
	for _, c := range clients {
		if !c.Enabled || c.Email == "" || c.Password == "" {
			continue
		}
		accounts = append(accounts, SOCKSAccount{User: c.Email, Pass: c.Password})
	}
	if len(accounts) > 0 {
		auth = "password"
	}
	return SOCKSInboundSettings{
		Auth:     auth,
		Accounts: accounts,
		UDP:      true,
	}
}

// DokodemoInboundSettings is a plain tunnel-forwarder (xray's "dokodemo-
// door" name = "any door"). Used to expose a fixed upstream through the
// panel; no per-client credentials.
type DokodemoInboundSettings struct {
	Address        string `json:"address"`
	Port           int    `json:"port"`
	Network        string `json:"network,omitempty"` // "tcp" | "udp" | "tcp,udp"
	FollowRedirect bool   `json:"followRedirect,omitempty"`
}

// buildDokodemoSettings reads the address+port out of the inbound's
// stored settings JSON. Callers pass address/port explicitly since
// there are no clients in this shape.
func buildDokodemoSettings(address string, port int, network string) DokodemoInboundSettings {
	if network == "" {
		network = "tcp,udp"
	}
	return DokodemoInboundSettings{
		Address: address,
		Port:    port,
		Network: network,
	}
}
