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
	"fmt"

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
			notImplemented(r, "10.4 (SOCKS proxies)")
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
		paintServiceActions(st)
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

func paintServiceActions(st service.State) {
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
			{"9", "ถอนการติดตั้ง"},
		}
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
