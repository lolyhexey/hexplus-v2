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
	"strings"

	"github.com/lolyhexey/hexplus/internal/pki"
)

// OVPNInput is everything BuildOVPN needs to render one .ovpn file.
type OVPNInput struct {
	Username   string
	RemoteHost string
	RemotePort int    // 0 -> the port in server.conf (see ResolveEndpoint)
	Proto      string // "" -> the proto in server.conf (see ResolveEndpoint)
}

// ResolveEndpoint fills the port and proto the client file will dial. A
// value the caller set is kept (validated); an unset one is read from the
// OpenVPN server config at confPath, so the file matches what the server
// really listens on instead of a fixed 1194/udp. If a value is unset and
// the config is unreadable it fails, because guessing is how a profile that
// cannot connect gets handed out.
func ResolveEndpoint(in OVPNInput, confPath string) (OVPNInput, error) {
	in.Proto = strings.ToLower(in.Proto)
	if in.Proto != "" && in.Proto != "udp" && in.Proto != "tcp" {
		return in, fmt.Errorf("invalid proto %q (want udp or tcp)", in.Proto)
	}
	if in.RemotePort < 0 || in.RemotePort > 65535 {
		return in, fmt.Errorf("invalid remote port %d (want 1-65535)", in.RemotePort)
	}
	if in.RemotePort != 0 && in.Proto != "" {
		return in, nil
	}
	proto, port, err := pki.ReadServerListen(confPath)
	if err != nil {
		return in, fmt.Errorf("cannot read the server's port/proto (%w); pass --remote-port and --proto", err)
	}
	if in.RemotePort == 0 {
		in.RemotePort = port
	}
	if in.Proto == "" {
		in.Proto = proto
	}
	return in, nil
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
	clientCert, err := os.ReadFile(pki.ClientsDir + "/" + in.Username + ".crt")
	if err != nil {
		return nil, fmt.Errorf("read client cert for %s: %w", in.Username, err)
	}
	clientKey, err := os.ReadFile(pki.ClientsDir + "/" + in.Username + ".key")
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
