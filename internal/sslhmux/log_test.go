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

// deadPort returns a loopback port nothing listens on.
func deadPort(t *testing.T) int {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := portOf(t, l.Addr().String())
	l.Close()
	return port
}

// probe sends first to a mux resolving its backends from l and returns once
// the mux has closed the connection (every probe below ends in a drop).
func probe(t *testing.T, l live, first []byte) {
	t.Helper()
	useLive(t, l)
	muxAddr, _ := startMux(t)
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

func TestClassify(t *testing.T) {
	for _, tc := range []struct {
		name string
		peek []byte
		want string
	}{
		{"ssh", []byte("SSH-2.0-x"), "ssh"},
		{"tls", []byte{0x16, 0x03, 0x01, 0x02}, "ssl"},
		{"http get", []byte("GET / HT"), "http"},
		{"http connect", []byte("CONNECT "), "http"},
		{"openvpn", []byte{0x00, 0x0e, 0x38, 0x01}, "other/openvpn"},
		{"garbage", []byte{0xde, 0xad}, "other/openvpn"},
	} {
		if got := classify(tc.peek); got != tc.want {
			t.Errorf("%s: classify = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestHandleMuxConnLogsUnreachableBackend(t *testing.T) {
	lines := captureFailLog(t)
	dead := deadPort(t)
	probe(t, live{ssh: dead}, []byte("SSH-2.0-"))
	got := lines()
	want := fmt.Sprintf("ssh backend 127.0.0.1:%d unreachable", dead)
	if len(got) != 1 || !strings.Contains(got[0], want) || !strings.Contains(got[0], "client 127.0.0.1") {
		t.Fatalf("log = %q, want one line containing %q and the client", got, want)
	}
}

func TestHandleMuxConnLogsMissingBackend(t *testing.T) {
	lines := captureFailLog(t)
	probe(t, live{}, []byte{0x16, 0x03, 0x01, 0x02, 0x00, 0x01, 0x00, 0x01})
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "no ssl backend is configured") || !strings.Contains(got[0], "127.0.0.1") {
		t.Fatalf("log = %q, want one missing-backend line", got)
	}
}

func TestHandleMuxConnLabelsUnrecognisedTrafficAsFallback(t *testing.T) {
	lines := captureFailLog(t)
	probe(t, live{ovpn: deadPort(t)}, []byte{0xde, 0xad, 0xbe, 0xef, 0xde, 0xad, 0xbe, 0xef})
	got := lines()
	if len(got) != 1 || !strings.Contains(got[0], "other/openvpn backend") {
		t.Fatalf("log = %q, want the fallback to be named as such", got)
	}
}

func TestHandleMuxConnFoldsRepeatedFailures(t *testing.T) {
	lines := captureFailLog(t)
	l := live{ssh: deadPort(t)}
	for i := 0; i < 20; i++ {
		probe(t, l, []byte("SSH-2.0-"))
	}
	if got := lines(); len(got) != 1 {
		t.Fatalf("20 failed sessions wrote %d lines: %q", len(got), got)
	}
}

func TestHandleMuxConnEmptyConnectsAreNotLogged(t *testing.T) {
	lines := captureFailLog(t)
	useLive(t, live{ssh: deadPort(t)})
	for i := 0; i < 20; i++ {
		a, b := net.Pipe()
		b.Close() // a scanner or health check that connects and hangs up
		handleMuxConn(a, Config{})
	}
	if got := lines(); len(got) != 0 {
		t.Fatalf("empty connects were logged: %q", got)
	}
}
