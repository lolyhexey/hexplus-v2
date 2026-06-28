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
	"os/exec"
	"strconv"
	"strings"
	"time"

	"io/fs"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/progress"
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
			{"4", "รีสตาร์ท SQUID"},
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
		case "4", "04":
			if err := service.Restart(svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			} else {
				fmt.Println("\n" + cGrnBold + "รีสตาร์ท SQUID สำเร็จ" + cReset)
			}
			waitEnter(r)
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

	// --- questions ---
	ip := defaultServerIP()
	if ip == "" {
		ip = "ไม่สามารถตรวจสอบ IP ได้"
	}
	fmt.Printf("%sยืนยัน IP ของคุณเพื่อดำเนินการต่อ: %s%s%s\n\n", cWhtBold, cYelBold, ip, cReset)
	fmt.Printf("%sกรุณาใส่พอร์ตที่คุณต้องการ ?%s\n\n", cGrnBold, cReset)
	fmt.Printf("%s[!] พอร์ตพร็อกซี่ตัวอย่าง %sEX: 80 8080%s\n\n", cYelBold, cWhtBold, cReset)
	fmt.Print(cGrnBold + "กรุณาใส่พอร์ต" + cYelBold + ": " + cReset)
	portLine, _ := r.ReadString('\n')
	port, convErr := strconv.Atoi(strings.TrimSpace(portLine))
	if convErr != nil || port < 1 || port > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}
	fmt.Println()
	paintOptions([][2]string{{"1", "พร็อกซี่เวอร์ชั่น 3.3.X"}, {"2", "พร็อกซี่เวอร์ชั่น 3.5.X"}})
	fmt.Println()
	fmt.Print(cGrnBold + "เลือกเวอร์ชั่น" + cRedBold + "?" + cWhtBold + " : " + cReset)
	verLine, _ := r.ReadString('\n')
	_ = strings.TrimSpace(verLine)

	// --- install with progress ---
	clearScreen()
	paintTitleBar("              ติดตั้งพร็อกซี่                ")
	fmt.Println()

	var listening bool
	if err := progress.Run([]progress.Step{
		{Label: "แตกไฟล์ binary + unit", Work: func() error {
			_, err := service.InstallService(svc)
			return err
		}},
		{Label: "เขียน squid.conf", Work: func() error {
			return os.WriteFile("/etc/squid/squid.conf", []byte(buildSquidConf(port, ip)), 0o644)
		}},
		{Label: "เริ่ม SQUID", Work: func() error {
			_ = service.Enable(svc)
			if err := service.Start(svc); err != nil {
				return err
			}
			time.Sleep(700 * time.Millisecond)
			listening, _ = service.ListenStatus(port, svc.PortProto)
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println()
	if listening {
		fmt.Println(cGrnBold + "ติดตั้งพร็อกซี่สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold + " SQUID เริ่มทำงานไม่สำเร็จ — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

func squidUninstall(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	paintTitleBar("            ถอนการติดตั้งพร็อกซี่              ")
	fmt.Print("\n" + cYelBold + "ต้องการลบ SQUID PROXY หรือไม่ " + cRedBold + "? " + cGrnBold + "[s/n]: " + cReset)
	line, _ := r.ReadString('\n')
	if strings.TrimSpace(line) != "s" {
		return nil
	}

	clearScreen()
	paintTitleBar("            ถอนการติดตั้งพร็อกซี่              ")
	fmt.Println()

	if err := progress.Run([]progress.Step{
		{Label: "หยุด + ลบ SQUID", Work: func() error {
			// UninstallService calls Stop+Disable internally — don't pre-stop.
			_, err := service.UninstallService(svc)
			return err
		}},
		{Label: "ล้าง config + ข้อมูล", Work: func() error {
			for _, p := range []string{"/etc/squid", "/usr/share/squid/mime.conf", "/var/spool/squid/errors"} {
				_ = os.RemoveAll(p)
			}
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
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

// buildSquidConf returns a complete squid.conf matching v1 conexao's template.
// Key difference from the Squid default: NO "http_access deny CONNECT !SSL_ports"
// rule, so HTTP Injector can CONNECT to this server's SSH port (22) through
// Squid to establish an SSH tunnel. The "acl SSH dst" ACL allows only CONNECT
// requests destined for this server's own IP — not an open proxy.
func buildSquidConf(port int, serverIP string) string {
	sshACL := ""
	if serverIP != "" {
		sshACL = "acl SSH dst " + serverIP + "/32\n" +
			"acl SSH dst 127.0.0.1/32\n"
	}
	sshAllow := ""
	if serverIP != "" {
		sshAllow = "http_access allow SSH\n"
	}
	return fmt.Sprintf(`http_port %d
acl localhost src 127.0.0.1/32 ::1
acl to_localhost dst 127.0.0.0/8 0.0.0.0/32 ::1
acl localnet src 10.0.0.0/8
acl localnet src 172.16.0.0/12
acl localnet src 192.168.0.0/16
acl SSL_ports port 443
acl Safe_ports port 80
acl Safe_ports port 21
acl Safe_ports port 443
acl Safe_ports port 70
acl Safe_ports port 210
acl Safe_ports port 1025-65535
acl Safe_ports port 280
acl Safe_ports port 488
acl Safe_ports port 591
acl Safe_ports port 777
acl CONNECT method CONNECT
%s%shttp_access allow localnet
http_access allow localhost
http_access deny all
visible_hostname HEXPLUS
via off
forwarded_for off
pipeline_prefetch off
cache deny all
unlinkd_program /bin/true
access_log none
cache_log /dev/null
cache_effective_user nobody
icon_directory /var/spool/squid/icons
error_directory /var/spool/squid/errors
logfile_daemon /usr/lib/squid/log_file_daemon
pid_filename /var/spool/squid/squid.pid
coredump_dir /var/spool/squid
`, port, sshACL, sshAllow)
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
			{"3", "รีสตาร์ท DROPBEAR"},
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
			clearScreen()
			paintTitleBar("              ลบ DROPBEAR               ")
			fmt.Print("\n" + cYelBold + "ต้องการลบ DROPBEAR หรือไม่ " + cRedBold + "? " + cGrnBold + "[s/n]: " + cReset)
			conf, _ := r.ReadString('\n')
			if strings.TrimSpace(conf) != "s" {
				continue
			}
			clearScreen()
			paintTitleBar("              ลบ DROPBEAR               ")
			fmt.Println()
			if err := progress.Run([]progress.Step{
				{Label: "หยุด + ลบ DROPBEAR", Work: func() error {
					_, err := service.UninstallService(svc)
					return err
				}},
			}); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
				continue
			}
			fmt.Println("\n" + cGrnBold + "ลบ DROPBEAR สำเร็จแล้ว" + cReset)
			waitEnter(r)
			return nil
		case "3", "03":
			if err := service.Restart(svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			} else {
				fmt.Println("\n" + cGrnBold + "รีสตาร์ท DROPBEAR สำเร็จ" + cReset)
			}
			waitEnter(r)
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

	if err := progress.Run([]progress.Step{
		{Label: "แตกไฟล์ binary + unit", Work: func() error {
			_, err := service.InstallService(svc)
			return err
		}},
		{Label: "เริ่ม DROPBEAR", Work: func() error {
			_ = service.Enable(svc)
			return service.Start(svc)
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
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
		webMark := markerOff()
		if isFileServerOn() {
			webMark = markerOn()
		}
		multiMark := markerOff()
		if ovpnConfContains("duplicate-cn") {
			multiMark = markerOn()
		}

		paintTitleBar("          จัดการ OPENVPN           ")
		fmt.Printf("\n%sพอร์ต%s: %s%d%s\n\n",
			cYelBold, cWhtBold, cGrnBold, port, cReset)

		// [3] and [4] use inline label+marker, not paintOptions, to match v1 format.
		fmt.Printf("%s[%s1%s] %s• %sเปลี่ยนพอร์ต%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s2%s] %s• %sลบ OPENVPN%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s3%s] %s• %sOVPN ผ่านลิงก์ %s%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, webMark, cReset)
		fmt.Printf("%s[%s4%s] %s• %sMULTILOGIN OVPN %s%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, multiMark, cReset)
		fmt.Printf("%s[%s5%s] %s• %sเปลี่ยน HOST DNS%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s6%s] %s• %sรีสตาร์ท OPENVPN%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s0%s] %s• %sย้อนกลับ%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
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
			clearScreen()
			paintTitleBar("             ลบ OPENVPN              ")
			fmt.Print("\n" + cYelBold + "ต้องการลบ OPENVPN หรือไม่ " + cRedBold + "? " + cGrnBold + "[s/n]: " + cReset)
			confirm, _ := r.ReadString('\n')
			if strings.TrimSpace(confirm) != "s" {
				continue
			}
			clearScreen()
			paintTitleBar("             ลบ OPENVPN              ")
			fmt.Println()
			if err := progress.Run([]progress.Step{
				{Label: "หยุด + ลบ OPENVPN", Work: func() error {
					_, err := service.UninstallService(svc)
					return err
				}},
				{Label: "ล้าง iptables + rc.local + /etc/openvpn", Work: func() error {
					cleanupOpenVPN()
					return nil
				}},
			}); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
				continue
			}
			fmt.Println("\n" + cGrnBold + "ลบ OPENVPN สำเร็จแล้ว" + cReset)
			waitEnter(r)
			return nil
		case "3", "03":
			toggleOVPNWeb(r)
		case "4", "04":
			toggleMultilogin(r, svc)
		case "5", "05":
			if err := dnsHostMenu(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "6", "06":
			if err := service.Restart(svc); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			} else {
				fmt.Println("\n" + cGrnBold + "รีสตาร์ท OPENVPN สำเร็จ" + cReset)
			}
			waitEnter(r)
		default:
			fmt.Println("\n" + cRedBold + "กรุณาเลือกให้ถูกต้อง..." + cReset)
			time.Sleep(2 * time.Second)
		}
	}
}

// ovpnConfContains checks if /etc/openvpn/server.conf contains a keyword.
func ovpnConfContains(keyword string) bool {
	data, err := os.ReadFile("/etc/openvpn/server.conf")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == keyword {
			return true
		}
	}
	return false
}

// isFileServerOn reports whether hexplus-fileserver.service is active.
func isFileServerOn() bool {
	svc, ok := service.ByName("fileserver")
	if !ok {
		return false
	}
	st, _ := service.Status(svc)
	return st.ActiveState == "active"
}

// toggleOVPNWeb starts or stops the built-in hexplus file server (port 82).
// No Apache2 needed — hexplus serves /root/openvpn/ via net/http.
func toggleOVPNWeb(r *bufio.Reader) {
	clearScreen()
	fsSvc, _ := service.ByName("fileserver")
	var err error
	if isFileServerOn() {
		paintTitleBar("          OVPN ผ่านลิงก์ (ปิด)         ")
		fmt.Println()
		err = progress.Run([]progress.Step{
			{Label: "ปิด file server", Work: func() error {
				return systemctlRun("disable", "--now", fsSvc.UnitName)
			}},
		})
	} else {
		paintTitleBar("          OVPN ผ่านลิงก์ (เปิด)        ")
		fmt.Println()
		err = progress.Run([]progress.Step{
			{Label: "สร้างโฟลเดอร์ /root/openvpn", Work: func() error {
				return os.MkdirAll("/root/openvpn", 0o700)
			}},
			{Label: "ติดตั้ง service unit", Work: func() error {
				_, werr := service.WriteUnits()
				return werr
			}},
			{Label: "เปิด file server พอร์ต 82", Work: func() error {
				return systemctlRun("enable", "--now", fsSvc.UnitName)
			}},
		})
	}
	if err != nil {
		fmt.Printf("\n"+cRedBold+"[ผิดพลาด] "+cYelBold+"%v"+cReset+"\n", err)
	} else {
		fmt.Printf("\n" + cGrnBold + "สำเร็จ!" + cReset + "\n")
	}
	waitEnter(r)
}

// toggleMultilogin adds or removes duplicate-cn from server.conf + restarts.
// ◉ = allowed (duplicate-cn present), ○ = blocked (absent).
func toggleMultilogin(r *bufio.Reader, svc service.Service) {
	clearScreen()
	if ovpnConfContains("duplicate-cn") {
		// Currently ON → turn off (block multilogin).
		paintTitleBar("          MULTILOGIN OVPN (บล็อก)       ")
		fmt.Println()
		fmt.Print(cRedBold + "กำลังบล็อก MULTILOGIN" + cGrnBold + "." + cYelBold + "." + cRedBold + ". " + cYelBold)
		if data, err := os.ReadFile("/etc/openvpn/server.conf"); err == nil {
			var lines []string
			for _, l := range strings.Split(string(data), "\n") {
				if strings.TrimSpace(l) != "duplicate-cn" {
					lines = append(lines, l)
				}
			}
			_ = os.WriteFile("/etc/openvpn/server.conf", []byte(strings.Join(lines, "\n")), 0o644)
		}
		service.Restart(svc)
		fmt.Println("Ok" + cReset)
	} else {
		// Currently OFF → turn on (allow multilogin).
		paintTitleBar("         MULTILOGIN OVPN (อนุญาต)      ")
		fmt.Println()
		fmt.Print(cGrnBold + "กำลังอนุญาต MULTILOGIN" + cGrnBold + "." + cYelBold + "." + cRedBold + ". " + cYelBold)
		if data, err := os.ReadFile("/etc/openvpn/server.conf"); err == nil {
			conf := string(data)
			if !strings.Contains(conf, "duplicate-cn") {
				conf = strings.TrimRight(conf, "\n") + "\nduplicate-cn\n"
				_ = os.WriteFile("/etc/openvpn/server.conf", []byte(conf), 0o644)
			}
		}
		service.Restart(svc)
		fmt.Println("Ok" + cReset)
	}
	waitEnter(r)
}

// dnsHostMenu mirrors v1's "เปลี่ยน HOST DNS" sub-menu:
//
//	[1] เพิ่ม HOST DNS  → add 127.0.0.1 hostname to /etc/hosts
//	[2] ลบ HOST DNS    → remove a hostname from /etc/hosts
//	[3] แก้ไขด้วยตนเอง → open /etc/hosts in nano
//	[0] ย้อนกลับ
func dnsHostMenu(r *bufio.Reader) error {
	for {
		clearScreen()
		paintTitleBar("         เปลี่ยน HOST DNS           ")
		fmt.Println()
		paintOptions([][2]string{
			{"1", "เพิ่ม HOST DNS"},
			{"2", "ลบ HOST DNS"},
			{"3", "แก้ไขด้วยตนเอง"},
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
			dnsAddHost(r)
		case "2", "02":
			dnsRemoveHost(r)
		case "3", "03":
			// Open /etc/hosts in the user's $EDITOR or nano.
			editor := os.Getenv("EDITOR")
			if editor == "" {
				editor = "nano"
			}
			fmt.Printf("\n%sกำลังแก้ไขไฟล์ %s/etc/hosts%s\n", cGrnBold, cWhtBold, cReset)
			fmt.Printf("%sคำเตือน! บันทึกโดยกดปุ่ม %sCtrl+X Y%s\n", cRedBold, cGrnBold, cReset)
			time.Sleep(2 * time.Second)
			cmd := exec.Command(editor, "/etc/hosts")
			cmd.Stdin = os.Stdin
			cmd.Stdout = os.Stdout
			cmd.Stderr = os.Stderr
			_ = cmd.Run()
			fmt.Println("\n" + cGrnBold + "แก้ไขสำเร็จแล้ว!" + cReset)
			time.Sleep(2 * time.Second)
		}
	}
}

// dnsAddHost adds "127.0.0.1 <hostname>" to /etc/hosts if not already present.
func dnsAddHost(r *bufio.Reader) {
	clearScreen()
	paintTitleBar("            เพิ่ม Host DNS            ")
	fmt.Println()
	// Show current non-localhost custom hosts pointing to 127.0.0.1
	data, _ := os.ReadFile("/etc/hosts")
	fmt.Printf("%sรายการ host ปัจจุบัน:%s\n\n", cYelBold, cReset)
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "127.0.0.1" && f[1] != "localhost" {
			fmt.Printf("%s%s%s\n", cGrnBold, f[1], cReset)
		}
	}
	fmt.Println()
	fmt.Print(cYelBold + "ใส่ host ที่ต้องการเพิ่ม" + cWhtBold + " : " + cReset)
	line, _ := r.ReadString('\n')
	host := strings.TrimSpace(line)
	if host == "" {
		fmt.Println(cRedBold + "\n[!] ค่าว่างเปล่าหรือไม่ถูกต้อง !" + cReset)
		time.Sleep(2 * time.Second)
		return
	}
	// Check already exists.
	if strings.Contains(string(data), " "+host) {
		fmt.Println(cRedBold + "\n[!] host นี้ถูกเพิ่มไว้แล้ว !" + cReset)
		time.Sleep(2 * time.Second)
		return
	}
	// Insert after line 2 (mirrors v1 sed -i "3i\127.0.0.1 $host").
	lines := strings.Split(string(data), "\n")
	newLines := make([]string, 0, len(lines)+1)
	if len(lines) >= 3 {
		newLines = append(newLines, lines[:2]...)
		newLines = append(newLines, "127.0.0.1 "+host)
		newLines = append(newLines, lines[2:]...)
	} else {
		newLines = append(lines, "127.0.0.1 "+host)
	}
	_ = os.WriteFile("/etc/hosts", []byte(strings.Join(newLines, "\n")), 0o644)
	fmt.Println("\n" + cGrnBold + "[✓] เพิ่ม host สำเร็จแล้ว !" + cReset)
	time.Sleep(2 * time.Second)
}

// dnsRemoveHost removes a 127.0.0.1 host entry from /etc/hosts.
func dnsRemoveHost(r *bufio.Reader) {
	clearScreen()
	paintTitleBar("            ลบ Host DNS            ")
	data, err := os.ReadFile("/etc/hosts")
	if err != nil {
		fmt.Println(cRedBold + "อ่าน /etc/hosts ไม่ได้" + cReset)
		time.Sleep(2 * time.Second)
		return
	}
	// Collect custom 127.0.0.1 entries.
	var hosts []string
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "127.0.0.1" && f[1] != "localhost" {
			hosts = append(hosts, f[1])
		}
	}
	if len(hosts) == 0 {
		fmt.Println("\n" + cYelBold + "ไม่มี host ที่จะลบ" + cReset)
		time.Sleep(2 * time.Second)
		return
	}
	fmt.Printf("%sรายการ host ปัจจุบัน:%s\n\n", cYelBold, cReset)
	for i, h := range hosts {
		fmt.Printf("%s[%s%d%s] %s- %s%s%s\n",
			cYelBold, cRedBold, i+1, cYelBold, cWhtBold, cGrnBold, h, cReset)
	}
	fmt.Println()
	fmt.Printf("%sเลือก host ที่ต้องการลบ [1-%d]%s: ", cGrnBold, len(hosts), cReset)
	line, _ := r.ReadString('\n')
	idx, convErr := strconv.Atoi(strings.TrimSpace(line))
	if convErr != nil || idx < 1 || idx > len(hosts) {
		fmt.Println(cRedBold + "[!] กรุณาเลือกให้ถูกต้อง !" + cReset)
		time.Sleep(2 * time.Second)
		return
	}
	target := hosts[idx-1]
	var newLines []string
	for _, l := range strings.Split(string(data), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 && f[0] == "127.0.0.1" && f[1] == target {
			continue
		}
		newLines = append(newLines, l)
	}
	_ = os.WriteFile("/etc/hosts", []byte(strings.Join(newLines, "\n")), 0o644)
	fmt.Println("\n" + cRedBold + "[✓] ลบ host สำเร็จแล้ว !" + cReset)
	time.Sleep(2 * time.Second)
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
		{"1", "ระบบ (System resolvers)"},
		{"2", "Google (แนะนำ)"},
		{"3", "OpenDNS"},
		{"4", "Cloudflare"},
		{"5", "Hurricane Electric"},
		{"6", "Verisign"},
		{"7", "DNS Performance"},
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

	// ---- ถาม PKI (interaction ก่อน progress) ----
	pkiWorkFn := openvpnAskPKI(r)

	// ---- progress ----
	clearScreen()
	paintTitleBar("              ติดตั้ง OPENVPN               ")
	fmt.Println()

	pkiLabel := "เริ่มต้น PKI (CA + cert + ta.key)"
	if pki.IsInitialized() {
		pkiLabel = "PKI มีอยู่แล้ว (ข้าม)"
	}

	var listening bool
	steps := []progress.Step{
		{Label: "แตกไฟล์ binary + unit", Work: func() error {
			_, err := service.InstallService(svc)
			return err
		}},
		{Label: pkiLabel, Work: pkiWorkFn},
		{Label: "เขียน server.conf", Work: func() error {
			return pki.WriteServerConf(port, proto, dnsChoice, ipLine)
		}},
		{Label: "ตั้งค่าเครือข่าย (IP forward + SNAT)", Work: func() error {
			setupNetworking(port, proto, ipLine)
			return nil
		}},
		{Label: "เริ่ม OPENVPN", Work: func() error {
			_ = service.Enable(svc)
			if err := service.Start(svc); err != nil {
				return err
			}
			for i := 0; i < 8; i++ {
				time.Sleep(500 * time.Millisecond)
				if ok, _ := service.ListenStatus(port, proto); ok {
					listening = true
					break
				}
			}
			return nil
		}},
	}
	if err := progress.Run(steps); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println()
	if listening {
		fmt.Println(cGrnBold + "ติดตั้ง OPENVPN สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold + " OPENVPN เริ่มทำงานไม่สำเร็จ — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

// setupNetworking enables IP forwarding and adds iptables SNAT so OpenVPN
// clients can reach the internet. Mirrors v1 conexao lines 1358-1384.
// Failures are non-fatal — logged to stderr, installer continues.
func setupNetworking(port int, proto, serverIP string) {
	// 1. Enable IP forwarding immediately.
	_ = os.WriteFile("/proc/sys/net/ipv4/ip_forward", []byte("1\n"), 0o644)

	// 2. Persist in /etc/sysctl.conf.
	if raw, err := os.ReadFile("/etc/sysctl.conf"); err == nil {
		content := string(raw)
		const key = "net.ipv4.ip_forward"
		if strings.Contains(content, key) {
			var lines []string
			for _, l := range strings.Split(content, "\n") {
				if strings.Contains(l, key) {
					lines = append(lines, key+"=1")
				} else {
					lines = append(lines, l)
				}
			}
			_ = os.WriteFile("/etc/sysctl.conf", []byte(strings.Join(lines, "\n")), 0o644)
		} else {
			f, err := os.OpenFile("/etc/sysctl.conf", os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				_, _ = f.WriteString("\nnet.ipv4.ip_forward=1\n")
				_ = f.Close()
			}
		}
	}

	// 3. iptables SNAT — let VPN clients reach the internet.
	exec.Command("iptables", "-t", "nat", "-A", "POSTROUTING",
		"-s", "10.8.0.0/16", "-j", "SNAT", "--to", serverIP).Run()

	// 4. Open the VPN port + allow forwarding if a DROP/REJECT policy exists.
	out, _ := exec.Command("iptables", "-L", "-n").Output()
	if strings.Contains(string(out), "REJECT") || strings.Contains(string(out), "DROP") {
		exec.Command("iptables", "-I", "INPUT", "-p", proto,
			"--dport", strconv.Itoa(port), "-j", "ACCEPT").Run()
		exec.Command("iptables", "-I", "FORWARD",
			"-s", "10.8.0.0/16", "-j", "ACCEPT").Run()
		exec.Command("iptables", "-I", "FORWARD",
			"-m", "state", "--state", "RELATED,ESTABLISHED", "-j", "ACCEPT").Run()
	}

	// 5. Disable IPv6 (v1 conexao does this to prevent leaks).
	_ = os.WriteFile("/proc/sys/net/ipv6/conf/all/disable_ipv6", []byte("1\n"), 0o644)

	// 6. Block outbound SMTP/POP3 — VPS providers penalise spam relays.
	// v1 adds these in rc.local; we apply them immediately too.
	for _, args := range [][]string{
		{"-A", "INPUT", "-p", "tcp", "--dport", "25", "-j", "DROP"},
		{"-A", "INPUT", "-p", "tcp", "--dport", "110", "-j", "DROP"},
		{"-A", "OUTPUT", "-p", "tcp", "--dport", "25", "-j", "DROP"},
		{"-A", "OUTPUT", "-p", "tcp", "--dport", "110", "-j", "DROP"},
		{"-A", "FORWARD", "-p", "tcp", "--dport", "25", "-j", "DROP"},
		{"-A", "FORWARD", "-p", "tcp", "--dport", "110", "-j", "DROP"},
	} {
		exec.Command("iptables", args...).Run()
	}

	// 7. Persist all rules across reboots via rc.local.
	rclocal := "/etc/rc.local"
	if _, err := os.Stat(rclocal); os.IsNotExist(err) {
		_ = os.WriteFile(rclocal, []byte("#!/bin/sh -e\nexit 0\n"), 0o755)
	}
	rules := []string{
		"echo 1 > /proc/sys/net/ipv4/ip_forward",
		"echo 1 > /proc/sys/net/ipv6/conf/all/disable_ipv6",
		fmt.Sprintf("iptables -t nat -A POSTROUTING -s 10.8.0.0/16 -j SNAT --to %s", serverIP),
		"iptables -A INPUT -p tcp --dport 25 -j DROP",
		"iptables -A INPUT -p tcp --dport 110 -j DROP",
		"iptables -A OUTPUT -p tcp --dport 25 -j DROP",
		"iptables -A OUTPUT -p tcp --dport 110 -j DROP",
		"iptables -A FORWARD -p tcp --dport 25 -j DROP",
		"iptables -A FORWARD -p tcp --dport 110 -j DROP",
	}
	if raw, err := os.ReadFile(rclocal); err == nil {
		content := string(raw)
		var extra []string
		for _, rule := range rules {
			if !strings.Contains(content, rule) {
				extra = append(extra, rule)
			}
		}
		if len(extra) > 0 {
			// Insert before the final line (usually "exit 0").
			lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
			insertAt := len(lines)
			if insertAt > 0 {
				insertAt-- // before last line
			}
			newLines := append(lines[:insertAt], append(extra, lines[insertAt:]...)...)
			_ = os.WriteFile(rclocal, []byte(strings.Join(newLines, "\n")+"\n"), 0o755)
		}
	}
}

// openvpnAskPKI shows the PKI source menu (requires user input) and returns
// a work function that runs the actual generation without further interaction.
// Returns a no-op if PKI is already initialized.
func openvpnAskPKI(r *bufio.Reader) func() error {
	if pki.IsInitialized() {
		return func() error { return nil }
	}

	clearScreen()
	printSep()
	fmt.Println(cGrnBold + " HEXPLUS — ตั้งค่า PKI" + cReset)
	printSep()
	fmt.Println(cYelBold + "[1]" + cWhtBold + " ใช้ CA จาก git" +
		cRedBold + " (CN: lolouch.com, อายุ 100 ปี)" + cYelBold + " — แนะนำ")
	fmt.Println(cYelBold + "[2]" + cWhtBold + " สร้าง CA ใหม่" +
		cRedBold + " (ตั้งค่าเอง)")
	fmt.Print(cGrnBold + "เลือกตัวเลือก " + cYelBold + "?" + cRedBold + "?" + cWhtBold + " " + cReset)
	line, _ := r.ReadString('\n')
	choice := strings.TrimSpace(line)

	if choice == "2" {
		opts := openvpnAskPKICustom(r)
		return func() error {
			_, err := pki.Init(opts)
			return err
		}
	}

	// Option 1: load embedded CA bytes now (before progress starts).
	pkiFS := assets.PKI()
	caCert, errCA := fs.ReadFile(pkiFS, "ca.crt")
	caKey, errKey := fs.ReadFile(pkiFS, "ca.key")
	taKey, errTA := fs.ReadFile(pkiFS, "ta.key")
	return func() error {
		if errCA != nil {
			return fmt.Errorf("อ่าน embedded ca.crt: %w", errCA)
		}
		if errKey != nil {
			return fmt.Errorf("อ่าน embedded ca.key: %w", errKey)
		}
		if errTA != nil {
			return fmt.Errorf("อ่าน embedded ta.key: %w", errTA)
		}
		_, err := pki.InstallWithCA(caCert, caKey, taKey, "", true)
		return err
	}
}

// openvpnAskPKICustom collects custom CA parameters from the user and returns
// InitOptions. The actual generation happens later inside progress.Run.
func openvpnAskPKICustom(r *bufio.Reader) pki.InitOptions {
	fmt.Println()
	fmt.Println(cGrnBold + "— สร้าง CA ใหม่ —" + cReset)

	readField := func(label, example, def string) string {
		fmt.Printf("%s%s %s(ตัวอย่าง: %s)%s [Enter = %s]: ",
			cYelBold, label, cRedBold, example, cWhtBold, def)
		line, _ := r.ReadString('\n')
		v := strings.TrimSpace(line)
		if v == "" {
			return def
		}
		return v
	}

	caCN := readField("CA Common Name     ", "lolouch.com", "lolouch.com")
	serverCN := readField("Server Common Name ", "KSMLB by LO LY", "KSMLB by LO LY")
	org := readField("Organization       ", "lolouch.com", "lolouch.com")
	yearsStr := readField("อายุ CA (ปี)        ", "100", "100")
	years := 100
	if n, err := strconv.Atoi(yearsStr); err == nil && n > 0 {
		years = n
	}

	return pki.InitOptions{
		CACommonName:     caCN,
		ServerCommonName: serverCN,
		Org:              org,
		CAValidityYears:  years,
		Force:            true,
	}
}

// cleanupOpenVPN removes everything setupNetworking wrote and deletes
// /etc/openvpn (including PKI). Matches v1's rmv_open() cleanup.
func cleanupOpenVPN() {
	const rclocal = "/etc/rc.local"

	// Read the server IP from rc.local before we delete anything — it's
	// stored in the SNAT line "iptables -t nat -A POSTROUTING -s 10.8.0.0/16
	// -j SNAT --to <IP>".
	serverIP := ""
	if raw, err := os.ReadFile(rclocal); err == nil {
		for _, line := range strings.Split(string(raw), "\n") {
			if strings.Contains(line, "-j SNAT --to ") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "--to" && i+1 < len(parts) {
						serverIP = parts[i+1]
					}
				}
			}
		}
	}

	// Remove live iptables SNAT rule.
	if serverIP != "" {
		exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING",
			"-s", "10.8.0.0/16", "-j", "SNAT", "--to", serverIP).Run()
		fmt.Println(cYelBold + "  - iptables SNAT 10.8.0.0/16 → " + serverIP + cReset)
	}

	// Clean rc.local: remove every line that setupNetworking injected.
	cleanPrefixes := []string{
		"echo 1 > /proc/sys/net/ipv4/ip_forward",
		"echo 1 > /proc/sys/net/ipv6/conf/all/disable_ipv6",
		"iptables -t nat -A POSTROUTING -s 10.8.0.0/16",
		"iptables -A INPUT -p tcp --dport 25 -j DROP",
		"iptables -A INPUT -p tcp --dport 110 -j DROP",
		"iptables -A OUTPUT -p tcp --dport 25 -j DROP",
		"iptables -A OUTPUT -p tcp --dport 110 -j DROP",
		"iptables -A FORWARD -p tcp --dport 25 -j DROP",
		"iptables -A FORWARD -p tcp --dport 110 -j DROP",
	}
	if raw, err := os.ReadFile(rclocal); err == nil {
		var kept []string
		for _, line := range strings.Split(string(raw), "\n") {
			drop := false
			for _, pfx := range cleanPrefixes {
				if strings.Contains(line, pfx) {
					drop = true
					break
				}
			}
			if !drop {
				kept = append(kept, line)
			}
		}
		_ = os.WriteFile(rclocal, []byte(strings.Join(kept, "\n")), 0o755)
		fmt.Println(cYelBold + "  - rc.local: ลบ iptables entries" + cReset)
	}

	// Remove /etc/openvpn (configs + PKI + certs).
	if err := os.RemoveAll("/etc/openvpn"); err == nil {
		fmt.Println(cYelBold + "  - /etc/openvpn" + cReset)
	}
}

// ovpnPort reads the port from /etc/openvpn/server.conf, fallback 1194.
func ovpnPort() int { return readOpenVPNPort(service.Service{Port: 1194}) }

// ovpnProto reads proto from /etc/openvpn/server.conf, fallback "tcp".
func ovpnProto() string {
	if data, err := os.ReadFile("/etc/openvpn/server.conf"); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			trim := strings.TrimSpace(line)
			if strings.HasPrefix(trim, "proto ") {
				fields := strings.Fields(trim)
				if len(fields) >= 2 {
					return fields[1]
				}
			}
		}
	}
	return "tcp"
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
