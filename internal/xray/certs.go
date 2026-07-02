package xray

// certs.go: manage TLS certificates that xray-core reads for VLESS /
// VMess / Trojan + TLS inbounds. Two sources are supported:
//
//   - manual:  operator uploads a PEM cert + key; we store both under
//              XrayStateDir/certs/<domain>/
//   - acme:    HTTP-01 challenge against Let's Encrypt; the panel briefly
//              binds :80 to serve the challenge token, requests the
//              cert, and writes it to the same location.
//
// The panel DB's certs table indexes each pair by domain, records the
// NotAfter timestamp for renewal cron, and hands out the on-disk paths
// to inbound builders via CertPathsFor(domain).

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/crypto/acme"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// CertPaths is the on-disk pair xray-core references in its TLSSettings.
type CertPaths struct {
	CertFile string
	KeyFile  string
}

// CertsDir is the parent directory for all managed certs. One
// subdirectory per domain.
func CertsDir() string { return filepath.Join(paths.XrayStateDir, "certs") }

// CertPathsFor returns the on-disk pair for a domain. Callers check
// they exist before referencing them from an xray config.
func CertPathsFor(domain string) CertPaths {
	dir := filepath.Join(CertsDir(), sanitizeDomain(domain))
	return CertPaths{
		CertFile: filepath.Join(dir, "fullchain.pem"),
		KeyFile:  filepath.Join(dir, "privkey.pem"),
	}
}

// SaveManualCert persists an operator-supplied cert+key PEM pair.
// Returns the file paths + the parsed NotAfter (so the DB row can
// carry the renewal deadline).
func SaveManualCert(domain string, certPEM, keyPEM []byte) (CertPaths, time.Time, error) {
	if err := validatePEMPair(certPEM, keyPEM); err != nil {
		return CertPaths{}, time.Time{}, err
	}
	dir := filepath.Join(CertsDir(), sanitizeDomain(domain))
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return CertPaths{}, time.Time{}, err
	}
	certFile := filepath.Join(dir, "fullchain.pem")
	keyFile := filepath.Join(dir, "privkey.pem")
	if err := writeAtomic(certFile, certPEM, 0o600); err != nil {
		return CertPaths{}, time.Time{}, err
	}
	if err := writeAtomic(keyFile, keyPEM, 0o600); err != nil {
		return CertPaths{}, time.Time{}, err
	}
	notAfter, _ := readNotAfter(certPEM)
	return CertPaths{CertFile: certFile, KeyFile: keyFile}, notAfter, nil
}

// AcquireACMECert runs the Let's Encrypt HTTP-01 flow for the given
// domain. Binds :80 for the length of the challenge. Returns cert
// paths + expiry; the caller records both to the certs DB row.
//
// contactEmail is passed to ACME registration so notification mail
// lands somewhere sensible; empty is acceptable but discouraged.
func AcquireACMECert(ctx context.Context, domain, contactEmail string) (CertPaths, time.Time, error) {
	if domain == "" {
		return CertPaths{}, time.Time{}, errors.New("domain required")
	}
	// Verify :80 is free before we go to Let's Encrypt — LE will fail
	// the challenge silently otherwise. Explicit error beats an opaque
	// retry loop.
	if err := probePort80(); err != nil {
		return CertPaths{}, time.Time{}, err
	}

	accountKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CertPaths{}, time.Time{}, fmt.Errorf("gen account key: %w", err)
	}
	client := &acme.Client{
		Key:          accountKey,
		DirectoryURL: acme.LetsEncryptURL,
	}
	if _, err := client.Register(ctx, &acme.Account{
		Contact: contactSlice(contactEmail),
	}, acme.AcceptTOS); err != nil {
		return CertPaths{}, time.Time{}, fmt.Errorf("register: %w", err)
	}

	order, err := client.AuthorizeOrder(ctx, []acme.AuthzID{{Type: "dns", Value: domain}})
	if err != nil {
		return CertPaths{}, time.Time{}, fmt.Errorf("authorize: %w", err)
	}

	// Serve HTTP-01 challenge on :80 until validation clears.
	stopChallenge, err := serveHTTP01(client, order)
	if err != nil {
		return CertPaths{}, time.Time{}, err
	}
	defer stopChallenge()

	if _, err := client.WaitOrder(ctx, order.URI); err != nil {
		return CertPaths{}, time.Time{}, fmt.Errorf("wait order: %w", err)
	}

	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return CertPaths{}, time.Time{}, err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		DNSNames: []string{domain},
	}, certKey)
	if err != nil {
		return CertPaths{}, time.Time{}, err
	}
	certChain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return CertPaths{}, time.Time{}, fmt.Errorf("create cert: %w", err)
	}

	// Serialise: private key first then bundle the chain.
	var certPEM []byte
	for _, der := range certChain {
		certPEM = append(certPEM, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})...)
	}
	keyDER, err := x509.MarshalECPrivateKey(certKey)
	if err != nil {
		return CertPaths{}, time.Time{}, err
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: keyDER})
	return SaveManualCert(domain, certPEM, keyPEM)
}

