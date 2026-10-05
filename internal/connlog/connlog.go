// Package connlog rate-limits the failure lines of the SSL TUNNEL and SSLH
// daemons. Their ports face the Internet, so a repeated fault (a backend that
// is down) or a scanner must not flood the journal that the menu's "view log"
// shows only 50 lines of.
package connlog

import (
	"fmt"
	"net"
	"sync"
	"time"
)

// Throttle emits at most one line per key per window. Lines dropped inside the
// window are counted and reported on the next line for that key. Keys must
// come from a bounded set (a class name, a configured address), never from
// peer-controlled text: the map has no cap.
type Throttle struct {
	window time.Duration
	out    func(format string, args ...any)
	now    func() time.Time

	mu sync.Mutex
	m  map[string]entry
}

type entry struct {
	next       time.Time
	suppressed int
}

// New returns a Throttle that writes through out (usually log.Printf).
func New(window time.Duration, out func(format string, args ...any)) *Throttle {
	return &Throttle{window: window, out: out, now: time.Now, m: map[string]entry{}}
}

// Logf writes the line unless key was already written within the window.
func (t *Throttle) Logf(key, format string, args ...any) {
	t.mu.Lock()
	now := t.now()
	e := t.m[key]
	if !e.next.IsZero() && now.Before(e.next) {
		e.suppressed++
		t.m[key] = e
		t.mu.Unlock()
		return
	}
	suppressed := e.suppressed
	t.m[key] = entry{next: now.Add(t.window)}
	t.mu.Unlock()

	line := fmt.Sprintf(format, args...)
	if suppressed > 0 {
		line += fmt.Sprintf(" (+%d similar suppressed)", suppressed)
	}
	t.out("%s", line)
}

// IP returns the host part of addr, or "unknown" when there is none.
func IP(addr net.Addr) string {
	if addr == nil {
		return "unknown"
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil || host == "" {
		return "unknown"
	}
	return host
}
