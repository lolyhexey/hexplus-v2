// sys.go: system-tools menu sub-flows (options 11-16).
//
// These are read-only inspections plus a couple of self-config writes
// (sysctl tuning, PAM maxlogins). Everything uses stdlib only — v1 had
// these as separate bash scripts (Modulos/speedtest, bw_chart, optimize,
// etc.); we collapse them into the single hexplus binary so a fresh box
// gets all the v1 ergonomics with no extra scripts to install.

package menu

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
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
)

// ---------------------------------------------------------------------
// shared helpers (file-local; package-level helpers live in users.go)
// ---------------------------------------------------------------------

// sysHeader paints the v1 white-on-blue title bar used by every system
// sub-flow. Mirrors userHeader() but kept separate to avoid leaking that
// helper out of the user-management file.
func sysHeader(label string) {
	clearScreen()
	printSep()
	fmt.Printf("\033[44;1;37m            %s            \033[0m\n", label)
	printSep()
	fmt.Println()
}

// ensureRootMenu prints the Thai "must be root" line and returns false if
// the process is not running as uid 0. Used by Optimize / Limiter which
// touch /etc/sysctl.d and /etc/security. Distinct from page2.go's
// requireRoot() which returns an error for dispatcher-level handling.
func ensureRootMenu(r *bufio.Reader) bool {
	if os.Geteuid() == 0 {
		return true
	}
	fmt.Println(cRedBold + "ต้องรันด้วยสิทธิ์ root (ใช้ sudo)" + cReset)
	waitEnter(r)
	return false
}

// ---------------------------------------------------------------------
// 11 SPEED TEST
// ---------------------------------------------------------------------

// runSpeedTest measures latency to ipv4.icanhazip.com (HTTP GET RTT) and
// downloads a 10 MB file from speed.hetzner.de to estimate throughput.
// Both endpoints are unauthenticated public benchmark services.
func runSpeedTest(r *bufio.Reader) error {
	sysHeader("ทดสอบความเร็วเซิร์ฟเวอร์")

	client := &http.Client{Timeout: 60 * time.Second}

	// Ping-style RTT: time to first byte of a tiny endpoint.
	fmt.Println(cGrnBold + "กำลังวัด PING ..." + cReset)
	pingMs := ""
	t0 := time.Now()
	resp, err := client.Get("http://ipv4.icanhazip.com")
	if err != nil {
		pingMs = "ผิดพลาด: " + err.Error()
	} else {
		_, _ = io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		pingMs = fmt.Sprintf("%.0f ms", float64(time.Since(t0).Microseconds())/1000.0)
	}

	// Throughput: 10 MB blob from speed.hetzner.de.
	fmt.Println(cGrnBold + "กำลังวัด DOWNLOAD ..." + cReset)
	mbps := ""
	t1 := time.Now()
	resp2, err := client.Get("http://speed.hetzner.de/10MB.bin")
	if err != nil {
		mbps = "ผิดพลาด: " + err.Error()
	} else {
		n, copyErr := io.Copy(io.Discard, resp2.Body)
		resp2.Body.Close()
		dur := time.Since(t1).Seconds()
		if copyErr != nil {
			mbps = "ผิดพลาด: " + copyErr.Error()
		} else if dur > 0 && n > 0 {
			// bytes -> bits -> megabits
			mbps = fmt.Sprintf("%.2f Mbps", float64(n)*8/(dur*1_000_000))
		} else {
			mbps = "ไม่สามารถวัดได้"
		}
	}

	printSep()
	fmt.Printf("%sPING:%s     %s%s%s\n", cGrnBold, cReset, cWhtBold, pingMs, cReset)
	fmt.Printf("%sDOWNLOAD:%s %s%s%s\n", cGrnBold, cReset, cWhtBold, mbps, cReset)
	printSep()

	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 12 BW chart — live RX/TX on the default interface for 15 s
// ---------------------------------------------------------------------

// defaultIface returns the interface name with the default route
// (destination 00000000 in /proc/net/route). On non-Linux or missing
// /proc, returns "" — caller handles that.
func defaultIface() string {
	f, err := os.Open("/proc/net/route")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	// Skip header.
	if !sc.Scan() {
		return ""
	}
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		if fields[1] == "00000000" {
			return fields[0]
		}
	}
	return ""
}

