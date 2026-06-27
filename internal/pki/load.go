// load.go: read the CA back from disk so we can sign per-user client certs
// after `hexplus pki init` has been run.
//
// We keep the on-disk CA layout the source of truth: the CA cert lives at
// /etc/openvpn/pki/ca.crt and its private key at /etc/openvpn/pki/ca.key.
// This file just parses the PEM and hands back a *Cert that mirrors the
// one GenerateCA returns, so callers don't care whether the CA came from
// fresh generation or a load.

package pki

import (
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
)

// LoadCA parses /etc/openvpn/pki/ca.crt and /etc/openvpn/pki/ca.key into
// a *Cert ready to sign leaf certs. Returns a clear error when PKI hasn't
// been initialized so the CLI can tell the user to run `hexplus pki init`.
func LoadCA() (*Cert, error) {
	certPath := PKIDir + "/ca.crt"
	keyPath := PKIDir + "/ca.key"

	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("CA cert missing at %s; run 'hexplus pki init' first", certPath)
		}
		return nil, fmt.Errorf("read %s: %w", certPath, err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("CA key missing at %s; run 'hexplus pki init' first", keyPath)
		}
		return nil, fmt.Errorf("read %s: %w", keyPath, err)
	}

	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", certPath, err)
	}
	key, err := parseRSAKeyPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", keyPath, err)
	}
	return &Cert{
		Cert:    cert,
		Key:     key,
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}, nil
}

// LoadCAFromPEM parses a CA cert and key from raw PEM bytes (e.g. from
// embedded assets) without reading from disk. Mirrors LoadCA's return type.
func LoadCAFromPEM(certPEM, keyPEM []byte) (*Cert, error) {
	cert, err := parseCertPEM(certPEM)
	if err != nil {
		return nil, fmt.Errorf("CA cert PEM: %w", err)
	}
	key, err := parseRSAKeyPEM(keyPEM)
	if err != nil {
		return nil, fmt.Errorf("CA key PEM: %w", err)
	}
	return &Cert{
		Cert:    cert,
		Key:     key,
		CertPEM: certPEM,
		KeyPEM:  keyPEM,
	}, nil
}

// ReadFile is a public-ish helper so the user package can fetch ta.key
// (which is opaque bytes, not PEM) without duplicating error wrapping.
func ReadFile(path string) ([]byte, error) {
	return os.ReadFile(path)
}

func parseCertPEM(data []byte) (*x509.Certificate, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	return x509.ParseCertificate(block.Bytes)
}

func parseRSAKeyPEM(data []byte) (*rsa.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, errors.New("no PEM block found")
	}
	// Try PKCS#1 first because that's what GenerateCA writes; fall back
	// to PKCS#8 for keys loaded from other tooling (easy-rsa 3.x ships
	// PKCS#8 by default).
	if key, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
		return key, nil
	}
	parsed, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse RSA key: %w", err)
	}
	rsaKey, ok := parsed.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("PKCS#8 key is not RSA (got %T)", parsed)
	}
	return rsaKey, nil
}
