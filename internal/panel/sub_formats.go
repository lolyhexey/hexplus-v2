package panel

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// sub_formats.go: convert one xray inbound + client tuple into the
// proxies-block for popular client apps (Clash / Sing-box). Both
// formats are lossy vs a raw xray URL — anything Clash/Sing-box
// can't express (SS 2022 clients array, mKCP tuning) is dropped.

// clashProxy is a Clash proxy YAML row. We emit YAML by hand — Clash
// accepts JSON too but reference clients (Clash Meta, Mihomo) prefer
// YAML, and json.Marshal produces valid YAML for our shape.

type clashProxy map[string]any

// buildClashProxy converts one client into a single Clash proxy entry.
// Returns nil when the protocol can't be expressed (e.g. Dokodemo).
func buildClashProxy(
	inbound xray.InboundFromDB,
	streamJSON []byte,
	client xray.Client,
	address string,
) clashProxy {
	var s map[string]any
	if len(streamJSON) > 0 && string(streamJSON) != "{}" {
		_ = json.Unmarshal(streamJSON, &s)
	}
	name := fmt.Sprintf("%s-%s", inbound.Tag, client.Email)

	switch inbound.Protocol {
	case "vless":
		p := clashProxy{
			"name":               name,
			"type":               "vless",
			"server":             address,
			"port":               inbound.Port,
			"uuid":               client.UUID,
			"udp":                true,
			"network":            strOr(s["network"], "tcp"),
			"tls":                s["security"] == "tls" || s["security"] == "reality",
			"client-fingerprint": strOr(s["fingerprint"], "chrome"),
		}
		if sni, ok := s["sni"].(string); ok && sni != "" {
			p["servername"] = sni
		}
		if s["security"] == "reality" {
			var pubKey string
			if settings := jsonObj(inbound.Settings); settings != nil {
				pubKey, _ = settings["realityPublicKey"].(string)
			}
			names, _ := s["realityServerNames"].([]any)
			var sni string
			if len(names) > 0 {
				sni, _ = names[0].(string)
			}
			shortIDs, _ := s["realityShortIDs"].([]any)
			shortID := ""
			if len(shortIDs) > 0 {
				shortID, _ = shortIDs[0].(string)
			}
			p["servername"] = sni
			p["reality-opts"] = map[string]any{
				"public-key": pubKey,
				"short-id":   shortID,
			}
			p["flow"] = "xtls-rprx-vision"
		}
		if s["network"] == "ws" {
			p["ws-opts"] = map[string]any{
				"path":    strOr(s["path"], "/"),
				"headers": map[string]any{"Host": strOr(s["host"], "")},
			}
		}
		if s["network"] == "grpc" {
			p["grpc-opts"] = map[string]any{
				"grpc-service-name": strOr(s["serviceName"], "grpc"),
			}
		}
		return p

	case "vmess":
		p := clashProxy{
			"name":    name,
			"type":    "vmess",
			"server":  address,
			"port":    inbound.Port,
			"uuid":    client.UUID,
			"alterId": 0,
			"cipher":  "auto",
			"udp":     true,
			"network": strOr(s["network"], "tcp"),
		}
		if s["security"] == "tls" {
			p["tls"] = true
			if sni, ok := s["sni"].(string); ok && sni != "" {
				p["servername"] = sni
			}
		}
		if s["network"] == "ws" {
			p["ws-opts"] = map[string]any{
				"path":    strOr(s["path"], "/"),
				"headers": map[string]any{"Host": strOr(s["host"], "")},
			}
		}
		return p

	case "trojan":
		p := clashProxy{
			"name":     name,
			"type":     "trojan",
			"server":   address,
			"port":     inbound.Port,
			"password": client.Password,
			"udp":      true,
			"sni":      strOr(s["sni"], ""),
		}
		if s["network"] == "ws" {
			p["network"] = "ws"
			p["ws-opts"] = map[string]any{"path": strOr(s["path"], "/")}
		}
		return p

	case "shadowsocks":
		method := "aes-256-gcm"
		if settings := jsonObj(inbound.Settings); settings != nil {
			if m, ok := settings["method"].(string); ok && m != "" {
				method = m
			}
		}
		return clashProxy{
			"name":     name,
			"type":     "ss",
			"server":   address,
			"port":     inbound.Port,
			"cipher":   method,
			"password": client.Key,
			"udp":      true,
		}

	case "hysteria2":
		return clashProxy{
			"name":     name,
			"type":     "hysteria2",
			"server":   address,
			"port":     inbound.Port,
			"password": client.Password,
			"sni":      strOr(s["sni"], ""),
		}

	default:
		return nil
	}
}

