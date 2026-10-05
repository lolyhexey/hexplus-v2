package ssltunnel

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/lolyhexey/hexplus/internal/connlog"
)

// captureFailLog swaps failLog for one that records lines (window: one hour,
// so only the first line per key passes) and restores it afterwards.
func captureFailLog(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var lines []string
	old := failLog
	failLog = connlog.New(time.Hour, func(f string, a ...any) {
		mu.Lock()
		defer mu.Unlock()
		lines = append(lines, fmt.Sprintf(f, a...))
	})
	t.Cleanup(func() { failLog = old })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), lines...)
	}
}

func waitForLines(t *testing.T, lines func() []string, n int) []string {
	t.Helper()
	deadline := time.Now().Add(ioWait)
	for {
		if got := lines(); len(got) >= n {
			return got
		}
		if time.Now().After(deadline) {
			t.Fatalf("log = %q, want %d line(s)", lines(), n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func deadBackendAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close() // nothing listens here any more
	return addr
}

func TestHandleConnLogsDialFailureWithClientIP(t *testing.T) {
	lines := captureFailLog(t)
	serverCfg, clientCfg := testCerts(t)
	dead := deadBackendAddr(t)
	client, err := tls.Dial("tcp", startTunnel(t, serverCfg, dead), clientCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	expectEOF(t, "client", client)

	got := waitForLines(t, lines, 1)
	if len(got) != 1 || !strings.Contains(got[0], "backend dial failed") ||
		!strings.Contains(got[0], dead) || !strings.Contains(got[0], "client 127.0.0.1") {
		t.Fatalf("log = %q, want one dial-failure line naming the backend and the client", got)
	}
}

func TestHandleConnFoldsRepeatedDialFailures(t *testing.T) {
	lines := captureFailLog(t)
	serverCfg, clientCfg := testCerts(t)
	addr := startTunnel(t, serverCfg, deadBackendAddr(t))
	for i := 0; i < 20; i++ {
		client, err := tls.Dial("tcp", addr, clientCfg)
		if err != nil {
			t.Fatal(err)
		}
		expectEOF(t, "client", client)
		client.Close()
	}
	if got := lines(); len(got) != 1 {
		t.Fatalf("20 failed sessions wrote %d lines: %q", len(got), got)
	}
}

func TestHandleConnDoesNotLogPlainTextOnTLSPort(t *testing.T) {
	lines := captureFailLog(t)
	serverCfg, _ := testCerts(t)
	addr := startTunnel(t, serverCfg, deadBackendAddr(t))
	raw, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if _, err := raw.Write([]byte("GET / HTTP/1.1\r\nAuthorization: Basic c2VjcmV0\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	_ = raw.SetReadDeadline(time.Now().Add(ioWait))
	_, _ = io.Copy(io.Discard, raw) // the server answers with a TLS alert and closes
	if got := lines(); len(got) != 0 {
		t.Fatalf("scanner traffic was logged: %q", got)
	}
}

func TestHandleConnLogsRejectedCertificate(t *testing.T) {
	lines := captureFailLog(t)
	serverCfg, _ := testCerts(t)
	addr := startTunnel(t, serverCfg, deadBackendAddr(t))
	// No trust anchor: the client refuses the self-signed certificate and
	// sends a TLS alert, as a phone with a strict profile would.
	if c, err := tls.Dial("tcp", addr, &tls.Config{}); err == nil {
		c.Close()
		t.Fatal("handshake unexpectedly succeeded")
	}
	got := waitForLines(t, lines, 1)
	if !strings.Contains(got[0], "TLS handshake with 127.0.0.1 failed") || !strings.Contains(got[0], "remote error") {
		t.Fatalf("log = %q, want the peer's TLS alert with its address", got)
	}
}

func TestIsHandshakeNoise(t *testing.T) {
	noise := []error{
		io.EOF,
		io.ErrUnexpectedEOF,
		&net.OpError{Op: "read", Err: os.NewSyscallError("read", syscall.ECONNRESET)},
		&net.OpError{Op: "write", Err: os.NewSyscallError("write", syscall.EPIPE)},
		os.ErrDeadlineExceeded,
		context.DeadlineExceeded,
		tls.RecordHeaderError{Msg: "first record does not look like a TLS handshake"},
	}
	for _, err := range noise {
		if !isHandshakeNoise(err) {
			t.Errorf("isHandshakeNoise(%v) = false, want true", err)
		}
	}
	actionable := []error{
		&net.OpError{Op: "remote error", Err: errors.New("tls: bad certificate")},
		errors.New("tls: no cipher suite supported by both client and server"),
	}
	for _, err := range actionable {
		if isHandshakeNoise(err) {
			t.Errorf("isHandshakeNoise(%v) = true, want false", err)
		}
	}
}
