package acceptloop

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

// result is one scripted Accept outcome.
type result struct {
	conn net.Conn
	err  error
}

// fakeListener plays back a script, then blocks until Close.
type fakeListener struct {
	script chan result
	closed chan struct{}
	once   sync.Once
}

func newFakeListener(rs ...result) *fakeListener {
	l := &fakeListener{script: make(chan result, len(rs)+4), closed: make(chan struct{})}
	for _, r := range rs {
		l.script <- r
	}
	return l
}

func (l *fakeListener) Accept() (net.Conn, error) {
	select {
	case r := <-l.script:
		return r.conn, r.err
	default:
	}
	select {
	case r := <-l.script:
		return r.conn, r.err
	case <-l.closed:
		return nil, &net.OpError{Op: "accept", Err: net.ErrClosed}
	}
}

func (l *fakeListener) Close() error   { l.once.Do(func() { close(l.closed) }); return nil }
func (l *fakeListener) Addr() net.Addr { return &net.TCPAddr{} }

func accErr(errno syscall.Errno) error {
	return &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept4", errno)}
}

// recorder replaces the package hooks: every sleep is reported on slept and
// skipped, logs are captured, and the clock only moves when the test says so.
type recorder struct {
	slept chan time.Duration
	clock time.Time

	mu   sync.Mutex
	logs []string
}

func (r *recorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.logs...)
}

func stub(t *testing.T) *recorder {
	t.Helper()
	oldLog, oldNow, oldSleep := logf, now, sleep
	t.Cleanup(func() { logf, now, sleep = oldLog, oldNow, oldSleep })

	r := &recorder{slept: make(chan time.Duration, 64), clock: time.Unix(1_000_000, 0)}
	logf = func(f string, a ...any) {
		r.mu.Lock()
		defer r.mu.Unlock()
		r.logs = append(r.logs, fmt.Sprintf(f, a...))
	}
	now = func() time.Time {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.clock
	}
	sleep = func(ctx context.Context, d time.Duration) bool {
		r.slept <- d
		return ctx.Err() == nil
	}
	return r
}

func (r *recorder) advance(d time.Duration) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.clock = r.clock.Add(d)
}

func (r *recorder) nextSleep(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-r.slept:
		return d
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not back off")
		return 0
	}
}

// serveAsync runs Serve and returns a channel carrying its result.
func serveAsync(ctx context.Context, ln net.Listener, handle func(net.Conn)) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Serve(ctx, "test", ln, handle) }()
	return done
}

func waitConn(t *testing.T, ch <-chan net.Conn) net.Conn {
	t.Helper()
	select {
	case c := <-ch:
		return c
	case <-time.After(2 * time.Second):
		t.Fatal("handle was not called")
		return nil
	}
}

func TestServeRetriesTemporaryErrorsAndKeepsAccepting(t *testing.T) {
	for name, accept := range map[string]error{
		"EMFILE":  accErr(syscall.EMFILE),
		"timeout": &net.OpError{Op: "accept", Net: "tcp", Err: os.ErrDeadlineExceeded},
	} {
		t.Run(name, func(t *testing.T) {
			stub(t)
			c1, c2 := net.Pipe()
			defer c1.Close()
			defer c2.Close()
			ln := newFakeListener(result{err: accept}, result{err: accept}, result{conn: c1})

			got := make(chan net.Conn, 1)
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			done := serveAsync(ctx, ln, func(c net.Conn) { got <- c })

			if waitConn(t, got) != c1 {
				t.Fatal("handle got the wrong conn")
			}
			select {
			case err := <-done:
				t.Fatalf("Serve returned %v after a temporary accept error", err)
			default:
			}
			cancel()
			ln.Close()
			if err := <-done; err != nil {
				t.Fatalf("Serve after shutdown = %v, want nil", err)
			}
		})
	}
}

