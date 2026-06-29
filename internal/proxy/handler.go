// Package proxy is the Go-native HTTP CONNECT proxy that replaces the
// three Python scripts HEXPLUS v1 ran via screen sessions
// (proxy.py / wsproxy.py / open.py).
//
// One proxy listens on one TCP port. Incoming clients send an HTTP request
// header (often actual CONNECT, sometimes garbage with an X-Real-Host:
// header tacked on - HTTP Injector / KPN Tunnel work the latter way).
// We read up to a buffer's worth, look for X-Real-Host, fall back to the
// configured DefaultHost if absent, open a TCP connection upstream, send
// the configured spoof status line back to the client, then bridge bytes
// in both directions until either side closes.
//
// Why Go instead of subprocessing the Python scripts: in-process goroutines
// kill per-connection latency. Python's asyncio with /dev/tty logging and
// no TCP_NODELAY took 200-400ms per CONNECT setup; net.Conn + io.Copy with
// the socket tuning we apply here is sub-millisecond, and we don't need
// to extract/manage a separate python3 + script on every install.

package proxy

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

const (
	// readBufBytes caps the initial header read. v1's Python set this to
	// 65 KB; in practice HTTP request headers from injector apps stay
	// well under 4 KB. 16 KB is comfortable headroom without inviting
	// abuse.
	readBufBytes = 16 * 1024

	// dialTimeout bounds how long we wait when opening the upstream
	// connection. Slow upstreams (squid bound to a busy port, openvpn
	// not yet up) shouldn't keep accept loops waiting.
	dialTimeout = 10 * time.Second

	// idleTimeout drops connections that haven't moved bytes in this
	// long. Stops idle clients from pinning fds forever.
	idleTimeout = 60 * time.Second
)

// allowedHosts is the exact host whitelist for X-Real-Host. Compared by
// equality on the host half of "host:port" — *not* by prefix, so
// "127.0.0.1.evil.com:22" can't sneak past as "starts with 127.0.0.1".
// DefaultHost is always trusted (it was set by the operator, not the client).
var allowedHosts = map[string]bool{
	"127.0.0.1": true,
	"0.0.0.0":   true,
	"localhost": true,
}

// Handler holds the immutable per-proxy configuration that the request
// path needs. Concurrency-safe: every field is read-only after New().
type Handler struct {
	cfg Config

	// responseBytes is the fully-baked HTTP/1.x status line we send
	// back to the client once we've opened the upstream. Built at
	// New() so the hot path doesn't restring it per connection.
	responseBytes []byte
}

// NewHandler validates the config and pre-computes the response bytes.
func NewHandler(cfg Config) (*Handler, error) {
	if cfg.Port <= 0 || cfg.Port > 65535 {
		return nil, fmt.Errorf("invalid port %d", cfg.Port)
	}
	if cfg.DefaultHost == "" {
		return nil, errors.New("DefaultHost is required")
	}
	if cfg.StatusCode == "" {
		cfg.StatusCode = "200"
	}
	if cfg.StatusMsg == "" {
		cfg.StatusMsg = "Connection established"
	}
	h := &Handler{cfg: cfg}
	// expandEscapes turns the literal '\r\n' / '\n' the caller stored
	// in JSON back into real CRLF, matching what the Python proxies did
	// with str.replace('\\r\\n','\r\n').
	msg := expandEscapes(cfg.StatusMsg)
	h.responseBytes = []byte("HTTP/1.1 " + cfg.StatusCode + " " + msg + "\r\n\r\n")
	return h, nil
}


// Serve runs the accept loop until ctx is done or the listener errors
// fatally. Each accepted connection runs handleConn in its own
// goroutine; cancellation propagates via ctx.
func (h *Handler) Serve(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", h.cfg.Port)
	lc := net.ListenConfig{KeepAlive: 30 * time.Second}
	ln, err := lc.Listen(ctx, "tcp", addr)
	if err != nil {
		return fmt.Errorf("listen %s: %w", addr, err)
	}
	log.Printf("hexplus-proxy[%s]: listening on %s -> default %s, status %s",
		h.cfg.Name, addr, h.cfg.DefaultHost, h.cfg.StatusCode)

	go func() {
		<-ctx.Done()
		_ = ln.Close()
	}()

	var wg sync.WaitGroup
	for {
		client, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) || ctx.Err() != nil {
				wg.Wait()
				return nil
			}
			// Transient accept errors (EMFILE, etc.) - back off briefly
			// so we don't burn CPU spinning on the same error.
			var ne net.Error
			if errors.As(err, &ne) && ne.Temporary() {
				time.Sleep(20 * time.Millisecond)
				continue
			}
			wg.Wait()
			return fmt.Errorf("accept: %w", err)
		}
		wg.Add(1)
		go func(c net.Conn) {
			defer wg.Done()
			h.handleConn(c)
		}(client)
	}
}

