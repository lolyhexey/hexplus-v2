package netbridge

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

const wait = 3 * time.Second

// tcpPair returns the two ends of a loopback TCP connection.
func tcpPair(t *testing.T) (a, b *net.TCPConn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	acc := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		acc <- c
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	s := <-acc
	t.Cleanup(func() { c.Close(); s.Close() })
	return c.(*net.TCPConn), s.(*net.TCPConn)
}

// bridged wires clientPeer <-> Pipe <-> backendPeer over loopback and returns
// the two peers plus a channel closed when Pipe returns.
func bridged(t *testing.T) (clientPeer, backendPeer *net.TCPConn, done <-chan struct{}) {
	t.Helper()
	clientPeer, clientSide := tcpPair(t)
	backendSide, backendPeer := tcpPair(t)
	d := make(chan struct{})
	go func() { Pipe(clientSide, backendSide); close(d) }()
	return clientPeer, backendPeer, d
}

func expectEOF(t *testing.T, who string, c net.Conn) {
	t.Helper()
	_ = c.SetReadDeadline(time.Now().Add(wait))
	n, err := c.Read(make([]byte, 16))
	if n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("%s: Read = %d, %v; want EOF", who, n, err)
	}
}

func expectDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(wait):
		t.Fatal("Pipe did not return")
	}
}

func TestPipeForwardsBothWays(t *testing.T) {
	client, backend, done := bridged(t)
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = backend.SetReadDeadline(time.Now().Add(wait))
	if _, err := io.ReadFull(backend, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("backend got %q, %v", buf, err)
	}
	if _, err := backend.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(wait))
	if _, err := io.ReadFull(client, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("client got %q, %v", buf, err)
	}
	backend.Close()
	expectDone(t, done)
}

func TestPipeBackendCloseEndsSilentClient(t *testing.T) {
	client, backend, done := bridged(t)
	backend.Close()
	expectEOF(t, "client", client)
	expectDone(t, done)
}

func TestPipeClientCloseReachesBackend(t *testing.T) {
	client, backend, done := bridged(t)
	client.Close()
	expectEOF(t, "backend", backend)
	backend.Close()
	expectDone(t, done)
}

func TestPipeClientHalfCloseKeepsReplyDirection(t *testing.T) {
	client, backend, done := bridged(t)
	if _, err := client.Write([]byte("req")); err != nil {
		t.Fatal(err)
	}
	if err := client.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 3)
	_ = backend.SetReadDeadline(time.Now().Add(wait))
	if _, err := io.ReadFull(backend, buf); err != nil || string(buf) != "req" {
		t.Fatalf("backend got %q, %v", buf, err)
	}
	expectEOF(t, "backend", backend)

	if _, err := backend.Write([]byte("rsp")); err != nil {
		t.Fatal(err)
	}
	backend.Close()
	_ = client.SetReadDeadline(time.Now().Add(wait))
	got, err := io.ReadAll(client)
	if err != nil || string(got) != "rsp" {
		t.Fatalf("client got %q, %v; want the reply written after its half-close", got, err)
	}
	expectDone(t, done)
}

func TestPipeClientResetClosesBackend(t *testing.T) {
	client, backend, done := bridged(t)
	_ = client.SetLinger(0) // Close sends RST instead of FIN
	client.Close()
	expectEOF(t, "backend", backend)
	expectDone(t, done)
}

func TestDialTimesOut(t *testing.T) {
	old := dialTimeout
	dialTimeout = 300 * time.Millisecond
	t.Cleanup(func() { dialTimeout = old })

	// 192.0.2.0/24 (TEST-NET-1) is never routed. Depending on the host the
	// dial either hangs until the timeout or is rejected at once; only the
	// first case says anything about the timeout.
	start := time.Now()
	c, err := Dial("192.0.2.1:9")
	elapsed := time.Since(start)
	if err == nil {
		c.Close()
		t.Skip("TEST-NET-1 is reachable from this host")
	}
	if elapsed < 250*time.Millisecond {
		t.Skipf("dial failed at once (%v), the host rejects unroutable addresses", err)
	}
	var ne net.Error
	if !errors.As(err, &ne) || !ne.Timeout() {
		t.Fatalf("Dial error = %v, want a timeout", err)
	}
	if elapsed > 2*time.Second {
		t.Fatalf("Dial took %v with a %v timeout", elapsed, dialTimeout)
	}
}

func TestDialConnects(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	c, err := Dial(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	c.Close()
}
