// sslhmux_menu.go: TUI sub-menu for SSLH MULTIPLEX (conexao option 07).
//
// Mirrors the v1 sslh flow: install (auto-detect backends), manage (restart /
// uninstall / log). No external binary — hexplus itself is the multiplexer
// (hexplus sslhmux run, managed by systemd).

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
	"github.com/lolyhexey/hexplus/internal/sslhmux"
)

// sslhMuxMenu is the top-level router for option [07] in the conexao menu.
func sslhMuxMenu(r *bufio.Reader) error {
	for {
		clearScreen()
		cfg, _ := sslhmux.Load()
		installed := sslhmux.IsInstalled()

		if !installed {
			paintTitleBar("           จัดการ SSLH MULTIPLEX              ")
			fmt.Println()
			fmt.Printf("%s[!] พอร์ต 443 จะถูกใช้เป็นค่าเริ่มต้น%s\n\n", cYelBold, cReset)
			fmt.Printf("%sติดตั้ง SSLH MULTIPLEX?%s\n", cWhtBold, cReset)
			paintOptions([][2]string{
				{"1", "ติดตั้ง"},
				{"0", "ย้อนกลับ"},
			})
			fmt.Println()
			choice, err := menuPrompt(r)
			if err != nil {
				return err
			}
			switch strings.ToLower(strings.TrimSpace(choice)) {
			case "0", "00":
				return nil
			case "1", "01":
				if err := sslhMuxInstall(r); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			default:
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
			}
		} else {
			paintTitleBar("           จัดการ SSLH MULTIPLEX              ")
			fmt.Printf("\n%sพอร์ต%s: %s%d%s\n\n",
				cYelBold, cWhtBold, cGrnBold, cfg.Port, cReset)
			printLiveBackends()
			paintOptions([][2]string{
				{"1", "ลบ SSLH MULTIPLEX"},
				{"2", "รีสตาร์ท SSLH MULTIPLEX"},
				{"3", "ดู log SSLH MULTIPLEX"},
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
				if err := sslhMuxUninstall(r); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
					waitEnter(r)
				}
			case "2", "02":
				if err := systemctlRun("restart", sslhmux.UnitName); err != nil {
					fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				} else {
					fmt.Println("\n" + cGrnBold + "รีสตาร์ท SSLH MULTIPLEX สำเร็จ" + cReset)
				}
				waitEnter(r)
			case "3", "03":
				showUnitLog(r, sslhmux.UnitName)
			default:
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
			}
		}
	}
}

// printLiveBackends shows where each protocol is routed right now. The
// multiplexer reads these from the services' own configs on every
// connection, so there is nothing to refresh by hand.
func printLiveBackends() {
	live := sslhmux.Detect()
	fmt.Printf("%sปลายทางปัจจุบัน (อ่านจากการตั้งค่าจริงของแต่ละบริการ)%s\n", cYelBold, cReset)
	for _, b := range []struct{ label, addr string }{
		{"SSH     ", live.SSH},
		{"SSL     ", live.SSL},
		{"HTTP    ", live.HTTP},
		{"OPENVPN ", live.OpenVPN},
	} {
		addr := b.addr
		if addr == "" {
			addr = cRedBold + "ไม่ได้ติดตั้ง" + cReset
		}
		fmt.Printf("  %s%s%s→ %s%s\n", cWhtBold, b.label, cReset, cGrnBold, addr+cReset)
	}
	fmt.Println()
}

// sslhMuxInstall runs the full install flow: prompt port → auto-detect
// backends → save config → write unit → start service.
func sslhMuxInstall(r *bufio.Reader) error {
	clearScreen()
	paintTitleBar("           ติดตั้ง SSLH MULTIPLEX              ")
	fmt.Println()

	portStr, err := promptLineDefault(r, "พอร์ต SSLH MULTIPLEX", "443")
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

	// The daemon resolves its backends itself on every connection; the
	// detected addresses are saved only as a snapshot for older binaries.
	cfg := sslhmux.Detect()
	cfg.Port = port

	var listening bool
	if err := progress.Run([]progress.Step{
		{Label: "บันทึก config", Work: cfg.Save},
		{Label: "เขียน systemd unit", Work: func() error {
			return sslhmux.WriteUnit(cfg)
		}},
		{Label: "เริ่ม SSLH MULTIPLEX", Work: func() error {
			if err := systemctlRun("enable", "--now", sslhmux.UnitName); err != nil {
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
		fmt.Println(cGrnBold + "ติดตั้ง SSLH MULTIPLEX สำเร็จแล้ว !" + cYelBold + " พอร์ต: " + cWhtBold + strconv.Itoa(port) + cReset)
	} else {
		fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold + " SSLH MULTIPLEX เริ่มทำงานไม่สำเร็จ — ตรวจสอบ journalctl -u " + sslhmux.UnitName + cReset)
	}
	waitEnter(r)
	return nil
}

// sslhMuxUninstall stops + removes the service and config.
func sslhMuxUninstall(r *bufio.Reader) error {
	clearScreen()
	paintTitleBar("           ลบ SSLH MULTIPLEX              ")
	fmt.Print("\n" + cYelBold + "ต้องการลบ SSLH MULTIPLEX หรือไม่ " + cRedBold + "? " + cGrnBold + "[y/n]: " + cReset)
	line, _ := r.ReadString('\n')
	if !isYes(line) {
		return nil
	}

	clearScreen()
	paintTitleBar("           ลบ SSLH MULTIPLEX              ")
	fmt.Println()

	if err := progress.Run([]progress.Step{
		{Label: "หยุด + ลบ SSLH MULTIPLEX", Work: func() error {
			_ = systemctlRun("disable", "--now", sslhmux.UnitName)
			_ = sslhmux.RemoveUnit()
			_ = os.Remove(sslhmux.DBPath)
			return nil
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println("\n" + cGrnBold + "ลบ SSLH MULTIPLEX สำเร็จแล้ว" + cReset)
	waitEnter(r)
	return nil
}
