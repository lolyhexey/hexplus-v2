package user

import (
	"errors"
	"strings"
	"testing"
)

// OpenVPN (no PKCS#11) drops a password longer than 127 bytes during the
// TLS handshake, so such a password was accepted by the menus and the CLI
// and then could never log in.
func TestCheckVPNPassword(t *testing.T) {
	cases := []struct {
		name, pw string
		ok       bool
	}{
		{"ordinary", "Secret 1!", true},
		{"exactly the limit", strings.Repeat("a", MaxVPNPasswordLen), true},
		{"multibyte at the limit", strings.Repeat("ก", MaxVPNPasswordLen/3), true}, // 3 bytes each: 126
		{"one byte over", strings.Repeat("a", MaxVPNPasswordLen+1), false},
		{"multibyte over in bytes", strings.Repeat("ก", MaxVPNPasswordLen/3+1), false}, // 43 runes, 129 bytes
		{"empty", "", false},
		{"line feed", "a\nroot:x", false},
		{"carriage return", "a\rb", false},
		{"NUL", "a\x00b", false},
		{"colon is fine (chpasswd splits on the first)", "a:b:c", true},
	}
	for _, c := range cases {
		if err := CheckVPNPassword(c.pw); (err == nil) != c.ok {
			t.Errorf("%s: err = %v, want ok = %v", c.name, err, c.ok)
		}
	}
	if !errors.Is(CheckVPNPassword(strings.Repeat("a", 200)), ErrPasswordTooLong) {
		t.Error("a long password must report ErrPasswordTooLong")
	}
}