// renderClashYAML — extremely small YAML emitter for Clash's proxies
// block. Handles the specific shape we produce (strings, ints, bools,
// nested maps one level deep, list of maps for `proxies`).
func renderClashYAML(proxies []clashProxy, remark string) string {
	var b strings.Builder
	b.WriteString("# HEXPLUS Panel — " + remark + "\n")
	b.WriteString("proxies:\n")
	for _, p := range proxies {
		b.WriteString("  - ")
		first := true
		writeClashKV(&b, p, "    ", &first)
		b.WriteString("\n")
	}
	// Ship a minimal proxy-groups so the file is directly-usable.
	b.WriteString("proxy-groups:\n")
	b.WriteString("  - name: PROXY\n    type: select\n    proxies:\n")
	for _, p := range proxies {
		if name, ok := p["name"].(string); ok {
			b.WriteString("      - " + name + "\n")
		}
	}
	b.WriteString("rules:\n")
	b.WriteString("  - MATCH,PROXY\n")
	return b.String()
}

func writeClashKV(b *strings.Builder, m map[string]any, indent string, first *bool) {
	// Deterministic key order — pick a stable priority list, then rest.
	priority := []string{"name", "type", "server", "port", "uuid", "password",
		"cipher", "sni", "servername", "tls", "network", "udp", "alterId",
		"client-fingerprint", "flow"}
	seen := map[string]bool{}
	for _, k := range priority {
		if v, ok := m[k]; ok {
			writeClashOne(b, k, v, indent, first)
			seen[k] = true
		}
	}
	for k, v := range m {
		if seen[k] {
			continue
		}
		writeClashOne(b, k, v, indent, first)
	}
}

func writeClashOne(b *strings.Builder, k string, v any, indent string, first *bool) {
	if *first {
		*first = false
	} else {
		b.WriteString(indent)
	}
	switch vv := v.(type) {
	case string:
		fmt.Fprintf(b, "%s: %s\n", k, yamlString(vv))
	case bool:
		fmt.Fprintf(b, "%s: %v\n", k, vv)
	case int, int64, float64:
		fmt.Fprintf(b, "%s: %v\n", k, vv)
	case map[string]any:
		fmt.Fprintf(b, "%s:\n", k)
		inner := true
		for kk, vvv := range vv {
			b.WriteString(indent + "  ")
			inner = true
			writeClashOne(b, kk, vvv, indent+"  ", &inner)
		}
	default:
		fmt.Fprintf(b, "%s: %v\n", k, vv)
	}
}

