// v2ray.go: HEXPLUS menu entry point for the V2Ray web panel.
//
// Design: this file is a THIN wrapper around internal/panel. All the
// heavy lifting (config write, DB seed, systemd unit installation)
// lives in that package; this file just paints prompts, reads
// numbers, and calls the exported functions.

package menu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lolyhexey/hexplus/internal/panel"
	"github.com/lolyhexey/hexplus/internal/progress"
	"github.com/lolyhexey/hexplus/internal/service"
)

// runV2RayPanel is the option-33 handler wired into page2.go's
// dispatchPage2. Loops the sub-menu until the user chooses 09 to
// return.
func runV2RayPanel(r *bufio.Reader) error {
	for {
		paintV2RayMenu()
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		exit, err := dispatchV2Ray(choice, r)
		if err != nil {
			fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
			waitEnter(r)
			continue
		}
		if exit {
			return nil
		}
	}
}

func paintV2RayMenu() {
	clearScreen()
	printSep()
	printBanner()
	printSep()

	status := cRedBold + "○" + cReset + " ไม่ได้ติดตั้ง"
	installed := panel.IsInstalled()
	if installed {
		cfg, _, _ := panel.ShowInfo()
		unitMark := cRedBold + "◌" + cReset + " หยุด"
		if svc, ok := service.ByName("panel"); ok {
			if st, err := service.Status(svc); err == nil && st.ActiveState == "active" {
				unitMark = cGrnBold + "●" + cReset + " ทำงาน"
			}
		}
		status = fmt.Sprintf("%s◉%s ติดตั้งแล้ว | %s:%d%s | %s",
			cGrnBold, cReset, cWhtBold, cfg.Port, cReset, unitMark)
	}

	fmt.Println(cGrnBold + "V2RAY / XRAY WEB PANEL" + cReset)
	fmt.Println("  สถานะ: " + status)
	fmt.Println()
	fmt.Println(cYelBold + "หมายเหตุ:" + cReset + " panel ทำงานผ่าน HTTP ล้วนๆ")
	fmt.Println("  แนะนำใช้ผ่าน SSH tunnel หรือหลัง reverse proxy (Cloudflare/nginx)")
	fmt.Println("  ตัวอย่าง SSH tunnel: ssh -L 2053:localhost:2053 root@server-ip")
	fmt.Println()

	grid := []struct{ idx, label string }{
		{"01", "ติดตั้ง / รีเซ็ต V2Ray Panel"},
		{"02", "ถอนการติดตั้ง Panel"},
		{"03", "แสดง URL + Admin username"},
		{"04", "รีเซ็ตรหัสผ่าน Admin"},
		{"05", "เปลี่ยนพอร์ต Panel"},
		{"06", "Restart Panel + Xray"},
		{"07", "เปิดบริการ (enable + start)"},
		{"08", "ปิดบริการ (stop + disable)"},
		{"09", "ย้อนกลับ <<<"},
	}
	for _, row := range grid {
		fmt.Printf("%s[%s%s%s] %s• %s%s%s\n",
			cRedBold, cCyanBold, row.idx, cRedBold,
			cWhtBold, cYelBold, row.label, cReset)
	}
	fmt.Println()
	printSep()
	fmt.Println()
}

func dispatchV2Ray(choice string, r *bufio.Reader) (bool, error) {
	switch choice {
	case "0", "00", "9", "09":
		return true, nil
	case "1", "01":
		return false, handleWithWait(r, runPanelInstallMenu)
	case "2", "02":
		return false, handleWithWait(r, runPanelUninstallMenu)
	case "3", "03":
		return false, handleWithWait(r, runPanelShowMenu)
	case "4", "04":
		return false, handleWithWait(r, runPanelResetPasswordMenu)
	case "5", "05":
		return false, handleWithWait(r, runPanelChangePortMenu)
	case "6", "06":
		return false, handleWithWait(r, runPanelRestartMenu)
	case "7", "07":
		return false, handleWithWait(r, runPanelEnableMenu)
	case "8", "08":
		return false, handleWithWait(r, runPanelDisableMenu)
	default:
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return false, nil
	}
}

