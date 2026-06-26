// tls_auth.go: produce an OpenVPN static key (ta.key) in the format
// `openvpn --genkey secret` emits.
//
// The file layout (from openvpn's net/key.c):
//   #
//   # 2048 bit OpenVPN static key
//   #
//   -----BEGIN OpenVPN Static key V1-----
//   <256 bytes of random data, hex-encoded, 32 lowercase hex chars per line>
//   -----END OpenVPN Static key V1-----
//
// Matching that envelope exactly means OpenVPN's parser accepts our file
// without configuration changes. The actual entropy source is crypto/rand,
// same as `openvpn --genkey` (both ultimately come from /dev/urandom).

package pki

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

const (
	tlsAuthBytes    = 256 // 2048 bits, what OpenVPN's tls-auth expects.
	tlsAuthHexWidth = 32  // hex chars per line, matching `openvpn --genkey`.
)

// GenerateTLSAuth returns the on-disk bytes for /etc/openvpn/ta.key.
func GenerateTLSAuth() ([]byte, error) {
	raw := make([]byte, tlsAuthBytes)
	if _, err := rand.Read(raw); err != nil {
		return nil, fmt.Errorf("rand.Read: %w", err)
	}

	var b strings.Builder
	b.WriteString("#\n")
	b.WriteString("# 2048 bit OpenVPN static key\n")
	b.WriteString("#\n")
	b.WriteString("-----BEGIN OpenVPN Static key V1-----\n")
	enc := hex.EncodeToString(raw)
	for i := 0; i < len(enc); i += tlsAuthHexWidth {
		end := i + tlsAuthHexWidth
		if end > len(enc) {
			end = len(enc)
		}
		b.WriteString(enc[i:end])
		b.WriteByte('\n')
	}
	b.WriteString("-----END OpenVPN Static key V1-----\n")
	return []byte(b.String()), nil
}
