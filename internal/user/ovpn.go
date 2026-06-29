// ovpn.go: bundle a client-common header + ca.crt + per-user client cert
// + per-user client key + ta.key into one inline .ovpn file.
//
// "Inline" means every cert/key block sits between <ca>...</ca> markers
// inside the .ovpn instead of pointing at external paths. That's the
// format every consumer (OpenVPN Connect, OpenVPN for Android, the
// payload-injector apps HEXPLUS targets) expects.

package user

import (
	"bytes"
	"fmt"
	"os"

	"github.com/lolyhexey/hexplus/internal/pki"
)

// OVPNInput is everything BuildOVPN needs to render one .ovpn file.
type OVPNInput struct {
	Username   string
	RemoteHost string
	RemotePort int    // 0 -> 1194
	Proto      string // "" -> udp
}

// BuildOVPN reads the on-disk CA, the per-user client cert + key, and
// ta.key, then concatenates them around the rendered client-common
// header. Returns the bytes the caller writes to disk or stdout.
//
// Errors are tagged with whichever file or step blew up so the CLI
// surfaces "cert missing for <name>" or "PKI not initialized" cleanly.
func BuildOVPN(in OVPNInput) ([]byte, error) {
	header, err := pki.RenderClientCommon(pki.ClientConfigInput{
		RemoteHost: in.RemoteHost,
		RemotePort: in.RemotePort,
		Proto:      in.Proto,
	})
	if err != nil {
		return nil, fmt.Errorf("render client header: %w", err)
	}

	caPEM, err := os.ReadFile(pki.OpenVPNDir + "/ca.crt")
	if err != nil {
		return nil, fmt.Errorf("read ca.crt: %w", err)
	}
	// Find the user's clients folder. Multi-Cert users live under the
	// CA-specific folder; primary-CA users stay at pki.ClientsDir.
	clientsDir := pki.ClientsDir
	if db, err := Load(); err == nil {
		if rec, ok := db.Users[in.Username]; ok && rec.CA != "" {
			clientsDir = pki.ExtraCAClientsDir(rec.CA)
		}
	}
	clientCert, err := os.ReadFile(clientsDir + "/" + in.Username + ".crt")
	if err != nil {
		return nil, fmt.Errorf("read client cert for %s: %w", in.Username, err)
	}
	clientKey, err := os.ReadFile(clientsDir + "/" + in.Username + ".key")
	if err != nil {
		return nil, fmt.Errorf("read client key for %s: %w", in.Username, err)
	}
	taKey, err := os.ReadFile(pki.OpenVPNDir + "/ta.key")
	if err != nil {
		return nil, fmt.Errorf("read ta.key: %w", err)
	}

	var buf bytes.Buffer
	buf.Write(header)
	buf.WriteString("\n")
	wrap(&buf, "ca", caPEM)
	wrap(&buf, "cert", clientCert)
	wrap(&buf, "key", clientKey)
	wrap(&buf, "tls-auth", taKey)
	return buf.Bytes(), nil
}

// wrap appends a <tag>BODY</tag> block. Trailing newline normalization
// keeps the file readable when OpenVPN clients print it for debugging.
func wrap(buf *bytes.Buffer, tag string, body []byte) {
	buf.WriteString("<")
	buf.WriteString(tag)
	buf.WriteString(">\n")
	buf.Write(bytes.TrimRight(body, "\n"))
	buf.WriteString("\n</")
	buf.WriteString(tag)
	buf.WriteString(">\n")
}
