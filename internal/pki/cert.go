// Package pki produces and signs the certificates HEXPLUS OpenVPN needs.
//
// Replaces easy-rsa entirely: we use crypto/x509 + crypto/rsa + crypto/rand
// directly so the hexplus binary doesn't need bash/openssl/easy-rsa
// present on the target box. The output PEM files are byte-identical in
// shape to what easy-rsa produces and slot straight into the standard
// OpenVPN server.conf ca/cert/key directives.
//
// Key sizes and validity were picked to match modern OpenVPN deployment
// guides without being so aggressive that older client apps (KPN Tunnel,
// HTTP Injector, the official OpenVPN Connect for Android < 3.x) reject
// the certs:
//
//	CA:     RSA-2048, valid 10 years, basicConstraints CA=true.
//	Server: RSA-2048, valid 10 years, EKU = serverAuth, signed by CA.
//	Client: RSA-2048, valid 10 years, EKU = clientAuth, signed by CA.
//
// ECDSA P-256 would produce smaller faster keys but a couple of payload-
// injector apps still ship OpenSSL 1.0 forks that can't parse ECDSA
// in TLS handshake; RSA-2048 is the universal lowest-common-denominator.
package pki

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"math/big"
	"time"
)

const (
	keyBits      = 2048
	caValidity   = 100 * 365 * 24 * time.Hour
	leafValidity = 10 * 365 * 24 * time.Hour
)

// Cert bundles a parsed cert with its private key and the PEM blobs we
// actually persist. Keeping both forms means callers can inspect the
// x509.Certificate (for expiry, subject) without re-parsing.
type Cert struct {
	Cert    *x509.Certificate
	Key     *rsa.PrivateKey
	CertPEM []byte
	KeyPEM  []byte
}

// GenerateCA produces a self-signed CA. validity=0 uses the default 100-year
// constant; pass a positive duration to override (e.g. for the custom-CA TUI
// option). Anything that chains certs off this CA trusts this single key, so
// keep ca.key offline where possible.
func GenerateCA(commonName, org string, validity time.Duration) (*Cert, error) {
	if org == "" {
		org = "lolouch.com"
	}
	if validity <= 0 {
		validity = caValidity
	}
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("rsa GenerateKey: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{org},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(validity),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
		MaxPathLenZero:        false,
		MaxPathLen:            1,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, fmt.Errorf("CreateCertificate (CA): %w", err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("ParseCertificate (CA): %w", err)
	}
	return &Cert{
		Cert:    parsed,
		Key:     key,
		CertPEM: encodeCertPEM(der),
		KeyPEM:  encodeKeyPEM(key),
	}, nil
}

// GenerateServerCert issues a server-auth leaf signed by ca, with
// commonName as the Subject CN. Used for /etc/openvpn/server.crt.
func GenerateServerCert(ca *Cert, commonName string) (*Cert, error) {
	return generateLeaf(ca, commonName, x509.ExtKeyUsageServerAuth)
}

// GenerateClientCert issues a client-auth leaf signed by ca. Used for
// per-user .ovpn exports - one cert per HEXPLUS user.
func GenerateClientCert(ca *Cert, commonName string) (*Cert, error) {
	return generateLeaf(ca, commonName, x509.ExtKeyUsageClientAuth)
}

func generateLeaf(ca *Cert, commonName string, eku x509.ExtKeyUsage) (*Cert, error) {
	key, err := rsa.GenerateKey(rand.Reader, keyBits)
	if err != nil {
		return nil, fmt.Errorf("rsa GenerateKey: %w", err)
	}
	serial, err := newSerial()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	tmpl := &x509.Certificate{
		SerialNumber: serial,
		Subject: pkix.Name{
			CommonName:   commonName,
			Organization: []string{"lolouch.com"},
		},
		NotBefore:             now.Add(-1 * time.Hour),
		NotAfter:              now.Add(leafValidity),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:           []x509.ExtKeyUsage{eku},
		BasicConstraintsValid: true,
		IsCA:                  false,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, ca.Cert, &key.PublicKey, ca.Key)
	if err != nil {
		return nil, fmt.Errorf("CreateCertificate (%s): %w", commonName, err)
	}
	parsed, err := x509.ParseCertificate(der)
	if err != nil {
		return nil, fmt.Errorf("ParseCertificate (%s): %w", commonName, err)
	}
	return &Cert{
		Cert:    parsed,
		Key:     key,
		CertPEM: encodeCertPEM(der),
		KeyPEM:  encodeKeyPEM(key),
	}, nil
}

// newSerial draws a 128-bit positive integer for use as a cert serial.
// RFC 5280 wants serials to be unique within a CA; 128 bits of randomness
// makes collisions astronomically unlikely.
func newSerial() (*big.Int, error) {
	limit := new(big.Int).Lsh(big.NewInt(1), 128)
	n, err := rand.Int(rand.Reader, limit)
	if err != nil {
		return nil, fmt.Errorf("rand serial: %w", err)
	}
	return n, nil
}

func encodeCertPEM(der []byte) []byte {
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func encodeKeyPEM(key *rsa.PrivateKey) []byte {
	// PKCS#1 RSA. OpenVPN parses PKCS#1 directly via OpenSSL's
	// PEM_read_RSAPrivateKey; PKCS#8 also works but PKCS#1 matches
	// what easy-rsa emits, so byte-for-byte we look the same.
	return pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
}
