// proxies.go: SOCKS proxy sub-menu (conexao option 4).
//
// UI mirrors v1 conexao fun_socks() exactly:
//   [1] SOCKS SSH   ◉/○  (พอร์ต: XXXX)
//   [2] WEBSOCKET   ◉/○  (พอร์ต: XXXX)
//   [3] SOCKS OPENVPN ◉/○ (พอร์ต: XXXX)
//   [4] เปิดพอร์ต
//   [5] เปลี่ยนสถานะ SOCKS SSH
//   [6] เปลี่ยนสถานะ WEBSOCKET
//   [0] ย้อนกลับ
//
// Selecting 1/2/3 toggles the proxy on/off (install flow when off,
// stop+remove when on). The backend is Go-native systemd units instead
// of v1's python screen sessions, but the operator experience is identical.

package menu

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/lolyhexey/hexplus/internal/progress"
	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
)

// proxySlot describes one of the three fixed SOCKS proxy slots.
type proxySlot struct {
	key         string // DB key: "ssh", "ws", "openvpn"
	label       string // display label: "SOCKS SSH", "WEBSOCKET", "SOCKS OPENVPN"
	defPort     int
	defHost     string
	defCode     string
	defMsg      string
}

var proxySlots = []proxySlot{
	{
		key:     "ssh",
		label:   "SOCKS SSH",
		defPort: 8880,
		defHost: "127.0.0.1:22",
		defCode: "200",
		defMsg:  `Connection established\r\nContent-length: 0`,
	},
	{
		key:     "ws",
		label:   "WEBSOCKET",
		defPort: 8080,
		defHost: "127.0.0.1:22",
		defCode: "101",
		defMsg:  `<font color="null">HEXPLUS</font>`,
	},
	{
		key:     "openvpn",
		label:   "SOCKS OPENVPN",
		defPort: 1194,
		defHost: "",  // filled at runtime from OpenVPN config
		defCode: "101",
		defMsg:  `<font color="null">HEXPLUS</font>`,
	},
}

// colorPicker maps v1's 10-option color menu to HTML color names.
var colorPicker = []struct {
	label string
	value string
}{
	{"สีน้ำเงิน", "blue"},
	{"สีเขียว", "green"},
	{"สีแดง", "red"},
	{"สีเหลือง", "yellow"},
	{"สีชมพู", "#F535AA"},
	{"สีฟ้า", "cyan"},
	{"สีส้ม", "#FF7F00"},
	{"สีม่วง", "#9932CD"},
	{"สีดำ", "black"},
	{"ไม่มีสี", "null"},
}

// proxyPresets for install flow (1-5).
var proxyCodePresets = []struct {
	idx  string
	code string
	msg  string
	desc string
}{
	{"1", "200", `Connection established\r\nContent-length: 0`, "200 HTTP CONNECT proxy + Content-length (แนะนำ — สำหรับ SOCKS SSH)"},
	{"2", "101", `<font color="null">HEXPLUS</font>`, "101 WebSocket spoof handshake"},
	{"3", "400", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`, "400 Bad Request spoof + Content-length"},
	{"4", "520", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`, "520 Cloudflare error spoof + Content-length"},
	{"5", "", "", "กำหนดเอง"},
}

// runProxies is the entry point wired in conexao.go option 4.
func runProxies(r *bufio.Reader) error {
	for {
		db, _ := proxy.Load()
		clearScreen()
		printSep()
		fmt.Println("\033[44;1;37m            จัดการ PROXY SOCKS             \033[0m")
		printSep()
		fmt.Println()
		paintSocksList(db)
		fmt.Println()
		paintSocksMenu()
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0":
			return nil
		case "1":
			proxyToggle(r, db, &proxySlots[0])
		case "2":
			proxyToggle(r, db, &proxySlots[1])
		case "3":
			proxyToggle(r, db, &proxySlots[2])
		case "4":
			proxyOpenPort(r, db)
		case "5":
			proxyChangeStatus(r, db, &proxySlots[0])
		case "6":
			proxyChangeStatus(r, db, &proxySlots[1])
		default:
			fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
			waitEnter(r)
		}
	}
}

