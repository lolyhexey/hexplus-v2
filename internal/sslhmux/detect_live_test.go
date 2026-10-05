package sslhmux

import (
	"errors"
	"os"
	"testing"
)

// Backends are read on every connection. A read that fails for a reason
// other than "missing" (EMFILE while the mux is short of descriptors) used
// to look like "no config" and sent SSH to 22 and OpenVPN to 1194 although
// both had been moved.
func TestReadErrorsKeepTheLastKnownBackend(t *testing.T) {
	withFile(t, &sshdConfigPath, "Port 2222\n")
	oldOVPN := openvpnConfPaths
	t.Cleanup(func() { openvpnConfPaths = oldOVPN })
	withFile(t, &squidConfPath, "http_port 8000\n")
	ovpn := squidConfPath + ".ovpn"
	if err := os.WriteFile(ovpn, []byte("port 443\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	openvpnConfPaths = []string{ovpn}

	if DetectSSH() != "127.0.0.1:2222" || DetectHTTP() != "127.0.0.1:8000" || DetectOpenVPN() != "127.0.0.1:443" {
		t.Fatal("setup: backends not detected")
	}

	oldRead := readConf
	t.Cleanup(func() { readConf = oldRead })
	readConf = func(string) ([]byte, error) { return nil, errors.New("open: too many open files") }
	if got := DetectSSH(); got != "127.0.0.1:2222" {
		t.Errorf("SSH during a read error = %q, want the last known 127.0.0.1:2222", got)
	}
	if got := DetectHTTP(); got != "127.0.0.1:8000" {
		t.Errorf("HTTP during a read error = %q, want the last known 127.0.0.1:8000", got)
	}
	if got := DetectOpenVPN(); got != "127.0.0.1:443" {
		t.Errorf("OpenVPN during a read error = %q, want the last known 127.0.0.1:443", got)
	}

	// A file that is really gone is gone: defaults, and nothing remembered.
	readConf = func(string) ([]byte, error) { return nil, os.ErrNotExist }
	if DetectSSH() != "127.0.0.1:22" || DetectHTTP() != "" || DetectOpenVPN() != "127.0.0.1:1194" {
		t.Error("a missing file did not fall back to the defaults")
	}
	readConf = func(string) ([]byte, error) { return nil, errors.New("open: too many open files") }
	if got := DetectSSH(); got != "127.0.0.1:22" {
		t.Errorf("after the file was removed, a read error revived %q", got)
	}
}

// Backends follow the configs in both directions, not only the first time.
func TestBackendsFollowChangesBothWays(t *testing.T) {
	withFile(t, &sshdConfigPath, "Port 2222\n")
	if DetectSSH() != "127.0.0.1:2222" {
		t.Fatal("setup")
	}
	if err := os.WriteFile(sshdConfigPath, []byte("Port 2200\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectSSH(); got != "127.0.0.1:2200" {
		t.Errorf("SSH after a port change = %q", got)
	}

	withFile(t, &squidConfPath, "http_port 3128\n")
	if err := os.WriteFile(squidConfPath, []byte("http_port 8080\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := DetectHTTP(); got != "127.0.0.1:8080" {
		t.Errorf("HTTP after a port change = %q", got)
	}

	old := sslTunnelPort
	t.Cleanup(func() { sslTunnelPort = old })
	port := 4443
	sslTunnelPort = func() int { return port }
	if DetectSSL() != "127.0.0.1:4443" {
		t.Fatal("setup: SSL TUNNEL installed")
	}
	port = 0 // uninstalled
	if got := DetectSSL(); got != "" {
		t.Errorf("SSL after SSL TUNNEL was removed = %q, want none", got)
	}
}
