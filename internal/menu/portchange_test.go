package menu

import (
	"net"
	"strconv"
	"strings"
	"testing"
)

func TestRetargetLoopback(t *testing.T) {
	cases := []struct {
		addr, want string
		ok         bool
	}{
		{"127.0.0.1:1194", "127.0.0.1:443", true},
		{"localhost:1194", "localhost:443", true},
		{"0.0.0.0:1194", "0.0.0.0:443", true},
		{"[::1]:1194", "[::1]:443", true},
		{"127.0.0.1:22", "127.0.0.1:22", false},   // another port
		{"10.0.0.5:1194", "10.0.0.5:1194", false}, // a remote host is never touched
		{"garbage", "garbage", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := retargetLoopback(c.addr, 1194, 443)
		if got != c.want || ok != c.ok {
			t.Errorf("retargetLoopback(%q) = %q, %v; want %q, %v", c.addr, got, ok, c.want, c.ok)
		}
	}
}

// A port change onto a port something already holds used to rewrite the
// config and the firewall, restart a service that could not bind, and
// report success.
func TestPortChangeConflict(t *testing.T) {
	ln, err := net.Listen("tcp", ":0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := ln.Addr().(*net.TCPAddr).Port

	if err := portChangeConflict("tcp", held); err == nil || !strings.Contains(err.Error(), "ถูกใช้งานอยู่แล้ว") {
		t.Errorf("a held port was accepted: %v", err)
	}
	if err := portChangeConflict("tcp", held, 1194, held); err != nil {
		t.Errorf("the service's own port was refused: %v", err)
	}
	// Only the service's own ports are exempt: dropbear's 110 counts only
	// when the caller passes it (dropbear running).
	if err := portChangeConflict("tcp", held, 22000); err == nil {
		t.Error("a port held by someone else was accepted")
	}
}

// A TCP listener does not make the UDP port busy.
func TestPortChangeConflictIsPerProtocol(t *testing.T) {
	pc, err := net.ListenPacket("udp", ":0") // a port free for UDP
	if err != nil {
		t.Fatal(err)
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	ln, err := net.Listen("tcp", net.JoinHostPort("", strconv.Itoa(port)))
	if err != nil {
		t.Skipf("tcp/%d not available: %v", port, err)
	}
	defer ln.Close()
	if err := portChangeConflict("udp", port); err != nil {
		t.Errorf("udp blocked by a tcp listener: %v", err)
	}
	if err := portChangeConflict("tcp", port); err == nil {
		t.Error("tcp listener not seen")
	}
}

func TestSSLTunnelTargetWarning(t *testing.T) {
	old := listenStatus
	t.Cleanup(func() { listenStatus = old })
	listening := map[int]bool{22: true}
	listenStatus = func(port int, _ string) (bool, error) { return listening[port], nil }

	if w := sslTunnelTargetWarning("127.0.0.1:1194"); !strings.Contains(w, "127.0.0.1:1194") {
		t.Errorf("no warning for a dead local target: %q", w)
	}
	for _, target := range []string{"127.0.0.1:22", "10.0.0.5:1194", "garbage", ""} {
		if w := sslTunnelTargetWarning(target); w != "" {
			t.Errorf("warning for %q: %q", target, w)
		}
	}
}