// slotEntries returns all DB entries that belong to slot s.
// Matches key == s.key (legacy) or strings.HasPrefix(key, s.key+"-").
func slotEntries(db *proxy.DB, s *proxySlot) []proxy.Config {
	var out []proxy.Config
	for _, cfg := range db.All() {
		if cfg.Name == s.key || strings.HasPrefix(cfg.Name, s.key+"-") {
			out = append(out, cfg)
		}
	}
	return out
}

// paintSocksList prints each slot with all active ports (multi-port aware).
func paintSocksList(db *proxy.DB) {
	for _, s := range proxySlots {
		entries := slotEntries(db, &s)
		if len(entries) == 0 {
			fmt.Printf("  \033[1;31m○\033[0m \033[1;33m%s\033[0m\n", s.label)
			continue
		}
		var ports []string
		for _, e := range entries {
			up, _ := service.ListenStatus(e.Port, "tcp")
			marker := "\033[1;32m◉\033[0m"
			if !up {
				marker = "\033[1;31m○\033[0m"
			}
			ports = append(ports, marker+" \033[1;36m"+strconv.Itoa(e.Port)+"\033[0m")
		}
		fmt.Printf("  \033[1;33m%s\033[0m  %s\n", s.label, strings.Join(ports, "  "))
	}
}

func paintSocksMenu() {
	items := []struct{ idx, label string }{
		{"1", "SOCKS SSH"},
		{"2", "WEBSOCKET"},
		{"3", "SOCKS OPENVPN"},
		{"4", "เปิดพอร์ต"},
		{"5", "เปลี่ยนสถานะ SOCKS SSH"},
		{"6", "เปลี่ยนสถานะ WEBSOCKET"},
	}
	for _, it := range items {
		fmt.Printf("\033[1;31m[\033[1;36m%s\033[1;31m] \033[1;37m• \033[1;33m%s\033[0m\n", it.idx, it.label)
	}
	fmt.Printf("\033[1;31m[\033[1;36m0\033[1;31m] \033[1;37m• \033[1;33mย้อนกลับ\033[0m\n")
	fmt.Println()
	printSep()
	fmt.Print("\033[1;32mเลือกตัวเลือก \033[1;33m?\033[1;37m ")
}

