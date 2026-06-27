// embedded.go: install the pre-generated CA bundle from git instead of
// generating a fresh one. Used by the "use CA from git" option in the
// OpenVPN install TUI so multiple VPS instances share the same CA and
// one client .ovpn works on any of them.

package pki

import (
	"errors"
	"fmt"
	"os"
)

// InstallWithCA writes an externally-supplied CA cert/key and ta.key to disk,
// then generates a fresh server cert signed by that CA. Equivalent to Init
// but the CA comes from the caller (embedded assets) instead of being
// generated here.
//
// serverCN is the server certificate's Subject CN (shown as
// "[serverCN] Peer Connection Completed" in OpenVPN client logs).
// If empty, defaults to "KSMLB by LO LY".
func InstallWithCA(caCertPEM, caKeyPEM, taKeyPEM []byte, serverCN string, force bool) (InitResult, error) {
	var res InitResult

	if os.Geteuid() != 0 {
		return res, errors.New("pki install requires root; rerun under sudo")
	}
	if serverCN == "" {
		serverCN = "KSMLB by LO LY"
	}
	if IsInitialized() && !force {
		return res, errors.New("PKI already initialized; pass force=true to overwrite")
	}

	for _, d := range []string{OpenVPNDir, PKIDir, ClientsDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return res, fmt.Errorf("mkdir %s: %w", d, err)
		}
	}
	if err := os.Chmod(PKIDir, 0o700); err != nil {
		return res, fmt.Errorf("chmod %s: %w", PKIDir, err)
	}

	// Load CA from provided PEM bytes.
	ca, err := LoadCAFromPEM(caCertPEM, caKeyPEM)
	if err != nil {
		return res, fmt.Errorf("load embedded CA: %w", err)
	}

	// Write CA files.
	if err := writeAll(map[string]filePayload{
		PKIDir + "/ca.crt":     {data: caCertPEM, mode: 0o644},
		PKIDir + "/ca.key":     {data: caKeyPEM, mode: 0o600},
		OpenVPNDir + "/ca.crt": {data: caCertPEM, mode: 0o644},
	}, &res); err != nil {
		return res, err
	}

	// Generate server cert signed by embedded CA.
	srv, err := GenerateServerCert(ca, serverCN)
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

	// Write ta.key.
	if err := writeAll(map[string]filePayload{
		PKIDir + "/ta.key":     {data: taKeyPEM, mode: 0o600},
		OpenVPNDir + "/ta.key": {data: taKeyPEM, mode: 0o600},
	}, &res); err != nil {
		return res, err
	}

	return res, nil
}
