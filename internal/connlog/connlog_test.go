package connlog

import (
	"fmt"
	"net"
	"testing"
	"time"
)

func newTest(window time.Duration) (*Throttle, *[]string, *time.Time) {
	var lines []string
	clock := time.Unix(1_000_000, 0)
	t := New(window, func(f string, a ...any) { lines = append(lines, fmt.Sprintf(f, a...)) })
	t.now = func() time.Time { return clock }
	return t, &lines, &clock
}

func TestThrottleFirstAllowedRestSuppressed(t *testing.T) {
	th, lines, _ := newTest(time.Minute)
	for i := 0; i < 50; i++ {
		th.Logf("dial", "backend down %d", i)
	}
	if len(*lines) != 1 || (*lines)[0] != "backend down 0" {
		t.Fatalf("lines = %q, want only the first", *lines)
	}
}

func TestThrottleReportsSuppressedCountAfterWindow(t *testing.T) {
	th, lines, clock := newTest(time.Minute)
	for i := 0; i < 4; i++ {
		th.Logf("dial", "backend down")
	}
	*clock = clock.Add(time.Minute)
	th.Logf("dial", "backend down")
	if len(*lines) != 2 || (*lines)[1] != "backend down (+3 similar suppressed)" {
		t.Fatalf("lines = %q", *lines)
	}
	// The count starts over after it is reported.
	*clock = clock.Add(time.Minute)
	th.Logf("dial", "backend down")
	if (*lines)[2] != "backend down" {
		t.Fatalf("lines = %q, want no stale suppressed count", *lines)
	}
}

func TestThrottleKeysAreIndependent(t *testing.T) {
	th, lines, _ := newTest(time.Hour)
	th.Logf("a", "a")
	th.Logf("b", "b")
	th.Logf("a", "a again")
	if len(*lines) != 2 {
		t.Fatalf("lines = %q, want one per key", *lines)
	}
}

func TestIP(t *testing.T) {
	for _, tc := range []struct {
		addr net.Addr
		want string
	}{
		{&net.TCPAddr{IP: net.ParseIP("203.0.113.9"), Port: 5555}, "203.0.113.9"},
		{&net.TCPAddr{IP: net.ParseIP("2001:db8::1"), Port: 443}, "2001:db8::1"},
		{nil, "unknown"},
	} {
		if got := IP(tc.addr); got != tc.want {
			t.Errorf("IP(%v) = %q, want %q", tc.addr, got, tc.want)
		}
	}
}
