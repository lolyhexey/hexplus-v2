package xray

// share.go: generate share-link URIs for the supported protocols and
// render QR codes from them.
//
// Share link format is what the target client apps (v2rayN, v2rayNG,
// Nekoray, Shadowrocket, Clash Meta) understand — usually a scheme +
// user info + host:port + querystring + fragment(remark).
//
// Callers pass:
//   - the Inbound (protocol, port, tag) that owns the client
//   - the Client row (uuid/password/key + email)
//   - the TransportParams that were used to build the inbound
//   - the server address the user should hit (public IP or domain)
//   - an optional remark override (defaults to inbound tag "-" client email)

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/skip2/go-qrcode"
)

// ShareParams collects everything the link builders need.
type ShareParams struct {
	Address string // public server address (IP or domain)
	Remark  string // shown as fragment / label in the client
	Inbound dbInbound
	Client  Client
	Stream  TransportParams
}

// Link returns the share URI for the given client under the given
// inbound. Empty string + nil error means the protocol isn't shareable
// via URL (dokodemo/http/socks — those are point-to-point tunnels or
// need out-of-band credentials).
func Link(p ShareParams) (string, error) {
	remark := p.Remark
	if remark == "" {
		remark = fmt.Sprintf("%s-%s", p.Inbound.Tag, p.Client.Email)
	}
	switch p.Inbound.Protocol {
	case "vless":
		return vlessLink(p, remark), nil
	case "vmess":
		return vmessLink(p, remark), nil
	case "trojan":
		return trojanLink(p, remark), nil
	case "shadowsocks":
		return ssLink(p, remark), nil
	case "hysteria2":
		return hy2Link(p, remark), nil
	case "wireguard":
		// WG uses a plain conf file, not a URI. Callers fall back to
		// the config-file exporter for WG clients.
		return "", nil
	default:
		return "", nil
	}
}

func vlessLink(p ShareParams, remark string) string {
	q := url.Values{}
	q.Set("type", string(orDefault(p.Stream.Network, NetTCP)))
	q.Set("encryption", "none")

	switch p.Stream.Security {
	case SecReality:
		q.Set("security", "reality")
		if len(p.Stream.RealityServerNames) > 0 {
			q.Set("sni", p.Stream.RealityServerNames[0])
		}
		// pbk = the base64 public key derived from the server's private
		// key. Callers embed it in the inbound settings JSON at
		// creation time; extract if it's stashed there.
		if pbk := extractString(p.Inbound.Settings, "realityPublicKey", ""); pbk != "" {
			q.Set("pbk", pbk)
		}
		if len(p.Stream.RealityShortIDs) > 0 {
			q.Set("sid", p.Stream.RealityShortIDs[0])
		}
		if p.Stream.Fingerprint != "" {
			q.Set("fp", p.Stream.Fingerprint)
		} else {
			q.Set("fp", "chrome")
		}
		q.Set("flow", "xtls-rprx-vision")
	case SecTLS:
		q.Set("security", "tls")
		if p.Stream.SNI != "" {
			q.Set("sni", p.Stream.SNI)
		}
		if p.Stream.Fingerprint != "" {
			q.Set("fp", p.Stream.Fingerprint)
		}
	}

	switch p.Stream.Network {
	case NetWS:
		if p.Stream.Path != "" {
			q.Set("path", p.Stream.Path)
		}
		if p.Stream.Host != "" {
			q.Set("host", p.Stream.Host)
		}
	case NetGRPC:
		if p.Stream.ServiceName != "" {
			q.Set("serviceName", p.Stream.ServiceName)
		}
	}

	return fmt.Sprintf("vless://%s@%s:%d?%s#%s",
		p.Client.UUID, p.Address, p.Inbound.Port, q.Encode(), url.QueryEscape(remark))
}