func TestServeBackoffDoublesCapsAndResets(t *testing.T) {
	r := stub(t)
	var script []result
	for i := 0; i < 11; i++ {
		script = append(script, result{err: accErr(syscall.EMFILE)})
	}
	c1, c2 := net.Pipe()
	defer c1.Close()
	defer c2.Close()
	script = append(script, result{conn: c1}, result{err: accErr(syscall.EMFILE)})
	ln := newFakeListener(script...)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := serveAsync(ctx, ln, func(net.Conn) {})

	want := []time.Duration{
		5 * time.Millisecond, 10 * time.Millisecond, 20 * time.Millisecond, 40 * time.Millisecond,
		80 * time.Millisecond, 160 * time.Millisecond, 320 * time.Millisecond, 640 * time.Millisecond,
		time.Second, time.Second, time.Second,
		5 * time.Millisecond, // reset by the successful accept
	}
	for i, w := range want {
		if got := r.nextSleep(t); got != w {
			t.Fatalf("backoff #%d = %v, want %v", i+1, got, w)
		}
	}
	cancel()
	ln.Close()
	<-done
}

func TestServeCancelDuringBackoffReturnsPromptly(t *testing.T) {
	stub(t)
	entered := make(chan struct{}, 1)
	sleep = func(ctx context.Context, _ time.Duration) bool {
		entered <- struct{}{}
		tm := time.NewTimer(time.Hour)
		defer tm.Stop()
		select {
		case <-tm.C:
			return true
		case <-ctx.Done():
			return false
		}
	}
	ln := newFakeListener(result{err: accErr(syscall.EMFILE)})
	ctx, cancel := context.WithCancel(context.Background())
	done := serveAsync(ctx, ln, func(net.Conn) {})
	select {
	case <-entered:
	case err := <-done:
		t.Fatalf("Serve returned %v instead of backing off", err)
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not back off")
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("Serve = %v, want nil", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not stop while backing off")
	}
}

func TestServeReturnsNilWhenListenerClosed(t *testing.T) {
	stub(t)
	ln := newFakeListener()
	done := serveAsync(context.Background(), ln, func(net.Conn) {})
	ln.Close()
	if err := <-done; err != nil {
		t.Fatalf("Serve = %v, want nil on a closed listener", err)
	}
}

func TestServeReturnsNilWhenContextCancelled(t *testing.T) {
	stub(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// A non-temporary error, but the context is already done: shutdown, not a fault.
	ln := newFakeListener(result{err: errors.New("listener torn down")})
	if err := Serve(ctx, "test", ln, func(net.Conn) {}); err != nil {
		t.Fatalf("Serve = %v, want nil", err)
	}
}

func TestServeReturnsPermanentError(t *testing.T) {
	stub(t)
	boom := &net.OpError{Op: "accept", Net: "tcp", Err: os.NewSyscallError("accept4", syscall.EBADF)}
	ln := newFakeListener(result{err: boom})
	err := Serve(context.Background(), "test", ln, func(net.Conn) {})
	if !errors.Is(err, boom) {
		t.Fatalf("Serve = %v, want an error wrapping the accept error", err)
	}
}

func TestServeThrottlesRetryLog(t *testing.T) {
	r := stub(t)
	var script []result
	for i := 0; i < 5; i++ {
		script = append(script, result{err: accErr(syscall.EMFILE)})
	}
	ln := newFakeListener(script...)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := serveAsync(ctx, ln, func(net.Conn) {})

	for i := 0; i < 5; i++ {
		r.nextSleep(t)
	}
	if got := r.lines(); len(got) != 1 || !strings.HasPrefix(got[0], "test: accept error") {
		t.Fatalf("logs after 5 failures = %q, want exactly one line", got)
	}

	r.advance(2 * time.Minute)
	ln.script <- result{err: accErr(syscall.EMFILE)}
	r.nextSleep(t)
	cancel()
	ln.Close()
	<-done
	got := r.lines()
	if len(got) != 2 || !strings.Contains(got[1], "(5 failed accepts since the last report)") {
		t.Fatalf("logs = %q, want a second line after the window", got)
	}
}