// RemoveCert deletes both files. Missing files aren't an error.
func RemoveCert(domain string) error {
	dir := filepath.Join(CertsDir(), sanitizeDomain(domain))
	if err := os.RemoveAll(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// serveHTTP01 walks the order's authorizations, chooses the http-01
// challenge, spins up a listener on :80 that answers /.well-known/
// acme-challenge/*, and tells LE to verify. Returns a stop() callback
// the caller defers to shut the listener down.
func serveHTTP01(client *acme.Client, order *acme.Order) (func(), error) {
	if len(order.AuthzURLs) == 0 {
		return nil, errors.New("acme: order has no authorizations")
	}
	mux := http.NewServeMux()
	srv := &http.Server{Addr: ":80", Handler: mux}
	ready := make(chan struct{}, 1)

	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		return nil, fmt.Errorf("bind :80: %w (free the port or use a reverse proxy)", err)
	}
	go func() {
		_ = srv.Serve(ln)
	}()

	// Answer every LE-authorized token this order needs.
	ctx := context.Background()
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			_ = srv.Close()
			return nil, fmt.Errorf("get authz: %w", err)
		}
		var http01 *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "http-01" {
				http01 = c
				break
			}
		}
		if http01 == nil {
			_ = srv.Close()
			return nil, errors.New("acme: no http-01 challenge offered")
		}
		token := http01.Token
		keyAuth, err := client.HTTP01ChallengeResponse(token)
		if err != nil {
			_ = srv.Close()
			return nil, err
		}
		mux.HandleFunc("/.well-known/acme-challenge/"+token, func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte(keyAuth))
		})
		if _, err := client.Accept(ctx, http01); err != nil {
			_ = srv.Close()
			return nil, fmt.Errorf("accept: %w", err)
		}
	}
	select {
	case ready <- struct{}{}:
	default:
	}
	return func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
	}, nil
}

// probePort80 fails fast if :80 is already bound.
func probePort80() error {
	ln, err := net.Listen("tcp", ":80")
	if err != nil {
		return fmt.Errorf("port 80 in use (acme http-01 needs :80 free): %w", err)
	}
	_ = ln.Close()
	return nil
}

// contactSlice formats an ACME contact string ("mailto:...") — LE
// wants a slice; empty email skips the field.
func contactSlice(email string) []string {
	if email == "" {
		return nil
	}
	return []string{"mailto:" + email}
}

// validatePEMPair sanity-checks the two inputs — cert must parse as
// x509 and key must decode as PEM. We DON'T do a full cert-matches-key
// check here; xray-core will reject a mismatched pair on startup and
// journalctl will show the error.
func validatePEMPair(certPEM, keyPEM []byte) error {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return errors.New("cert PEM did not decode")
	}
	if _, err := x509.ParseCertificate(block.Bytes); err != nil {
		return fmt.Errorf("parse cert: %w", err)
	}
	if kBlock, _ := pem.Decode(keyPEM); kBlock == nil {
		return errors.New("key PEM did not decode")
	}
	return nil
}

// readNotAfter parses the first cert in a PEM bundle and returns its
// NotAfter. Zero time on failure — caller treats it as "unknown".
func readNotAfter(certPEM []byte) (time.Time, error) {
	block, _ := pem.Decode(certPEM)
	if block == nil {
		return time.Time{}, errors.New("no PEM block")
	}
	c, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return time.Time{}, err
	}
	return c.NotAfter, nil
}

// sanitizeDomain trims and lowercases so path lookups don't fork on
// case differences.
func sanitizeDomain(d string) string {
	out := make([]byte, 0, len(d))
	for i := 0; i < len(d); i++ {
		c := d[i]
		if c >= 'A' && c <= 'Z' {
			c += 'a' - 'A'
		}
		if c == '.' || c == '-' || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') {
			out = append(out, c)
		}
	}
	return string(out)
}

// writeAtomic mirrors the pattern used elsewhere in the codebase —
// tmpfile + rename so a crash mid-write can't leave a truncated cert.
func writeAtomic(dest string, data []byte, mode os.FileMode) error {
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// Compiler assurance that "crypto" import stays used — leaves room for
// switching account keys to RSA without a plain-key type export.
var _ crypto.Signer = (*ecdsa.PrivateKey)(nil)
