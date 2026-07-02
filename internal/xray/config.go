package xray

// config.go: build xray-core's config.json from the panel's DB state.
//
// The JSON schema is xray's own (see https://xtls.github.io/config/).
// We model it as Go structs rather than raw map[string]any so protocol
// evolutions surface as compile errors instead of silent runtime drift.
//
// Skeleton for Phase 1 — real inbound/outbound builders arrive with
// the individual protocol tasks in Phase 3.

// Config is the top-level xray-core config document.
type Config struct {
	Log       LogConfig     `json:"log"`
	API       *APIConfig    `json:"api,omitempty"`
	Stats     *StatsConfig  `json:"stats,omitempty"`
	Policy    *PolicyConfig `json:"policy,omitempty"`
	Inbounds  []Inbound     `json:"inbounds"`
	Outbounds []Outbound    `json:"outbounds"`
	Routing   *Routing      `json:"routing,omitempty"`
}

// LogConfig — xray-core log settings. Level "warning" is the sweet
// spot for panels: quiet enough to not spam journald, loud enough to
// see auth failures and TLS handshake errors.
type LogConfig struct {
	Loglevel string `json:"loglevel"`
}

// APIConfig exposes xray's internal gRPC API. We bind it to loopback
// only on port 10085 for the panel to poll stats and reload.
type APIConfig struct {
	Tag      string   `json:"tag"`
	Services []string `json:"services"`
}

// StatsConfig — empty object; presence of the key enables counters.
type StatsConfig struct{}

// PolicyConfig controls per-user counters and stats levels. The panel
// enables StatsUserUplink/Downlink so per-client accounting works.
type PolicyConfig struct {
	Levels map[string]PolicyLevel `json:"levels"`
	System *SystemPolicy          `json:"system,omitempty"`
}

// PolicyLevel is one row in the "policy.levels" map (keyed by numeric
// level as a string, per xray schema).
type PolicyLevel struct {
	StatsUserUplink   bool `json:"statsUserUplink"`
	StatsUserDownlink bool `json:"statsUserDownlink"`
}

// SystemPolicy — inbound/outbound-wide stats toggles.
type SystemPolicy struct {
	StatsInboundUplink    bool `json:"statsInboundUplink"`
	StatsInboundDownlink  bool `json:"statsInboundDownlink"`
	StatsOutboundUplink   bool `json:"statsOutboundUplink"`
	StatsOutboundDownlink bool `json:"statsOutboundDownlink"`
}

// Inbound is one listening endpoint. Protocol-specific settings live
// in Settings and StreamSettings which are typed per-protocol under
// their own files (config_vless.go, config_vmess.go, ...) added later.
type Inbound struct {
	Tag            string         `json:"tag"`
	Listen         string         `json:"listen,omitempty"`
	Port           int            `json:"port"`
	Protocol       string         `json:"protocol"`
	Settings       any            `json:"settings"`
	StreamSettings *StreamSetting `json:"streamSettings,omitempty"`
	Sniffing       *Sniffing      `json:"sniffing,omitempty"`
}

// Outbound routes matched traffic out. freedom/blackhole are the two
// we always ship; more (WARP, chain) join in Phase 8.
type Outbound struct {
	Tag      string `json:"tag"`
	Protocol string `json:"protocol"`
	Settings any    `json:"settings,omitempty"`
}

// StreamSetting is the transport+security envelope shared by every
// protocol (TCP/WS/gRPC/HTTPUpgrade + TLS/Reality). Kept minimal here;
// each transport contributes its own sub-struct in a later file.
type StreamSetting struct {
	Network         string `json:"network"`
	Security        string `json:"security,omitempty"`
	TLSSettings     any    `json:"tlsSettings,omitempty"`
	RealitySettings any    `json:"realitySettings,omitempty"`
	TCPSettings     any    `json:"tcpSettings,omitempty"`
	WSSettings      any    `json:"wsSettings,omitempty"`
	GRPCSettings    any    `json:"grpcSettings,omitempty"`
}

// Sniffing enables xray's protocol sniffer so routing rules can key on
// the outer domain of TLS/HTTP even inside VLESS/VMess.
type Sniffing struct {
	Enabled      bool     `json:"enabled"`
	DestOverride []string `json:"destOverride"`
}

// Routing holds the rule chain that maps inbound tag / SNI / user email
// to a chosen outbound tag. Filled in Phase 8.
type Routing struct {
	DomainStrategy string        `json:"domainStrategy,omitempty"`
	Rules          []RoutingRule `json:"rules,omitempty"`
}

// RoutingRule mirrors one entry from xray's "routing.rules" array.
type RoutingRule struct {
	Type        string   `json:"type"`
	InboundTag  []string `json:"inboundTag,omitempty"`
	Domain      []string `json:"domain,omitempty"`
	IP          []string `json:"ip,omitempty"`
	Protocol    []string `json:"protocol,omitempty"`
	Port        string   `json:"port,omitempty"` // "80,443" or "1000-2000"
	OutboundTag string   `json:"outboundTag,omitempty"`
}
