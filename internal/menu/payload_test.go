package menu

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/lolyhexey/hexplus/internal/pki"
)

func payloadConf(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "server.conf")
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func payloadSource(t *testing.T, port int, proto string) []byte {
	t.Helper()
	hdr, err := pki.RenderClientCommon(pki.ClientConfigInput{RemoteHost: "203.0.113.7", RemotePort: port, Proto: proto})
	if err != nil {
		t.Fatal(err)
	}
	return append(hdr, []byte("\n<ca>\nX\n</ca>\n")...)
}

// Regression: the payload used to write "remote <portal> 1194 udp" whatever
// the server listened on, so a 443/tcp server got a profile that could not
// connect.
func TestPatchPayloadFollowsServerConf(t *testing.T) {
	src := payloadSource(t, 443, "tcp")
	out, proto, port, err := patchPayload(src, "portal.ais.co.th", payloadConf(t, "port 443\nproto tcp\n"))
	if err != nil {
		t.Fatal(err)
	}
	got := string(out)
	if !strings.Contains(got, "remote portal.ais.co.th 443 tcp\n") {
		t.Errorf("remote line not pointed at the server's endpoint:\n%s", got)
	}
	if strings.Contains(got, "1194") || strings.Contains(got, " udp") {
		t.Errorf("legacy endpoint left in the payload:\n%s", got)
	}
	if port != 443 || proto != "tcp" {
		t.Errorf("reported %s/%d, want tcp/443", proto, port)
	}
}

func TestPatchPayloadUDPServer(t *testing.T) {
	src := payloadSource(t, 1194, "udp")
	out, proto, port, err := patchPayload(src, "portal.ais.co.th", payloadConf(t, "port 1194\nproto udp\n"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), "remote portal.ais.co.th 1194 udp\n") || port != 1194 || proto != "udp" {
		t.Errorf("got %s/%d:\n%s", proto, port, out)
	}
}

func TestPatchPayloadUnreadableConf(t *testing.T) {
	out, _, _, err := patchPayload(payloadSource(t, 443, "tcp"), "h", filepath.Join(t.TempDir(), "absent.conf"))
	if err == nil || out != nil {
		t.Fatalf("want an error and no output, got err=%v out=%q", err, out)
	}
}

func TestPatchPayloadNoRemoteLine(t *testing.T) {
	out, _, _, err := patchPayload([]byte(";remote old 1194 udp\nclient\n"), "h", payloadConf(t, "port 443\nproto tcp\n"))
	if err != nil {
		t.Fatal(err)
	}
	if out != nil {
		t.Errorf("a source with only a commented remote must report no match, got %q", out)
	}
}

func TestRewriteRemoteFirstOnly(t *testing.T) {
	src := []byte("client\nremote a 1194 udp\nremote b 1194\nresolv-retry infinite\n")
	out, ok := rewriteRemote(src, "h", 443, "tcp")
	if !ok {
		t.Fatal("no match")
	}
	want := "client\nremote h 443 tcp\nremote b 1194\nresolv-retry infinite\n"
	if string(out) != want {
		t.Errorf("got %q, want %q", out, want)
	}
}