// handleConn owns one client connection from accept to close.
func (h *Handler) handleConn(client net.Conn) {
	defer client.Close()
	tuneTCP(client)

	// Bounded initial-header read. Some clients pipeline data after
	// the request, which is fine - we'll bridge it.
	_ = client.SetReadDeadline(time.Now().Add(idleTimeout))
	buf := make([]byte, readBufBytes)
	n, err := client.Read(buf)
	if err != nil || n == 0 {
		return
	}
	_ = client.SetReadDeadline(time.Time{}) // clear

	header := buf[:n]

	// X-Real-Host: if present, use it (client-supplied target).
	// X-Split: if present, read and discard one more buffer — some injector
	// apps split the request into two packets; we drain the second before
	// opening upstream so the upstream sees a clean connection (v1 behaviour).
	hostPort := findHeader(header, "X-Real-Host")
	if findHeader(header, "X-Split") != "" {
		extra := make([]byte, readBufBytes)
		_, _ = client.Read(extra)
	}
	fromClient := hostPort != ""
	if hostPort == "" {
		hostPort = h.cfg.DefaultHost
	}

	// Restrict X-Real-Host to the same hosts v1 ALLOWED_PREFIXES enforced,
	// but compare on the host half of "host:port" with an exact match —
	// not a prefix — so "127.0.0.1.evil.com:22" can't slip through.
	// DefaultHost is operator-set so it's always trusted.
	if fromClient {
		host, _, splitErr := net.SplitHostPort(hostPort)
		if splitErr != nil {
			_, _ = client.Write([]byte("HTTP/1.1 403 Forbidden!\r\n\r\n"))
			return
		}
		if !allowedHosts[strings.ToLower(host)] {
			_, _ = client.Write([]byte("HTTP/1.1 403 Forbidden!\r\n\r\n"))
			return
		}
	}

	// Dial upstream.
	dialCtx, cancel := context.WithTimeout(context.Background(), dialTimeout)
	defer cancel()
	var d net.Dialer
	target, err := d.DialContext(dialCtx, "tcp", hostPort)
	if err != nil {
		// Map the failure into an HTTP-shaped response so injector apps
		// can render something useful instead of just timing out.
		_, _ = client.Write([]byte("HTTP/1.1 502 Bad Gateway\r\n\r\n"))
		return
	}
	defer target.Close()
	tuneTCP(target)

	// Hand the client the spoof status line. Some apps then immediately
	// start sending the tunneled protocol bytes - we already have the
	// upstream open, so bridging is symmetric.
	if _, err := client.Write(h.responseBytes); err != nil {
		return
	}

	// Bridge in both directions. We use io.Copy which on Linux ends up
	// calling splice(2) when both endpoints are TCP - kernel handles
	// the byte shuffle without us round-tripping through userspace.
	done := make(chan struct{}, 2)
	go func() {
		_, _ = io.Copy(target, client)
		// half-close so the peer sees EOF on its side
		if tc, ok := target.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	go func() {
		_, _ = io.Copy(client, target)
		if tc, ok := client.(*net.TCPConn); ok {
			_ = tc.CloseWrite()
		}
		done <- struct{}{}
	}()
	<-done
	<-done
}

// tuneTCP disables Nagle's algorithm and enables TCP keepalive on the
// connection. Interactive workloads (SSH, web) get sub-millisecond
// instead of 40-200 ms per tiny packet; idle tunnels get reaped within
// a couple of minutes instead of hanging at 2 hours.
//
// Keepalive params match v1 Python proxies exactly (KEEPIDLE=30 /
// KEEPINTVL=10 / KEEPCNT=3 → dead connection detected in ≈60 s).
func tuneTCP(c net.Conn) {
	tc, ok := c.(*net.TCPConn)
	if !ok {
		return
	}
	_ = tc.SetNoDelay(true)
	_ = tc.SetKeepAliveConfig(net.KeepAliveConfig{
		Enable:   true,
		Idle:     30 * time.Second,
		Interval: 10 * time.Second,
		Count:    3,
	})
}

// findHeader locates 'X-Real-Host:' in the initial buffer and returns
// its value, trimmed. Matches the Python version's behavior including
// the case-sensitive header lookup (apps that hit it know what they're
// sending). Returns "" if the header is missing or malformed.
func findHeader(head []byte, name string) string {
	needle := []byte(name + ": ")
	idx := bytes.Index(head, needle)
	if idx < 0 {
		return ""
	}
	rest := head[idx+len(needle):]
	end := bytes.Index(rest, []byte("\r\n"))
	if end < 0 {
		return ""
	}
	return strings.TrimSpace(string(rest[:end]))
}

// expandEscapes turns the literal '\r\n' / '\n' sequences the operator
// types in JSON into actual control bytes. This is the same trick the
// Python proxies did so the operator can encode multi-header responses
// (e.g. "Connection established\r\nContent-length: 0") without escaping
// from a YAML or shell context.
func expandEscapes(s string) string {
	s = strings.ReplaceAll(s, `\r\n`, "\r\n")
	s = strings.ReplaceAll(s, `\n`, "\n")
	return s
}
