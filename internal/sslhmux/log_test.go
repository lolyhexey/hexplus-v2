package sslhmux

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
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

// probe sends first to a mux serving cfg and returns once the mux has closed
// the connection (every probe below ends in a drop).
func probe(t *testing.T, cfg Config, first []byte) {
	t.Helper()
	muxAddr := listenLoopback(t, func(c net.Conn) { handleMuxConn(c, cfg) })
	c, err := net.Dial("tcp", muxAddr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write(first); err != nil {
		t.Fatal(err)
	}
	_ = c.SetReadDeadline(time.Now().Add(ioWait))
	_, err = io.Copy(io.Discard, c)
	// A reset is as good as a FIN here (the peer may close with unread data).
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		t.Fatalf("the mux did not close the connection: %v", err)
	}
}

func TestDetect(t *testing.T) {
	cfg := Config{SSH: "ssh:1", SSL: "ssl:2", HTTP: "http:3", OpenVPN: "ovpn:4"}
	for _, tc := range []struct {
		name  string
		peek  []byte
		kind  string
		wants string
	}{
		{"ssh", []byte("SSH-2.0-x"), "ssh", "ssh:1"},
		{"tls", []byte{0x16, 0x03, 0x01, 0x02}, "ssl", "ssl:2"},
		{"http get", []byte("GET / HT"), "http", "http:3"},
		{"http connect", []byte("CONNECT "), "http", "http:3"},
		{"openvpn", []byte{0x00, 0x0e, 0x38, 0x01}, "other/openvpn", "ovpn:4"},
		{"garbage", []byte{0xde, 0xad}, "other/openvpn", "ovpn:4"},
	} {
		kind, target := detect(tc.peek, cfg)
		if kind != tc.kind || target != tc.wants {
			t.Errorf("%s: detect = %q, %q; want %q, %q", tc.name, kind, target, tc.kind, tc.wants)
		}
	}
}

func TestHandleMuxConnLogsUnreachableBackend(t *testing.T) {
	lines := captureFailLog(t)
	dead := deadBackendAddr(t)
	probe(t, Config{SSH: dead}, []byte("SSH-2.0-"))
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "ssh backend "+dead+" unreachable") || !strings.Contains(got[0], "client 127.0.0.1") {
		t.Fatalf("log = %q, want one line naming the kind, the backend and the client", got)
	}
}

func TestHandleMuxConnLogsMissingBackend(t *testing.T) {
	lines := captureFailLog(t)
	probe(t, Config{SSL: ""}, []byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00, 0x01})
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "no ssl backend is configured") || !strings.Contains(got[0], "127.0.0.1") {
		t.Fatalf("log = %q, want one missing-backend line", got)
	}
}

func TestHandleMuxConnLabelsUnrecognisedTrafficAsFallback(t *testing.T) {
	lines := captureFailLog(t)
	probe(t, Config{OpenVPN: deadBackendAddr(t)}, []byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef})
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "other/openvpn backend") {
		t.Fatalf("log = %q, want the fallback to be named as such", got)
	}
}

func TestHandleMuxConnFoldsRepeatedFailures(t *testing.T) {
	lines := captureFailLog(t)
	cfg := Config{SSH: deadBackendAddr(t)}
	for i := 0; i < 20; i++ {
		probe(t, cfg, []byte("SSH-2.0-"))
	}
	if got := lines(); len(got) != 1 {
		t.Fatalf("20 failed sessions wrote %d lines: %q", len(got), got)
	}
}

func TestHandleMuxConnEmptyConnectsAreNotLogged(t *testing.T) {
	lines := captureFailLog(t)
	for i := 0; i < 20; i++ {
		a, b := net.Pipe()
		b.Close() // a scanner or health check that connects and hangs up
		handleMuxConn(a, Config{SSH: deadBackendAddr(t)})
	}
	if got := lines(); len(got) != 0 {
		t.Fatalf("empty connects were logged: %q", got)
	}
}
