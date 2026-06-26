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

	// The 21-item grid. Two columns, indices 01..10 on the left, 11..21
	// on the right (with 21 = "หน้าถัดไป"). v1 paints each row as one
	// echo -e with embedded \n; we use Printf the same way.
	grid := []struct{ left, right string }{
		{"สร้างบัญชี ผู้ใช้", "SPEED TEST"},
		{"สร้างบัญชี ผุ้ใช้ทดลอง", "BANNER"},
		{"ลบชื่อ ผู้ใช้", "กราฟเเสดงความเร็วเน็ต"},
		{"เช็คผู้ใช้ ออนไลน์", "เพิ่มประสิทธิภาพ"},
		{"เปลี่ยนวันหมดอายุ ผู้ใช้", "BACKUP"},
		{"เปลี่ยนจำกัด การเชื่อมต่อ", "VIRTUAL MEMORY"},
		{"เปลี่ยนรหัสผ่าน ผู้ใช้", "จำกัดการเชื่อมต่อ ○"},
		{"ลบผู้ใช้ที่ หมดอายุ", "BAD VPN ○"},
		{"เช็คบัญชีทั้งหมด", "ONLINE APP ○"},
		{"โหมดฟังชั่น", "ข้อมูล VPS >>>"},
	}
	for i, row := range grid {
		leftIdx := fmt.Sprintf("%02d", i+1)
		rightIdx := fmt.Sprintf("%02d", i+11)
		fmt.Printf("%s[%s%s%s] %s• %s%-32s %s[%s%s%s] %s• %s%s%s\n",
			cRedBold, cCyanBold, leftIdx, cRedBold,
			cWhtBold, cYelBold, row.left,
			cRedBold, cCyanBold, rightIdx, cRedBold,
			cWhtBold, cYelBold, row.right, cReset)
	}
	// Last row: 00 ออก / 21 หน้าถัดไป  (v1 actually puts หน้าถัดไป at 21,
	// but our grid above stopped at index 20 = "ข้อมูล VPS". Render 00 +
	// the "หน้าถัดไป" as the same final line.)
	fmt.Printf("%s[%s00%s] %s• %s%-32s %s[%s21%s] %s• %s%s%s\n",
		cRedBold, cCyanBold, cRedBold,
		cWhtBold, cYelBold, "ออก",
		cRedBold, cCyanBold, cRedBold,
		cWhtBold, cYelBold, "หน้าถัดไป >>>", cReset)

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
	case "10":
		return false, runConexao(r)
	case "21":
		// Page 2 - the system-admin grid (addhost/delhost/reboot/etc.)
		return false, runMainPage2(r)
	case "1", "01", "2", "02", "3", "03", "4", "04", "5", "05",
		"6", "06", "7", "07", "8", "08", "9", "09",
		"11", "12", "13", "14", "15", "16", "17", "18", "19", "20":
		return false, notImplemented(r, choice)
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

// runMainPage2 is the v1 second page (addhost/delhost/reboot/...). For
// now it's also a placeholder.
func runMainPage2(r *bufio.Reader) error {
	return notImplemented(r, "21")
}