// proxyToggle shows a submenu: add new port OR remove an existing one.
func proxyToggle(r *bufio.Reader, db *proxy.DB, s *proxySlot) {
	entries := slotEntries(db, s)
	clearScreen()
	fmt.Println("\033[44;1;37m             " + s.label + "              \033[0m")
	fmt.Println()

	if len(entries) == 0 {
		// No instances yet — go straight to install.
		if err := proxyInstall(r, db, s); err != nil {
			fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			waitEnter(r)
		}
		return
	}

	// Show existing ports with status.
	for i, e := range entries {
		up, _ := service.ListenStatus(e.Port, "tcp")
		marker := cGrnBold + "◉" + cReset
		if !up {
			marker = cRedBold + "○" + cReset
		}
		fmt.Printf("%s[%s%d%s] %s ลบพอร์ต %s%d%s\n",
			cRedBold, cCyanBold, i+1, cRedBold, marker, cWhtBold, e.Port, cReset)
	}
	addIdx := len(entries) + 1
	fmt.Printf("%s[%s%d%s] %s• %sเพิ่มพอร์ต %s ใหม่%s\n",
		cRedBold, cCyanBold, addIdx, cRedBold, cWhtBold, cYelBold, s.label, cReset)
	fmt.Printf("%s[%s0%s] %s• %sย้อนกลับ%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Println()

	choice, _ := menuPrompt(r)
	n, _ := strconv.Atoi(strings.TrimSpace(choice))

	switch {
	case n == 0:
		return
	case n == addIdx:
		clearScreen()
		fmt.Println("\033[44;1;37m             " + s.label + "              \033[0m")
		fmt.Println()
		if err := proxyInstall(r, db, s); err != nil {
			fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			waitEnter(r)
		}
	case n >= 1 && n <= len(entries):
		proxyRemoveEntry(r, db, entries[n-1])
	}
}

// proxyRemoveEntry stops and removes one proxy instance.
func proxyRemoveEntry(r *bufio.Reader, db *proxy.DB, cfg proxy.Config) {
	clearScreen()
	fmt.Printf("\033[41;1;37m  ลบพอร์ต %d (%s)  \033[0m\n\n", cfg.Port, cfg.Name)

	unitName := cfg.UnitName()
	_ = progress.Run([]progress.Step{
		{Label: fmt.Sprintf("ปิด + ลบ proxy พอร์ต %d", cfg.Port), Work: func() error {
			if err := systemctlRun("disable", "--now", unitName); err != nil {
				return err
			}
			_, _, _, _ = proxy.RemoveUnit(cfg)
			delete(db.Proxies, cfg.Name)
			return db.Save()
		}},
	})
	fmt.Printf("\n"+cGrnBold+"ลบพอร์ต %d สำเร็จแล้ว!"+cReset+"\n", cfg.Port)
	waitEnter(r)
}

// proxyInstall runs the full install flow: port → host → code → msg → start.
func proxyInstall(r *bufio.Reader, db *proxy.DB, s *proxySlot) error {
	portStr, err := promptLine(r, "ต้องการใช้พอร์ตใด ? : ")
	if err != nil {
		return err
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง: %q — พอร์ตต้องเป็นตัวเลข 1-65535", portStr)
	}
	for _, other := range db.All() {
		if other.Port == port && other.Name != s.key {
			return fmt.Errorf("พอร์ต %d ถูกใช้งานโดย proxy %q อยู่แล้ว", port, other.Name)
		}
	}

	defHost := s.defHost
	if s.key == "openvpn" {
		defHost = detectOpenVPNHost()
	}
	hostPrompt := fmt.Sprintf("DEFAULT HOST (host:port ที่จะ tunnel เมื่อ client ไม่ส่ง X-Real-Host) [%s]: ", defHost)
	host, err := promptLine(r, hostPrompt)
	if err != nil {
		return err
	}
	if host == "" {
		host = defHost
	}
	host = sanitizeASCII(host)
	if _, _, err := net.SplitHostPort(host); err != nil {
		return fmt.Errorf("DEFAULT HOST ไม่ถูกต้อง %q — ต้องเป็น host:port เช่น 127.0.0.1:22", host)
	}

	fmt.Println()
	fmt.Println(cGrnBold + "เลือก RESPONSE STATUS CODE:" + cReset)
	for _, p := range proxyCodePresets {
		fmt.Printf("  \033[1;31m[\033[1;36m%s\033[1;31m] \033[1;33m%s\033[0m\n", p.idx, p.desc)
	}
	codeChoice, err := promptLine(r, "เลือก [1-5] (default: 1): ")
	if err != nil {
		return err
	}
	if codeChoice == "" {
		codeChoice = "1"
	}

	var statusCode, defaultMsg string
	switch codeChoice {
	case "1", "2", "3", "4":
		for _, p := range proxyCodePresets {
			if p.idx == codeChoice {
				statusCode = p.code
				defaultMsg = p.msg
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
		defaultMsg = ""
	default:
		return fmt.Errorf("ตัวเลือกไม่ถูกต้อง: %q", codeChoice)
	}

	msgPrompt := fmt.Sprintf(`RESPONSE MSG [%s]: `, defaultMsg)
	msg, err := promptLine(r, msgPrompt)
	if err != nil {
		return err
	}
	if msg == "" {
		msg = defaultMsg
	}

	// Key: "{type}-{port}" so multiple instances of the same type coexist.
	cfg := proxy.Config{
		Name:        s.key + "-" + strconv.Itoa(port),
		Port:        port,
		DefaultHost: host,
		StatusCode:  statusCode,
		StatusMsg:   msg,
	}
	if _, err := proxy.NewHandler(cfg); err != nil {
		return err
	}

	fmt.Println()

	var up bool
	unitName := cfg.UnitName()
	if err := progress.Run([]progress.Step{
		{Label: "บันทึก config + เขียน unit file", Work: func() error {
			db.Proxies[s.key] = cfg
			if err := db.Save(); err != nil {
				return err
			}
			_, _, reloadErr, err := proxy.WriteUnit(cfg)
			if err != nil {
				return err
			}
			if reloadErr != nil {
				fmt.Fprintf(os.Stderr, "\n  คำเตือน: daemon-reload: %v\n", reloadErr)
			}
			return nil
		}},
		{Label: "เริ่ม " + s.label, Work: func() error {
			if err := systemctlRun("enable", "--now", unitName); err != nil {
				return err
			}
			for i := 0; i < 5; i++ {
				time.Sleep(300 * time.Millisecond)
				if ok, _ := service.ListenStatus(port, "tcp"); ok {
					up = true
					break
				}
			}
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println()
	if up {
		fmt.Println(cGrnBold + "เปิดใช้งาน SOCKS สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
			" PROXY SOCKS เริ่มทำงานไม่สำเร็จบนพอร์ต " + strconv.Itoa(port) +
			" — ตรวจสอบ: journalctl -u " + unitName + cReset)
	}
	waitEnter(r)
	return nil
}

// proxyOpenPort (option 4): opens an additional port via the OS firewall.
// Mirrors v1 behaviour: only works when SOCKS SSH is already running.
func proxyOpenPort(r *bufio.Reader, db *proxy.DB) {
	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "เปิดพอร์ต" + cReset)
	printSep()
	fmt.Println()

	cfg, exists := db.Proxies["ssh"]
	if !exists {
		fmt.Println(cRedBold + "ฟังก์ชันไม่พร้อมใช้งาน" + cReset)
		fmt.Println()
		fmt.Println(cYelBold + "กรุณาเปิดใช้งาน SOCKS ก่อน !" + cReset)
		waitEnter(r)
		return
	}
	fmt.Printf(cYelBold+"พอร์ตที่ใช้งานอยู่: "+cGrnBold+"%d\n"+cReset, cfg.Port)
	fmt.Println()

	portStr, err := promptLine(r, "ต้องการเปิดพอร์ตใด ? : ")
	if err != nil {
		return
	}
	port, err := strconv.Atoi(strings.TrimSpace(portStr))
	if err != nil || port < 1 || port > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return
	}

	// Try ufw first, fall back to iptables.
	opened := false
	if path, err := exec.LookPath("ufw"); err == nil {
		cmd := exec.Command(path, "allow", strconv.Itoa(port)+"/tcp")
		if out, err := cmd.CombinedOutput(); err == nil {
			_ = out
			opened = true
		}
	}
	if !opened {
		cmd := exec.Command("iptables", "-I", "INPUT", "-p", "tcp",
			"--dport", strconv.Itoa(port), "-j", "ACCEPT")
		if err := cmd.Run(); err == nil {
			opened = true
		}
	}
	fmt.Println()
	if opened {
		fmt.Println(cGrnBold + "เปิดใช้งาน PROXY SOCKS สำเร็จแล้ว" + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด] เปิดพอร์ตไม่สำเร็จ — ตรวจสอบ ufw/iptables" + cReset)
	}
	waitEnter(r)
}

// proxyChangeStatus (options 5/6): lets the operator set a custom status
// message + font color. When multiple instances exist, asks which one.
func proxyChangeStatus(r *bufio.Reader, db *proxy.DB, s *proxySlot) {
	clearScreen()
	printSep()
	fmt.Println(cWhtBold + "เปลี่ยนสถานะ " + s.label + cReset)
	printSep()
	fmt.Println()

	entries := slotEntries(db, s)
	if len(entries) == 0 {
		fmt.Println(cRedBold + "ฟังก์ชันไม่พร้อมใช้งาน" + cReset)
		fmt.Println()
		fmt.Println(cYelBold + "กรุณาเปิดใช้งาน " + s.label + " ก่อน !" + cReset)
		waitEnter(r)
		return
	}

	var cfg proxy.Config
	if len(entries) == 1 {
		cfg = entries[0]
	} else {
		// Multiple instances — ask which port.
		for i, e := range entries {
			fmt.Printf("%s[%s%d%s] %s• %sพอร์ต %d%s\n",
				cRedBold, cCyanBold, i+1, cRedBold, cWhtBold, cYelBold, e.Port, cReset)
		}
		fmt.Println()
		choice, _ := menuPrompt(r)
		n, _ := strconv.Atoi(strings.TrimSpace(choice))
		if n < 1 || n > len(entries) {
			return
		}
		cfg = entries[n-1]
		fmt.Println()
	}

	fmt.Printf(cYelBold+"สถานะปัจจุบัน: "+cGrnBold+"%s\n\n"+cReset, cfg.StatusMsg)

	text, err := promptLine(r, "ใส่ข้อความสถานะของคุณ : ")
	if err != nil {
		return
	}

	fmt.Println()
	for i, c := range colorPicker {
		fmt.Printf("\033[1;31m[\033[1;36m%02d\033[1;31m]\033[1;33m %s\033[0m\n", i+1, c.label)
	}
	fmt.Println()
	colorChoice, err := promptLine(r, "เลือกสีใด ? : ")
	if err != nil {
		return
	}
	n, _ := strconv.Atoi(strings.TrimSpace(colorChoice))
	color := "null"
	if n >= 1 && n <= len(colorPicker) {
		color = colorPicker[n-1].value
	}

	cfg.StatusMsg = fmt.Sprintf(`<font color="%s">%s</font>`, color, text)
	db.Proxies[s.key] = cfg
	fmt.Println()

	unitName := cfg.UnitName()
	if err := progress.Run([]progress.Step{
		{Label: "บันทึก config + เขียน unit file", Work: func() error {
			if err := db.Save(); err != nil {
				return err
			}
			_, _, _, _ = proxy.WriteUnit(cfg)
			return nil
		}},
		{Label: "รีสตาร์ท " + s.label, Work: func() error {
			return systemctlRun("restart", unitName)
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + err.Error() + cReset)
		waitEnter(r)
		return
	}

	fmt.Println("\n" + cGrnBold + "เปลี่ยนสถานะสำเร็จแล้ว!" + cReset)
	waitEnter(r)
}

// sanitizeASCII strips non-printable and non-ASCII characters from s.
// Prevents garbled terminal IME bytes from corrupting DB / unit files.
func sanitizeASCII(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r <= unicode.MaxASCII && unicode.IsPrint(r) {
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

// detectOpenVPNHost returns the OpenVPN server's listen address for use as
// the SOCKS OPENVPN default host. Falls back to 127.0.0.1:1194.
func detectOpenVPNHost() string {
	for _, path := range []string{"/etc/openvpn/server.conf", "/etc/openvpn/server/server.conf"} {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "port ") {
				p := strings.TrimSpace(strings.TrimPrefix(line, "port "))
				if _, err := strconv.Atoi(p); err == nil {
					return "0.0.0.0:" + p
				}
			}
		}
	}
	return "0.0.0.0:1194"
}

// systemctlRun shells out to systemctl. Kept here (not in service/) to
// avoid an import cycle.
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

// promptLine prints a prompt and returns the trimmed input line.
func promptLine(r *bufio.Reader, prompt string) (string, error) {
	fmt.Print(cGrnBold + prompt + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}
