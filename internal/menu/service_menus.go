// service_menus.go: per-service sub-menus that mirror v1's
// fun_squid / fun_drop / fun_openvpn layouts byte-for-byte.
//
// v1 paints distinct option sets per service (Squid has add-port/remove-port,
// Dropbear has limiter toggle, OpenVPN has DNS / multilogin) instead of
// the generic start/stop/restart/enable/disable grid v2.2.0 shipped.
// This file replaces serviceMenu()'s body with per-service routers.

package menu

import (
	"bufio"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/service"
)

// menuPrompt is the trailing "เลือกตัวเลือก ??:" line v1 uses across
// every conexao sub-menu. Read+trim the user's reply.
func menuPrompt(r *bufio.Reader) (string, error) {
	fmt.Print(cGrnBold + "เลือกตัวเลือก " + cYelBold + "?" + cRedBold + "?" + cWhtBold + " " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// paintTitleBar reproduces v1's \E[44;1;37m...\E[0m white-on-blue bar.
func paintTitleBar(title string) {
	fmt.Printf("\033[44;1;37m%s\033[0m\n", title)
}

// paintOptions emits the v1 numbered-option grid: [N] • <label>.
func paintOptions(opts [][2]string) {
	for _, o := range opts {
		fmt.Printf("%s[%s%s%s] %s• %s%s%s\n",
			cRedBold, cCyanBold, o[0], cRedBold,
			cWhtBold, cYelBold, o[1], cReset)
	}
}

// promptLineDefault prints "PROMPT [default]: " and returns the user's input,
// falling back to defaultVal if the user presses Enter without typing.
func promptLineDefault(r *bufio.Reader, prompt, defaultVal string) (string, error) {
	fmt.Printf("%s%s %s[%s]%s: %s", cGrnBold, prompt, cYelBold, defaultVal, cGrnBold, cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return defaultVal, err
	}
	line = strings.TrimSpace(line)
	if line == "" {
		return defaultVal, nil
	}
	return line, nil
}

// defaultServerIP tries to detect the VPS's public IPv4 via icanhazip.com
// (same approach v1 uses: wget -qO- ipv4.icanhazip.com).
func defaultServerIP() string {
	cl := &http.Client{Timeout: 5 * time.Second}
	resp, err := cl.Get("https://ipv4.icanhazip.com")
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// =====================================================================
// SQUID (fun_squid in Modulos/conexao)
// =====================================================================

func squidMenu(r *bufio.Reader, svc service.Service) error {
	for {
		clearScreen()
		st, _ := service.Status(svc)
		running := st.UnitExists && st.ActiveState == "active"
		ports := readSquidPorts()

		paintTitleBar("          จัดการ SQUID PROXY           ")
		if running && len(ports) > 0 {
			fmt.Printf("\n%sพอร์ต%s: %s%s%s\n",
				cYelBold, cWhtBold, cGrnBold, strings.Join(ports, " "), cReset)
		}
		fmt.Println()

		firstLabel := "ติดตั้ง SQUID PROXY"
		if st.UnitExists {
			firstLabel = "ถอนการติดตั้งพร็อกซี่"
		}
		paintOptions([][2]string{
			{"1", firstLabel},
			{"2", "เพิ่มพอร์ตพร็อกซี่"},
			{"3", "ลบพอร์ตพร็อกซี่"},
			{"0", "ย้อนกลับ"},
		})
		fmt.Println()

		choice, err := menuPrompt(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1", "01":
			if st.UnitExists {
				if err := squidUninstall(r, svc); err != nil {
					return err
				}
			} else {
				if err := squidInstall(r, svc); err != nil {
					return err
				}
			}
		case "2", "02":
			if !st.UnitExists {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold +
					" SQUID ยังไม่ได้ติดตั้ง: กรุณาติดตั้ง SQUID ก่อน" + cReset)
				waitEnter(r)
				continue
			}
			if err := squidAddPort(r, svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "3", "03":
			if !st.UnitExists {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold +
					" SQUID ยังไม่ได้ติดตั้ง: กรุณาติดตั้ง SQUID ก่อน" + cReset)
				waitEnter(r)
				continue
			}
			if err := squidRemovePort(r, svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		default:
			fmt.Println("\n" + cRedBold + "กรุณาเลือกให้ถูกต้อง..." + cReset)
			time.Sleep(2 * time.Second)
		}
	}
}

// readSquidPorts parses /etc/squid/squid.conf for every "http_port N" line
// and returns the port numbers in declaration order. v1 displays multiple
// ports separated by spaces, matching this list.
func readSquidPorts() []string {
	data, err := os.ReadFile("/etc/squid/squid.conf")
	if err != nil {
		return nil
	}
	var ports []string
	for _, line := range strings.Split(string(data), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "http_port ") {
			continue
		}
		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}
		// http_port may be "3128" or "192.168.0.1:3128"; take the port half.
		val := fields[1]
		if idx := strings.LastIndex(val, ":"); idx >= 0 {
			val = val[idx+1:]
		}
		if _, err := strconv.Atoi(val); err == nil {
			ports = append(ports, val)
		}
	}
	return ports
}

func squidInstall(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("              ติดตั้งพร็อกซี่                ")
	fmt.Println()
	res, err := service.InstallService(svc)
	if err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	for _, p := range res.Extracted {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}
	for _, p := range res.UnitsWritten {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}
	for _, p := range res.ConfigsWritten {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}

	// v1 conexao: auto-start + verify via netstat.
	_ = service.Enable(svc)
	if err := service.Start(svc); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold +
			" SQUID เริ่มทำงานไม่สำเร็จ: " + err.Error() +
			" — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
		waitEnter(r)
		return nil
	}
	time.Sleep(700 * time.Millisecond)
	listening, _ := service.ListenStatus(svc.Port, svc.PortProto)
	if listening {
		fmt.Println("\n" + cGrnBold + "ติดตั้งพร็อกซี่สำเร็จแล้ว!" + cReset)
	} else {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold +
			" SQUID เริ่มทำงานไม่สำเร็จ: บริการไม่ทำงานหลังติดตั้ง — ตรวจสอบ journalctl -u " +
			svc.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

func squidUninstall(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("            ถอนการติดตั้งพร็อกซี่              ")
	fmt.Println("\n" + cGrnBold + "กำลังลบ SQUID PROXY !" + cReset)
	res, err := service.UninstallService(svc)
	if err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	for _, p := range res.Removed {
		fmt.Println(cYelBold + "  - " + p + cReset)
	}
	fmt.Println("\n" + cGrnBold + "ลบ SQUID สำเร็จแล้ว !" + cReset)
	waitEnter(r)
	return nil
}

func squidAddPort(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("         เพิ่มพอร์ตพร็อกซี่         ")
	ports := readSquidPorts()
	fmt.Printf("\n%sพอร์ตที่ใช้งานอยู่: %s%s%s\n\n",
		cYelBold, cGrnBold, strings.Join(ports, " "), cReset)

	fmt.Print(cGrnBold + "ต้องการเพิ่มพอร์ตใด " + cYelBold + "?" + cWhtBold + " " + cReset)
	line, _ := r.ReadString('\n')
	portStr := strings.TrimSpace(line)
	port, err := strconv.Atoi(portStr)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง: ต้องเป็นตัวเลข 1-65535")
	}
	for _, p := range ports {
		if p == portStr {
			return fmt.Errorf("พอร์ต %s ใช้งานอยู่แล้วใน squid.conf", portStr)
		}
	}

	// Append a new "http_port NNN" line.
	data, err := os.ReadFile("/etc/squid/squid.conf")
	if err != nil {
		return fmt.Errorf("อ่าน squid.conf ไม่ได้: %w", err)
	}
	newConf := string(data)
	if !strings.HasSuffix(newConf, "\n") {
		newConf += "\n"
	}
	newConf += "http_port " + portStr + "\n"
	if err := os.WriteFile("/etc/squid/squid.conf", []byte(newConf), 0o644); err != nil {
		return fmt.Errorf("เขียน squid.conf ไม่ได้: %w", err)
	}

	fmt.Println("\n" + cGrnBold + "กำลังเพิ่มพอร์ตให้ SQUID!" + cReset)
	if err := service.Restart(svc); err != nil {
		return fmt.Errorf("รีสตาร์ท SQUID ไม่สำเร็จ: %w", err)
	}
	time.Sleep(700 * time.Millisecond)
	listening, _ := service.ListenStatus(port, "tcp")
	if listening {
		fmt.Println("\n" + cGrnBold + "ติดตั้งเสร็จเรียบร้อยเเล้ว!" + cReset)
	} else {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold +
			" พอร์ตใหม่ไม่ขึ้น — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

func squidRemovePort(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("        ลบพอร์ตของ SQUID        ")
	ports := readSquidPorts()
	fmt.Printf("\n%sพอร์ตที่ใช้งานอยู่: %s%s%s\n\n",
		cYelBold, cGrnBold, strings.Join(ports, " "), cReset)
	if len(ports) <= 1 {
		return fmt.Errorf("ต้องเหลือพอร์ตอย่างน้อย 1 พอร์ต — เพิ่มพอร์ตอื่นก่อนลบ")
	}

	fmt.Print(cGrnBold + "ต้องการลบพอร์ตใด " + cYelBold + "?" + cWhtBold + " " + cReset)
	line, _ := r.ReadString('\n')
	portStr := strings.TrimSpace(line)
	if _, err := strconv.Atoi(portStr); err != nil {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง: %q", portStr)
	}

	data, err := os.ReadFile("/etc/squid/squid.conf")
	if err != nil {
		return fmt.Errorf("อ่าน squid.conf ไม่ได้: %w", err)
	}
	var out []string
	removed := false
	for _, ln := range strings.Split(string(data), "\n") {
		trim := strings.TrimSpace(ln)
		if strings.HasPrefix(trim, "http_port ") {
			fields := strings.Fields(trim)
			if len(fields) >= 2 {
				val := fields[1]
				if idx := strings.LastIndex(val, ":"); idx >= 0 {
					val = val[idx+1:]
				}
				if val == portStr {
					removed = true
					continue
				}
			}
		}
		out = append(out, ln)
	}
	if !removed {
		return fmt.Errorf("ไม่พบพอร์ต %s ใน squid.conf", portStr)
	}
	if err := os.WriteFile("/etc/squid/squid.conf", []byte(strings.Join(out, "\n")), 0o644); err != nil {
		return fmt.Errorf("เขียน squid.conf ไม่ได้: %w", err)
	}

	fmt.Println("\n" + cGrnBold + "กำลังลบพอร์ตจาก SQUID!" + cReset)
	if err := service.Restart(svc); err != nil {
		return fmt.Errorf("รีสตาร์ท SQUID ไม่สำเร็จ: %w", err)
	}
	fmt.Println("\n" + cGrnBold + "ลบพอร์ตสำเร็จเเล้ว!" + cReset)
	waitEnter(r)
	return nil
}

// =====================================================================
// DROPBEAR (fun_drop in Modulos/conexao)
// =====================================================================

func dropbearMenu(r *bufio.Reader, svc service.Service) error {
	for {
		clearScreen()
		st, _ := service.Status(svc)

		// v1 fun_drop only shows the management screen when dropbear is
		// listening. When not installed, we offer an "install" path.
		if !st.UnitExists {
			return dropbearInstall(r, svc)
		}

		port := readDropbearPort(svc)
		paintTitleBar("              จัดการ DROPBEAR               ")
		fmt.Printf("\n%sพอร์ต%s: %s%d%s\n\n",
			cYelBold, cWhtBold, cGrnBold, port, cReset)

		paintOptions([][2]string{
			{"1", "เปลี่ยนพอร์ต DROPBEAR"},
			{"2", "ลบ DROPBEAR"},
			{"0", "ย้อนกลับ"},
		})
		fmt.Println()
		choice, err := menuPrompt(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1", "01":
			if err := changeServicePort(r, svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "2", "02":
			res, err := service.UninstallService(svc)
			if err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
				continue
			}
			fmt.Println("\n" + cGrnBold + "ลบ DROPBEAR สำเร็จแล้ว" + cReset)
			for _, p := range res.Removed {
				fmt.Println(cYelBold + "  - " + p + cReset)
			}
			waitEnter(r)
			return nil
		default:
			fmt.Println("\n" + cRedBold + "กรุณาเลือกให้ถูกต้อง..." + cReset)
			time.Sleep(2 * time.Second)
		}
	}
}

func dropbearInstall(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("              ติดตั้ง DROPBEAR               ")
	fmt.Println()
	res, err := service.InstallService(svc)
	if err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	for _, p := range res.Extracted {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}
	_ = service.Enable(svc)
	if err := service.Start(svc); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	fmt.Println("\n" + cGrnBold + "ติดตั้ง DROPBEAR สำเร็จแล้ว!" + cReset)
	waitEnter(r)
	return nil
}

// readDropbearPort reads the current port from the systemd drop-in we wrote
// for port changes, falling back to the Service struct's default port.
func readDropbearPort(svc service.Service) int {
	if data, err := os.ReadFile("/etc/systemd/system/hexplus-dropbear.service.d/port.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			if strings.HasPrefix(line, "Environment=DROPBEAR_PORT=") {
				val := strings.TrimPrefix(line, "Environment=DROPBEAR_PORT=")
				if p, err := strconv.Atoi(strings.TrimSpace(val)); err == nil {
					return p
				}
			}
		}
	}
	return svc.Port
}

// =====================================================================
// OPENVPN (fun_openvpn in Modulos/conexao)
// =====================================================================

func openvpnMenu(r *bufio.Reader, svc service.Service) error {
	for {
		clearScreen()
		st, _ := service.Status(svc)
		if !st.UnitExists {
			return openvpnInstall(r, svc)
		}

		port := readOpenVPNPort(svc)
		paintTitleBar("          จัดการ OPENVPN           ")
		fmt.Printf("\n%sพอร์ต%s: %s%d%s\n\n",
			cYelBold, cWhtBold, cGrnBold, port, cReset)

		paintOptions([][2]string{
			{"1", "เปลี่ยนพอร์ต"},
			{"2", "ลบ OPENVPN"},
			{"3", "ปรับแต่ง Payload (.ovpn)"},
			{"0", "ย้อนกลับ"},
		})
		fmt.Println()
		choice, err := menuPrompt(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1", "01":
			if err := changeServicePort(r, svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "2", "02":
			res, err := service.UninstallService(svc)
			if err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
				continue
			}
			fmt.Println("\n" + cGrnBold + "ลบ OPENVPN สำเร็จแล้ว" + cReset)
			for _, p := range res.Removed {
				fmt.Println(cYelBold + "  - " + p + cReset)
			}
			waitEnter(r)
			return nil
		case "3", "03":
			if err := runPayload(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		default:
			fmt.Println("\n" + cRedBold + "กรุณาเลือกให้ถูกต้อง..." + cReset)
			time.Sleep(2 * time.Second)
		}
	}
}

func openvpnInstall(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("              ติดตั้ง OPENVPN               ")
	fmt.Println()

	// ---- ถาม IP ----
	serverIP := defaultServerIP()
	ipLine, _ := promptLineDefault(r, "ยืนยัน IP ของคุณเพื่อดำเนินการต่อ", serverIP)
	if ipLine == "" {
		fmt.Println(cRedBold + "[ผิดพลาด] IP ไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}

	// ---- ถามพอร์ต ----
	portLine, _ := promptLineDefault(r, "ต้องการใช้พอร์ตใด?", "443")
	port, err := strconv.Atoi(strings.TrimSpace(portLine))
	if err != nil || port < 1 || port > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}

	// ---- ถาม DNS ----
	fmt.Println()
	dnsOptions := [][2]string{
		{"1", "ระบบ"},
		{"2", "Google (แนะนำ)"},
		{"3", "OpenDNS"},
		{"4", "Cloudflare"},
		{"5", "Hurricane Electric"},
		{"6", "Verisign"},
	}
	for _, o := range dnsOptions {
		fmt.Printf(cRedBold+"["+cCyanBold+"%s"+cRedBold+"] "+cYelBold+"%s\n"+cReset, o[0], o[1])
	}
	fmt.Println()
	dnsChoice, _ := promptLineDefault(r, "ต้องการใช้ DNS ใด?", "2")

	// ---- ถาม Protocol ----
	fmt.Println()
	fmt.Printf(cRedBold+"["+cCyanBold+"1"+cRedBold+"] "+cYelBold+"UDP\n"+cReset)
	fmt.Printf(cRedBold+"["+cCyanBold+"2"+cRedBold+"] "+cYelBold+"TCP (แนะนำ)\n"+cReset)
	fmt.Println()
	protoChoice, _ := promptLineDefault(r, "ต้องการใช้โปรโตคอลใดกับ OPENVPN?", "2")
	proto := "tcp"
	if strings.TrimSpace(protoChoice) == "1" {
		proto = "udp"
	}

	// ---- extract binary + unit ----
	fmt.Println()
	fmt.Println(cGrnBold + "กำลังติดตั้ง OPENVPN !" + cReset)
	fmt.Println()
	res, err := service.InstallService(svc)
	if err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	for _, p := range res.Extracted {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}
	for _, p := range res.UnitsWritten {
		fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
	}

	// ---- init PKI (CA + server cert + ta.key) ----
	fmt.Println()
	if !pki.IsInitialized() {
		fmt.Println(cGrnBold + "กำลังสร้าง PKI (CA, cert, ta.key)..." + cReset)
		pkiRes, pkiErr := pki.Init(pki.InitOptions{})
		if pkiErr != nil {
			fmt.Println(cRedBold + "[ผิดพลาด] PKI: " + cYelBold + pkiErr.Error() + cReset)
			waitEnter(r)
			return nil
		}
		for _, p := range pkiRes.Written {
			fmt.Println(cGrnBold + "  + " + cWhtBold + p + cReset)
		}
	} else {
		fmt.Println(cYelBold + "  PKI มีอยู่แล้ว ข้ามขั้นตอนนี้" + cReset)
	}

	// ---- เขียน server.conf ด้วยค่าจริง ----
	if err := pki.WriteServerConf(port, proto, dnsChoice, ipLine); err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] server.conf: " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	fmt.Println(cGrnBold + "  + " + cWhtBold + "/etc/openvpn/server.conf" + cReset)

	// ---- start ----
	_ = service.Enable(svc)
	if err := service.Start(svc); err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] เริ่ม OPENVPN ไม่สำเร็จ: " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	// Retry for up to 4 seconds (OpenVPN can take a moment to bind the port).
	var listening bool
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		ok, _ := service.ListenStatus(port, proto)
		if ok {
			listening = true
			break
		}
	}
	fmt.Println()
	if listening {
		fmt.Println(cGrnBold + "ติดตั้ง OPENVPN สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
			" OPENVPN เริ่มทำงานไม่สำเร็จ: บริการไม่ทำงานหลังติดตั้ง — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

func readOpenVPNPort(svc service.Service) int {
	if data, err := os.ReadFile("/etc/openvpn/server.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "port ") {
				fields := strings.Fields(trim)
				if len(fields) >= 2 {
					if p, err := strconv.Atoi(fields[1]); err == nil {
						return p
					}
				}
			}
		}
	}
	return svc.Port
}
