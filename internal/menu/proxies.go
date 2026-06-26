// proxies.go: the SOCKS-proxy management sub-menu (option 10.4).
//
// v1 ran three separate Python proxies (proxy.py, wsproxy.py, open.py)
// under screen sessions and the conexao menu let the operator turn each
// on/off and tweak the spoof status line. v2 collapses all three into
// the Go-native internal/proxy package - one DB row per proxy, one
// systemd unit each. This file is the Thai TUI on top of that backend.
//
// We deliberately mirror the structure of serviceMenu(): a top-level
// listing of what's configured plus a small numeric action grid. The
// operator's muscle memory ("01 = add, 02 = remove, 00 = back") matches
// the rest of the conexao flow.

package menu

import (
	"bufio"
	"fmt"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
)

// proxyPresets mirrors the cmd/hexplus presets so the menu offers the
// same WebSocket/CONNECT response shapes v1 customers expect. Kept in
// this file rather than reaching into cmd/hexplus so the menu package
// has no upward dependency.
var proxyPresets = []struct {
	idx  string
	code string
	msg  string
	desc string
}{
	{"1", "101", `<font color="null">HEXPLUS</font>`, "101 Switching Protocols (WebSocket spoof)"},
	{"2", "200", `Connection established\r\nContent-length: 0`, "200 Connection established (CONNECT spoof)"},
	{"3", "400", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`, "400 Bad Request (HEXPLUS marker)"},
	{"4", "520", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`, "520 Unknown Error (HEXPLUS marker)"},
}

// runProxies is the entry point conexao.go wires option 4 to. Loops
// until the user picks 0 (back).
func runProxies(r *bufio.Reader) error {
	for {
		clearScreen()
		paintProxiesHeader()
		if err := paintProxiesList(); err != nil {
			fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		}
		paintProxiesMenu()
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1", "01":
			if err := proxyAdd(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			}
			waitEnter(r)
		case "2", "02":
			if err := proxyRemove(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			}
			waitEnter(r)
		case "3", "03":
			if err := proxyDetail(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			}
			waitEnter(r)
		default:
			fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
			waitEnter(r)
		}
	}
}

func paintProxiesHeader() {
	printSep()
	fmt.Println("\033[44;1;37m            จัดการ SOCKS Proxy            \033[0m")
	printSep()
	fmt.Println()
}

// paintProxiesList prints the configured proxies with a ●/○ listen
// marker so the operator can see at a glance which are up.
func paintProxiesList() error {
	db, err := proxy.Load()
	if err != nil {
		return err
	}
	all := db.All()
	if len(all) == 0 {
		fmt.Println(cYelBold + "(ยังไม่มี Proxy ที่กำหนด)" + cReset)
		fmt.Println()
		return nil
	}
	for _, c := range all {
		marker := markerOff()
		if up, _ := service.ListenStatus(c.Port, "tcp"); up {
			marker = markerOn()
		}
		fmt.Printf("%s%s%-12s%s  %sพอร์ต %s%-5d%s  %sdefault→%s%-20s%s  %sCODE %s%s%s\n",
			marker, cWhtBold, c.Name, cReset,
			cGrnBold, cWhtBold, c.Port, cReset,
			cGrnBold, cWhtBold, c.DefaultHost, cReset,
			cGrnBold, cWhtBold, c.StatusCode, cReset)
	}
	fmt.Println()
	return nil
}

