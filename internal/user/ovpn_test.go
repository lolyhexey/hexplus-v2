package user

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeServerConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestResolveEndpoint(t *testing.T) {
	tcp443 := writeServerConf(t, "port 443\nproto tcp\n")
	missing := filepath.Join(t.TempDir(), "absent.conf")

	cases := []struct {
		name  string
		in    OVPNInput
		conf  string
		port  int
		proto string
		err   string
	}{
		{"nothing set follows server.conf", OVPNInput{}, tcp443, 443, "tcp", ""},
		{"port override keeps conf proto", OVPNInput{RemotePort: 8443}, tcp443, 8443, "tcp", ""},
		{"proto override keeps conf port", OVPNInput{Proto: "udp"}, tcp443, 443, "udp", ""},
		{"proto is case-insensitive", OVPNInput{Proto: "TCP", RemotePort: 1}, missing, 1, "tcp", ""},
		{"both set never reads the file", OVPNInput{RemotePort: 1194, Proto: "udp"}, missing, 1194, "udp", ""},
		{"unreadable conf and nothing set fails", OVPNInput{}, missing, 0, "", "--remote-port"},
		{"unreadable conf and port missing fails", OVPNInput{Proto: "udp"}, missing, 0, "", "--remote-port"},
		{"unreadable conf and proto missing fails", OVPNInput{RemotePort: 443}, missing, 0, "", "--proto"},
		{"bad proto", OVPNInput{Proto: "foo", RemotePort: 443}, tcp443, 0, "", "invalid proto"},
		{"port too large", OVPNInput{Proto: "tcp", RemotePort: 70000}, tcp443, 0, "", "invalid remote port"},
		{"negative port", OVPNInput{Proto: "tcp", RemotePort: -1}, tcp443, 0, "", "invalid remote port"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ResolveEndpoint(c.in, c.conf)
			if c.err != "" {
				if err == nil || !strings.Contains(err.Error(), c.err) {
					t.Fatalf("err = %v, want one containing %q", err, c.err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got.RemotePort != c.port || got.Proto != c.proto {
				t.Errorf("got %s/%d, want %s/%d", got.Proto, got.RemotePort, c.proto, c.port)
			}
		})
	}
}
