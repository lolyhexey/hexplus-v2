package xray

// transport.go: shared transport + security envelopes for every
// inbound. Each protocol picks a NetworkKind + SecurityKind and passes
// protocol-specific params through Extras; buildStreamSetting turns
// that triplet into xray's StreamSetting.
//
// Adding a new transport (say XHTTP) means: bump the NetworkKind
// consts, extend the switch in buildStreamSetting, and add a matching
// sub-struct here.

import (
	"fmt"
)

// NetworkKind is the L4 transport for an inbound.
type NetworkKind string

const (
	NetTCP          NetworkKind = "tcp"
	NetWS           NetworkKind = "ws"
	NetGRPC         NetworkKind = "grpc"
	NetHTTPUpgrade  NetworkKind = "httpupgrade"
	NetXHTTP        NetworkKind = "xhttp"
	NetKCP          NetworkKind = "kcp"
)

// SecurityKind is the TLS/Reality/None envelope.
type SecurityKind string

const (
	SecNone    SecurityKind = "none"
	SecTLS     SecurityKind = "tls"
	SecReality SecurityKind = "reality"
)

// TransportParams collects every knob a caller might set for a stream.
// Zero values are safe defaults (network=tcp, security=none).
type TransportParams struct {
	Network  NetworkKind
	Security SecurityKind

	// WS/HTTPUpgrade/XHTTP-only
	Path string
	Host string

	// gRPC-only
	ServiceName string

	// TLS-only
	SNI           string
	ALPN          []string
	Fingerprint   string   // "chrome" | "firefox" | "safari" | "ios" | "android" | "randomized" | ""
	Certificates  []TLSCertificate

	// Reality-only
	RealityDest        string   // e.g. "www.microsoft.com:443"
	RealityServerNames []string // fronting hostnames
	RealityPrivateKey  string   // base64 RawURL
	RealityShortIDs    []string
	RealitySpiderX     string   // default "/"
}

// TLSCertificate mirrors xray's tlsSettings.certificates[] shape.
type TLSCertificate struct {
	CertificateFile string   `json:"certificateFile,omitempty"`
	KeyFile         string   `json:"keyFile,omitempty"`
	Certificate     []string `json:"certificate,omitempty"`
	Key             []string `json:"key,omitempty"`
}

// WSSettings drives xray's wsSettings inbound block.
type WSSettings struct {
	Path    string            `json:"path,omitempty"`
	Headers map[string]string `json:"headers,omitempty"`
}

// GRPCSettings drives xray's grpcSettings.
type GRPCSettings struct {
	ServiceName string `json:"serviceName,omitempty"`
	MultiMode   bool   `json:"multiMode,omitempty"`
}

// HTTPUpgradeSettings drives xray's httpupgradeSettings.
type HTTPUpgradeSettings struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
}

// XHTTPSettings drives xray's xhttpSettings.
type XHTTPSettings struct {
	Path string `json:"path,omitempty"`
	Host string `json:"host,omitempty"`
	Mode string `json:"mode,omitempty"` // "auto" | "packet-up" | "stream-up"
}

// TCPSettings drives xray's tcpSettings — mostly the header for
// "obfuscation as HTTP" mode.
type TCPSettings struct {
	Header map[string]any `json:"header,omitempty"`
}

// TLSSettings mirrors xray's tlsSettings inbound block. Kept minimal
// on purpose; add fields as new use cases arrive.
type TLSSettings struct {
	ServerName    string           `json:"serverName,omitempty"`
	ALPN          []string         `json:"alpn,omitempty"`
	Fingerprint   string           `json:"fingerprint,omitempty"`
	Certificates  []TLSCertificate `json:"certificates,omitempty"`
	MinVersion    string           `json:"minVersion,omitempty"`
	MaxVersion    string           `json:"maxVersion,omitempty"`
}

// RealitySettings mirrors xray's realitySettings inbound block.
type RealitySettings struct {
	Show        bool     `json:"show"`
	Dest        string   `json:"dest"`
	Xver        int      `json:"xver"`
	ServerNames []string `json:"serverNames"`
	PrivateKey  string   `json:"privateKey"`
	ShortIDs    []string `json:"shortIds"`
	SpiderX     string   `json:"spiderX,omitempty"`
}

// buildStreamSetting assembles a StreamSetting from the params.
func buildStreamSetting(p TransportParams) (*StreamSetting, error) {
	if p.Network == "" {
		p.Network = NetTCP
	}
	if p.Security == "" {
		p.Security = SecNone
	}
	s := &StreamSetting{
		Network: string(p.Network),
	}
	if p.Security != SecNone {
		s.Security = string(p.Security)
	}

	// Transport-specific block.
	switch p.Network {
	case NetTCP:
		// TCP is the default; no settings needed unless we later add
		// header obfuscation.
	case NetWS:
		ws := WSSettings{Path: p.Path}
		if p.Host != "" {
			ws.Headers = map[string]string{"Host": p.Host}
		}
		s.WSSettings = ws
	case NetGRPC:
		s.GRPCSettings = GRPCSettings{ServiceName: p.ServiceName}
	case NetHTTPUpgrade:
		s.TCPSettings = HTTPUpgradeSettings{Path: p.Path, Host: p.Host}
	case NetXHTTP:
		s.TCPSettings = XHTTPSettings{Path: p.Path, Host: p.Host, Mode: "auto"}
	case NetKCP:
		// mKCP settings kept default; per-inbound tuning arrives when
		// we surface knobs in the UI.
	default:
		return nil, fmt.Errorf("unknown network kind %q", p.Network)
	}

	// Security-specific block.
	switch p.Security {
	case SecNone:
		// nothing
	case SecTLS:
		s.TLSSettings = TLSSettings{
			ServerName:   p.SNI,
			ALPN:         p.ALPN,
			Fingerprint:  p.Fingerprint,
			Certificates: p.Certificates,
		}
	case SecReality:
		if p.RealityPrivateKey == "" {
			return nil, fmt.Errorf("reality: privateKey required")
		}
		if p.RealityDest == "" {
			return nil, fmt.Errorf("reality: dest required")
		}
		spider := p.RealitySpiderX
		if spider == "" {
			spider = "/"
		}
		s.RealitySettings = RealitySettings{
			Dest:        p.RealityDest,
			ServerNames: p.RealityServerNames,
			PrivateKey:  p.RealityPrivateKey,
			ShortIDs:    p.RealityShortIDs,
			SpiderX:     spider,
		}
	default:
		return nil, fmt.Errorf("unknown security kind %q", p.Security)
	}

	return s, nil
}
