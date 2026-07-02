package xray

// generate.go: query the panel DB, build the full xray Config, write
// it to paths.XrayConfigPath atomically. Called before every Reload
// so the file on disk always reflects the latest DB state.
//
// Inbound row shape (see internal/panel/db/migrations.go):
//   settings TEXT — protocol-specific JSON keyed off protocol name
//   stream   TEXT — TransportParams JSON (transport + security)
//
// Extras — Reality keys, Shadowsocks method, WG secret key, Dokodemo
// address/port — live inside the settings JSON so the schema doesn't
// have to grow a column per protocol.

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// Inbound-row shape the generator reads from panel_db.
type dbInbound struct {
	ID       int64
	Tag      string
	Protocol string
	Listen   string
	Port     int
	Settings []byte
	Stream   []byte
	Sniffing bool
	Enabled  bool
}

// GenerateAndWrite fetches state from the panel DB, builds a Config,
// marshals it, and writes it to paths.XrayConfigPath. Returns the
// number of enabled inbounds written and any error.
func GenerateAndWrite(sqldb *sql.DB) (int, error) {
	cfg, count, err := Generate(sqldb)
	if err != nil {
		return 0, err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return 0, fmt.Errorf("marshal config: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(paths.XrayConfigPath), 0o750); err != nil {
		return 0, fmt.Errorf("mkdir xray state: %w", err)
	}
	tmp := paths.XrayConfigPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o640); err != nil {
		return 0, fmt.Errorf("write tmp: %w", err)
	}
	if err := os.Rename(tmp, paths.XrayConfigPath); err != nil {
		_ = os.Remove(tmp)
		return 0, fmt.Errorf("rename: %w", err)
	}
	return count, nil
}

// Generate reads inbounds+clients and returns an in-memory Config plus
// the count of enabled inbounds that made it in.
func Generate(sqldb *sql.DB) (Config, int, error) {
	cfg := Config{
		Log: LogConfig{Loglevel: "warning"},
		API: &APIConfig{
			Tag:      "api",
			Services: []string{"HandlerService", "LoggerService", "StatsService"},
		},
		Stats: &StatsConfig{},
		Policy: &PolicyConfig{
			Levels: map[string]PolicyLevel{
				"0": {StatsUserUplink: true, StatsUserDownlink: true},
			},
			System: &SystemPolicy{
				StatsInboundUplink:    true,
				StatsInboundDownlink:  true,
				StatsOutboundUplink:   true,
				StatsOutboundDownlink: true,
			},
		},
		Inbounds: []Inbound{
			// API inbound bound to loopback:10085 — Phase 7's stats
			// poller connects here. Kept in every generated config so
			// the panel can pull stats even when the user has zero
			// user-facing inbounds configured.
			{
				Tag:      "api",
				Listen:   "127.0.0.1",
				Port:     10085,
				Protocol: "dokodemo-door",
				Settings: map[string]any{"address": "127.0.0.1"},
			},
		},
		Outbounds: []Outbound{
			{Tag: "direct", Protocol: "freedom"},
			{Tag: "block", Protocol: "blackhole"},
		},
		Routing: &Routing{
			DomainStrategy: "AsIs",
			Rules: []RoutingRule{
				{
					Type:        "field",
					InboundTag:  []string{"api"},
					OutboundTag: "api",
				},
			},
		},
	}

	rows, err := sqldb.Query(`
		SELECT id, tag, protocol, listen, port, settings, stream, sniffing, enabled
		FROM inbounds WHERE enabled = 1 ORDER BY id
	`)
	if err != nil {
		return cfg, 0, fmt.Errorf("query inbounds: %w", err)
	}
	defer rows.Close()

	var inbounds []dbInbound
	for rows.Next() {
		var in dbInbound
		var sniffInt, enInt int
		if err := rows.Scan(&in.ID, &in.Tag, &in.Protocol, &in.Listen, &in.Port,
			&in.Settings, &in.Stream, &sniffInt, &enInt); err != nil {
			return cfg, 0, err
		}
		in.Sniffing = sniffInt != 0
		in.Enabled = enInt != 0
		inbounds = append(inbounds, in)
	}
	if err := rows.Err(); err != nil {
		return cfg, 0, err
	}

	count := 0
	for _, in := range inbounds {
		built, err := buildInbound(sqldb, in)
		if err != nil {
			// Skip broken inbounds; log via error surfaced to caller
			// only when nothing built. For now we prefer partial
			// success (one bad inbound doesn't take down the whole
			// xray daemon).
			continue
		}
		cfg.Inbounds = append(cfg.Inbounds, built)
		count++
	}

	// User-defined outbounds land AFTER the two synthesized ones
	// (direct/block) so a routing rule with outboundTag="direct" still
	// hits our freedom outbound even if the user names their WARP
	// outbound "direct" (we detect that clash later; for now duplicate
	// tags are the operator's problem).
	if err := appendUserOutbounds(sqldb, &cfg); err != nil {
		return cfg, count, err
	}
	if err := appendUserRoutingRules(sqldb, &cfg); err != nil {
		return cfg, count, err
	}
	return cfg, count, nil
}

