package sslhmux

import (
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const ioWait = 3 * time.Second

// live is the state of the services the mux resolves its backends from. A
// zero field means the service has nothing configured (no file, no tunnel).
type live struct{ ssh, ssl, http, ovpn int }

// useLive points the detectors at temp files describing l and restores the
// real paths when the test ends. Nothing under /etc or /var is read.
func useLive(t *testing.T, l live) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, format string, port int) string {
		path := filepath.Join(dir, name)
		if port != 0 {
			if err := os.WriteFile(path, []byte(fmt.Sprintf(format, port)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return path
	}
	oldSSH, oldSquid, oldOVPN, oldSSL := sshdConfigPath, squidConfPath, openvpnConfPaths, sslTunnelPort
	t.Cleanup(func() {
		sshdConfigPath, squidConfPath, openvpnConfPaths, sslTunnelPort = oldSSH, oldSquid, oldOVPN, oldSSL
	})
	sshdConfigPath = write("sshd_config", "Port %d\n", l.ssh)
	squidConfPath = write("squid.conf", "http_port %d\n", l.http)
	openvpnConfPaths = []string{write("server.conf", "port %d\n", l.ovpn)}
	sslTunnelPort = func() int { return l.ssl }
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, p, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(p)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

// listenLoopback listens on 127.0.0.1 and hands every accepted conn to fn in
// its own goroutine until the test ends.
func listenLoopback(t *testing.T, fn func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go fn(c)
		}
	}()
	return ln.Addr().String()
}

// dialMux connects to a mux that resolves its backends from the current
// detector state, and writes first.
func dialMux(t *testing.T, muxAddr string, first string) net.Conn {
	t.Helper()
	client, err := net.Dial("tcp", muxAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := client.Write([]byte(first)); err != nil {
		t.Fatal(err)
	}
	return client
}

func startMux(t *testing.T) (addr string, port int) {
	t.Helper()
	// The mux's own port is only known once it listens, so the handler reads
	// it back from the listener address.
	var muxPort int
	addr = listenLoopback(t, func(c net.Conn) { handleMuxConn(c, Config{Port: muxPort}) })
	muxPort = portOf(t, addr)
	return addr, muxPort
}

// muxSession opens one client connection to a mux whose SSH backend is a
// fresh listener, writes an SSH banner, and returns both ends of the session.
func muxSession(t *testing.T) (client, backend net.Conn) {
	t.Helper()
	accepted := make(chan net.Conn, 1)
	sshAddr := listenLoopback(t, func(c net.Conn) { accepted <- c })
	useLive(t, live{ssh: portOf(t, sshAddr)})
	muxAddr, _ := startMux(t)

	client = dialMux(t, muxAddr, "SSH-2.0-test\r\n")
	select {
	case backend = <-accepted:
	case <-time.After(ioWait):
		t.Fatal("the mux never dialed the SSH backend")
	}
	t.Cleanup(func() { backend.Close() })
	return client, backend
}

func expectEOF(t *testing.T, who string, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(ioWait))
	n, err := c.Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("%s: Read = %d, %v; want EOF", who, n, err)
	}
}

func TestHandleMuxConnReplaysPeekedBytes(t *testing.T) {
	_, backend := muxSession(t)
	want := "SSH-2.0-test\r\n"
	buf := make([]byte, len(want))
	_ = backend.SetReadDeadline(time.Now().Add(ioWait))
	if _, err := io.ReadFull(backend, buf); err != nil || string(buf) != want {
		t.Fatalf("backend got %q, %v; want %q", buf, err, want)
	}
}

// Before the fix the mux waited on the client after the backend ended, so a
// client of a backend that hung up never saw EOF.
func TestHandleMuxConnBackendCloseReachesClient(t *testing.T) {
	client, backend := muxSession(t)
	// Let the mux deliver the whole banner first: closing with unread client
	// data would make the OS reset the client instead of ending it cleanly.
	_ = backend.SetReadDeadline(time.Now().Add(ioWait))
	if _, err := io.ReadFull(backend, make([]byte, len("SSH-2.0-test\r\n"))); err != nil {
		t.Fatal(err)
	}
	backend.Close()
	expectEOF(t, "client", client)
}

func TestHandleMuxConnClientCloseReachesBackend(t *testing.T) {
	client, backend := muxSession(t)
	client.Close()
	// Skip the replayed banner, then expect the FIN.
	_ = backend.SetReadDeadline(time.Now().Add(ioWait))
	if _, err := io.ReadAll(backend); err != nil {
		t.Fatalf("backend read ended with %v; want a clean EOF", err)
	}
}

