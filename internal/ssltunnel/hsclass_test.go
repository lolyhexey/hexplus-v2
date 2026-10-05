package ssltunnel

import (
	"errors"
	"testing"
)

// Every handshake failure used to share one throttle key, so a scanner
// repeating "unsupported versions" all day hid a customer's "no cipher
// suite" failure. Each kind gets its own key; peer-chosen values in the
// message must not create new keys.
func TestHandshakeClass(t *testing.T) {
	cases := []struct{ msg, want string }{
		{"tls: client offered only unsupported versions: [302 301]", "tls: client offered only unsupported versions"},
		{"tls: client offered only unsupported versions: [300]", "tls: client offered only unsupported versions"},
		{"tls: no cipher suite supported by both client and server", "tls: no cipher suite supported by both client and server"},
		{"tls: oversized record received with length 20000", "tls: oversized record received with length"},
		{"tls: oversized record received with length 17000", "tls: oversized record received with length"},
		{"read tcp 1.2.3.4:443->5.6.7.8:9: tls: unsupported SSLv2 handshake received", "tls: unsupported SSLv"},
		{"something else entirely", "something else entirely"},
	}
	for _, c := range cases {
		if got := handshakeClass(errors.New(c.msg)); got != c.want {
			t.Errorf("handshakeClass(%q) = %q, want %q", c.msg, got, c.want)
		}
	}
	a := handshakeClass(errors.New("tls: client offered only unsupported versions: [302]"))
	b := handshakeClass(errors.New("tls: no cipher suite supported by both client and server"))
	if a == b {
		t.Error("two different failures share a throttle key")
	}
}
