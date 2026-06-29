// Package ssltunnel runs a self-signed TLS listener that forwards every
// decrypted connection to a local target (port 22 for SSH, port 80 for
// WebSocket). No external binary (stunnel4) is required — everything is
// implemented with crypto/tls from the Go standard library.
package ssltunnel

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"time"
)

const (
	// DBPath stores the JSON config (port + target).
	DBPath = "/var/lib/hexplus/ssltunnel.json"

	// CertFile is the PEM-encoded self-signed TLS certificate.
	CertFile = "/usr/local/lib/hexplus/ssltunnel.crt"

	// KeyFile is the PEM-encoded RSA 2048 private key (mode 0600).
	KeyFile = "/usr/local/lib/hexplus/ssltunnel.key"

	// UnitName is the systemd service unit for the tunnel.
	UnitName = "hexplus-ssltunnel.service"
)

// Config is persisted to DBPath.
type Config struct {
	Port   int    `json:"port"`
	Target string `json:"target"`
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

// Save writes cfg to DBPath as JSON with mode 0600.
func (cfg Config) Save() error {
	if err := os.MkdirAll(filepath.Dir(DBPath), 0o755); err != nil {
		return err
	}
	data, err := json.Marshal(cfg)
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

// IsInstalled returns true when CertFile exists and a port is configured.
func IsInstalled() bool {
	cfg, err := Load()
	if err != nil || cfg.Port <= 0 {
		return false
	}
	_, err = os.Stat(CertFile)
	return err == nil
}

// GenerateCert creates a new RSA 2048 self-signed TLS certificate valid for
// 10 years and writes CertFile (0644) and KeyFile (0600).
func GenerateCert() error {
	if err := os.MkdirAll(filepath.Dir(CertFile), 0o755); err != nil {
		return err
	}

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return err
	}

	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName: "HEXPLUS SSL Tunnel",
		},
		NotBefore:             time.Now(),
		NotAfter:              time.Now().Add(10 * 365 * 24 * time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}

	certDER, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return err
	}

	// Write certificate (0644).
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER})
	tmpCert := CertFile + ".tmp"
	if err := os.WriteFile(tmpCert, certPEM, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpCert, CertFile); err != nil {
		_ = os.Remove(tmpCert)
		return err
	}

	// Write private key (0600).
	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	tmpKey := KeyFile + ".tmp"
	if err := os.WriteFile(tmpKey, keyPEM, 0o600); err != nil {
		return err
	}
	if err := os.Rename(tmpKey, KeyFile); err != nil {
		_ = os.Remove(tmpKey)
		return err
	}

	return nil
}

// Run loads the config, opens a TLS listener on cfg.Port, and forwards each
// accepted connection to cfg.Target via bidirectional io.Copy. It returns
// when ctx is cancelled (listener is closed to unblock Accept).
func Run(ctx context.Context) error {
	cfg, err := Load()
	if err != nil {
		return err
	}
	if cfg.Port <= 0 {
		return errors.New("ssltunnel not configured (run 'hexplus menu' to install)")
	}

	tlsCert, err := tls.LoadX509KeyPair(CertFile, KeyFile)
	if err != nil {
		return fmt.Errorf("load cert/key: %w", err)
	}
	tlsCfg := &tls.Config{Certificates: []tls.Certificate{tlsCert}}

	ln, err := tls.Listen("tcp", fmt.Sprintf(":%d", cfg.Port), tlsCfg)
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
		go handleConn(conn, cfg.Target)
	}
}

// handshakeTimeout bounds TLS handshake + initial peek so half-open scanners
// don't pin a goroutine and an upstream conn forever.
const handshakeTimeout = 10 * time.Second

func handleConn(src net.Conn, target string) {
	// Force handshake BEFORE dialing the upstream — scanners that open TCP
	// but never start a handshake would otherwise leak both ends.
	if tlsConn, ok := src.(*tls.Conn); ok {
		hctx, cancel := context.WithTimeout(context.Background(), handshakeTimeout)
		if err := tlsConn.HandshakeContext(hctx); err != nil {
			cancel()
			src.Close()
			return
		}
		cancel()
	}
	dst, err := net.Dial("tcp", target)
	if err != nil {
		src.Close()
		return
	}
	bridge(src, dst)
}

func bridge(src, dst net.Conn) {
	defer src.Close()
	defer dst.Close()
	go io.Copy(dst, src)
	io.Copy(src, dst)
}