func runPanelInstallMenu(r *bufio.Reader) error {
	if os.Geteuid() != 0 {
		return fmt.Errorf("ต้อง root")
	}
	fmt.Print(cYelBold + "พอร์ต (Enter = 2053): " + cReset)
	line, _ := r.ReadString('\n')
	port := panel.DefaultPort
	if s := strings.TrimSpace(line); s != "" {
		p, err := strconv.Atoi(s)
		if err != nil || p <= 0 || p > 65535 {
			return fmt.Errorf("พอร์ตไม่ถูกต้อง")
		}
		port = p
	}

	keepDB := false
	if panel.IsInstalled() {
		fmt.Print(cYelBold + "พบข้อมูลเดิม เก็บ client/inbound เดิมไว้? (y/N): " + cReset)
		line, _ := r.ReadString('\n')
		keepDB = strings.EqualFold(strings.TrimSpace(line), "y")
	}

	// Run the install under a progress-bar animation so the operator
	// sees continuous feedback while xray extracts (~30MB), systemd
	// units get written, and both services enable+start.  The whole
	// thing typically takes 3-8 seconds — long enough that a silent
	// pause feels like a hang.
	var res panel.InstallResult
	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "เตรียม config + PKI", Work: func() error {
			var err error
			res, err = panel.Install(panel.InstallOptions{Port: port, KeepDB: keepDB})
			return err
		}},
		{Label: "เปิดใช้งานบริการ (xray + panel)", Work: func() error { return nil }},
	}); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "ติดตั้งสำเร็จ" + cReset)
	fmt.Printf("  พอร์ต:      %s%d%s\n", cWhtBold, res.Port, cReset)
	fmt.Printf("  URL:        %shttp://<server-ip>:%d%s/%s\n", cWhtBold, res.Port, res.URLPrefix, cReset)
	fmt.Printf("  Admin:      %s%s%s\n", cWhtBold, res.AdminUsername, cReset)
	fmt.Printf("  รหัสผ่าน:   %s%s%s\n", cYelBold, res.AdminPassword, cReset)
	if res.Started {
		fmt.Printf("  สถานะ:      %sทำงาน%s (xray + panel enabled + started อัตโนมัติ)\n", cGrnBold, cReset)
	} else {
		fmt.Printf("  สถานะ:      %sยังไม่ start%s — เปิดผ่านเมนู 07 หรือ systemctl\n", cYelBold, cReset)
	}
	fmt.Println()
	fmt.Println(cRedBold + "*** จดรหัสผ่านไว้ที่ไหนก็ได้ — จะไม่โชว์อีก ***" + cReset)
	return nil
}

func runPanelUninstallMenu(r *bufio.Reader) error {
	if !panel.IsInstalled() {
		fmt.Println(cYelBold + "ยังไม่ได้ติดตั้ง" + cReset)
		return nil
	}
	fmt.Print(cYelBold + "ลบข้อมูล DB ด้วยหรือไม่ (client, admin, sessions)? (y/N): " + cReset)
	line, _ := r.ReadString('\n')
	wipe := strings.EqualFold(strings.TrimSpace(line), "y")
	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "หยุดบริการ + ลบ systemd unit", Work: func() error { return panel.Uninstall(wipe) }},
	}); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "ถอนการติดตั้งสำเร็จ" + cReset)
	if !wipe {
		fmt.Println("  DB ยังอยู่ที่ /var/lib/hexplus/panel/panel.db")
	}
	return nil
}

func runPanelShowMenu(_ *bufio.Reader) error {
	if !panel.IsInstalled() {
		fmt.Println(cYelBold + "ยังไม่ได้ติดตั้ง — เลือก 01 ก่อน" + cReset)
		return nil
	}
	cfg, username, err := panel.ShowInfo()
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("  พอร์ต:  %s%d%s\n", cWhtBold, cfg.Port, cReset)
	fmt.Printf("  URL:    %shttp://<server-ip>:%d%s/%s\n", cWhtBold, cfg.Port, cfg.URLPrefix, cReset)
	if username != "" {
		fmt.Printf("  Admin:  %s%s%s (รหัสผ่านเข้ารหัสไว้ ใช้ 04 เพื่อรีเซ็ต)\n",
			cWhtBold, username, cReset)
	}
	return nil
}

