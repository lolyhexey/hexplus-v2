// clientconf.go: the client-side OpenVPN directives that go at the top of
// every per-user .ovpn export. Mirrors v1 conexao's client-common.txt.

package pki

import (
	"bytes"
	"fmt"
	"text/template"
)

// ClientConfigInput drives the client header rendering.
type ClientConfigInput struct {
	// RemoteHost is the server's public address (IP or hostname).
	// Required - empty string fails template execution.
	RemoteHost string

	// RemotePort defaults to 1194 if zero.
	RemotePort int

	// Proto is "udp" or "tcp" depending on what the server is listening
	// for. Empty defaults to "udp".
	Proto string
}

// RenderClientCommon emits the header portion of a .ovpn file —
// the directives shared by every client, before the inline <ca>...</ca>
// etc. blocks. The user package concatenates this with the embedded
// PEM blobs to produce the final file.
func RenderClientCommon(in ClientConfigInput) ([]byte, error) {
	if in.RemoteHost == "" {
		return nil, fmt.Errorf("RemoteHost is required")
	}
	if in.RemotePort == 0 {
		in.RemotePort = 1194
	}
	if in.Proto == "" {
		in.Proto = "udp"
	}

	t, err := template.New("client").Parse(clientCommonTmpl)
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.Execute(&buf, in); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// clientCommonTmpl mirrors v1 conexao's client-common.txt exactly.
const clientCommonTmpl = `client
dev tun
proto {{.Proto}}
sndbuf 0
rcvbuf 0
remote {{.RemoteHost}} {{.RemotePort}}
resolv-retry infinite
nobind
persist-key
persist-tun
remote-cert-tls server
tls-version-min 1.2
data-ciphers AES-256-GCM:AES-128-GCM:AES-256-CBC
data-ciphers-fallback AES-256-CBC
cipher AES-256-CBC
auth SHA256
setenv opt block-outside-dns
key-direction 1
verb 3
auth-user-pass
keepalive 10 120
float
`
