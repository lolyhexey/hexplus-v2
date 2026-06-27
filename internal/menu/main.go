// main.go: the top-level menu replicating Modulos/menu byte-for-byte.
//
// The exact escape sequences come from Modulos/menu lines 246-393.
// Every newline, every color, every spacing is intentional - v1
// customers' muscle memory keys off the column positions of the
// option indices, so anything that shifts them will visibly break
// the look.

package menu

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// Run is the entry point cmd/hexplus calls when the operator types
// `hexplus menu` or just `hexplus` on an installed box.
//
// We loop forever reading numbers from stdin; the user exits explicitly
// via option 00 or by sending EOF (Ctrl+D).
func Run() error {
	ensureSSHConfig()
	r := bufio.NewReader(os.Stdin)
	for {
		if err := paintMainMenu(); err != nil {
			return err
		}
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		if exit, err := dispatchMain(choice, r); err != nil {
			fmt.Println(cRedBold+"[ผิดพลาด] "+cYelBold+err.Error(), cReset)
			waitEnter(r)
		} else if exit {
			return nil
		}
	}
}

// readChoice asks the standard v1 prompt and returns the trimmed input.
func readChoice(r *bufio.Reader) (string, error) {
	fmt.Print(cGrnBold + "CHOOSE OPTION " + cYelBold + "?" + cRedBold + "?" + cWhtBold + " : " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// waitEnter is the "kd ENTER กลับสู่เมนูหลัก" prompt v1 paints after
// every sub-action. Keeps the menu's pacing identical.
func waitEnter(r *bufio.Reader) {
	fmt.Print("\n" + cRedBold + "ENTER " + cYelBold + "กลับสู่เมนูหลัก !" + cGrnBold + "MENU!" + cReset)
	_, _ = r.ReadString('\n')
}

// paintMainMenu prints the v1 main screen. Field widths and spacing
// match Modulos/menu line-by-line; do not "improve" them.
func paintMainMenu() error {
	clearScreen()
	st := CollectStats()

	printSep()
	printBanner()
	printSep()

	fmt.Println(cGrnBold + "SYSTEM             MEMORY RAM        CPU ")
	fmt.Printf("%sOS: %s%-14s %sทั้งหมด:%s %s  %sCORE: %s%d%s\n",
		cRedBold, cWhtBold, st.OS, cRedBold, cWhtBold, st.RAMTotal,
		cRedBold, cWhtBold, st.CPUCores, cReset)
	fmt.Printf("%sเวลา: %s%s     %sการใช้งาน: %s%-8s%sการใช้งาน: %s%s%s\n",
		cRedBold, cWhtBold, st.Time, cRedBold, cWhtBold, st.MemUsedPct,
		cRedBold, cWhtBold, st.CPUUsedPct, cReset)
	printSep()
	fmt.Printf("%sออนไลน์:%s %-5d      %sหมดอายุ: %s%-5d     %sทั้งหมด: %s%d%s\n",
		cGrnBold, cWhtBold, st.OnlineNow, cRedBold, cWhtBold, st.ExpiredCt,
		cYelBold, cWhtBold, st.TotalUsers, cReset)
	printSep()
	fmt.Println()

	// Two-column grid. BANNER, VIRTUAL MEMORY, BAD VPN, ONLINE APP were
	// dropped in v2.2 (user feedback — see memory:feedback-dropped-features),
	// so the right column compacts to 8 surviving entries. Numeric IDs match
	// the dispatchMain switch below 1:1.

	// Read the limiter marker from disk so the indicator matches reality
	// (the same file sys.go's runLimiter toggles).
	limiterMark := cRedBold + "○" + cReset
	if _, statErr := os.Stat("/etc/security/limits.d/hexplus.conf"); statErr == nil {
		limiterMark = cGrnBold + "◉" + cReset
	}
	// v1 paints ">>>" with inter-character color gradient
	// (Modulos/menu line 392-393: red ">", yellow ">", green ">").
	arrow := cRedBold + ">" + cYelBold + ">" + cGrnBold + ">" + cReset

	grid := []struct {
		leftIdx, leftLabel   string
		rightIdx, rightLabel string
	}{
		{"01", "สร้างบัญชี ผู้ใช้", "11", "SPEED TEST"},
		{"02", "สร้างบัญชี ผุ้ใช้ทดลอง", "12", "กราฟเเสดงความเร็วเน็ต"},
		{"03", "ลบชื่อ ผู้ใช้", "13", "เพิ่มประสิทธิภาพ"},
		{"04", "เช็คผู้ใช้ ออนไลน์", "14", "BACKUP"},
		{"05", "เปลี่ยนวันหมดอายุ ผู้ใช้", "15", "จำกัดการเชื่อมต่อ " + limiterMark},
		{"06", "เปลี่ยนจำกัด การเชื่อมต่อ", "16", "ข้อมูล VPS " + arrow},
		{"07", "เปลี่ยนรหัสผ่าน ผู้ใช้", "17", "หน้าถัดไป " + arrow},
		{"08", "ลบผู้ใช้ที่ หมดอายุ", "", ""},
		{"09", "เช็คบัญชีทั้งหมด", "", ""},
		{"10", "โหมดฟังชั่น", "", ""},
	}
	for _, row := range grid {
		if row.rightIdx == "" {
			fmt.Printf("%s[%s%s%s] %s• %s%-32s%s\n",
				cRedBold, cCyanBold, row.leftIdx, cRedBold,
				cWhtBold, cYelBold, row.leftLabel, cReset)
			continue
		}
		fmt.Printf("%s[%s%s%s] %s• %s%-32s %s[%s%s%s] %s• %s%s%s\n",
			cRedBold, cCyanBold, row.leftIdx, cRedBold,
			cWhtBold, cYelBold, row.leftLabel,
			cRedBold, cCyanBold, row.rightIdx, cRedBold,
			cWhtBold, cYelBold, row.rightLabel, cReset)
	}
	// Final row: 00 ออก (alone — page-2 link is row 7 right side).
	fmt.Printf("%s[%s00%s] %s• %s%s%s\n",
		cRedBold, cCyanBold, cRedBold,
		cWhtBold, cYelBold, "ออก", cReset)

	fmt.Println()
	printSep()
	fmt.Println()
	return nil
}

// dispatchMain routes the user's choice to a handler. Returns (exit, err);
// exit==true causes Run() to return cleanly.
//
// For the first iteration of this rewrite, we wire option 10 (conexao =
// install/manage services + SOCKS proxies) to the new lazy-install flow,
// and the rest get a placeholder that says the feature is coming. v1
// customers who depend on options 1-9 (user mgmt) get them ported in
// the next patch in this series.
func dispatchMain(choice string, r *bufio.Reader) (bool, error) {
	switch choice {
	case "0", "00":
		fmt.Println(cRedBold + "ออก..." + cReset)
		return true, nil

	// User mgmt (01-09) — handled by users.go.
	case "1", "01":
		return false, runCreateUser(r)
	case "2", "02":
		return false, runCreateTrial(r)
	case "3", "03":
		return false, runRemoveUser(r)
	case "4", "04":
		return false, runSSHMonitor(r)
	case "5", "05":
		return false, runChangeExpiry(r)
	case "6", "06":
		return false, runChangeLimit(r)
	case "7", "07":
		return false, runChangePassword(r)
	case "8", "08":
		return false, runCleanExpired(r)
	case "9", "09":
		return false, runListUsers(r)

	// โหมดฟังชั่น (services + SOCKS) — handled by conexao.go.
	case "10":
		return false, runConexao(r)

	// System tools (11-16) — handled by sys.go.
	case "11":
		return false, runSpeedTest(r)
	case "12":
		return false, runBWChart(r)
	case "13":
		return false, runOptimize(r)
	case "14":
		return false, runBackup(r)
	case "15":
		return false, runLimiter(r)
	case "16":
		return false, runVPSInfo(r)

	// Page 2 (admin) — handled by page2.go.
	case "17":
		return false, runMainPage2(r)

	default:
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง กรุณาเลือกตัวเลขจากเมนู" + cReset)
		waitEnter(r)
		return false, nil
	}
}

// notImplemented is the temporary stub for menu options that v1 has
// but v2 hasn't ported yet. Keeps the menu rendering exact while we
// fill in the backends.
func notImplemented(r *bufio.Reader, opt string) error {
	clearScreen()
	fmt.Println(cYelBold + "ตัวเลือก " + cCyanBold + opt + cYelBold + " กำลังทำใน HEXPLUS v2")
	fmt.Println("ฟีเจอร์นี้จะมาในรุ่นถัดไป (v2.x)")
	fmt.Println()
	fmt.Println("ใช้คำสั่ง CLI ระหว่างนี้ได้ที่:")
	fmt.Println("  hexplus user add/list/remove/export")
	fmt.Println("  hexplus proxy add/list/remove")
	fmt.Println("  hexplus service start/stop/restart")
	fmt.Println("  hexplus pki init/status")
	fmt.Print(cReset)
	waitEnter(r)
	return nil
}
