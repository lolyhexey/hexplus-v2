package xray

// client.go: CRUD for individual clients under an inbound. These are
// the units the panel exposes to end users — one row per share link,
// with quota + expiry + IP-limit attached.
//
// Client identity is per-protocol:
//   - VLESS / VMess: UUID
//   - Trojan: password (bcrypt-hashed at rest? — TBD)
//   - Shadowsocks: shared key
//
// Skeleton for Phase 1; real CRUD lands with Phase 5.

import "time"

// Client is one addressable end-user credential attached to an inbound.
// The Protocol field determines which of UUID/Password/Key is populated.
type Client struct {
	ID         int64
	InboundID  int64
	Email      string
	Protocol   string
	UUID       string
	Password   string
	Key        string
	QuotaBytes int64 // 0 = unlimited
	UsedBytes  int64
	ExpiresAt  time.Time // zero = never
	IPLimit    int       // 0 = unlimited concurrent IPs
	Enabled    bool
	CreatedAt  time.Time
}
