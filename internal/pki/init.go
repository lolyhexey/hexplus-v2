// init.go: orchestrate PKI initialization for OpenVPN.
//
// Lays down the standard /etc/openvpn directory layout HEXPLUS uses:
//
//   /etc/openvpn/
//     server.conf           - OpenVPN server config
//     ca.crt                - CA cert OpenVPN advertises to clients
//     server.crt            - server leaf cert
//     server.key            - server private key (chmod 600)
//     ta.key                - tls-auth HMAC key (chmod 600)
//     pki/
//       ca.crt              - duplicate of ca.crt for the PKI store
//       ca.key              - CA private key (chmod 600); offline-able
//       server.crt
//       server.key
//       ta.key
//       clients/            - per-client certs added by `hexplus user add`
//
// The duplicate top-level files are what server.conf references; the
// pki/ tree is the authoritative store the user-management code reads
// when signing per-user client certs.

package pki

import (
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// OpenVPNDir is the path OpenVPN's config conventionally lives in,
	// and where HEXPLUS writes server-side material.
	OpenVPNDir = "/etc/openvpn"

	// PKIDir is the working PKI store, kept under OpenVPNDir so a
	// directory backup of /etc/openvpn captures both server config and
	// CA material together.
	PKIDir = OpenVPNDir + "/pki"

	// ClientsDir holds per-user issued client certs. `hexplus user add`
	// writes here.
	ClientsDir = PKIDir + "/clients"
)

// InitOptions controls Init. Defaults work for a fresh box; the
// Force flag is the only way to overwrite an existing PKI (we're
// paranoid about clobbering an in-use CA).
type InitOptions struct {
	// CACommonName goes into the CA's Subject CN. Defaults to "HEXPLUS CA".
	CACommonName string
	// ServerCommonName goes into the server cert's Subject CN. Defaults
	// to "server". The TLS handshake doesn't care because we use
	// --remote-cert-tls server (EKU check), not CN matching.
	ServerCommonName string
	// Force overwrites an existing PKI. Dangerous - if any clients are
	// already in the field, regenerating the CA invalidates their certs.
	Force bool
}

// InitResult lists every file Init touched, for the CLI to render.
type InitResult struct {
	Written []string
	Skipped []string
}

// IsInitialized returns true when the four canonical PKI files all
// exist. Cheap pre-check used by `hexplus pki init` to decide whether
// to bail out (without --force) or by the install flow to skip the
// "you need to run pki init" notice.
func IsInitialized() bool {
	for _, p := range []string{
		OpenVPNDir + "/ca.crt",
		OpenVPNDir + "/server.crt",
		OpenVPNDir + "/server.key",
		OpenVPNDir + "/ta.key",
	} {
		if _, err := os.Stat(p); err != nil {
			return false
		}
	}
	return true
}

// Init runs the full first-time PKI bootstrap: CA, server cert, ta.key,
// server.conf. Idempotent unless opts.Force is set.
func Init(opts InitOptions) (InitResult, error) {
	var res InitResult

	if os.Geteuid() != 0 {
		return res, errors.New("pki init requires root; rerun under sudo")
	}
	if opts.CACommonName == "" {
		opts.CACommonName = "HEXPLUS CA"
	}
	if opts.ServerCommonName == "" {
		opts.ServerCommonName = "server"
	}

	if IsInitialized() && !opts.Force {
		return res, errors.New("PKI already initialized at " + OpenVPNDir + "; pass --force to regenerate (invalidates existing client certs)")
	}

	for _, d := range []string{OpenVPNDir, PKIDir, ClientsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return res, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	if err := os.Chmod(PKIDir, 0o700); err != nil {
		return res, fmt.Errorf("chmod %s: %w", PKIDir, err)
	}

	// 1. CA. Slow step on tiny VPSes (RSA-2048 keygen ~1s on a 1-core
	// shared instance, but we only do this once.)
	ca, err := GenerateCA(opts.CACommonName)
	if err != nil {
		return res, err
	}
	if err := writeAll(map[string]filePayload{
		PKIDir + "/ca.crt":     {data: ca.CertPEM, mode: 0o644},
		PKIDir + "/ca.key":     {data: ca.KeyPEM, mode: 0o600},
		OpenVPNDir + "/ca.crt": {data: ca.CertPEM, mode: 0o644},
	}, &res); err != nil {
		return res, err
	}

	// 2. Server cert + key, signed by the CA we just generated.
	srv, err := GenerateServerCert(ca, opts.ServerCommonName)
	if err != nil {
		return res, err
	}
	if err := writeAll(map[string]filePayload{
		PKIDir + "/server.crt":     {data: srv.CertPEM, mode: 0o644},
		PKIDir + "/server.key":     {data: srv.KeyPEM, mode: 0o600},
		OpenVPNDir + "/server.crt": {data: srv.CertPEM, mode: 0o644},
		OpenVPNDir + "/server.key": {data: srv.KeyPEM, mode: 0o600},
	}, &res); err != nil {
		return res, err
	}

	// 3. tls-auth key. Identical bytes go to both locations so server.conf
	// references the top-level path while the PKI store keeps a record.
	taKey, err := GenerateTLSAuth()
	if err != nil {
		return res, err
	}
	if err := writeAll(map[string]filePayload{
		PKIDir + "/ta.key":     {data: taKey, mode: 0o600},
		OpenVPNDir + "/ta.key": {data: taKey, mode: 0o600},
	}, &res); err != nil {
		return res, err
	}

	// 4. server.conf. Always rewritten on init - if the user edited the
	// conf, --force is the contract that says "I know I'm overwriting".
	confBytes := []byte(defaultServerConf)
	if err := writeFile(OpenVPNDir+"/server.conf", confBytes, 0o644, &res); err != nil {
		return res, err
	}

	return res, nil
}

// CertInfo summarizes one stored cert for `hexplus pki status`.
type CertInfo struct {
	Path     string
	Present  bool
	Subject  string
	NotAfter string // YYYY-MM-DD
	Issuer   string
}

// InspectCert reads a PEM cert file and returns a summary. Missing
// files come back with Present=false and the other fields empty.
func InspectCert(path string) (CertInfo, error) {
	info := CertInfo{Path: path}
	raw, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return info, nil
		}
		return info, fmt.Errorf("read %s: %w", path, err)
	}
	blk, _ := pem.Decode(raw)
	if blk == nil {
		return info, fmt.Errorf("%s: no PEM block found", path)
	}
	cert, err := x509.ParseCertificate(blk.Bytes)
	if err != nil {
		return info, fmt.Errorf("%s: %w", path, err)
	}
	info.Present = true
	info.Subject = cert.Subject.String()
	info.Issuer = cert.Issuer.String()
	info.NotAfter = cert.NotAfter.Format("2006-01-02")
	return info, nil
}

// --- helpers below this line ---

type filePayload struct {
	data []byte
	mode os.FileMode
}

func writeAll(files map[string]filePayload, res *InitResult) error {
	// Sorting the map keys gives deterministic written-list ordering for
	// the CLI; not user-visible if we didn't, but easier to test.
	keys := make([]string, 0, len(files))
	for k := range files {
		keys = append(keys, k)
	}
	// Sort lexically for stable output.
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] > keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	for _, dest := range keys {
		fp := files[dest]
		if err := writeFile(dest, fp.data, fp.mode, res); err != nil {
			return err
		}
	}
	return nil
}

func writeFile(dest string, data []byte, mode os.FileMode, res *InitResult) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir parent of %s: %w", dest, err)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	res.Written = append(res.Written, dest)
	return nil
}