func vmessLink(p ShareParams, remark string) string {
	// vmess:// URIs are base64-encoded JSON in v2rayN's classic format.
	body := map[string]any{
		"v":    "2",
		"ps":   remark,
		"add":  p.Address,
		"port": p.Inbound.Port,
		"id":   p.Client.UUID,
		"aid":  0,
		"scy":  "auto",
		"net":  string(orDefault(p.Stream.Network, NetTCP)),
		"type": "none",
		"host": p.Stream.Host,
		"path": p.Stream.Path,
		"tls":  "",
		"sni":  p.Stream.SNI,
	}
	if p.Stream.Security == SecTLS {
		body["tls"] = "tls"
	}
	raw, _ := json.Marshal(body)
	return "vmess://" + base64.StdEncoding.EncodeToString(raw)
}

func trojanLink(p ShareParams, remark string) string {
	q := url.Values{}
	q.Set("type", string(orDefault(p.Stream.Network, NetTCP)))
	if p.Stream.Security == SecTLS {
		q.Set("security", "tls")
		if p.Stream.SNI != "" {
			q.Set("sni", p.Stream.SNI)
		}
	}
	if p.Stream.Network == NetWS {
		if p.Stream.Path != "" {
			q.Set("path", p.Stream.Path)
		}
		if p.Stream.Host != "" {
			q.Set("host", p.Stream.Host)
		}
	}
	return fmt.Sprintf("trojan://%s@%s:%d?%s#%s",
		url.QueryEscape(p.Client.Password), p.Address, p.Inbound.Port,
		q.Encode(), url.QueryEscape(remark))
}

func ssLink(p ShareParams, remark string) string {
	method := extractString(p.Inbound.Settings, "method", "aes-256-gcm")
	// The classic SS URI encodes method:password (base64) in the userinfo.
	userinfo := base64.RawURLEncoding.EncodeToString([]byte(method + ":" + p.Client.Key))
	return fmt.Sprintf("ss://%s@%s:%d#%s", userinfo, p.Address, p.Inbound.Port, url.QueryEscape(remark))
}

func hy2Link(p ShareParams, remark string) string {
	q := url.Values{}
	if p.Stream.SNI != "" {
		q.Set("sni", p.Stream.SNI)
	}
	q.Set("insecure", "0")
	return fmt.Sprintf("hysteria2://%s@%s:%d/?%s#%s",
		url.QueryEscape(p.Client.Password), p.Address, p.Inbound.Port,
		q.Encode(), url.QueryEscape(remark))
}

// QRPNG renders text as a PNG QR code at the requested pixel size.
// Used by the /api/clients/{id}/qr endpoint.
func QRPNG(text string, size int) ([]byte, error) {
	if size <= 0 {
		size = 256
	}
	return qrcode.Encode(text, qrcode.Medium, size)
}

// orDefault returns v if non-empty, else fallback. Shortcut used by
// the link builders where TransportParams zero-values slip through.
func orDefault[T ~string](v T, fallback T) T {
	if v == "" {
		return fallback
	}
	return v
}

// InboundFromDB is a small helper exported so the panel package can
// build a ShareParams without duplicating the dbInbound shape.
type InboundFromDB struct {
	ID       int64
	Tag      string
	Protocol string
	Port     int
	Settings []byte
}

// ShareForClient is the exported entry point the panel handlers use.
// It hides the internal dbInbound type from callers so the API surface
// of internal/xray stays small.
func ShareForClient(inbound InboundFromDB, streamJSON []byte, client Client, address, remark string) (string, error) {
	var tp TransportParams
	if len(streamJSON) > 0 && string(streamJSON) != "{}" {
		if err := json.Unmarshal(streamJSON, &tp); err != nil {
			return "", err
		}
	}
	return Link(ShareParams{
		Address: address,
		Remark:  remark,
		Inbound: dbInbound{
			ID:       inbound.ID,
			Tag:      inbound.Tag,
			Protocol: inbound.Protocol,
			Port:     inbound.Port,
			Settings: inbound.Settings,
		},
		Client: client,
		Stream: tp,
	})
}

// Ensure strings.HasPrefix stays used elsewhere; imported for future
// share formats.
var _ = strings.HasPrefix