// ifaceBytes returns (rx, tx) from /proc/net/dev for the named iface.
// Returns (0, 0, error) on parse failure or unknown iface.
func ifaceBytes(name string) (uint64, uint64, error) {
	f, err := os.Open("/proc/net/dev")
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	prefix := name + ":"
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, prefix) {
			continue
		}
		// "iface: rx_bytes rx_packets ... tx_bytes tx_packets ..."
		// After splitting on ":", first token after is rx_bytes; tx_bytes
		// is the 9th field overall on the right side.
		rest := strings.Fields(line[len(prefix):])
		if len(rest) < 16 {
			return 0, 0, fmt.Errorf("malformed /proc/net/dev row")
		}
		rx, err1 := strconv.ParseUint(rest[0], 10, 64)
		tx, err2 := strconv.ParseUint(rest[8], 10, 64)
		if err1 != nil || err2 != nil {
			return 0, 0, fmt.Errorf("malformed counters")
		}
		return rx, tx, nil
	}
	return 0, 0, fmt.Errorf("iface %s not found", name)
}

func runBWChart(r *bufio.Reader) error {
	sysHeader("กราฟแสดงความเร็วเน็ต (15 วินาที)")

	iface := defaultIface()
	if iface == "" {
		fmt.Println(cRedBold + "ไม่พบอินเทอร์เฟซเครือข่ายหลัก" + cReset)
		waitEnter(r)
		return nil
	}
	fmt.Printf("%sอินเทอร์เฟซ:%s %s%s%s\n\n", cGrnBold, cReset, cWhtBold, iface, cReset)

	fmt.Printf("%s%-10s %-12s %-12s%s\n", cGrnBold, "เวลา", "RX Mbps", "TX Mbps", cReset)
	printSep()

	prevRx, prevTx, err := ifaceBytes(iface)
	if err != nil {
		fmt.Println(cRedBold + "อ่าน /proc/net/dev ไม่สำเร็จ: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	for i := 1; i <= 15; i++ {
		time.Sleep(1 * time.Second)
		rx, tx, err := ifaceBytes(iface)
		if err != nil {
			fmt.Println(cRedBold + "อ่าน /proc/net/dev ไม่สำเร็จ: " + err.Error() + cReset)
			break
		}
		dRx := float64(rx-prevRx) * 8 / 1_000_000
		dTx := float64(tx-prevTx) * 8 / 1_000_000
		fmt.Printf("%s%-10s %s%-12.2f %-12.2f%s\n",
			cWhtBold, time.Now().Format("15:04:05"),
			cWhtBold, dRx, dTx, cReset)
		prevRx, prevTx = rx, tx
		_ = i
	}

	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 13 เพิ่มประสิทธิภาพ — sysctl tuning for VPN throughput
// ---------------------------------------------------------------------

// optimizeSysctl is the canonical content of /etc/sysctl.d/99-hexplus.conf.
// Values are the same ones v1's Modulos/optimize wrote — modern Linux
// defaults assume desktop traffic, these widen TCP buffers + enable BBR
// for the heavier VPN/proxy mix HEXPLUS boxes carry.
const optimizeSysctl = `# HEXPLUS v2 — VPN-friendly kernel tuning.
# Generated by 'hexplus' menu option 13. Safe to re-run; overwrites.
net.ipv4.ip_forward = 1
net.ipv4.tcp_keepalive_time = 600
net.core.rmem_max = 16777216
net.core.wmem_max = 16777216
net.ipv4.tcp_rmem = 4096 87380 16777216
net.ipv4.tcp_wmem = 4096 65536 16777216
net.ipv4.tcp_congestion_control = bbr
`

func runOptimize(r *bufio.Reader) error {
	sysHeader("เพิ่มประสิทธิภาพระบบ")

	if !ensureRootMenu(r) {
		return nil
	}

	path := "/etc/sysctl.d/99-hexplus.conf"
	if err := os.WriteFile(path, []byte(optimizeSysctl), 0o644); err != nil {
		fmt.Println(cRedBold + "เขียนไฟล์ไม่สำเร็จ: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	cmd := exec.Command("sysctl", "-p", path)
	out, err := cmd.CombinedOutput()
	if len(out) > 0 {
		fmt.Print(cWhtBold + string(out) + cReset)
	}
	if err != nil {
		fmt.Println(cRedBold + "sysctl -p ผิดพลาด: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	fmt.Println(cGrnBold + "เพิ่มประสิทธิภาพระบบ สำเร็จ" + cReset)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 14 BACKUP — tar.gz of HEXPLUS state + service configs
// ---------------------------------------------------------------------

// backupSources is the set of directories rolled into the tar.gz. Each
// is skipped silently if missing (e.g. a box that hasn't installed
// OpenVPN yet has no /etc/openvpn).
var backupSources = []string{
	"/var/lib/hexplus",
	"/etc/openvpn",
	"/etc/squid",
	"/etc/dropbear",
}

// addDirToTar walks dir and writes every regular file + dir entry into
// the open tar.Writer. Symlinks are stored as symlinks; sockets/devices
// are skipped (backup is for plain config + state).
func addDirToTar(tw *tar.Writer, root string) error {
	return filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		// Build header.
		var link string
		if info.Mode()&os.ModeSymlink != 0 {
			t, lerr := os.Readlink(p)
			if lerr != nil {
				return lerr
			}
			link = t
		}
		hdr, herr := tar.FileInfoHeader(info, link)
		if herr != nil {
			return herr
		}
		// Use absolute path inside the tar so restore is unambiguous.
		hdr.Name = strings.TrimPrefix(p, "/")
		if info.IsDir() {
			hdr.Name += "/"
		}
		if werr := tw.WriteHeader(hdr); werr != nil {
			return werr
		}
		// Skip body for non-regular files (dirs, symlinks already in header).
		if !info.Mode().IsRegular() {
			return nil
		}
		f, oerr := os.Open(p)
		if oerr != nil {
			// Read-restricted file: log + continue rather than abort.
			return nil
		}
		defer f.Close()
		_, cerr := io.Copy(tw, f)
		return cerr
	})
}

func runBackup(r *bufio.Reader) error {
	sysHeader("BACKUP ระบบ HEXPLUS")

	now := time.Now()
	base := "/root/hexplus-backup-" + now.Format("2006-01-02") + ".tar.gz"
	out := base
	if _, err := os.Stat(out); err == nil {
		// Already taken today — disambiguate with HH:MM:SS.
		out = "/root/hexplus-backup-" + now.Format("2006-01-02") + "_" + now.Format("150405") + ".tar.gz"
	}

	f, err := os.Create(out)
	if err != nil {
		fmt.Println(cRedBold + "สร้างไฟล์สำรองไม่สำเร็จ: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	defer f.Close()

	gz := gzip.NewWriter(f)
	tw := tar.NewWriter(gz)

	included := 0
	for _, dir := range backupSources {
		if _, err := os.Stat(dir); err != nil {
			continue
		}
		fmt.Printf("%sรวม:%s %s%s%s\n", cGrnBold, cReset, cWhtBold, dir, cReset)
		if err := addDirToTar(tw, dir); err != nil {
			fmt.Println(cRedBold + "เกิดข้อผิดพลาดขณะรวม " + dir + ": " + err.Error() + cReset)
		}
		included++
	}

	if err := tw.Close(); err != nil {
		fmt.Println(cRedBold + "ปิด tar ไม่สำเร็จ: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	if err := gz.Close(); err != nil {
		fmt.Println(cRedBold + "ปิด gzip ไม่สำเร็จ: " + err.Error() + cReset)
		waitEnter(r)
		return nil
	}

	if included == 0 {
		fmt.Println(cYelBold + "ไม่พบไดเรกทอรีให้สำรอง" + cReset)
	} else {
		fmt.Printf("\n%sสำรองข้อมูลสำเร็จ:%s %s%s%s\n",
			cGrnBold, cReset, cWhtBold, out, cReset)
	}

	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 15 LIMITER — PAM maxlogins via /etc/security/limits.d
// ---------------------------------------------------------------------

const limiterPath = "/etc/security/limits.d/hexplus.conf"

func runLimiter(r *bufio.Reader) error {
	sysHeader("จำกัดการเชื่อมต่อพร้อมกัน")

	if !ensureRootMenu(r) {
		return nil
	}

	if _, err := os.Stat(limiterPath); err == nil {
		// Currently enabled — offer to remove.
		fmt.Println(cGrnBold + "สถานะปัจจุบัน: เปิดอยู่" + cReset)
		ans, err := readLine(r, "ปิดการจำกัดเซสชั่น? [y/N]")
		if err != nil {
			return err
		}
		if strings.EqualFold(ans, "y") || strings.EqualFold(ans, "yes") {
			if rerr := os.Remove(limiterPath); rerr != nil {
				fmt.Println(cRedBold + "ลบไฟล์ไม่สำเร็จ: " + rerr.Error() + cReset)
				waitEnter(r)
				return nil
			}
			fmt.Println(cGrnBold + "ปิดการจำกัดเซสชั่นแล้ว" + cReset)
		} else {
			fmt.Println(cYelBold + "ไม่มีการเปลี่ยนแปลง" + cReset)
		}
	} else {
		// Currently disabled — prompt for N.
		fmt.Println(cYelBold + "สถานะปัจจุบัน: ปิดอยู่" + cReset)
		nStr, err := readLine(r, "จำกัดเซสชั่นพร้อมกัน:")
		if err != nil {
			return err
		}
		n, perr := strconv.Atoi(nStr)
		if perr != nil || n < 1 || n > 10 {
			fmt.Println(cRedBold + "จำนวนไม่ถูกต้อง: ต้องเป็นตัวเลข 1-10" + cReset)
			waitEnter(r)
			return nil
		}
		content := fmt.Sprintf("# HEXPLUS v2 — PAM maxlogins cap.\n* hard maxlogins %d\n", n)
		if werr := os.WriteFile(limiterPath, []byte(content), 0o644); werr != nil {
			fmt.Println(cRedBold + "เขียนไฟล์ไม่สำเร็จ: " + werr.Error() + cReset)
			waitEnter(r)
			return nil
		}
		fmt.Printf("%sเปิดการจำกัดเซสชั่น:%s %s%d เซสชั่นต่อผู้ใช้%s\n",
			cGrnBold, cReset, cWhtBold, n, cReset)
	}

	// Post-change status line.
	if _, err := os.Stat(limiterPath); err == nil {
		fmt.Println(cGrnBold + "สถานะใหม่: เปิดอยู่" + cReset)
	} else {
		fmt.Println(cYelBold + "สถานะใหม่: ปิดอยู่" + cReset)
	}

	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 16 ข้อมูล VPS — read-only system info screen
// ---------------------------------------------------------------------

// osPrettyName parses /etc/os-release PRETTY_NAME=... (handles quoted
// and unquoted forms). Falls back to "" when unavailable.
func osPrettyName() string {
	f, err := os.Open("/etc/os-release")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			v := strings.TrimPrefix(line, "PRETTY_NAME=")
			v = strings.Trim(v, `"`)
			return v
		}
	}
	return ""
}

// kernelRelease shells to `uname -r` — /proc/version has it but with a
// lot of extra build-date noise the bash menu didn't show.
func kernelRelease() string {
	out, err := exec.Command("uname", "-r").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// cpuModelName returns the first "model name" line from /proc/cpuinfo
// (all cores share the same name on every box v1 supports).
func cpuModelName() string {
	f, err := os.Open("/proc/cpuinfo")
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "model name") {
			parts := strings.SplitN(line, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

// memTotalHuman returns the "total" column from `free -h` Mem: row.
func memTotalHuman() string {
	out, err := exec.Command("free", "-h").Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(line, "Mem:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				return fields[1]
			}
		}
	}
	return ""
}

// diskRootHuman returns "used/total (pct)" for /. Uses `df -h /` and
// reads the last non-header row (df may wrap long device names onto
// two lines for ZFS pools etc.).
func diskRootHuman() string {
	out, err := exec.Command("df", "-h", "/").Output()
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimRight(string(out), "\n"), "\n")
	if len(lines) < 2 {
		return ""
	}
	last := lines[len(lines)-1]
	fields := strings.Fields(last)
	// df row: Filesystem Size Used Avail Use% Mounted-on
	if len(fields) >= 5 {
		return fmt.Sprintf("%s / %s (%s)", fields[2], fields[1], fields[4])
	}
	return ""
}

// publicIP reads /etc/IP if the v1/v2 install dropped one there.
// Returns empty otherwise — we don't reach out to the network from a
// menu option that's labelled read-only.
func publicIP() string {
	data, err := os.ReadFile("/etc/IP")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

// uptimeHuman parses /proc/uptime (seconds since boot) and formats
// "Xd Yh Zm".
func uptimeHuman() string {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return ""
	}
	sec, err := strconv.ParseFloat(fields[0], 64)
	if err != nil {
		return ""
	}
	total := int64(sec)
	days := total / 86400
	hours := (total % 86400) / 3600
	mins := (total % 3600) / 60
	return fmt.Sprintf("%dd %dh %dm", days, hours, mins)
}

func runVPSInfo(r *bufio.Reader) error {
	sysHeader("ข้อมูล VPS")

	hostname, _ := os.Hostname()
	cpuModel := cpuModelName()
	cpuLine := cpuModel
	if cpuLine == "" {
		cpuLine = fmt.Sprintf("%d cores", runtime.NumCPU())
	} else {
		cpuLine = fmt.Sprintf("%s (%d cores)", cpuModel, runtime.NumCPU())
	}

	rows := []struct {
		label, value string
	}{
		{"โฮสต์:", hostname},
		{"ระบบ:", osPrettyName()},
		{"เคอร์เนล:", kernelRelease()},
		{"สถาปัตยกรรม:", runtime.GOARCH},
		{"ซีพียู:", cpuLine},
		{"หน่วยความจำ:", memTotalHuman()},
		{"ดิสก์ /:", diskRootHuman()},
		{"ที่อยู่ IP:", publicIP()},
		{"เวลาทำงาน:", uptimeHuman()},
	}
	for _, row := range rows {
		fmt.Printf("%s%-16s%s %s%s%s\n",
			cGrnBold, row.label, cReset,
			cWhtBold, row.value, cReset)
	}

	printSep()
	waitEnter(r)
	return nil
}