func runPanelResetPasswordMenu(r *bufio.Reader) error {
	if !panel.IsInstalled() {
		return fmt.Errorf("ยังไม่ได้ติดตั้ง")
	}
	fmt.Print(cYelBold + "ชื่อ admin (Enter = admin): " + cReset)
	line, _ := r.ReadString('\n')
	user := strings.TrimSpace(line)
	if user == "" {
		user = "admin"
	}
	fmt.Print(cYelBold + "รหัสผ่านใหม่ (Enter = สุ่มให้): " + cReset)
	pwLine, _ := r.ReadString('\n')
	pw, err := panel.ResetAdminPassword(user, strings.TrimSpace(pwLine))
	if err != nil {
		return err
	}
	fmt.Println()
	fmt.Printf("รีเซ็ตแล้ว: %s%s%s / %s%s%s\n",
		cWhtBold, user, cReset, cYelBold, pw, cReset)
	return nil
}

func runPanelChangePortMenu(r *bufio.Reader) error {
	if !panel.IsInstalled() {
		return fmt.Errorf("ยังไม่ได้ติดตั้ง")
	}
	fmt.Print(cYelBold + "พอร์ตใหม่: " + cReset)
	line, _ := r.ReadString('\n')
	p, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || p <= 0 || p > 65535 {
		return fmt.Errorf("พอร์ตไม่ถูกต้อง")
	}
	fmt.Println()
	steps := []progress.Step{
		{Label: "อัปเดต panel.yaml", Work: func() error { return panel.ChangePort(p) }},
	}
	if svc, ok := service.ByName("panel"); ok {
		steps = append(steps, progress.Step{
			Label: fmt.Sprintf("Restart hexplus-panel (bind to :%d)", p),
			Work:  func() error { return service.Restart(svc) },
		})
	}
	if err := progress.Run(steps); err != nil {
		return err
	}
	fmt.Printf("%sเปลี่ยนพอร์ตเป็น %d เรียบร้อย%s\n", cGrnBold, p, cReset)
	return nil
}

func runPanelRestartMenu(_ *bufio.Reader) error {
	if !panel.IsInstalled() {
		return fmt.Errorf("ยังไม่ได้ติดตั้ง")
	}
	fmt.Println()
	var steps []progress.Step
	for _, name := range []string{"xray", "panel"} {
		if svc, ok := service.ByName(name); ok {
			steps = append(steps, progress.Step{
				Label: "Restart hexplus-" + svc.Name,
				Work:  func() error { return service.Restart(svc) },
			})
		}
	}
	if err := progress.Run(steps); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "Restart เรียบร้อย" + cReset)
	return nil
}

func runPanelEnableMenu(_ *bufio.Reader) error {
	if !panel.IsInstalled() {
		return fmt.Errorf("ยังไม่ได้ติดตั้ง — เลือก 01 ก่อน")
	}
	fmt.Println()
	var steps []progress.Step
	for _, name := range []string{"xray", "panel"} {
		if svc, ok := service.ByName(name); ok {
			steps = append(steps,
				progress.Step{Label: "Enable hexplus-" + svc.Name,
					Work: func() error { return service.Enable(svc) }},
				progress.Step{Label: "Start hexplus-" + svc.Name,
					Work: func() error { return service.Start(svc) }},
			)
		}
	}
	if err := progress.Run(steps); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "เปิดใช้งานเรียบร้อย" + cReset)
	return nil
}

func runPanelDisableMenu(_ *bufio.Reader) error {
	fmt.Println()
	var steps []progress.Step
	for _, name := range []string{"panel", "xray"} {
		if svc, ok := service.ByName(name); ok {
			steps = append(steps,
				progress.Step{Label: "Stop hexplus-" + svc.Name,
					Work: func() error { _ = service.Stop(svc); return nil }},
				progress.Step{Label: "Disable hexplus-" + svc.Name,
					Work: func() error { return service.Disable(svc) }},
			)
		}
	}
	if err := progress.Run(steps); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "ปิดบริการเรียบร้อย" + cReset)
	return nil
}
