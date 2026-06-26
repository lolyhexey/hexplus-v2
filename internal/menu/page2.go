// page2.go: HEXPLUS v2 admin menu — "หน้าถัดไป" (Modulos/menu page 2).
//
// Why one file: v1 lumped these admin actions on a single screen and they
// share the same "type a 2-digit code, watch a prompt, return" rhythm.
// Keeping them together makes it easy to verify the numeric IDs (21-32)
// still line up byte-for-byte with what v1 customers' muscle memory
// expects when they upgrade.
//
// Numeric IDs are load-bearing. Reordering anything here will break
// muscle memory; add new options at the bottom, never reshuffle.

package menu

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/install"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/version"
)

// autoMenuFile is the profile.d hook the [26] toggle drops in/out to
// auto-launch the menu on every interactive login. Lives under profile.d
// because /etc/profile is shared with the distro and the operator may have
// edits there we shouldn't trample.
const (
	autoMenuFile   = "/etc/profile.d/hexplus-menu.sh"
	autoMenuScript = "[ -t 0 ] && hexplus menu\n"
	sshdConfigPath = "/etc/ssh/sshd_config"
	timeRebootCron = "/etc/cron.d/hexplus-reboot"
)

// runMainPage2 is the v1 page-2 main loop. Mirrors the structure of
// paintMainMenu + Run: clear, paint, read choice, dispatch, repeat.
func runMainPage2(r *bufio.Reader) error {
	for {
		paintPage2()
		choice, err := readChoice(r)
		if err != nil {
			return err
		}
		exit, err := dispatchPage2(choice, r)
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

// dispatchPage2 routes a single keystroke to its handler. The "exit" return
// signals runMainPage2 to bail to the main menu without waitEnter — used
// for the three handlers (23/27/28) where the binary or system state goes
// away under us.
//
// Pacing rule: handlers that print results and return normally do NOT
// waitEnter themselves; the wait happens here via handleWithWait. Stub
// handlers (21/22/31) call notImplemented which already waits internally,
// so we route them through plain calls without an extra wait.
func dispatchPage2(choice string, r *bufio.Reader) (bool, error) {
	switch choice {
	case "0", "00", "29":
		return true, nil

	// 21/22: addhost/delhost — v1 had the OpenVPN host-override system;
	// v2 hasn't ported it yet. Stubs handle their own waitEnter.
	case "21":
		return false, runAddHost(r)
	case "22":
		return false, runDelHost(r)

	case "23":
		// Reboot — don't waitEnter (system is going down); on error fall
		// back to the menu so the operator can see what happened.
		if err := runRebootSystem(r); err != nil {
			return false, err
		}
		return true, nil

	case "24":
		return false, handleWithWait(r, runRestartServices)
	case "25":
		return false, handleWithWait(r, runRootPassword)
	case "26":
		return false, handleWithWait(r, runAutoMenu)

	case "27":
		// Self-update replaces the running binary; we can't safely keep
		// looping in the old image, so bail to main + exit-flow.
		if err := runSelfUpdate(r); err != nil {
			return false, err
		}
		return true, nil

	case "28":
		// Uninstall removes /usr/local/bin/hexplus while we're still
		// running; same rationale as self-update.
		if err := runUninstall(r); err != nil {
			return false, err
		}
		return true, nil

	case "30":
		return false, handleWithWait(r, runEnableRoot)
	case "31":
		// Stub — notImplemented already waits.
		return false, runSetSpeed(r)
	case "32":
		return false, handleWithWait(r, runTimeReboot)

	default:
		fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง กรุณาเลือกตัวเลขจากเมนู" + cReset)
		waitEnter(r)
		return false, nil
	}
}

// handleWithWait runs fn and adds the standard "ENTER กลับสู่เมนูหลัก" wait
// regardless of outcome, mirroring v1's `read` after every sub-action.
func handleWithWait(r *bufio.Reader, fn func(*bufio.Reader) error) error {
	err := fn(r)
	if err != nil {
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
	}
	waitEnter(r)
	return nil
}

// paintPage2 mirrors paintMainMenu's banner + system info + grid layout
// using the page-2 option set. Spacing is `%-32s` like page 1 to keep the
// right-column indices vertically aligned.
func paintPage2() {
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

	// The [26] marker reflects whether the auto-menu profile hook is
	// installed. Computed here so the next render after toggling reflects
	// the change immediately.
	autoMark := markerOff()
	if _, err := os.Stat(autoMenuFile); err == nil {
		autoMark = markerOn()
	}
	autoLabel := "เปิดเมนู อัตโนมัติ " + autoMark

	// Two-column grid; right column has only 3 entries (30/31/32) and the
	// left runs 21-29 + 00. Stick to the page-1 colour pattern.
	grid := []struct {
		leftIdx, leftLabel   string
		rightIdx, rightLabel string
	}{
		{"21", "เพิ่มโฮสต์", "30", "เปิดผู้ใช้รูท"},
		{"22", "ลบโฮสต์", "31", "ตั้งความเร็วอินเทอร์เน็ต"},
		{"23", "รีบูตเซิร์ฟเวอร์ใหม่", "32", "ตั้งเวลารีบูตระบบ"},
		{"24", "รีบูตระบบใหม่", "", ""},
		{"25", "เปลี่ยนรหัสผ่านรูท", "", ""},
		{"26", autoLabel, "", ""},
		{"27", "อัพเดตสคริปต์", "", ""},
		{"28", "ถอนการติดตั้งสคริปต์", "", ""},
		{"29", "ย้อนกลับ <<<", "", ""},
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
	fmt.Printf("%s[%s00%s] %s• %s%s%s\n",
		cRedBold, cCyanBold, cRedBold,
		cWhtBold, cYelBold, "ออก", cReset)

	fmt.Println()
	printSep()
	fmt.Println()
}

// requireRoot is the standard "bail with red Thai message" guard the
// system-affecting handlers share. Returns nil when root, an error otherwise
// so the dispatcher prints + paces uniformly.
func requireRoot() error {
	if os.Geteuid() != 0 {
		return errors.New("ต้องรันด้วยสิทธิ์ root")
	}
	return nil
}

// -----------------------------------------------------------------------
// Sub-handlers
// -----------------------------------------------------------------------

// runAddHost stub: v1's addhost wired OpenVPN's `remote` directive list.
// v2 doesn't have the host-override system yet (see roadmap P6.x).
// TODO(v2.x): port the host file (~/Modulos/addhost) when the
// OpenVPN profile generator gets multi-host support.
func runAddHost(r *bufio.Reader) error {
	return notImplemented(r, "21 เพิ่มโฮสต์")
}

// runDelHost stub: paired with runAddHost; ports together.
// TODO(v2.x): see runAddHost.
func runDelHost(r *bufio.Reader) error {
	return notImplemented(r, "22 ลบโฮสต์")
}

// runRebootSystem (23): confirm + invoke /sbin/shutdown -r now. Falls back
// to /sbin/reboot for Alpine-style boxes that don't ship shutdown.
func runRebootSystem(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Print(cYelBold + "รีบูตเซิร์ฟเวอร์? " + cRedBold + "[y/N]: " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		fmt.Println(cYelBold + "ยกเลิก" + cReset)
		// Brief pause so the cancel message is readable before
		// dispatchPage2 redraws the menu underneath us.
		time.Sleep(1 * time.Second)
		return nil
	}

	fmt.Println(cGrnBold + "กำลังรีบูตระบบ..." + cReset)
	if err := exec.Command("/sbin/shutdown", "-r", "now").Run(); err == nil {
		return nil
	}
	// shutdown not found / failed — try reboot. Anything reaches stdout we
	// surface as the returned error.
	if err := exec.Command("/sbin/reboot").Run(); err != nil {
		return fmt.Errorf("รีบูตล้มเหลว: %w", err)
	}
	return nil
}

// runRestartServices (24): walks every service descriptor, restarts the
// ones whose systemd unit actually exists on this box, and prints
// per-service success/failure in Thai.
func runRestartServices(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Println(cGrnBold + "กำลังรีสตาร์ทบริการที่ติดตั้ง..." + cReset)
	fmt.Println()

	any := false
	for _, svc := range service.All() {
		st, _ := service.Status(svc)
		if !st.UnitExists {
			continue
		}
		any = true
		if err := service.Restart(svc); err != nil {
			fmt.Printf("%sรีสตาร์ท %s — ล้มเหลว: %s%s\n",
				cRedBold, svc.Name, err.Error(), cReset)
			continue
		}
		fmt.Printf("%sรีสตาร์ท %s — %sสำเร็จ%s\n",
			cWhtBold, svc.Name, cGrnBold, cReset)
	}
	if !any {
		fmt.Println(cYelBold + "ยังไม่มีบริการที่ติดตั้ง" + cReset)
	}
	return nil
}

// runRootPassword (25): pipe `root:NEWPASS` into chpasswd. Password is
// echoed because v1 always took it visibly — switching to a hidden prompt
// would surprise existing customers. Operators who care about shoulder
// surfing can `passwd root` from a shell instead.
func runRootPassword(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Print(cYelBold + "รหัสผ่านรูทใหม่: " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	pw := strings.TrimRight(line, "\r\n")
	if pw == "" {
		return errors.New("รหัสผ่านว่างเปล่า")
	}

	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader("root:" + pw + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("chpasswd: %w: %s", err, strings.TrimSpace(string(out)))
	}
	fmt.Println(cGrnBold + "ตั้งรหัสผ่านรูทสำเร็จ" + cReset)
	return nil
}

// runAutoMenu (26) toggles the profile.d hook that drops users into the
// HEXPLUS menu on login. Stat-then-act is fine: there's no concurrent
// admin clicking 26 at the same instant on a single VPS.
func runAutoMenu(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	if _, err := os.Stat(autoMenuFile); err == nil {
		if err := os.Remove(autoMenuFile); err != nil {
			return fmt.Errorf("ลบ %s: %w", autoMenuFile, err)
		}
		fmt.Println(cYelBold + "ปิดเมนูอัตโนมัติแล้ว " + markerOff() + cReset)
		return nil
	}
	if err := os.WriteFile(autoMenuFile, []byte(autoMenuScript), 0o755); err != nil {
		return fmt.Errorf("เขียน %s: %w", autoMenuFile, err)
	}
	fmt.Println(cGrnBold + "เปิดเมนูอัตโนมัติแล้ว " + markerOn() + cReset)
	return nil
}

// ghRelease / ghAsset are the minimum subset of the GitHub release JSON
// runSelfUpdate consumes. Keep loose-typed: extra fields ignored by
// encoding/json so future GH schema additions don't break us.
type ghRelease struct {
	TagName string    `json:"tag_name"`
	Assets  []ghAsset `json:"assets"`
}

type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
}

// runSelfUpdate (27 attscript): query GitHub releases, pick the asset
// matching the host's GOARCH, replace the running binary via atomic rename.
//
// Why atomic rename instead of in-place rewrite: Linux refuses write() to
// a busy executable text segment. rename() onto the same path is allowed
// because the kernel keeps the old inode open for the live process while
// new exec()s pick up the replacement.
func runSelfUpdate(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Println(cGrnBold + "กำลังตรวจสอบรุ่นล่าสุด..." + cReset)

	const releaseURL = "https://api.github.com/repos/lolyhexey/hexplus-v2/releases/latest"
	client := &http.Client{Timeout: 30 * time.Second}

	req, err := http.NewRequest(http.MethodGet, releaseURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("ขอข้อมูลรุ่นล้มเหลว: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("GitHub API ตอบกลับ HTTP %d", resp.StatusCode)
	}

	var rel ghRelease
	if err := json.NewDecoder(resp.Body).Decode(&rel); err != nil {
		return fmt.Errorf("ไม่สามารถอ่าน JSON: %w", err)
	}
	if rel.TagName == "" {
		return errors.New("ไม่พบ tag_name ในผลลัพธ์")
	}

	if rel.TagName == version.Version || rel.TagName == "v"+version.Version {
		fmt.Println(cGrnBold + "เป็นรุ่นล่าสุดอยู่แล้ว (" + rel.TagName + ")" + cReset)
		// Pause so the operator sees the result before re-paint.
		time.Sleep(2 * time.Second)
		return nil
	}

	// Pick the asset whose name contains the host arch. Linux-only by
	// design - v2 doesn't ship Windows / macOS builds.
	var asset *ghAsset
	for i := range rel.Assets {
		name := strings.ToLower(rel.Assets[i].Name)
		if strings.Contains(name, runtime.GOARCH) && strings.Contains(name, "linux") {
			asset = &rel.Assets[i]
			break
		}
	}
	if asset == nil {
		// Fall back to arch-only match (older release naming).
		for i := range rel.Assets {
			if strings.Contains(strings.ToLower(rel.Assets[i].Name), runtime.GOARCH) {
				asset = &rel.Assets[i]
				break
			}
		}
	}
	if asset == nil {
		return fmt.Errorf("ไม่พบ asset สำหรับ %s", runtime.GOARCH)
	}

	fmt.Println(cYelBold + "ดาวน์โหลด " + asset.Name + "..." + cReset)

	dlResp, err := client.Get(asset.BrowserDownloadURL)
	if err != nil {
		return fmt.Errorf("ดาวน์โหลดล้มเหลว: %w", err)
	}
	defer dlResp.Body.Close()
	if dlResp.StatusCode != http.StatusOK {
		return fmt.Errorf("ดาวน์โหลดตอบกลับ HTTP %d", dlResp.StatusCode)
	}

	// Resolve the self path first so we know which directory to drop the
	// temp file in — same-fs rename keeps atomicity guarantees.
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate self: %w", err)
	}
	if resolved, rerr := filepath.EvalSymlinks(self); rerr == nil {
		self = resolved
	}
	tmp := self + ".new"

	out, err := os.OpenFile(tmp, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return fmt.Errorf("เปิดไฟล์ %s: %w", tmp, err)
	}
	if _, err := io.Copy(out, dlResp.Body); err != nil {
		out.Close()
		_ = os.Remove(tmp)
		return fmt.Errorf("คัดลอกไฟล์: %w", err)
	}
	if err := out.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	// chmod again in case umask shaved the executable bit.
	if err := os.Chmod(tmp, 0o755); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, self); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename: %w", err)
	}

	fmt.Println()
	fmt.Println(cGrnBold + "อัพเดตเป็น " + rel.TagName + " สำเร็จ" + cReset)
	fmt.Println(cYelBold + "รัน 'hexplus' อีกครั้ง" + cReset)
	return nil
}

// runUninstall (28 delscript): confirm + call install.Uninstall(). The
// installer itself walks the service set; we just gate it on a y/N.
func runUninstall(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Print(cRedBold + "ถอนการติดตั้ง HEXPLUS? " + cYelBold + "[y/N]: " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	ans := strings.ToLower(strings.TrimSpace(line))
	if ans != "y" && ans != "yes" {
		fmt.Println(cYelBold + "ยกเลิก" + cReset)
		time.Sleep(1 * time.Second)
		// Not really "exit" semantically; signal back to the loop by
		// returning a sentinel? Simpler: redraw means staying — but our
		// dispatcher exits on success. Return a benign error so dispatch
		// shows it and stays in the menu.
		return errors.New("ผู้ใช้ยกเลิก")
	}
	if err := install.Uninstall(); err != nil {
		return err
	}
	fmt.Println(cGrnBold + "ถอนการติดตั้งสำเร็จ" + cReset)
	fmt.Println(cYelBold + "ลาก่อน" + cReset)
	time.Sleep(2 * time.Second)
	return nil
}

// runEnableRoot (30 changeroot): flip PermitRootLogin to yes in sshd_config
// and restart the SSH unit. We rewrite all matches (commented or not) and
// append a line only when none existed, so we don't accumulate duplicate
// directives across repeated invocations.
func runEnableRoot(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()

	data, err := os.ReadFile(sshdConfigPath)
	if err != nil {
		return fmt.Errorf("อ่าน %s: %w", sshdConfigPath, err)
	}

	lines := strings.Split(string(data), "\n")
	replaced := false
	for i, line := range lines {
		trimmed := strings.TrimLeft(line, " \t#")
		fields := strings.Fields(trimmed)
		if len(fields) >= 1 && fields[0] == "PermitRootLogin" {
			lines[i] = "PermitRootLogin yes"
			replaced = true
		}
	}
	if !replaced {
		// Append cleanly — trailing newline preserved.
		if len(lines) > 0 && lines[len(lines)-1] == "" {
			lines[len(lines)-1] = "PermitRootLogin yes"
			lines = append(lines, "")
		} else {
			lines = append(lines, "PermitRootLogin yes")
		}
	}
	if err := os.WriteFile(sshdConfigPath, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", sshdConfigPath, err)
	}

	// Try "ssh" first (Debian/Ubuntu unit), fall back to "sshd" (RHEL).
	if err := exec.Command("systemctl", "restart", "ssh").Run(); err != nil {
		if err := exec.Command("systemctl", "restart", "sshd").Run(); err != nil {
			fmt.Println(cYelBold + "อัปเดต sshd_config สำเร็จ แต่รีสตาร์ทบริการล้มเหลว — กรุณารัน 'systemctl restart sshd' ด้วยตนเอง" + cReset)
			return nil
		}
	}
	fmt.Println(cGrnBold + "เปิดผู้ใช้รูทผ่าน SSH สำเร็จ" + cReset)
	return nil
}

// runSetSpeed (31 stub): v1 used ethtool to negotiate link speed; we can't
// ship ethtool from a single-binary, so this stays a stub until we wrap
// the ethtool ioctls in Go.
// TODO(v2.x): wrap SIOCETHTOOL ioctls so we don't depend on ethtool(8).
func runSetSpeed(r *bufio.Reader) error {
	return notImplemented(r, "31 ตั้งความเร็วอินเทอร์เน็ต")
}

// runTimeReboot (32): drop a cron.d entry to reboot every N hours.
// We rewrite the whole file each time so adjusting N doesn't duplicate
// schedules.
func runTimeReboot(r *bufio.Reader) error {
	if err := requireRoot(); err != nil {
		return err
	}
	clearScreen()
	fmt.Print(cYelBold + "ตั้งเวลารีบูตทุก N ชั่วโมง (1-24): " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 1 || n > 24 {
		return errors.New("ต้องเป็นตัวเลข 1-24")
	}

	// /etc/cron.d entries require user field. 0 minute every N hours.
	body := fmt.Sprintf("SHELL=/bin/sh\nPATH=/usr/local/sbin:/usr/local/bin:/sbin:/bin:/usr/sbin:/usr/bin\n0 */%d * * * root /sbin/shutdown -r now\n", n)
	if err := os.WriteFile(timeRebootCron, []byte(body), 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", timeRebootCron, err)
	}
	fmt.Printf("%sตั้งเวลารีบูตทุก %d ชั่วโมง สำเร็จ%s\n", cGrnBold, n, cReset)
	return nil
}