func paintProxiesMenu() {
	items := []struct {
		idx, label string
	}{
		{"01", "เพิ่ม Proxy ใหม่"},
		{"02", "ลบ Proxy"},
		{"03", "รายละเอียด Proxy"},
	}
	for _, it := range items {
		fmt.Printf("%s[%s%s%s] %s• %s%s%s\n",
			cRedBold, cCyanBold, it.idx, cRedBold,
			cWhtBold, cYelBold, it.label, cReset)
	}
	fmt.Printf("%s[%s00%s] %s• %sย้อนกลับ%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Println()
	printSep()
	fmt.Println()
}

// proxyAdd prompts for the fields hexplus proxy add accepts on the
// CLI, validates by spinning up a Handler, then persists + writes the
// systemd unit.
func proxyAdd(r *bufio.Reader) error {
	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "เพิ่ม Proxy ใหม่" + cReset)
	printSep()
	fmt.Println()

	name, err := promptLine(r, "ชื่อ Proxy (a-z, 2-32): ")
	if err != nil {
		return err
	}
	if err := proxy.ValidateName(name); err != nil {
		return err
	}

	db, err := proxy.Load()
	if err != nil {
		return err
	}
	if _, exists := db.Proxies[name]; exists {
		return fmt.Errorf("proxy %q มีอยู่แล้ว", name)
	}

	portStr, err := promptLine(r, "พอร์ต (1-65535): ")
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง: %q", portStr)
	}

	target, err := promptLine(r, "Target host:port [127.0.0.1:22]: ")
	if err != nil {
		return err
	}
	if target == "" {
		target = "127.0.0.1:22"
	}

	fmt.Println()
	fmt.Println(cWhtBold + "เลือก Preset ตอบกลับ:" + cReset)
	for _, p := range proxyPresets {
		fmt.Printf("  %s[%s%s%s] %s%s%s\n",
			cRedBold, cCyanBold, p.idx, cRedBold,
			cYelBold, p.desc, cReset)
	}
	fmt.Printf("  %s[%s5%s] %sกำหนดเอง%s\n",
		cRedBold, cCyanBold, cRedBold, cYelBold, cReset)
	choice, err := promptLine(r, "เลือก [1]: ")
	if err != nil {
		return err
	}
	if choice == "" {
		choice = "1"
	}
	var statusCode, statusMsg string
	switch choice {
	case "1", "2", "3", "4":
		for _, p := range proxyPresets {
			if p.idx == choice {
				statusCode = p.code
				statusMsg = p.msg
			}
		}
	case "5":
		statusCode, err = promptLine(r, "Status code (เช่น 200): ")
		if err != nil {
			return err
		}
		if statusCode == "" {
			return fmt.Errorf("status code ห้ามว่าง")
		}
		statusMsg, err = promptLine(r, `Status message (ใช้ \r\n สำหรับขึ้นบรรทัดใหม่): `)
		if err != nil {
			return err
		}
	default:
		return fmt.Errorf("ตัวเลือก preset ไม่ถูกต้อง: %q", choice)
	}

	cfg := proxy.Config{
		Name:        name,
		Port:        port,
		DefaultHost: target,
		StatusCode:  statusCode,
		StatusMsg:   statusMsg,
	}
	// Validate the full config the same way the CLI does so a bad
	// status code is caught before any file is written.
	if _, err := proxy.NewHandler(cfg); err != nil {
		return err
	}

	db.Proxies[name] = cfg
	if err := db.Save(); err != nil {
		return err
	}
	unitPath, written, reloadErr, err := proxy.WriteUnit(cfg)
	if err != nil {
		return err
	}
	if reloadErr != nil {
		fmt.Println(cYelBold + "  คำเตือน: systemctl daemon-reload: " + reloadErr.Error() + cReset)
	}

	// v1 conexao (lines 1564-1579) starts the SOCKS proxy immediately and
	// verifies via netstat. Replicate that with systemctl enable --now +
	// /proc/net/tcp poll so the operator sees the same "เปิดใช้งาน SOCKS
	// สำเร็จแล้ว" success / red-error wording as in v1.
	unitName := cfg.UnitName()
	if enableErr := systemctlRun("enable", "--now", unitName); enableErr != nil {
		fmt.Println()
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
			" PROXY SOCKS เริ่มทำงานไม่สำเร็จ: systemctl enable --now " +
			unitName + ": " + enableErr.Error() + cReset)
		_ = written // keep ref so go vet doesn't complain
		_ = unitPath
		return nil
	}
	// systemd 'started' the service but the listener doesn't bind
	// instantly on slower boxes; sleep briefly then check.
	time.Sleep(700 * time.Millisecond)
	listening, _ := service.ListenStatus(cfg.Port, "tcp")
	fmt.Println()
	if listening {
		fmt.Println(cGrnBold + "เปิดใช้งาน SOCKS สำเร็จแล้ว" + cReset)
		fmt.Printf("%sพอร์ต %s%d%s กำลังฟัง%s\n", cGrnBold, cCyanBold, cfg.Port, cGrnBold, cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
			" PROXY SOCKS เริ่มทำงานไม่สำเร็จบนพอร์ต " +
			strconv.Itoa(cfg.Port) +
			": ไม่พบ socket ใน LISTEN — ตรวจสอบ journalctl -u " + unitName + cReset)
	}
	return nil
}

// systemctlRun is a thin shell-out for systemctl <verb> <args...>. Lives
// here rather than in service/ because we want to avoid an import cycle
// (service already imports menu? no — it does not — but we keep this
// local to keep the proxies code self-contained). Errors include the
// combined stderr so the menu surfaces useful diagnostics.
func systemctlRun(args ...string) error {
	cmd := exec.Command("systemctl", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		return fmt.Errorf("%w: %s", err, msg)
	}
	return nil
}