func yamlString(s string) string {
	if s == "" {
		return `""`
	}
	// Quote when the string contains a colon/hash/curly brace that
	// bare-YAML would misparse.
	needs := strings.ContainsAny(s, ":#[]{}&*!|>\"'%@`\n")
	if !needs {
		return s
	}
	// Use single-quote form; escape existing single quotes.
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

// ── Sing-box ────────────────────────────────────────────────────────

// buildSingBoxOutbound produces a Sing-box outbound block for one client.
// Returns nil when the protocol has no direct Sing-box mapping.
func buildSingBoxOutbound(
	inbound xray.InboundFromDB,
	streamJSON []byte,
	client xray.Client,
	address string,
) map[string]any {
	var s map[string]any
	if len(streamJSON) > 0 && string(streamJSON) != "{}" {
		_ = json.Unmarshal(streamJSON, &s)
	}
	tag := fmt.Sprintf("%s-%s", inbound.Tag, client.Email)

	switch inbound.Protocol {
	case "vless":
		o := map[string]any{
			"type":        "vless",
			"tag":         tag,
			"server":      address,
			"server_port": inbound.Port,
			"uuid":        client.UUID,
		}
		if s["security"] == "reality" {
			var pubKey, sni, shortID string
			if settings := jsonObj(inbound.Settings); settings != nil {
				pubKey, _ = settings["realityPublicKey"].(string)
			}
			names, _ := s["realityServerNames"].([]any)
			if len(names) > 0 {
				sni, _ = names[0].(string)
			}
			shortIDs, _ := s["realityShortIDs"].([]any)
			if len(shortIDs) > 0 {
				shortID, _ = shortIDs[0].(string)
			}
			o["tls"] = map[string]any{
				"enabled":     true,
				"server_name": sni,
				"reality": map[string]any{
					"enabled":    true,
					"public_key": pubKey,
					"short_id":   shortID,
				},
				"utls": map[string]any{
					"enabled":     true,
					"fingerprint": strOr(s["fingerprint"], "chrome"),
				},
			}
			o["flow"] = "xtls-rprx-vision"
		} else if s["security"] == "tls" {
			o["tls"] = map[string]any{
				"enabled":     true,
				"server_name": strOr(s["sni"], ""),
			}
		}
		if s["network"] == "ws" {
			o["transport"] = map[string]any{
				"type":    "ws",
				"path":    strOr(s["path"], "/"),
				"headers": map[string]any{"Host": strOr(s["host"], "")},
			}
		} else if s["network"] == "grpc" {
			o["transport"] = map[string]any{
				"type":         "grpc",
				"service_name": strOr(s["serviceName"], "grpc"),
			}
		}
		return o

	case "vmess":
		o := map[string]any{
			"type":        "vmess",
			"tag":         tag,
			"server":      address,
			"server_port": inbound.Port,
			"uuid":        client.UUID,
			"security":    "auto",
			"alter_id":    0,
		}
		if s["security"] == "tls" {
			o["tls"] = map[string]any{
				"enabled":     true,
				"server_name": strOr(s["sni"], ""),
			}
		}
		return o

	case "trojan":
		o := map[string]any{
			"type":        "trojan",
			"tag":         tag,
			"server":      address,
			"server_port": inbound.Port,
			"password":    client.Password,
		}
		if s["security"] == "tls" {
			o["tls"] = map[string]any{
				"enabled":     true,
				"server_name": strOr(s["sni"], ""),
			}
		}
		return o

	case "shadowsocks":
		method := "aes-256-gcm"
		if settings := jsonObj(inbound.Settings); settings != nil {
			if m, ok := settings["method"].(string); ok && m != "" {
				method = m
			}
		}
		return map[string]any{
			"type":        "shadowsocks",
			"tag":         tag,
			"server":      address,
			"server_port": inbound.Port,
			"method":      method,
			"password":    client.Key,
		}

	case "hysteria2":
		o := map[string]any{
			"type":        "hysteria2",
			"tag":         tag,
			"server":      address,
			"server_port": inbound.Port,
			"password":    client.Password,
		}
		if sni, _ := s["sni"].(string); sni != "" {
			o["tls"] = map[string]any{"enabled": true, "server_name": sni}
		}
		return o

	default:
		return nil
	}
}

func renderSingBoxJSON(outbounds []map[string]any) string {
	tags := make([]string, 0, len(outbounds))
	for _, o := range outbounds {
		if t, ok := o["tag"].(string); ok {
			tags = append(tags, t)
		}
	}
	selector := map[string]any{
		"type":      "selector",
		"tag":       "proxy",
		"outbounds": tags,
	}
	all := append([]map[string]any{selector}, outbounds...)
	all = append(all, map[string]any{"type": "direct", "tag": "direct"})
	all = append(all, map[string]any{"type": "block", "tag": "block"})

	root := map[string]any{
		"log":       map[string]any{"level": "info"},
		"outbounds": all,
		"route": map[string]any{
			"final": "proxy",
			"rules": []any{
				map[string]any{"outbound": "direct", "network": []string{"udp"}, "port": []int{53}},
			},
		},
	}
	raw, _ := json.MarshalIndent(root, "", "  ")
	return string(raw)
}

// helpers ─────────────────────────────────────────────────────────────

func strOr(v any, fallback string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return fallback
}

func jsonObj(raw []byte) map[string]any {
	if len(raw) == 0 {
		return nil
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil
	}
	return m
}
