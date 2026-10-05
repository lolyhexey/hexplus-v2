package ssltunnel

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"errors"
	"io"
	"math/big"
	"net"
	"testing"
	"time"
)

const ioWait = 3 * time.Second

// testCerts returns a server and a client TLS config sharing a throwaway
// self-signed certificate for 127.0.0.1.
func testCerts(t *testing.T) (server, client *tls.Config) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ssltunnel test"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	cert, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(cert)
	return &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}},
		&tls.Config{RootCAs: pool}
}

// startTunnel runs handleConn behind a TLS listener forwarding to target and
// returns the address clients dial.
func startTunnel(t *testing.T, serverCfg *tls.Config, target string) string {
	t.Helper()
	ln, err := tls.Listen("tcp", "127.0.0.1:0", serverCfg)
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
			go handleConn(c, target)
		}
	}()
	return ln.Addr().String()
}

// startBackend listens on loopback and hands every accepted conn to the
// returned channel.
func startBackend(t *testing.T) (string, <-chan net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	ch := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			ch <- c
		}
	}()
	return ln.Addr().String(), ch
}

// tunnelPair opens one tunnel session and returns both ends of it.
func tunnelPair(t *testing.T) (client *tls.Conn, backend net.Conn) {
	t.Helper()
	serverCfg, clientCfg := testCerts(t)
	backendAddr, accepted := startBackend(t)
	addr := startTunnel(t, serverCfg, backendAddr)

	client, err := tls.Dial("tcp", addr, clientCfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	select {
	case backend = <-accepted:
	case <-time.After(ioWait):
		t.Fatal("the tunnel never dialed the backend")
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

func TestHandleConnForwardsBothWays(t *testing.T) {
	client, backend := tunnelPair(t)
	if _, err := client.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	buf := make([]byte, 4)
	_ = backend.SetReadDeadline(time.Now().Add(ioWait))
	if _, err := io.ReadFull(backend, buf); err != nil || string(buf) != "ping" {
		t.Fatalf("backend got %q, %v", buf, err)
	}
	if _, err := backend.Write([]byte("pong")); err != nil {
		t.Fatal(err)
	}
	_ = client.SetReadDeadline(time.Now().Add(ioWait))
	if _, err := io.ReadFull(client, buf); err != nil || string(buf) != "pong" {
		t.Fatalf("client got %q, %v", buf, err)
	}
}

// A client that disconnects must not leave the backend session (sshd) open.
func TestHandleConnClientCloseReachesBackend(t *testing.T) {
	client, backend := tunnelPair(t)
	client.Close()
	expectEOF(t, "backend", backend)
}

func TestHandleConnClientResetReachesBackend(t *testing.T) {
	client, backend := tunnelPair(t)
	tcp := client.NetConn().(*net.TCPConn)
	_ = tcp.SetLinger(0) // Close sends RST, as a dead or killed client would
	tcp.Close()
	expectEOF(t, "backend", backend)
}

func TestHandleConnBackendCloseReachesClient(t *testing.T) {
	client, backend := tunnelPair(t)
	backend.Close()
	expectEOF(t, "client", client)
}

func TestHandleConnDialFailureClosesClient(t *testing.T) {
	serverCfg, clientCfg := testCerts(t)
	dead, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := dead.Addr().String()
	dead.Close() // nothing listens here any more
	client, err := tls.Dial("tcp", startTunnel(t, serverCfg, addr), clientCfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetReadDeadline(time.Now().Add(ioWait))
	if n, err := client.Read(make([]byte, 1)); n != 0 || err == nil {
		t.Fatalf("Read = %d, %v; want the tunnel to close the client", n, err)
	}
}