// proxyRemove lists the configured proxies, lets the operator pick one
// by index, deletes the DB row and removes the systemd unit.
func proxyRemove(r *bufio.Reader) error {
	db, err := proxy.Load()
	if err != nil {
		return err
	}
	all := db.All()
	if len(all) == 0 {
		return fmt.Errorf("ยังไม่มี Proxy ให้ลบ")
	}

	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "ลบ Proxy" + cReset)
	printSep()
	fmt.Println()
	for i, c := range all {
		fmt.Printf("  %s[%s%2d%s] %s%-12s%s  พอร์ต %d\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cYelBold, c.Name, cReset, c.Port)
	}
	fmt.Println()

	pick, err := promptLine(r, "เลือกหมายเลข Proxy (0 = ยกเลิก): ")
	if err != nil {
		return err
	}
	if pick == "0" || pick == "" {
		return nil
	}
	n, err := strconv.Atoi(pick)
	if err != nil || n < 1 || n > len(all) {
		return fmt.Errorf("หมายเลขไม่ถูกต้อง: %q", pick)
	}
	cfg := all[n-1]

	delete(db.Proxies, cfg.Name)
	if err := db.Save(); err != nil {
		return err
	}
	unitPath, removed, reloadErr, err := proxy.RemoveUnit(cfg)
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Println(cGrnBold + "ลบ Proxy " + cWhtBold + cfg.Name + cGrnBold + " เรียบร้อย" + cReset)
	if removed {
		fmt.Println(cGrnBold + "  - " + cWhtBold + unitPath + cReset)
	} else {
		fmt.Println(cYelBold + "  (ไม่พบ unit file: " + unitPath + ")" + cReset)
	}
	if reloadErr != nil {
		fmt.Println(cYelBold + "  คำเตือน: systemctl daemon-reload: " + reloadErr.Error() + cReset)
	}
	return nil
}

// proxyDetail dumps the full Config of one selected proxy plus a live
// "is it listening?" probe of /proc/net.
func proxyDetail(r *bufio.Reader) error {
	db, err := proxy.Load()
	if err != nil {
		return err
	}
	all := db.All()
	if len(all) == 0 {
		return fmt.Errorf("ยังไม่มี Proxy ที่กำหนด")
	}

	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "รายละเอียด Proxy" + cReset)
	printSep()
	fmt.Println()
	for i, c := range all {
		fmt.Printf("  %s[%s%2d%s] %s%-12s%s  พอร์ต %d\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cYelBold, c.Name, cReset, c.Port)
	}
	fmt.Println()

	pick, err := promptLine(r, "เลือกหมายเลข Proxy (0 = ยกเลิก): ")
	if err != nil {
		return err
	}
	if pick == "0" || pick == "" {
		return nil
	}
	n, err := strconv.Atoi(pick)
	if err != nil || n < 1 || n > len(all) {
		return fmt.Errorf("หมายเลขไม่ถูกต้อง: %q", pick)
	}
	c := all[n-1]

	fmt.Println()
	fmt.Println(cWhtBold + "ชื่อ:         " + cYelBold + c.Name + cReset)
	fmt.Println(cWhtBold + "พอร์ต:        " + cYelBold + strconv.Itoa(c.Port) + cReset)
	fmt.Println(cWhtBold + "Default host: " + cYelBold + c.DefaultHost + cReset)
	fmt.Println(cWhtBold + "Status code:  " + cYelBold + c.StatusCode + cReset)
	fmt.Println(cWhtBold + "Status msg:   " + cYelBold + c.StatusMsg + cReset)
	if len(c.AllowedHosts) > 0 {
		fmt.Println(cWhtBold + "Allowed:      " + cYelBold + strings.Join(c.AllowedHosts, ", ") + cReset)
	}
	fmt.Println(cWhtBold + "Unit:         " + cYelBold + c.UnitName() + cReset)

	if up, err := service.ListenStatus(c.Port, "tcp"); err == nil {
		if up {
			fmt.Println(cWhtBold + "สถานะพอร์ต:   " + cGrnBold + "ทำงาน (LISTEN)" + cReset)
		} else {
			fmt.Println(cWhtBold + "สถานะพอร์ต:   " + cRedBold + "ไม่ได้ฟัง" + cReset)
		}
	}
	return nil
}

// promptLine prints prompt, reads one line, returns trimmed text. Small
// helper around bufio.Reader so each prompt site stays one line.
func promptLine(r *bufio.Reader, prompt string) (string, error) {
	fmt.Print(cGrnBold + prompt + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
