// payload.go: the "ปรับแต่ง Payload" flow inside the OPENVPN service
// sub-menu.
//
// What this does: regenerates one existing user's .ovpn under a new
// `remote <portal-host> <port> <proto>` line (the port and proto the
// server really listens on, read from server.conf) so an injector app (HTTP
// Injector, KPN Tunnel, eProxy) sees the configured carrier portal as
// the OpenVPN endpoint. The certs and crypto bundle are untouched - we
// only rewrite the `remote` line. The output goes to
// /root/<name>-<PRESET>.ovpn so the original /root/<name>.ovpn stays
// intact for clients that don't need a payload.
//
// v1 did this with a sed inside Plus/payload; here we do it natively
// so injection is one menu pick and doesn't depend on a working sed
// binary on the box.

package menu

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/user"
)

// payloadPreset is one carrier preset the operator can pick. Names go
// into the output filename suffix so the operator can keep multiple
// payloads per user side by side.
type payloadPreset struct {
	idx        string
	name       string
	remoteHost string
	label      string
}

// payloadPresets ship the three Thai carriers HEXPLUS v1 customers
// asked for most often. "กำหนดเอง" sits below as option 4.
var payloadPresets = []payloadPreset{
	{"1", "AIS", "portal.ais.co.th", "AIS — portal.ais.co.th"},
	{"2", "TRUE", "portal.truemove.com", "TRUE — portal.truemove.com"},
	{"3", "DTAC", "portal.dtac.co.th", "DTAC — portal.dtac.co.th"},
}

// remoteLineRE matches the `remote <host> <port> <proto>` line we
// rewrite. Anchored to the start of a line so commented-out `remote`
// lines (some operators ship a list with `;` prefixes) stay untouched.
var remoteLineRE = regexp.MustCompile(`(?m)^remote\s+\S+\s+\d+(\s+\S+)?\s*$`)

// runPayload is OpenVPN menu option [12].
func runPayload(r *bufio.Reader) error {
	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "ปรับแต่ง Payload สำหรับ OpenVPN" + cReset)
	printSep()
	fmt.Println()

	users, err := user.List()
	if err != nil {
		return fmt.Errorf("โหลดผู้ใช้: %w", err)
	}
	if len(users) == 0 {
		fmt.Println(cYelBold + "ยังไม่มีผู้ใช้ — สร้างผู้ใช้ก่อน" + cReset)
		waitEnter(r)
		return nil
	}

	for i, u := range users {
		fmt.Printf("  %s[%s%2d%s] %s%s%s\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cYelBold, u.Name, cReset)
	}
	fmt.Println()

	pick, err := promptLine(r, "เลือกผู้ใช้ (0 = ยกเลิก): ")
	if err != nil {
		return err
	}
	if pick == "0" || pick == "" {
		return nil
	}
	n, err := strconv.Atoi(pick)
	if err != nil || n < 1 || n > len(users) {
		return fmt.Errorf("หมายเลขผู้ใช้ไม่ถูกต้อง: %q", pick)
	}
	username := users[n-1].Name

	fmt.Println()
	fmt.Println(cWhtBold + "เลือก Preset Payload:" + cReset)
	for _, p := range payloadPresets {
		fmt.Printf("  %s[%s%s%s] %s%s%s\n",
			cRedBold, cCyanBold, p.idx, cRedBold,
			cYelBold, p.label, cReset)
	}
	fmt.Printf("  %s[%s4%s] %sกำหนดเอง%s\n",
		cRedBold, cCyanBold, cRedBold, cYelBold, cReset)
	fmt.Printf("  %s[%s0%s] %sยกเลิก%s\n",
		cRedBold, cCyanBold, cRedBold, cYelBold, cReset)

	choice, err := promptLine(r, "เลือก: ")
	if err != nil {
		return err
	}
	if choice == "0" || choice == "" {
		return nil
	}

	var presetName, remoteHost string
	switch choice {
	case "1", "2", "3":
		for _, p := range payloadPresets {
			if p.idx == choice {
				presetName = p.name
				remoteHost = p.remoteHost
			}
		}
	case "4":
		remoteHost, err = promptLine(r, "Remote host (เช่น portal.example.com): ")
		if err != nil {
			return err
		}
		if remoteHost == "" {
			return errors.New("remote host ห้ามว่าง")
		}
		presetName, err = promptLine(r, "ชื่อ Preset (สำหรับชื่อไฟล์, ตัวอักษรอังกฤษ): ")
		if err != nil {
			return err
		}
		if presetName == "" {
			presetName = "CUSTOM"
		}
		presetName = sanitizePresetName(presetName)
	default:
		return fmt.Errorf("ตัวเลือกไม่ถูกต้อง: %q", choice)
	}

	// Source .ovpn is what `user add` wrote at /root/<name>.ovpn.
	srcPath := "/root/" + username + ".ovpn"
	src, err := os.ReadFile(srcPath)
	if err != nil {
		return fmt.Errorf("อ่าน %s: %w", srcPath, err)
	}

	// The default is OpenVPN's own endpoint; a client that comes in
	// through SSLH or SSL TUNNEL needs that service's port instead.
	defProto, defPort, err := pki.ReadServerListen(pki.ServerConfPath)
	if err != nil {
		return fmt.Errorf("อ่าน %s: %w", pki.ServerConfPath, err)
	}
	fmt.Println(cWhtBold + "ถ้าลูกค้าเข้าทาง SSLH หรือ SSL TUNNEL ให้ใส่พอร์ตของบริการนั้น และโปรโตคอล tcp" + cReset)
	portIn, err := promptLineDefault(r, "พอร์ต", strconv.Itoa(defPort))
	if err != nil {
		return err
	}
	wantPort, err := strconv.Atoi(strings.TrimSpace(portIn))
	if err != nil {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง: %q", portIn)
	}
	protoIn, err := promptLineDefault(r, "โปรโตคอล (tcp/udp)", defProto)
	if err != nil {
		return err
	}

	patched, proto, port, err := patchPayload(src, remoteHost, pki.ServerConfPath, wantPort, strings.ToLower(strings.TrimSpace(protoIn)))
	if err != nil {
		return err
	}
	if patched == nil {
		return fmt.Errorf("ไม่พบบรรทัด 'remote ...' ใน %s", srcPath)
	}

	dstPath := "/root/" + username + "-" + presetName + ".ovpn"
	if err := os.WriteFile(dstPath, patched, 0o600); err != nil {
		return fmt.Errorf("เขียน %s: %w", dstPath, err)
	}

	fmt.Println()
	fmt.Println(cGrnBold + "บันทึก " + cWhtBold + dstPath + cGrnBold + " สำเร็จ" + cReset)
	fmt.Println(cWhtBold + "  remote: " + cYelBold + remoteHost + fmt.Sprintf(" %d %s", port, proto) + cReset)
	waitEnter(r)
	return nil
}

