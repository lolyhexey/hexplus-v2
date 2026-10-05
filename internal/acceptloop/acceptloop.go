// Package acceptloop is the accept loop shared by the SSL TUNNEL and SSLH
// multiplexer daemons. It keeps the listener alive across accept errors that
// describe the process (out of file descriptors, a connection aborted before
// accept) rather than the listener, so established sessions survive them.
package acceptloop

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"time"
)

const (
	minDelay = 5 * time.Millisecond
	maxDelay = time.Second
	// logEvery bounds how often a long streak of failures is reported.
	logEvery = time.Minute
)

// Overridable for tests.
var (
	logf  = log.Printf
	now   = time.Now
	sleep = func(ctx context.Context, d time.Duration) bool {
		t := time.NewTimer(d)
		defer t.Stop()
		select {
		case <-t.C:
			return true
		case <-ctx.Done():
			return false
		}
	}
)

// Serve accepts connections from ln and runs handle for each in its own
// goroutine. The caller owns ln and is expected to close it when ctx is done.
//
// It returns nil when ctx is cancelled or ln is closed. A temporary accept
// error (EMFILE, ENFILE, ECONNABORTED, ...) is retried with a backoff of 5ms
// doubling to 1s, reset after the next successful accept. Any other accept
// error is returned, so a listener that is really broken still stops the
// process and lets systemd restart it.
func Serve(ctx context.Context, name string, ln net.Listener, handle func(net.Conn)) error {
	var (
		delay     time.Duration
		retries   int
		lastLogAt time.Time
	)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			// Temporary is deprecated for general use, but it is the exact
			// predicate net/http and internal/proxy use for accept errors.
			var ne net.Error
			if !errors.As(err, &ne) || !ne.Temporary() {
				return fmt.Errorf("accept: %w", err)
			}
			if delay == 0 {
				delay = minDelay
			} else if delay *= 2; delay > maxDelay {
				delay = maxDelay
			}
			retries++
			if t := now(); lastLogAt.IsZero() || t.Sub(lastLogAt) >= logEvery {
				logf("%s: accept error: %v; retrying in %v (%d failed accepts since the last report)", name, err, delay, retries)
				lastLogAt, retries = t, 0
			}
			if !sleep(ctx, delay) {
				return nil
			}
			continue
		}
		delay = 0
		go handle(conn)
	}
}
