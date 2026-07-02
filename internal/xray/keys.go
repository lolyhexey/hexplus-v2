package xray

// keys.go: cryptographic helpers used by the protocol builders.
// Consolidated in one file so the "how do I gen a Reality key or a
// VMess UUID" question has one obvious place to look.

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"

	"github.com/google/uuid"
)

// NewUUID returns a v4 UUID string in the canonical 8-4-4-4-12 form.
// Used by VLESS + VMess for per-client identity.
func NewUUID() string {
	return uuid.NewString()
}

// RealityKeyPair is one Reality x25519 identity — private key stays on
// the server, public key ships in the client's share link.
type RealityKeyPair struct {
	Private string // base64 RawURL
	Public  string // base64 RawURL
}

// NewRealityKeyPair generates a fresh x25519 pair using the same
// base64.RawURLEncoding format xray-core's `xray x25519` command emits
// so the share-link consumer sees the exact string an operator would
// paste from xray's own tool.
func NewRealityKeyPair() (RealityKeyPair, error) {
	curve := ecdh.X25519()
	priv, err := curve.GenerateKey(rand.Reader)
	if err != nil {
		return RealityKeyPair{}, fmt.Errorf("gen x25519: %w", err)
	}
	return RealityKeyPair{
		Private: base64.RawURLEncoding.EncodeToString(priv.Bytes()),
		Public:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
	}, nil
}

// NewShortID returns a random hex short-id for Reality. n is the byte
// length; xray accepts 0-16 hex chars, so n must be <= 8.
func NewShortID(n int) (string, error) {
	if n < 0 || n > 8 {
		return "", fmt.Errorf("short-id byte length %d out of range 0..8", n)
	}
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

// NewSSKey generates a Shadowsocks shared key sized for the given
// method. AEAD ciphers demand exact key lengths:
//   - aes-128-gcm / 2022-blake3-aes-128-gcm: 16 bytes
//   - aes-256-gcm / chacha20-ietf-poly1305 / 2022-blake3-aes-256-gcm: 32 bytes
// Output is standard base64 so it drops directly into xray's config.
func NewSSKey(method string) (string, error) {
	size := 32
	switch method {
	case "aes-128-gcm", "2022-blake3-aes-128-gcm":
		size = 16
	}
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(buf), nil
}

// NewTrojanPassword returns a URL-safe random password suitable for
// Trojan's plaintext auth. 24 bytes = ~192 bits of entropy.
func NewTrojanPassword() (string, error) {
	buf := make([]byte, 24)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf), nil
}
