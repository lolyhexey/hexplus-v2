// ssltunnel_menu.go: TUI sub-menu for SSL TUNNEL (conexao option 06).
//
// Mirrors v1's fun_ssl layout: install (standard/websocket), change port,
// uninstall, restart, view logs. No external binary — hexplus itself is
// the TLS terminator (hexplus ssltunnel run, managed by systemd).

package menu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/progress"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// sslTunnelMenu is the top-level router for option [06] in the conexao menu.
func sslTunnelMenu(r *bufio.Reader) error {
	for {
		clearScreen()
		cfg, _ := ssltunnel.Load()
		installed := ssltunnel.IsInstalled()

		if !installed {
			paintTitleBar("              จัดการ SSL TUNNEL               ")
			fmt.Println()
			paintOptions([][2]string{
				{"1", "ติดตั้ง SSL TUNNEL แบบมาตรฐาน  (target: 127.0.0.1:22)"},
				{"2", "ติดตั้ง SSL TUNNEL WEBSOCKET   (target: 127.0.0.1:80)"},
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
				if err := sslTunnelInstall(r, "127.0.0.1:22"); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			case "2", "02":
				if err := sslTunnelInstall(r, "127.0.0.1:80"); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			default:
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
			}
		} else {
			paintTitleBar("              จัดการ SSL TUNNEL               ")
			fmt.Printf("\n%sพอร์ต%s: %s%d%s  →  %s%s%s\n\n",
				cYelBold, cWhtBold, cGrnBold, cfg.Port, cReset,
				cCyanBold, cfg.Target, cReset)
			paintOptions([][2]string{
				{"1", "เปลี่ยนพอร์ต SSL TUNNEL"},
				{"2", "ลบ SSL TUNNEL"},
				{"3", "รีสตาร์ท SSL TUNNEL"},
				{"4", "ดู log SSL TUNNEL"},
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
				if err := sslTunnelChangePort(r, cfg); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			case "2", "02":
				if err := sslTunnelUninstall(r); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			case "3", "03":
				if err := systemctlRun("restart", ssltunnel.UnitName); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				} else {
					fmt.Println("\n" + cGrnBold + "รีสตาร์ท SSL TUNNEL สำเร็จ" + cReset)
				}
				waitEnter(r)
			case "4", "04":
				showUnitLog(r, ssltunnel.UnitName)
			default:
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
			}
		}
	}
}

// sslTunnelInstall runs the full install flow for the given target address.
// target is "127.0.0.1:22" (standard SSH) or "127.0.0.1:80" (WebSocket).
func sslTunnelInstall(r *bufio.Reader, target string) error {
	clearScreen()
	paintTitleBar("              ติดตั้ง SSL TUNNEL               ")
	fmt.Println()

	portStr, err := promptLineDefault(r, "กำหนดพอร์ตสำหรับ SSL TUNNEL", "443")
	if err != nil {
		return err
	}
	port, convErr := strconv.Atoi(strings.TrimSpace(portStr))
	if convErr != nil || port < 1 || port > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}
	if err := checkPortFree(port, "tcp"); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	cfg := ssltunnel.Config{Port: port, Target: target}

	var listening bool
	if err := progress.Run([]progress.Step{
		{Label: "สร้างใบรับรอง TLS", Work: ssltunnel.GenerateCert},
		{Label: "บันทึก config", Work: cfg.Save},
		{Label: "เขียน systemd unit", Work: func() error {
			return ssltunnel.WriteUnit(cfg)
		}},
		{Label: "เริ่ม SSL TUNNEL", Work: func() error {
			if err := systemctlRun("enable", "--now", ssltunnel.UnitName); err != nil {
				return err
			}
			time.Sleep(700 * time.Millisecond)
			listening, _ = service.ListenStatus(port, "tcp")
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println()
	if listening {
		fmt.Println(cGrnBold + "ติดตั้ง SSL TUNNEL สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold + " SSL TUNNEL เริ่มทำงานไม่สำเร็จ — ตรวจสอบ journalctl -u " + ssltunnel.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

// sslTunnelUninstall stops + removes the service, cert, key, and config.
func sslTunnelUninstall(r *bufio.Reader) error {
	clearScreen()
	paintTitleBar("              ลบ SSL TUNNEL               ")
	fmt.Print("\n" + cYelBold + "ต้องการลบ SSL TUNNEL หรือไม่ " + cRedBold + "? " + cGrnBold + "[s/n]: " + cReset)
	line, _ := r.ReadString('\n')
	if strings.TrimSpace(line) != "s" {
		return nil
	}

	clearScreen()
	paintTitleBar("              ลบ SSL TUNNEL               ")
	fmt.Println()

	if err := progress.Run([]progress.Step{
		{Label: "หยุด + ลบ SSL TUNNEL", Work: func() error {
			_ = systemctlRun("disable", "--now", ssltunnel.UnitName)
			_ = ssltunnel.RemoveUnit()
			_ = os.Remove(ssltunnel.CertFile)
			_ = os.Remove(ssltunnel.KeyFile)
			_ = os.Remove(ssltunnel.DBPath)
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println("\n" + cGrnBold + "ลบ SSL TUNNEL สำเร็จแล้ว" + cReset)
	waitEnter(r)
	return nil
}

// sslTunnelChangePort asks for a new port and restarts the tunnel.
func sslTunnelChangePort(r *bufio.Reader, cfg ssltunnel.Config) error {
	clearScreen()
	paintTitleBar("              เปลี่ยนพอร์ต SSL TUNNEL               ")
	fmt.Println()

	portStr, err := promptLineDefault(r, "พอร์ตใหม่สำหรับ SSL TUNNEL", strconv.Itoa(cfg.Port))
	if err != nil {
		return err
	}
	port, convErr := strconv.Atoi(strings.TrimSpace(portStr))
	if convErr != nil || port < 1 || port > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}
	if port == cfg.Port {
		fmt.Println("\n" + cYelBold + "พอร์ตเดิม — ไม่มีการเปลี่ยนแปลง" + cReset)
		waitEnter(r)
		return nil
	}
	// Skip checkPortFree on no-change; on actual change, the live tunnel
	// is still bound to the OLD port, so the NEW port should be free.
	if err := checkPortFree(port, "tcp"); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	cfg.Port = port
	if err := cfg.Save(); err != nil {
		return err
	}
	if err := ssltunnel.WriteUnit(cfg); err != nil {
		return err
	}
	if err := systemctlRun("restart", ssltunnel.UnitName); err != nil {
		fmt.Println("\n" + cYelBold + "คำเตือน: รีสตาร์ทไม่สำเร็จ: " + err.Error() + cReset)
	}
	fmt.Println("\n" + cGrnBold + fmt.Sprintf("เปลี่ยนพอร์ตเป็น %d สำเร็จ", port) + cReset)
	waitEnter(r)
	return nil
}