// appendUserOutbounds reads enabled rows from the outbounds table and
// tacks them onto the Config. Settings/stream blobs are passed through
// as-is (map[string]any) so protocol schema evolution doesn't require
// a code change here — the panel API is the source of truth.
func appendUserOutbounds(sqldb *sql.DB, cfg *Config) error {
	rows, err := sqldb.Query(`SELECT tag, protocol, settings, stream FROM outbounds WHERE enabled = 1 ORDER BY id`)
	if err != nil {
		return fmt.Errorf("query outbounds: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var tag, protocol, settings, stream string
		if err := rows.Scan(&tag, &protocol, &settings, &stream); err != nil {
			return err
		}
		var s any
		if settings != "" {
			var m map[string]any
			if err := json.Unmarshal([]byte(settings), &m); err == nil {
				s = m
			}
		}
		cfg.Outbounds = append(cfg.Outbounds, Outbound{
			Tag:      tag,
			Protocol: protocol,
			Settings: s,
		})
	}
	return rows.Err()
}

// appendUserRoutingRules translates the routing_rules table into
// RoutingRule entries and appends them AFTER the built-in api rule.
// The built-in api rule stays first so it always wins.
func appendUserRoutingRules(sqldb *sql.DB, cfg *Config) error {
	rows, err := sqldb.Query(`
		SELECT outbound_tag, inbound_tag, domains, ips
		FROM routing_rules WHERE enabled = 1 ORDER BY priority, id
	`)
	if err != nil {
		return fmt.Errorf("query rules: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var outTag, inTag, domains, ips string
		if err := rows.Scan(&outTag, &inTag, &domains, &ips); err != nil {
			return err
		}
		rule := RoutingRule{
			Type:        "field",
			OutboundTag: outTag,
		}
		if inTag != "" {
			rule.InboundTag = []string{inTag}
		}
		rule.Domain = splitNL(domains)
		rule.IP = splitNL(ips)
		cfg.Routing.Rules = append(cfg.Routing.Rules, rule)
	}
	return rows.Err()
}

// splitNL splits a newline-joined blob and drops empty entries. Same
// convention as api_routing.splitLines but returns nil (not [])
// because RoutingRule fields are omitted-when-empty in the JSON.
func splitNL(s string) []string {
	if s == "" {
		return nil
	}
	out := []string{}
	for _, line := range splitNoAlloc(s) {
		if line != "" {
			out = append(out, line)
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

// splitNoAlloc is a lightweight strings.Split without the "" heap
// churn; keeps generate.go dependency-free for callers that might
// re-run this loop tens of times per second under heavy churn.
func splitNoAlloc(s string) []string {
	out := []string{}
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// buildInbound assembles one Inbound from a DB row, delegating to the
// per-protocol builder and the shared transport builder.
func buildInbound(sqldb *sql.DB, in dbInbound) (Inbound, error) {
	clients, err := loadClients(sqldb, in.ID)
	if err != nil {
		return Inbound{}, err
	}

	// Parse the stored transport-params blob (may be empty for
	// UDP-only protocols like WG/Hy2 that ignore the stream envelope).
	var tp TransportParams
	if len(in.Stream) > 0 && string(in.Stream) != "{}" {
		if err := json.Unmarshal(in.Stream, &tp); err != nil {
			return Inbound{}, fmt.Errorf("stream json: %w", err)
		}
	}
	stream, err := buildStreamSetting(tp)
	if err != nil {
		return Inbound{}, err
	}

	out := Inbound{
		Tag:            in.Tag,
		Listen:         in.Listen,
		Port:           in.Port,
		Protocol:       in.Protocol,
		StreamSettings: stream,
	}
	if in.Sniffing {
		out.Sniffing = &Sniffing{
			Enabled:      true,
			DestOverride: []string{"http", "tls", "quic"},
		}
	}

	// Protocol-specific settings dispatch.
	switch in.Protocol {
	case "vless":
		flow := ""
		if tp.Security == SecReality {
			flow = "xtls-rprx-vision"
		}
		out.Settings = buildVLESSSettings(clients, flow)
	case "vmess":
		out.Settings = buildVMessSettings(clients)
	case "trojan":
		flow := ""
		if tp.Security == SecReality {
			flow = "xtls-rprx-vision"
		}
		out.Settings = buildTrojanSettings(clients, flow)
	case "shadowsocks":
		method := extractString(in.Settings, "method", "aes-256-gcm")
		out.Settings = buildSSSettings(method, clients)
	case "hysteria2":
		out.Settings = buildHysteria2Settings(clients)
	case "wireguard":
		secret := extractString(in.Settings, "secretKey", "")
		if secret == "" {
			return Inbound{}, errors.New("wireguard: secretKey missing in settings")
		}
		out.Settings = buildWireGuardSettings(secret, clients)
	case "http":
		out.Settings = buildHTTPSettings(clients)
	case "socks":
		out.Settings = buildSOCKSSettings(clients)
	case "dokodemo-door":
		addr := extractString(in.Settings, "address", "127.0.0.1")
		port := extractInt(in.Settings, "port", 0)
		if port == 0 {
			return Inbound{}, errors.New("dokodemo-door: port missing in settings")
		}
		out.Settings = buildDokodemoSettings(addr, port, extractString(in.Settings, "network", "tcp,udp"))
	default:
		return Inbound{}, fmt.Errorf("unknown protocol %q", in.Protocol)
	}
	return out, nil
}

// loadClients returns every client row attached to the inbound. Order
// is stable by id so config diffs stay small between regens.
func loadClients(sqldb *sql.DB, inboundID int64) ([]Client, error) {
	rows, err := sqldb.Query(`
		SELECT id, inbound_id, email, protocol, uuid, password, shared_key,
		       quota_bytes, used_bytes, expires_at, ip_limit, enabled, created_at
		FROM clients WHERE inbound_id = ? ORDER BY id
	`, inboundID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Client
	for rows.Next() {
		var c Client
		var expUnix, createdUnix int64
		var enabledInt int
		if err := rows.Scan(&c.ID, &c.InboundID, &c.Email, &c.Protocol,
			&c.UUID, &c.Password, &c.Key,
			&c.QuotaBytes, &c.UsedBytes, &expUnix, &c.IPLimit,
			&enabledInt, &createdUnix); err != nil {
			return nil, err
		}
		if expUnix > 0 {
			c.ExpiresAt = time.Unix(expUnix, 0)
		}
		c.CreatedAt = time.Unix(createdUnix, 0)
		c.Enabled = enabledInt != 0
		out = append(out, c)
	}
	return out, rows.Err()
}

// extractString reads a top-level string field out of a JSON blob,
// returning the fallback when the field is absent or the blob is
// malformed. Used to pluck protocol-specific extras (SS method, WG
// secretKey, Dokodemo target) without a separate DB column per proto.
func extractString(raw []byte, key, fallback string) string {
	if len(raw) == 0 {
		return fallback
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fallback
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return fallback
}

// extractInt is the int variant of extractString.
func extractInt(raw []byte, key string, fallback int) int {
	if len(raw) == 0 {
		return fallback
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		return fallback
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	}
	return fallback
}