// firstBytesBackend accepts connections and reports the first 8 bytes of each.
func firstBytesBackend(t *testing.T) (addr string, got <-chan string) {
	t.Helper()
	ch := make(chan string, 8)
	addr = listenLoopback(t, func(c net.Conn) {
		defer c.Close()
		buf := make([]byte, 8)
		_ = c.SetReadDeadline(time.Now().Add(ioWait))
		if _, err := io.ReadFull(c, buf); err == nil {
			ch <- string(buf)
		}
	})
	return addr, ch
}

func waitString(t *testing.T, ch <-chan string) string {
	t.Helper()
	select {
	case s := <-ch:
		return s
	case <-time.After(ioWait):
		t.Fatal("no connection reached the backend")
		return ""
	}
}

// The defect: the OpenVPN backend used to be frozen at install time, so after
// the OpenVPN port changed the mux kept dialing the old, dead port.
func TestMuxFollowsOpenVPNPortChange(t *testing.T) {
	oldAddr, oldGot := firstBytesBackend(t)
	newAddr, newGot := firstBytesBackend(t)
	muxAddr, _ := startMux(t)
	openvpnFrame := "\x00\x0e\x38\x01\x02\x03\x04\x05"

	useLive(t, live{ovpn: portOf(t, oldAddr)})
	c1 := dialMux(t, muxAddr, openvpnFrame)
	if got := waitString(t, oldGot); got != openvpnFrame {
		t.Fatalf("old backend got %q", got)
	}
	c1.Close()

	// OpenVPN moved to another port; the mux is not restarted.
	useLive(t, live{ovpn: portOf(t, newAddr)})
	dialMux(t, muxAddr, openvpnFrame)
	if got := waitString(t, newGot); got != openvpnFrame {
		t.Fatalf("new backend got %q", got)
	}
	select {
	case got := <-oldGot:
		t.Fatalf("a connection still reached the old port: %q", got)
	default:
	}
}

func TestMuxFollowsSSLTunnelInstallAndRemoval(t *testing.T) {
	tunnelAddr, tunnelGot := firstBytesBackend(t)
	muxAddr, _ := startMux(t)
	hello := "\x16\x03\x01\x00\x10\x01\x00\x00"

	// SSL TUNNEL is not installed: the TLS client is dropped, not handed to
	// whatever else (Squid) might sit on a guessed port.
	useLive(t, live{})
	c := dialMux(t, muxAddr, hello)
	_ = c.SetReadDeadline(time.Now().Add(ioWait))
	_, err := io.Copy(io.Discard, c)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("a TLS client was left hanging with no SSL backend")
	}

	// SSL TUNNEL gets installed on a port: TLS clients now reach it.
	useLive(t, live{ssl: portOf(t, tunnelAddr)})
	dialMux(t, muxAddr, hello)
	if got := waitString(t, tunnelGot); got != hello {
		t.Fatalf("tunnel got %q", got)
	}
}

func TestMuxFollowsSquidAndSSHPortChanges(t *testing.T) {
	squidAddr, squidGot := firstBytesBackend(t)
	sshAddr, sshGot := firstBytesBackend(t)
	muxAddr, _ := startMux(t)

	useLive(t, live{http: portOf(t, squidAddr), ssh: portOf(t, sshAddr)})
	dialMux(t, muxAddr, "CONNECT x:1 HTTP/1.1\r\n\r\n")
	if got := waitString(t, squidGot); got != "CONNECT " {
		t.Fatalf("squid got %q", got)
	}
	dialMux(t, muxAddr, "SSH-2.0-OpenSSH\r\n")
	if got := waitString(t, sshGot); got != "SSH-2.0-" {
		t.Fatalf("sshd got %q", got)
	}
}

// A backend port equal to the mux's own port (the port-change menus do not
// forbid it) must not make the mux connect to itself.
func TestMuxRefusesToDialItself(t *testing.T) {
	lines := captureFailLog(t)
	muxAddr, muxPort := startMux(t)
	useLive(t, live{ovpn: muxPort})

	c := dialMux(t, muxAddr, "\x00\x0e\x38\x01\x02\x03\x04\x05")
	_ = c.SetReadDeadline(time.Now().Add(ioWait))
	_, err := io.Copy(io.Discard, c)
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatal("the connection was not dropped")
	}
	if got := lines(); len(got) != 1 {
		t.Fatalf("log = %q, want one self-loop line", got)
	}
}
