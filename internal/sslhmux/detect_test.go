package sslhmux

import (
	"os"
	"path/filepath"
	"testing"
)

// withFile points *path at a temp file with the given content ("" = no file)
// for the rest of the test.
func withFile(t *testing.T, path *string, content string) {
	t.Helper()
	old := *path
	t.Cleanup(func() { *path = old })
	*path = filepath.Join(t.TempDir(), "conf")
	if content != "" {
		if err := os.WriteFile(*path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

func TestDetectOpenVPN(t *testing.T) {
	for _, tc := range []struct{ name, conf, want string }{
		{"plain", "port 1195\nproto tcp\n", "127.0.0.1:1195"},
		{"https port", "port 443\n", "127.0.0.1:443"},
		{"tab separated", "  port\t4443\n", "127.0.0.1:4443"},
		{"commented out", "#port 1194\nport 8443\n", "127.0.0.1:8443"},
		{"last port wins", "port 1194\nport 1195\n", "127.0.0.1:1195"},
		{"invalid ignored", "port abc\nport 70000\n", "127.0.0.1:1194"},
		{"no directive", "proto tcp\n", "127.0.0.1:1194"},
		{"no file", "", "127.0.0.1:1194"},
	} {
		old := openvpnConfPaths
		var p string
		withFile(t, &p, tc.conf)
		openvpnConfPaths = []string{filepath.Join(t.TempDir(), "absent.conf"), p}
		if got := DetectOpenVPN(); got != tc.want {
			t.Errorf("%s: DetectOpenVPN = %q, want %q", tc.name, got, tc.want)
		}
		openvpnConfPaths = old
	}
}

func TestDetectSSL(t *testing.T) {
	old := sslTunnelPort
	t.Cleanup(func() { sslTunnelPort = old })

	sslTunnelPort = func() int { return 0 }
	if got := DetectSSL(); got != "" {
		t.Errorf("DetectSSL with no tunnel = %q, want empty (not Squid's port)", got)
	}
	sslTunnelPort = func() int { return 4443 }
	if got := DetectSSL(); got != "127.0.0.1:4443" {
		t.Errorf("DetectSSL = %q", got)
	}
}

func TestDetectHTTP(t *testing.T) {
	for _, tc := range []struct{ name, conf, want string }{
		{"port only", "http_port 3128\n", "127.0.0.1:3128"},
		{"first of several", "http_port 8080\nhttp_port 3128\n", "127.0.0.1:8080"},
		{"bound host kept", "http_port 192.168.1.5:3128\n", "192.168.1.5:3128"},
		{"wildcard bind", "http_port 0.0.0.0:3128\n", "127.0.0.1:3128"},
		{"empty host", "http_port :3128\n", "127.0.0.1:3128"},
		{"ipv6", "http_port [::1]:3129\n", "[::1]:3129"},
		{"ipv6 wildcard", "http_port [::]:3129\n", "127.0.0.1:3129"},
		{"options", "http_port 3128 name=main\n", "127.0.0.1:3128"},
		{"skips intercept", "http_port 3129 intercept\nhttp_port 3128\n", "127.0.0.1:3128"},
		{"skips accel", "http_port 80 accel vhost\n", ""},
		{"commented out", "#http_port 3128\n", ""},
		{"no directive", "acl all src all\n", ""},
		{"bad port", "http_port nope\n", ""},
		{"no file", "", ""},
	} {
		withFile(t, &squidConfPath, tc.conf)
		if got := DetectHTTP(); got != tc.want {
			t.Errorf("%s: DetectHTTP = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestDetectSSH(t *testing.T) {
	for _, tc := range []struct{ name, conf, want string }{
		{"first port", "Port 2222\nPort 22\n", "127.0.0.1:2222"},
		{"first removed", "Port 22\n", "127.0.0.1:22"},
		{"commented", "#Port 22\nPort 2200\n", "127.0.0.1:2200"},
		{"keyword case", "port 2201\n", "127.0.0.1:2201"},
		{"no directive", "PermitRootLogin no\n", "127.0.0.1:22"},
		{"no file", "", "127.0.0.1:22"},
	} {
		withFile(t, &sshdConfigPath, tc.conf)
		if got := DetectSSH(); got != tc.want {
			t.Errorf("%s: DetectSSH = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPointsAtSelf(t *testing.T) {
	for _, tc := range []struct {
		target string
		port   int
		want   bool
	}{
		{"127.0.0.1:443", 443, true},
		{"[::1]:443", 443, true},
		{"localhost:443", 443, true},
		{"0.0.0.0:443", 443, true},
		{"127.0.0.1:1194", 443, false},
		{"192.0.2.7:443", 443, false},
		{"garbage", 443, false},
	} {
		if got := pointsAtSelf(tc.target, tc.port); got != tc.want {
			t.Errorf("pointsAtSelf(%q, %d) = %v, want %v", tc.target, tc.port, got, tc.want)
		}
	}
}