// patchPayload points src's `remote` line at host with the port and proto
// the server config at confPath listens on, unless the operator gave others
// (wantPort > 0, wantProto != ""): clients that come in through SSLH or SSL
// TUNNEL use that service's port, over tcp. It returns nil bytes when src
// has no `remote` line, and an error (before anything is written) when the
// server config cannot be read or an override is invalid.
func patchPayload(src []byte, host, confPath string, wantPort int, wantProto string) (patched []byte, proto string, port int, err error) {
	proto, port, err = pki.ReadServerListen(confPath)
	if err != nil {
		return nil, "", 0, fmt.Errorf("อ่าน %s: %w", confPath, err)
	}
	if wantPort != 0 {
		if wantPort < 1 || wantPort > 65535 {
			return nil, "", 0, fmt.Errorf("พอร์ตไม่ถูกต้อง: %d", wantPort)
		}
		port = wantPort
	}
	if wantProto != "" {
		if wantProto != "tcp" && wantProto != "udp" {
			return nil, "", 0, fmt.Errorf("โปรโตคอลต้องเป็น tcp หรือ udp: %q", wantProto)
		}
		proto = wantProto
	}
	out, replaced := rewriteRemote(src, host, port, proto)
	if !replaced {
		return nil, proto, port, nil
	}
	return out, proto, port, nil
}

// rewriteRemote replaces the first non-comment `remote <host> <port>
// [<proto>]` line with `remote host port proto`. Returns (patched,
// replaced); replaced=false means the source had no matching line.
func rewriteRemote(src []byte, host string, port int, proto string) ([]byte, bool) {
	replacement := fmt.Sprintf("remote %s %d %s", host, port, proto)
	replaced := false
	out := remoteLineRE.ReplaceAllStringFunc(string(src), func(m string) string {
		if replaced {
			return m
		}
		replaced = true
		return replacement
	})
	return []byte(out), replaced
}

// sanitizePresetName keeps only [A-Za-z0-9_-] for the filename suffix
// so the output path can't be turned into a directory traversal or
// shell-special string by a typo at the prompt.
func sanitizePresetName(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'A' && r <= 'Z', r >= 'a' && r <= 'z', r >= '0' && r <= '9', r == '_', r == '-':
			b.WriteRune(r)
		}
	}
	if b.Len() == 0 {
		return "CUSTOM"
	}
	return strings.ToUpper(b.String())
}
