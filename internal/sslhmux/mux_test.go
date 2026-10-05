package sslhmux

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

const ioWait = 3 * time.Second

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

// muxSession opens one client connection to a mux whose SSH backend is a
// fresh listener, writes an SSH banner, and returns both ends of the session.
func muxSession(t *testing.T) (client, backend net.Conn) {
	t.Helper()
	accepted := make(chan net.Conn, 1)
	sshAddr := listenLoopback(t, func(c net.Conn) { accepted <- c })
	muxAddr := listenLoopback(t, func(c net.Conn) { handleMuxConn(c, Config{SSH: sshAddr}) })

	client, err := net.Dial("tcp", muxAddr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	if _, err := client.Write([]byte("SSH-2.0-test\r\n")); err != nil {
		t.Fatal(err)
	}
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
