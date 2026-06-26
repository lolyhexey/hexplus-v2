// conexao.go: the "โหมดฟังชั่น" sub-menu (option 10 from the main grid).
//
// In v1 Modulos/conexao manages the install/uninstall + port-config of
// Squid, OpenVPN, Dropbear, and the SOCKS proxies. v2 splits that into:
//   - per-service install/uninstall (extract + write unit + bootstrap)
//   - per-service start/stop/enable/disable
//   - SOCKS proxy add/list/remove (which were always in-binary)
//
// We keep the same numeric grid layout the v1 conexao paints so screen
// shots from the old menu match the new one.

package menu

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"

	"github.com/lolyhexey/hexplus/internal/service"
)

// runConexao paints the sub-menu and dispatches. Loops until the user
// picks 0 (back).
func runConexao(r *bufio.Reader) error {
	for {
		clearScreen()
		paintConexaoHeader()
		paintConexaoMenu()
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1":
			if err := serviceMenu(r, "openvpn"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "2":
			if err := serviceMenu(r, "squid"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "3":
			if err := serviceMenu(r, "dropbear"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "4":
			if err := runProxies(r); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		default:
			fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
			waitEnter(r)
		}
	}
}

func paintConexaoHeader() {
	printSep()
	fmt.Println("\033[44;1;37m            โหมดฟังชั่น (จัดการบริการ)            \033[0m")
	printSep()
}

func paintConexaoMenu() {
	states, _ := service.StatusAll()
	stateOf := map[string]service.State{}
	for _, s := range states {
		stateOf[s.Service.Name] = s
	}

	items := []struct {
		idx  string
		name string
		key  string
	}{
		{"01", "OPENVPN", "openvpn"},
		{"02", "SQUID", "squid"},
		{"03", "DROPBEAR", "dropbear"},
		{"04", "SOCKS PROXY", ""},
	}
	for _, it := range items {
		marker := markerOff()
		statusText := cRedBold + "ยังไม่ติดตั้ง"
		if it.key != "" {
			st := stateOf[it.key]
			if !st.UnitExists {
				marker = markerOff()
				statusText = cRedBold + "ยังไม่ติดตั้ง"
			} else if st.ActiveState == "active" {
				marker = markerOn()
				statusText = cGrnBold + "ทำงาน"
			} else {
				marker = cYelBold + "◐ " + cReset
				statusText = cYelBold + "ติดตั้งแล้ว ปิดอยู่"
			}
		}
		if it.key == "" {
			marker = cWhtBold + "› " + cReset
			statusText = cWhtBold + "(จัดการ proxies)"
		}
		fmt.Printf("%s[%s%s%s] %s• %s%-15s %s %s%s\n",
			cRedBold, cCyanBold, it.idx, cRedBold,
			cWhtBold, cYelBold, it.name, marker, statusText, cReset)
	}
	fmt.Printf("%s[%s00%s] %s• %sย้อนกลับ%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Println()
	printSep()
	fmt.Println()
}

// serviceMenu renders the install / start / stop / enable / disable
// sub-menu for one service. This is where the lazy install lands -
// "install" extracts JUST this binary, writes JUST its unit, bootstraps
// JUST its config.
func serviceMenu(r *bufio.Reader, name string) error {
	svc, ok := service.ByName(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	for {
		clearScreen()
		st, _ := service.Status(svc)
		paintServiceHeader(name)
		paintServiceState(st)
		paintServiceActions(st, name)
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1": // install
			if st.UnitExists {
				fmt.Println("\n" + cYelBold + "ติดตั้งแล้ว — ไม่ต้องทำซ้ำ" + cReset)
				waitEnter(r)
				continue
			}
			res, err := service.InstallService(svc)
			if err != nil {
				return err
			}
			fmt.Println("\n" + cGrnBold + "ติดตั้ง " + svc.Name + " สำเร็จ!" + cReset)
			for _, p := range res.Extracted {
				fmt.Println("  + " + p)
			}
			for _, p := range res.UnitsWritten {
				fmt.Println("  + " + p)
			}
			for _, p := range res.ConfigsWritten {
				fmt.Println("  + " + p)
			}
			waitEnter(r)
		case "2": // start
			if err := service.Start(svc); err != nil {
				return err
			}
			fmt.Println("\n" + cGrnBold + "เริ่ม " + svc.Name + " แล้ว" + cReset)
			waitEnter(r)
		case "3": // stop
			if err := service.Stop(svc); err != nil {
				return err
			}
			fmt.Println("\n" + cYelBold + "หยุด " + svc.Name + " แล้ว" + cReset)
			waitEnter(r)
		case "4": // restart
			if err := service.Restart(svc); err != nil {
				return err
			}
			fmt.Println("\n" + cGrnBold + "รีสตาร์ท " + svc.Name + " แล้ว" + cReset)
			waitEnter(r)
		case "5": // enable
			if err := service.Enable(svc); err != nil {
				return err
			}
			fmt.Println("\n" + cGrnBold + "เปิดอัตโนมัติเมื่อบูต" + cReset)
			waitEnter(r)
		case "6": // disable
			if err := service.Disable(svc); err != nil {
				return err
			}
			fmt.Println("\n" + cYelBold + "ปิดอัตโนมัติเมื่อบูต" + cReset)
			waitEnter(r)
		case "7": // change port
			if !st.UnitExists {
				fmt.Println("\n" + cYelBold + "ติดตั้งก่อนจึงเปลี่ยนพอร์ตได้" + cReset)
				waitEnter(r)
				continue
			}
			if err := changeServicePort(r, svc); err != nil {
				return err
			}
		case "8": // openvpn-only: payload editor
			if name != "openvpn" || !st.UnitExists {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
				continue
			}
			if err := runPayload(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "9": // uninstall
			res, err := service.UninstallService(svc)
			if err != nil {
				return err
			}
			fmt.Println("\n" + cYelBold + "ถอนติดตั้ง " + svc.Name + " แล้ว" + cReset)
			for _, p := range res.Removed {
				fmt.Println("  - " + p)
			}
			waitEnter(r)
		default:
			fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
			waitEnter(r)
		}
	}
}

func paintServiceHeader(name string) {
	printSep()
	fmt.Printf("\033[44;1;37m            จัดการ %s            \033[0m\n", name)
	printSep()
}

func paintServiceState(st service.State) {
	if !st.UnitExists {
		fmt.Println(cRedBold + "สถานะ: " + cYelBold + "ยังไม่ติดตั้ง" + cReset)
	} else if st.ActiveState == "active" {
		fmt.Println(cGrnBold + "สถานะ: " + cWhtBold + "ทำงาน" + cReset)
		if st.MainPID != "" && st.MainPID != "0" {
			fmt.Println(cGrnBold + "PID: " + cWhtBold + st.MainPID + cReset)
		}
	} else {
		fmt.Println(cYelBold + "สถานะ: " + cWhtBold + st.ActiveState + "/" + st.SubState + cReset)
	}
	fmt.Println(cGrnBold + "พอร์ต: " + cWhtBold + fmt.Sprintf("%d/%s", st.Service.Port, st.Service.PortProto) + cReset)
	fmt.Println()
}

func paintServiceActions(st service.State, name string) {
	if !st.UnitExists {
		fmt.Printf("%s[%s1%s] %s• %sติดตั้ง%s\n",
			cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	} else {
		actions := []struct {
			idx, label string
		}{
			{"2", "เริ่มทำงาน"},
			{"3", "หยุดทำงาน"},
			{"4", "รีสตาร์ท"},
			{"5", "เปิดอัตโนมัติเมื่อบูต"},
			{"6", "ปิดอัตโนมัติเมื่อบูต"},
			{"7", "เปลี่ยนพอร์ต"},
		}
		// openvpn gets one extra action: rewrite a user's .ovpn with
		// a carrier-portal `remote` line for HTTP Injector / KPN Tunnel
		// use. Squid and Dropbear don't ship .ovpn files, so we skip
		// the option there to keep the grid honest.
		if name == "openvpn" {
			actions = append(actions, struct{ idx, label string }{"8", "ปรับแต่ง Payload"})
		}
		actions = append(actions, struct{ idx, label string }{"9", "ถอนการติดตั้ง"})
		for _, a := range actions {
			fmt.Printf("%s[%s%s%s] %s• %s%s%s\n",
				cRedBold, cCyanBold, a.idx, cRedBold,
				cWhtBold, cYelBold, a.label, cReset)
		}
	}
	fmt.Printf("%s[%s00%s] %s• %sย้อนกลับ%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Println()
	printSep()
	fmt.Println()
}

// changeServicePort drives the "พอร์ตปัจจุบัน NN. พอร์ตใหม่ ?" prompt
// for one service. We persist into the service's own config (openvpn,
// squid) or a systemd drop-in (dropbear, which v1 had no config file
// for) and then daemon-reload + restart so the new port is live.
func changeServicePort(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	printSep()
	fmt.Printf("\033[44;1;37m            เปลี่ยนพอร์ต %s            \033[0m\n", svc.Name)
	printSep()
	fmt.Println()

	currentPort, _ := readPersistedPort(svc)
	if currentPort == 0 {
		currentPort = svc.Port
	}

	fmt.Printf("%sพอร์ตปัจจุบัน: %s%d%s\n", cWhtBold, cYelBold, currentPort, cReset)
	in, err := promptLine(r, "พอร์ตใหม่ (1-65535, 0 = ยกเลิก): ")
	if err != nil {
		return err
	}
	if in == "" || in == "0" {
		return nil
	}
	newPort, err := strconv.Atoi(in)
	if err != nil || newPort < 1 || newPort > 65535 {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + "พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}

	needReload := false
	switch svc.Name {
	case "openvpn":
		if err := rewriteConfPort("/etc/openvpn/server.conf", `(?m)^\s*port\s+\d+\b`, fmt.Sprintf("port %d", newPort)); err != nil {
			return err
		}
	case "squid":
		if err := rewriteConfPort("/etc/squid/squid.conf", `(?m)^\s*http_port\s+\d+\b`, fmt.Sprintf("http_port %d", newPort)); err != nil {
			return err
		}
	case "dropbear":
		if err := writeDropbearPortDropIn(newPort); err != nil {
			return err
		}
		needReload = true
	default:
		return fmt.Errorf("ไม่รองรับการเปลี่ยนพอร์ตของ %s", svc.Name)
	}

	if needReload {
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			fmt.Println("\n" + cYelBold + "คำเตือน: systemctl daemon-reload: " + string(out) + cReset)
		}
	}
	if err := service.Restart(svc); err != nil {
		fmt.Println("\n" + cYelBold + "คำเตือน: รีสตาร์ทไม่สำเร็จ: " + err.Error() + cReset)
	}
	fmt.Println("\n" + cGrnBold + fmt.Sprintf("เปลี่ยนพอร์ตเป็น %d สำเร็จ", newPort) + cReset)
	waitEnter(r)
	return nil
}

// readPersistedPort tries to extract the currently-configured port from
// disk. Returns 0 + nil when the file doesn't exist (caller falls back
// to svc.Port).
func readPersistedPort(svc service.Service) (int, error) {
	switch svc.Name {
	case "openvpn":
		return scanFilePort("/etc/openvpn/server.conf", `(?m)^\s*port\s+(\d+)\b`)
	case "squid":
		return scanFilePort("/etc/squid/squid.conf", `(?m)^\s*http_port\s+(\d+)\b`)
	case "dropbear":
		path := filepath.Join(service.SystemdUnitDir, "hexplus-dropbear.service.d", "port.conf")
		return scanFilePort(path, `(?m)DROPBEAR_PORT=(\d+)`)
	}
	return 0, nil
}

func scanFilePort(path, pattern string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	re := regexp.MustCompile(pattern)
	m := re.FindSubmatch(data)
	if len(m) < 2 {
		return 0, nil
	}
	return strconv.Atoi(string(m[1]))
}

// rewriteConfPort rewrites the first `port`/`http_port` line in a
// service config. Idempotent: writes are skipped when the file already
// matches the desired output. Replacement is anchored with regexp so
// commented-out lines (#port 1194) are untouched.
func rewriteConfPort(path, pattern, replacement string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("อ่าน %s: %w", path, err)
	}
	re := regexp.MustCompile(pattern)
	if !re.Match(data) {
		// Fall through to append at the end — the line wasn't there.
		out := bytes.TrimRight(data, "\n")
		out = append(out, '\n')
		out = append(out, []byte(replacement+"\n")...)
		return writeIfChanged(path, data, out)
	}
	out := re.ReplaceAll(data, []byte(replacement))
	return writeIfChanged(path, data, out)
}

func writeIfChanged(path string, prev, next []byte) error {
	if bytes.Equal(prev, next) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, next, 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

// writeDropbearPortDropIn rewrites
// /etc/systemd/system/hexplus-dropbear.service.d/port.conf with a fresh
// ExecStart= override. Two ExecStart= lines: the blank one clears the
// inherited ExecStart from the parent unit, the second line is what we
// actually want systemd to run. (systemd quirk: appending one
// ExecStart= without first blanking would queue *both*.)
func writeDropbearPortDropIn(port int) error {
	dir := filepath.Join(service.SystemdUnitDir, "hexplus-dropbear.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	content := fmt.Sprintf(`# Generated by hexplus menu (port change). Safe to delete to revert.
[Service]
Environment=DROPBEAR_PORT=%d
ExecStart=
ExecStart=/usr/local/lib/hexplus/dropbear -F -E -R -p %d
`, port, port)
	dest := filepath.Join(dir, "port.conf")
	prev, _ := os.ReadFile(dest)
	if bytes.Equal(prev, []byte(content)) {
		return nil
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", dest, err)
	}
	return nil
}
