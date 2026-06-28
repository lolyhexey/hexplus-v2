// Package sslhmux implements a TCP port multiplexer that inspects the first
// bytes of each incoming connection and routes to the correct backend:
// SSH, SSL/TLS, HTTP proxy (Squid), or OpenVPN.
//
// No external binary is required — this is pure Go, managed by a systemd
// unit (hexplus-sslhmux.service) that calls `hexplus sslhmux run`.
package sslhmux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
)

const (
	// DBPath stores the JSON config (port + backend addresses).
	DBPath = "/var/lib/hexplus/sslhmux.json"

	// UnitName is the systemd service unit for the multiplexer.
	UnitName = "hexplus-sslhmux.service"
)

// Config is persisted to DBPath.
type Config struct {
	Port    int    `json:"port"`
	SSH     string `json:"ssh"`     // e.g. "127.0.0.1:22"
	SSL     string `json:"ssl"`     // e.g. "127.0.0.1:777"
	HTTP    string `json:"http"`    // e.g. "127.0.0.1:3128"
	OpenVPN string `json:"openvpn"` // e.g. "127.0.0.1:1194"
}

// Load reads DBPath. Returns a zero Config (no error) when the file is absent.
func Load() (Config, error) {
	data, err := os.ReadFile(DBPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return Config{}, nil
		}
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

// Save writes cfg to DBPath as JSON with mode 0600 (atomic write).
func (c Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(DBPath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(c)
	if err != nil {
		return err
	}
	tmp := DBPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmp, DBPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// IsInstalled returns true when DBPath exists and a port is configured.
func IsInstalled() bool {
	cfg, err := Load()
	if err != nil || cfg.Port <= 0 {
		return false
	}
	_, statErr := os.Stat(DBPath)
	return statErr == nil
}

// peekedConn wraps net.Conn so the already-read bytes are prepended back
// to the read stream — the backend sees ALL bytes including the ones we peeked.
type peekedConn struct {
	net.Conn
	r io.Reader // io.MultiReader(bytes.NewReader(peeked), conn)
}

func (c *peekedConn) Read(b []byte) (int, error) { return c.r.Read(b) }

// detect returns the backend address based on the first bytes of the connection.
// Match order: SSH → TLS → HTTP → OpenVPN (fallback).
func detect(peek []byte, cfg Config) string {
	// SSH: starts with "SSH-"
	if len(peek) >= 4 && string(peek[:4]) == "SSH-" {
		return cfg.SSH
	}
	// TLS ClientHello: first byte 0x16 (content type handshake), second 0x03
	if len(peek) >= 2 && peek[0] == 0x16 && peek[1] == 0x03 {
		return cfg.SSL
	}
	// HTTP methods (4-byte prefix match)
	httpPrefixes := [][]byte{
		[]byte("GET "), []byte("POST"), []byte("HEAD"), []byte("CONN"),
		[]byte("PUT "), []byte("DELE"), []byte("OPTI"),
	}
	for _, p := range httpPrefixes {
		if bytes.HasPrefix(peek, p) {
			return cfg.HTTP
		}
	}
	// Anything else (OpenVPN, etc.)
	return cfg.OpenVPN
}

// bridge copies bidirectionally between src and dst, closing both when done.
func bridge(src, dst net.Conn) {
	defer src.Close()
	defer dst.Close()
	done := make(chan struct{})
	go func() { io.Copy(dst, src); close(done) }()
	io.Copy(src, dst)
	<-done
}

// Run loads the config, opens a TCP listener on cfg.Port, peeks the first
// bytes of each accepted connection, routes to the correct backend, and
// bridges bidirectionally. Returns when ctx is cancelled.
func Run(ctx context.Context) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if cfg.Port <= 0 {
		return errors.New("sslhmux not configured (run 'hexplus menu' to install)")
	}

	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", cfg.Port))
	if err != nil {
		return fmt.Errorf("listen :%d: %w", cfg.Port, err)
	}

	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	for {
		conn, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return fmt.Errorf("accept: %w", err)
			}
		}
		go handleMuxConn(conn, cfg)
	}
}

// handleMuxConn peeks up to 8 bytes, detects the protocol, dials the backend,
// and bridges the connection. Silently closes on any error.
func handleMuxConn(conn net.Conn, cfg Config) {
	peek := make([]byte, 8)
	n, err := io.ReadFull(conn, peek)
	// n==0 means no bytes at all; other errors besides ErrUnexpectedEOF are fatal.
	if n == 0 || (err != nil && err != io.ErrUnexpectedEOF) {
		conn.Close()
		return
	}
	peek = peek[:n]

	target := detect(peek, cfg)
	if target == "" {
		conn.Close()
		return
	}

	dst, err := net.Dial("tcp", target)
	if err != nil {
		conn.Close()
		return
	}

	// Wrap conn so the backend receives the peeked bytes too.
	pc := &peekedConn{
		Conn: conn,
		r:    io.MultiReader(bytes.NewReader(peek), conn),
	}
	go bridge(pc, dst)
}
