// users.go: user-management menu sub-flows (options 01-09).
//
// Each function takes the menu's stdin reader and drives a Thai-labeled
// prompt sequence that ends in a call into internal/user.*. v1 customers
// type a number, walk through prompts, get an .ovpn file back.
//
// Prompt strings and the surrounding ANSI styling are copied from the
// Modulos/criarusuario, criarteste, remover, mudardata, alterarlimite,
// alterarsenha, expcleaner, infousers, sshmonitor bash scripts so v1
// muscle memory transfers cleanly. Don't rewrite them into "better"
// English/Thai — sellers screenshot the prompts to teach customers.

package menu

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/user"
)

// ---------------------------------------------------------------------
// shared helpers
// ---------------------------------------------------------------------

// userHeader paints the v1 white-on-blue title bar shown above every
// user-management form ("\E[44;1;37m   <label>   \E[0m").
func userHeader(label string) {
	clearScreen()
	printSep()
	fmt.Printf("\033[44;1;37m            %s            \033[0m\n", label)
	printSep()
	fmt.Println()
}

// readLine prompts in v1's green-label / white-input style and returns
// the trimmed reply.
func readLine(r *bufio.Reader, label string) (string, error) {
	fmt.Print(cGrnBold + label + cWhtBold + " ")
	line, err := r.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

// errLine prints v1's "[ผิดพลาด] <msg>" diagnostic.
func errLine(msg string) {
	fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " " + msg + cReset)
}

// okLine prints a green success line.
func okLine(msg string) {
	fmt.Println(cGrnBold + msg + cReset)
}

// defaultRemoteHost mirrors cmd/hexplus's helper of the same name. The
// menu can't reach into cmd/, so we keep a small copy here. /etc/IP is
// written by v1 install scripts (and v2 install when present).
func defaultRemoteHost() string {
	if data, err := os.ReadFile("/etc/IP"); err == nil {
		ip := strings.TrimSpace(string(data))
		if ip != "" {
			return ip
		}
	}
	return "127.0.0.1"
}

// listAndPick paints the numbered user list (v1's "[NN] - user" rows)
// and waits for the operator to type a number. Returns the selected
// Record. If the operator types nothing, returns an error matching
// v1's "ไม่ได้เลือกหมายเลขผู้ใช้" wording.
func listAndPick(r *bufio.Reader, prompt string) (user.Record, error) {
	records := systemUsers()
	if len(records) == 0 {
		return user.Record{}, fmt.Errorf("ยังไม่มีผู้ใช้ในระบบ — สร้างบัญชีก่อน")
	}
	for i, rec := range records {
		fmt.Printf("%s[%s%02d%s] %s- %s%s%s\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cWhtBold, cGrnBold, rec.Name, cReset)
	}
	fmt.Println()
	pick, err := readLine(r, prompt)
	if err != nil {
		return user.Record{}, err
	}
	if pick == "" {
		return user.Record{}, fmt.Errorf("ไม่ได้เลือกหมายเลขผู้ใช้ — กรุณาพิมพ์หมายเลข 1-%d", len(records))
	}
	idx, err := strconv.Atoi(pick)
	if err != nil || idx < 1 || idx > len(records) {
		return user.Record{}, fmt.Errorf("หมายเลข %q ไม่ตรงกับผู้ใช้ในรายการ — กรุณาพิมพ์หมายเลข 1-%d", pick, len(records))
	}
	return records[idx-1], nil
}

// systemUsers returns all non-root system accounts (UID ≥ 1000, ≠ nobody)
// from /etc/passwd, the same source v1 uses for every user-management
// screen. Metadata (limit) is merged from:
//  1. /var/lib/hexplus/users.json  — v2 DB (a Limit of 0 there falls through)
//  2. /root/usuarios.db            — v1 DB (format: "name limit\n")
//  3. otherwise 0 = no limit
//
// The same precedence as the OpenVPN device-limit guard (ovpnguard.limitFor),
// so the screens show the limit that is actually enforced.
func systemUsers() []user.Record {
	raw, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return nil
	}
	var names []string
	for _, line := range strings.Split(string(raw), "\n") {
		f := strings.SplitN(line, ":", 4)
		if len(f) < 4 || f[0] == "" || f[0] == "nobody" {
			continue
		}
		uid, err := strconv.Atoi(f[2])
		if err != nil || uid < 1000 {
			continue
		}
		names = append(names, f[0])
	}
	sort.Strings(names)

	db, _ := user.Load()

	v1Limits := map[string]int{}
	if b, err := os.ReadFile("/root/usuarios.db"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			flds := strings.Fields(l)
			if len(flds) >= 2 {
				if n, err := strconv.Atoi(flds[1]); err == nil {
					v1Limits[flds[0]] = n
				}
			}
		}
	}

	out := make([]user.Record, 0, len(names))
	for _, name := range names {
		if rec, ok := db.Users[name]; ok {
			// "Change expiry" creates a users.json row with Limit 0 for a
			// user that only had a v1 limit; that row must not hide it.
			if rec.Limit <= 0 && v1Limits[name] > 0 {
				rec.Limit = v1Limits[name]
			}
			out = append(out, rec)
			continue
		}
		// No limit recorded anywhere means none: that is what the device-
		// limit guard enforces, and what the user DB documents for 0.
		rec := user.Record{Name: name}
		if lim, ok := v1Limits[name]; ok {
			rec.Limit = lim
		}
		out = append(out, rec)
	}
	return out
}

// writeV1Compat writes the password to /etc/SSHPlus/senha/<name> and
// appends/updates the limit entry in /root/usuarios.db — the two side-
// car files v1 reads for infousers and alterarlimite.
func writeV1Compat(name, password string, limit int) {
	_ = os.MkdirAll("/etc/SSHPlus/senha", 0o755)
	_ = os.WriteFile("/etc/SSHPlus/senha/"+name, []byte(password+"\n"), 0o600)

	// Update /root/usuarios.db: remove old entry then append new one.
	updateV1DB(name, limit)
}

func updateV1DB(name string, limit int) {
	raw, _ := os.ReadFile("/root/usuarios.db")
	var lines []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.Fields(l) != nil && len(strings.Fields(l)) > 0 && strings.Fields(l)[0] == name {
			continue
		}
		lines = append(lines, l)
	}
	// strip trailing blank
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "" {
		lines = lines[:len(lines)-1]
	}
	lines = append(lines, fmt.Sprintf("%s %d", name, limit))
	_ = os.WriteFile("/root/usuarios.db", []byte(strings.Join(lines, "\n")+"\n"), 0o644)
}

func deleteV1Compat(name string) {
	_ = os.Remove("/etc/SSHPlus/senha/" + name)
	_ = os.Remove("/etc/SSHPlus/userteste/" + name + ".sh")
	// Remove from usuarios.db
	if raw, err := os.ReadFile("/root/usuarios.db"); err == nil {
		var lines []string
		for _, l := range strings.Split(string(raw), "\n") {
			f := strings.Fields(l)
			if len(f) > 0 && f[0] == name {
				continue
			}
			lines = append(lines, l)
		}
		_ = os.WriteFile("/root/usuarios.db", []byte(strings.Join(lines, "\n")), 0o644)
	}
}

// validateMenuName enforces v1's naming rule: letter-first, max 10 chars.
func validateMenuName(name string) error {
	if len(name) == 0 {
		return fmt.Errorf("ชื่อผู้ใช้ว่างเปล่า — กรุณาพิมพ์ชื่อผู้ใช้ก่อนกด ENTER")
	}
	if len(name) > 10 {
		return fmt.Errorf("ชื่อผู้ใช้ยาวเกินไป — ต้องไม่เกิน 10 ตัวอักษร")
	}
	if err := user.ValidateName(name); err != nil {
		return fmt.Errorf("ต้องขึ้นต้นด้วยตัวอักษร a-z หรือ A-Z แล้วตามด้วย a-z, A-Z, 0-9, _ หรือ -")
	}
	return nil
}

// ---------------------------------------------------------------------
// 01 สร้างบัญชี ผู้ใช้
// ---------------------------------------------------------------------

func runCreateUser(r *bufio.Reader) error {
	userHeader("สร้างชิ่อผู้ใช้")

	name, err := readLine(r, "User:")
	if err != nil {
		return err
	}
	if err := validateMenuName(name); err != nil {
		errLine("สร้างผู้ใช้ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	if exists, _ := user.SystemUserExists(name); exists {
		errLine("สร้างผู้ใช้ไม่สำเร็จ: มีผู้ใช้ชื่อนี้อยู่แล้ว — กรุณาใช้ชื่ออื่น")
		waitEnter(r)
		return nil
	}

	pw, err := readLine(r, "Password:")
	if err != nil {
		return err
	}
	if msg := menuPasswordProblem(pw); msg != "" {
		errLine(msg)
		waitEnter(r)
		return nil
	}

	limStr, err := readLine(r, "limited connection:")
	if err != nil {
		return err
	}
	limit, err := strconv.Atoi(limStr)
	if err != nil || limit < 1 {
		errLine("จำนวนอุปกรณ์ที่ใช้พร้อมกันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 1 ขึ้นไป")
		waitEnter(r)
		return nil
	}

	daysStr, err := readLine(r, "Expire (วัน):")
	if err != nil {
		return err
	}
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		errLine("จำนวนวันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 1 ขึ้นไป")
		waitEnter(r)
		return nil
	}

	host := defaultRemoteHost()

	// Create system user + set password + set expiry.
	expiresAt := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Truncate(24 * time.Hour)
	if err := user.CreateSystemUser(name, expiresAt); err != nil {
		errLine("สร้างผู้ใช้ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	if err := user.SetPassword(name, pw); err != nil {
		_ = user.DeleteSystemUser(name)
		errLine("ตั้งรหัสผ่านไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	// Persist metadata in v2 DB + v1 compat files.
	db, _ := user.Load()
	db.Users[name] = user.Record{
		Name:      name,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		ExpiresAt: expiresAt,
		Limit:     limit,
	}
	_ = db.Save()
	writeV1Compat(name, pw, limit)

	// Generate .ovpn only if PKI is already initialized.
	ovpnPath := ""
	if pki.IsInitialized() {
		host2, err2 := readLine(r, "Remote IP ["+host+"]:")
		if err2 == nil && host2 != "" {
			host = host2
		}
		// User already exists — sign cert then export.
		if ca, err2 := pki.LoadCA(); err2 == nil {
			if clientCert, err2 := pki.GenerateClientCert(ca, name); err2 == nil {
				_ = os.MkdirAll(pki.ClientsDir, 0o700)
				_ = os.WriteFile(pki.ClientsDir+"/"+name+".crt", clientCert.CertPEM, 0o644)
				_ = os.WriteFile(pki.ClientsDir+"/"+name+".key", clientCert.KeyPEM, 0o600)
				if ovpnBytes, err2 := user.Export(name, user.OVPNInput{
					RemoteHost: host, RemotePort: ovpnPort(), Proto: ovpnProto(),
				}); err2 == nil {
					ovpnPath = "/root/" + name + ".ovpn"
					_ = os.WriteFile(ovpnPath, ovpnBytes, 0o600)
					// Mirror to /root/openvpn/ for the built-in file server.
					_ = os.MkdirAll("/root/openvpn", 0o700)
					_ = os.WriteFile("/root/openvpn/"+name+".ovpn", ovpnBytes, 0o600)
				}
			}
		}
	}

	clearScreen()
	fmt.Println("\033[44;1;37m       สร้างบัญชี SSH แล้ว !       \033[0m")
	fmt.Println()
	fmt.Printf("%sIP: %s%s\n", cGrnBold, cWhtBold, host)
	fmt.Printf("%sUser: %s%s\n", cGrnBold, cWhtBold, name)
	fmt.Printf("%sPassword: %s%s\n", cGrnBold, cWhtBold, pw)
	fmt.Printf("%sจำนวนอุปกรณ์ที่ใช้พร้อมกัน: %s%d\n", cGrnBold, cWhtBold, limit)
	fmt.Printf("%sวันหมดอายุ: %s%s (%s%d %sวัน%s)\n",
		cGrnBold, cWhtBold, expiresAt.Format("02/01/2006"),
		cYelBold, days, cWhtBold, cReset)
	if ovpnPath != "" {
		fmt.Printf("%sไฟล์ .ovpn: %s%s%s\n", cGrnBold, cCyanBold, ovpnPath, cReset)
		if isFileServerOn() {
			fmt.Printf("%sดาวน์โหลด: %shttp://%s:82/%s.ovpn%s\n",
				cGrnBold, cCyanBold, host, name, cReset)
		}
	}
	fmt.Println()
	okLine("เพิ่มผู้ใช้สำเร็จ!")
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 02 สร้างบัญชี ผุ้ใช้ทดลอง
// ---------------------------------------------------------------------

func runCreateTrial(r *bufio.Reader) error {
	userHeader("สร้างบัญชีผู้ใช้ทดลอง")

	// Show existing trial users (from /etc/SSHPlus/userteste/).
	entries, _ := os.ReadDir("/etc/SSHPlus/userteste")
	if len(entries) == 0 {
		fmt.Println(cRedBold + "ไม่มีผู้ใช้ทดลองที่ใช้งานอยู่!" + cReset)
	} else {
		fmt.Println(cGrnBold + "บัญชีผู้ใช้ทดลอง!" + cReset)
		for _, e := range entries {
			n := strings.TrimSuffix(e.Name(), ".sh")
			fmt.Println(cWhtBold + n + cReset)
		}
	}
	fmt.Println()

	name, err := readLine(r, "User:")
	if err != nil {
		return err
	}
	if err := validateMenuName(name); err != nil {
		errLine("สร้างผู้ใช้ทดลองไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	if exists, _ := user.SystemUserExists(name); exists {
		errLine("สร้างผู้ใช้ทดลองไม่สำเร็จ: มีผู้ใช้ชื่อนี้อยู่แล้ว — กรุณาใช้ชื่ออื่น")
		waitEnter(r)
		return nil
	}

	pw, err := readLine(r, "Password:")
	if err != nil {
		return err
	}
	if msg := menuPasswordProblem(pw); msg != "" {
		errLine(msg)
		waitEnter(r)
		return nil
	}

	limStr, err := readLine(r, "limited connection:")
	if err != nil {
		return err
	}
	limit, err := strconv.Atoi(limStr)
	if err != nil || limit < 1 {
		errLine("จำนวนอุปกรณ์ที่ใช้พร้อมกันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 1 ขึ้นไป")
		waitEnter(r)
		return nil
	}

	minStr, err := readLine(r, "นาที (Ex: 60):")
	if err != nil {
		return err
	}
	minutes, err := strconv.Atoi(minStr)
	if err != nil || minutes < 1 {
		errLine("จำนวนนาทีไม่ถูกต้อง: ต้องเป็นตัวเลขที่มากกว่าศูนย์เท่านั้น")
		waitEnter(r)
		return nil
	}

	// Create system user (no expiry — `at` job deletes it after N minutes).
	if err := user.CreateSystemUser(name, time.Time{}); err != nil {
		errLine("สร้างผู้ใช้ทดลองไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	if err := user.SetPassword(name, pw); err != nil {
		_ = user.DeleteSystemUser(name)
		errLine("ตั้งรหัสผ่านไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	writeV1Compat(name, pw, limit)

	// Write cleanup script.
	_ = os.MkdirAll("/etc/SSHPlus/userteste", 0o755)
	script := fmt.Sprintf(`#!/bin/bash
pkill -f "%s"
userdel --force %s
grep -v ^%s[[:space:]] /root/usuarios.db > /tmp/ph ; cat /tmp/ph > /root/usuarios.db
rm /etc/SSHPlus/senha/%s > /dev/null 2>&1
rm -rf /etc/SSHPlus/userteste/%s.sh
exit
`, name, name, name, name, name)
	scriptPath := "/etc/SSHPlus/userteste/" + name + ".sh"
	_ = os.WriteFile(scriptPath, []byte(script), 0o755)

	// Schedule deletion.
	atOut, atErr := exec.Command("at", "-f", scriptPath, "now", "+", strconv.Itoa(minutes), "minutes").CombinedOutput()
	if atErr != nil {
		fmt.Printf("%s[คำเตือน]%s at ล้มเหลว: %s — ลบด้วยตนเองเมื่อหมดเวลา%s\n",
			cYelBold, cReset, strings.TrimSpace(string(atOut)), cReset)
	}

	clearScreen()
	fmt.Println("\033[44;1;37m     สร้างบัญชีผู้ใช้ทดลอง เรียบร้อย     \033[0m")
	fmt.Println()
	fmt.Printf("%sIP: %s%s\n", cGrnBold, cWhtBold, defaultRemoteHost())
	fmt.Printf("%sผู้ใช้: %s%s\n", cGrnBold, cWhtBold, name)
	fmt.Printf("%sรหัสผ่าน: %s%s\n", cGrnBold, cWhtBold, pw)
	fmt.Printf("%sจำนวนอุปกรณ์ที่ใช้พร้อมกัน: %s%d\n", cGrnBold, cWhtBold, limit)
	fmt.Printf("%sอายุใช้งาน: %s%d นาที\n", cGrnBold, cWhtBold, minutes)
	fmt.Println()
	fmt.Printf("%sหลังจากเวลาที่กำหนด ผู้ใช้%s\n", cYelBold, cReset)
	fmt.Printf("%s%s %sจะถูกตัดการเชื่อมต่อและลบ.%s\n", cGrnBold, name, cYelBold, cReset)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 03 ลบชื่อ ผู้ใช้
// ---------------------------------------------------------------------

func runRemoveUser(r *bufio.Reader) error {
	userHeader("ลบชื่อผู้ใช้ SSH")

	fmt.Printf("%s[%s1%s]%s ลบชื่อผู้ใช้\n", cRedBold, cCyanBold, cRedBold, cYelBold)
	fmt.Printf("%s[%s2%s]%s ลบชื่อผู้ใช้ทั้งหมด\n", cRedBold, cCyanBold, cRedBold, cYelBold)
	fmt.Printf("%s[%s3%s]%s ออก\n%s", cRedBold, cCyanBold, cRedBold, cYelBold, cReset)
	fmt.Println()
	choice, err := readLine(r, "CHOOSE OPTION ? :")
	if err != nil {
		return err
	}

	switch choice {
	case "1":
		rec, err := listAndPick(r, "เลือกผู้ใช้ที่จะลบ:")
		if err != nil {
			errLine(err.Error())
			waitEnter(r)
			return nil
		}
		confirm, err := readLine(r, "ลบจริง? [y/N]:")
		if err != nil {
			return err
		}
		if strings.ToLower(confirm) != "y" {
			fmt.Println(cYelBold + "ยกเลิก." + cReset)
			waitEnter(r)
			return nil
		}
		if err := user.Remove(rec.Name); err != nil {
			errLine("ลบผู้ใช้ไม่สำเร็จ: " + err.Error())
			waitEnter(r)
			return nil
		}
		kickOVPN(rec.Name)
		deleteV1Compat(rec.Name)
		fmt.Println()
		fmt.Printf("\033[41;1;37m User %s ลบเรียบร้อย! \033[0m\n", rec.Name)

	case "2":
		confirm, err := readLine(r, "ลบผู้ใช้ทั้งหมดจริง? [y/N]:")
		if err != nil {
			return err
		}
		if strings.ToLower(confirm) != "y" {
			fmt.Println(cYelBold + "ยกเลิก." + cReset)
			waitEnter(r)
			return nil
		}
		records := systemUsers()
		if len(records) == 0 {
			errLine("ไม่มีผู้ใช้ในระบบ")
			waitEnter(r)
			return nil
		}
		for _, rec := range records {
			_ = user.Remove(rec.Name)
			kickOVPN(rec.Name)
			deleteV1Compat(rec.Name)
			fmt.Printf("%s- %s%s%s ลบแล้ว\n", cRedBold, cWhtBold, rec.Name, cReset)
		}
		fmt.Println()
		okLine("ลบผู้ใช้ทั้งหมดสำเร็จ!")

	default:
		fmt.Println(cYelBold + "ยกเลิก." + cReset)
	}

	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 04 เช็คผู้ใช้ ออนไลน์
// ---------------------------------------------------------------------

func runSSHMonitor(r *bufio.Reader) error {
	clearScreen()
	fmt.Println("\033[44;1;37m ชื่อผู้ใช้         สถานะ       เชื่อมต่อ     เวลา   \033[0m")
	fmt.Println()

	sshUsers := readSSHLogins()
	ovpnUsers := readOpenVPNUsers()
	dropbearUsers := readDropbearLogins()
	sshTimes := readSSHTimes()
	ovpnTimes := readOpenVPNTimes()

	records := systemUsers()
	if len(records) == 0 {
		fmt.Println(cYelBold + "ยังไม่มีผู้ใช้ในระบบ" + cReset)
		waitEnter(r)
		return nil
	}

	for _, rec := range records {
		ssh := sshUsers[rec.Name]
		ovp := ovpnUsers[rec.Name]
		drp := dropbearUsers[rec.Name]
		conex := ssh + ovp + drp

		var statusText string
		if conex == 0 {
			statusText = cRedBold + "ออฟไลน์ " + cYelBold + "      "
		} else {
			statusText = cGrnBold + "ออนไลน์ " + cYelBold + "      "
		}
		limit := "∞"
		if rec.Limit > 0 {
			limit = strconv.Itoa(rec.Limit)
		}

		timer := "00:00:00"
		if ssh > 0 {
			if t, ok := sshTimes[rec.Name]; ok {
				timer = t
			}
		} else if ovp > 0 {
			if t, ok := ovpnTimes[rec.Name]; ok {
				timer = t
			}
		}

		fmt.Printf("%s %-15s %s %-13s %-10s %s\n",
			cYelBold,
			rec.Name,
			statusText,
			fmt.Sprintf("%d/%s", conex, limit),
			timer,
			cReset)
		fmt.Println(cBluBold + separator + cReset)
	}
	waitEnter(r)
	return nil
}

// parseSSHTimes maps each user to their longest-running connection from a
// `ps -eo etimes=,args=` dump. etimes is elapsed seconds already, so this
// reads no clock and needs no date-format handling.
func parseSSHTimes(psOut string) map[string]string {
	longest := map[string]int{}
	for _, line := range strings.Split(psOut, "\n") {
		secsField, args, ok := strings.Cut(strings.TrimSpace(line), " ")
		if !ok {
			continue
		}
		user := sshPrivSessionUser(strings.TrimSpace(args))
		if user == "" || user == "root" {
			continue
		}
		secs, err := strconv.Atoi(secsField)
		if err != nil {
			continue
		}
		if secs > longest[user] {
			longest[user] = secs
		}
	}
	out := make(map[string]string, len(longest))
	for user, secs := range longest {
		out[user] = fmt.Sprintf("%02d:%02d:%02d", secs/3600, secs/60%60, secs%60)
	}
	return out
}

// readSSHTimes returns a map[user]elapsed for each user's longest-running SSH
// connection, formatted as HH:MM:SS. Reads ps rather than `who` for the same
// reason as readSSHLogins: PTY-less tunnel sessions never reach utmp.
func readSSHTimes() map[string]string {
	data, err := exec.Command("ps", "-eo", "etimes=,args=").Output()
	if err != nil {
		return map[string]string{}
	}
	return parseSSHTimes(string(data))
}

// readOpenVPNTimes returns a map[user]elapsed parsed from "Connected Since"
// in the status log. Handles status-version 2/3 (what serverconf.go writes)
// via the epoch field, and status-version 1 for installs whose server.conf
// predates that directive. Every instance's log is read, as readOpenVPNUsers
// does: users on extra ports and CPU-spread workers have a time too.
func readOpenVPNTimes() map[string]string {
	out := map[string]string{}
	var data []byte
	paths, _ := filepath.Glob("/var/log/openvpn-status*.log")
	if len(paths) == 0 {
		paths = []string{"/etc/openvpn/openvpn-status.log"} // pre-v2 installs
	}
	for _, p := range paths {
		if d, err := os.ReadFile(p); err == nil {
			data = append(append(data, d...), '\n')
		}
	}
	if data == nil {
		return out
	}
	now := time.Now()
	inList := false
	for _, line := range strings.Split(string(data), "\n") {
		// status-version 2 (comma) / 3 (tab): the CLIENT_LIST row carries an
		// epoch, so no date-format parsing is needed. Field order per the
		// binary's own HEADER row:
		//   [1]=Common Name ... [7]=Connected Since [8]=Connected Since (time_t)
		sep := ""
		if strings.HasPrefix(line, "CLIENT_LIST,") {
			sep = ","
		} else if strings.HasPrefix(line, "CLIENT_LIST\t") {
			sep = "\t"
		}
		if sep != "" {
			parts := strings.Split(line, sep)
			if len(parts) < 9 || parts[1] == "" || parts[1] == "Common Name" {
				continue
			}
			epoch, err := strconv.ParseInt(parts[8], 10, 64)
			if err != nil {
				continue
			}
			elapsed := now.Sub(time.Unix(epoch, 0))
			h := int(elapsed.Hours())
			m := int(elapsed.Minutes()) % 60
			s := int(elapsed.Seconds()) % 60
			out[parts[1]] = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
			continue
		}
		if strings.HasPrefix(line, "Common Name,") {
			inList = true
			continue
		}
		if strings.HasPrefix(line, "ROUTING TABLE") || strings.HasPrefix(line, "GLOBAL STATS") {
			inList = false
			continue
		}
		if !inList || line == "" {
			continue
		}
		// "username,real_addr,bytes_rx,bytes_tx,connected_since"
		parts := strings.Split(line, ",")
		if len(parts) < 5 {
			continue
		}
		username := parts[0]
		since := parts[4] // "2026-06-27 16:58:36"
		t, err := time.ParseInLocation("2006-01-02 15:04:05", since, now.Location())
		if err != nil {
			continue
		}
		elapsed := now.Sub(t)
		h := int(elapsed.Hours())
		m := int(elapsed.Minutes()) % 60
		s := int(elapsed.Seconds()) % 60
		out[username] = fmt.Sprintf("%02d:%02d:%02d", h, m, s)
	}
	return out
}

// sshPrivSessionUser returns the login name out of an sshd privilege-separation
// monitor line, or "" when the line is not one:
//
//	sshd: someone [priv]
//
// Exactly one such process exists per SSH connection. Lines for the listener,
// the session children, and connections that have not authenticated yet
// ("unknown", "[accepted]", "[net]") all fail to match.
//
// OpenSSH 9.8 renamed the per-connection binary to sshd-session, so the tag
// ahead of the colon is accepted in either spelling. Only the sshd form is
// verified here — the servers this ships to run OpenSSH 8.2.
func sshPrivSessionUser(args string) string {
	tag, rest, ok := strings.Cut(args, ": ")
	if !ok || (tag != "sshd" && tag != "sshd-session") {
		return ""
	}
	user, ok := strings.CutSuffix(rest, " [priv]")
	if !ok || user == "" || user == "unknown" {
		return ""
	}
	return user
}

// countSSHPrivSessions merges one `ps -eo args=` dump into per-user counts.
// Split out from readSSHLogins so the parsing can be tested without a host.
func countSSHPrivSessions(psOut string, out map[string]int) {
	for _, line := range strings.Split(psOut, "\n") {
		user := sshPrivSessionUser(strings.TrimSpace(line))
		if user == "" || user == "root" {
			continue
		}
		out[user]++
	}
}

// readSSHLogins returns a map[user]count of live SSH connections.
//
// This counts "sshd: <user> [priv]" processes instead of reading utmp through
// `who`. sshd only writes a utmp record for sessions that allocate a PTY, and
// the tunnel clients this product exists to serve request no PTY at all — so
// `who` reported every single one of them as offline. Counting the privilege-
// separation monitor also answers what pushed us to `who` in the first place:
// that process is root-owned on every OpenSSH version, so it does not matter
// whether the session child runs as the login user or as root.
func readSSHLogins() map[string]int {
	out := map[string]int{}
	data, err := exec.Command("ps", "-eo", "args=").Output()
	if err != nil {
		return out
	}
	countSSHPrivSessions(string(data), out)
	return out
}

// readOpenVPNUsers parses every OpenVPN status log for connected clients
// and merges the counts: the primary /var/log/openvpn-status.log plus each
// extra instance's openvpn-status<N>.log. Supports status-version 1 (CSV
// after a "Common Name," header), 2 (comma-separated CLIENT_LIST rows —
// what serverconf.go writes) and 3 (same but tab-separated), so installs
// upgraded from an older conf keep reporting while their server.conf still
// says version 1.
func readOpenVPNUsers() map[string]int {
	out := map[string]int{}
	paths, _ := filepath.Glob("/var/log/openvpn-status*.log")
	if len(paths) == 0 {
		// legacy fallback for pre-v2 installs
		paths = []string{"/etc/openvpn/openvpn-status.log"}
	}
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		countOVPNStatus(string(data), out)
	}
	return out
}

// countOVPNStatus merges one status log's client list into counts.
func countOVPNStatus(data string, out map[string]int) {
	inList := false
	for _, line := range strings.Split(data, "\n") {
		// status-version 2: "CLIENT_LIST,<Common Name>,<Real Address>,..."
		// The header row starts with "HEADER," so it never matches here.
		if strings.HasPrefix(line, "CLIENT_LIST,") {
			parts := strings.SplitN(line, ",", 3)
			if len(parts) >= 2 && parts[1] != "" && parts[1] != "Common Name" {
				out[parts[1]]++
			}
			continue
		}
		// status-version 3: identical to 2 but tab-separated
		if strings.HasPrefix(line, "CLIENT_LIST\t") {
			parts := strings.SplitN(line, "\t", 3)
			if len(parts) >= 2 && parts[1] != "Common Name" {
				out[parts[1]]++
			}
			continue
		}
		// status-version 1 (default): CSV section between header and ROUTING TABLE
		if strings.HasPrefix(line, "Common Name,") {
			inList = true
			continue
		}
		if strings.HasPrefix(line, "ROUTING TABLE") || strings.HasPrefix(line, "GLOBAL STATS") {
			inList = false
			continue
		}
		if inList && line != "" {
			parts := strings.SplitN(line, ",", 2)
			if len(parts) >= 1 && parts[0] != "" {
				out[parts[0]]++
			}
		}
	}
}

// readDropbearLogins: dropbear writes to syslog rather than utmp so we
// approximate by scanning `ps`. Same parser shape as readSSHLogins but
// looking for "dropbear" comm.
func readDropbearLogins() map[string]int {
	out := map[string]int{}
	cmd := exec.Command("ps", "-eo", "user=,comm=")
	data, err := cmd.Output()
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		if !strings.Contains(fields[1], "dropbear") {
			continue
		}
		if fields[0] == "root" {
			continue
		}
		out[fields[0]]++
	}
	return out
}

// ---------------------------------------------------------------------
// 05 เปลี่ยนวันหมดอายุ ผู้ใช้
// ---------------------------------------------------------------------

func runChangeExpiry(r *bufio.Reader) error {
	userHeader("เปลี่ยนวันหมดอายุผู้ใช้")

	rec, err := listAndPick(r, "CHOOSE USER ?:")
	if err != nil {
		errLine(err.Error())
		waitEnter(r)
		return nil
	}

	fmt.Println()
	fmt.Println(cRedBold + "EX:" + cYelBold + " จำนวนวันจากวันนี้ เช่น 30  หรือ วันที่ DD/MM/YYYY เช่น 31/12/2025" + cReset)
	fmt.Println()
	input, err := readLine(r, fmt.Sprintf("วันหมดอายุใหม่สำหรับ %s:", rec.Name))
	if err != nil {
		return err
	}
	if input == "" {
		errLine("ไม่ได้ระบุ — กรุณาพิมพ์จำนวนวัน หรือ วันที่ DD/MM/YYYY")
		waitEnter(r)
		return nil
	}

	var days int
	if days64, err2 := strconv.ParseInt(input, 10, 64); err2 == nil {
		days = int(days64)
		if days < 0 {
			errLine("จำนวนวันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 0 ขึ้นไป")
			waitEnter(r)
			return nil
		}
	} else {
		// Try DD/MM/YYYY
		t, err2 := time.Parse("02/01/2006", input)
		if err2 != nil {
			errLine("รูปแบบไม่ถูกต้อง: ใส่จำนวนวัน หรือ วันที่ DD/MM/YYYY")
			waitEnter(r)
			return nil
		}
		// The account then stops working on that date (shadow semantics);
		// days < 1 would mean "never expires" to UpdateExpiry.
		days = int(utcDay(t) - utcDay(time.Now()))
		if days < 1 {
			errLine("วันที่ที่ระบุผ่านมาแล้ว — ใส่วันที่ในอนาคต")
			waitEnter(r)
			return nil
		}
	}

	if err := user.UpdateExpiry(rec.Name, days); err != nil {
		errLine("เปลี่ยนวันหมดอายุไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	fmt.Println()
	if days == 0 {
		fmt.Printf("\033[44;1;37m เปลี่ยนวันหมดอายุของผู้ใช้ %s สำเร็จ: ไม่หมดอายุ \033[0m\n", rec.Name)
	} else {
		expDate := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format("02/01/2006")
		fmt.Printf("\033[44;1;37m เปลี่ยนวันหมดอายุของผู้ใช้ %s สำเร็จ: %s \033[0m\n", rec.Name, expDate)
	}
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 06 เปลี่ยนจำกัด การเชื่อมต่อ
// ---------------------------------------------------------------------

func runChangeLimit(r *bufio.Reader) error {
	userHeader("เปลี่ยนขีดจำกัดการเชื่อมต่อ")

	rec, err := listAndPick(r, "CHOOSE USER ?:")
	if err != nil {
		errLine(err.Error())
		waitEnter(r)
		return nil
	}

	limStr, err := readLine(r, fmt.Sprintf("จำนวนการเชื่อมต่อพร้อมกันของผู้ใช้ %s:", rec.Name))
	if err != nil {
		return err
	}
	limit, err := strconv.Atoi(limStr)
	if err != nil || limit < 1 {
		errLine("จำนวนอุปกรณ์ที่ใช้พร้อมกันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 1 ขึ้นไป")
		waitEnter(r)
		return nil
	}

	if err := user.UpdateLimit(rec.Name, limit); err != nil {
		errLine("บันทึกฐานข้อมูลไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	updateV1DB(rec.Name, limit)
	fmt.Println()
	fmt.Printf("\033[44;1;37m ตั้งจำนวนอุปกรณ์ที่ใช้พร้อมกันใหม่สำหรับ %s เป็น %d เครื่อง \033[0m\n", rec.Name, limit)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 07 เปลี่ยนรหัสผ่าน ผู้ใช้
// ---------------------------------------------------------------------

func runChangePassword(r *bufio.Reader) error {
	userHeader("เปลี่ยนรหัสผ่านผู้ใช้งาน")

	rec, err := listAndPick(r, "CHOOSE USER CHANGE ?:")
	if err != nil {
		errLine(err.Error())
		waitEnter(r)
		return nil
	}

	pw, err := readLine(r, fmt.Sprintf("รหัสผ่านใหม่สำหรับผู้ใช้ %s:", rec.Name))
	if err != nil {
		return err
	}
	if msg := menuPasswordProblem(pw); msg != "" {
		errLine(msg)
		waitEnter(r)
		return nil
	}

	if err := user.UpdatePassword(rec.Name, pw); err != nil {
		errLine("เปลี่ยนรหัสผ่านไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	_ = os.MkdirAll("/etc/SSHPlus/senha", 0o755)
	_ = os.WriteFile("/etc/SSHPlus/senha/"+rec.Name, []byte(pw+"\n"), 0o600)
	fmt.Println()
	fmt.Printf("\033[41;1;37m รหัสผ่านผู้ใช้ %s ได้ถูกเปลี่ยนเป็น: %s \033[0m\n", rec.Name, pw)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 08 ลบผู้ใช้ที่ หมดอายุ
// ---------------------------------------------------------------------

func runCleanExpired(r *bufio.Reader) error {
	userHeader("ลบผู้ใช้ที่หมดอายุ")

	fmt.Println("\033[44;1;37m ผู้ใช้          วันหมดอายุ      สถานะ              \033[0m")
	fmt.Println()

	var allRemoved []string
	// Iterate ALL system users and check chage directly — same as v1
	// expcleaner. user.CleanExpired() only sees the JSON DB and would miss
	// pure v1 accounts that were created before hexplus v2 was installed.
	for _, rec := range systemUsers() {
		exp, daysLeft := chageExpiry(rec.Name)
		if exp == "never" || exp == "" {
			continue // no expiry configured — skip silently
		}
		var statusCol string
		if daysLeft >= 0 {
			statusCol = cGrnBold + "ไม่ถูกลบออก" + cReset
		} else {
			statusCol = cRedBold + "ถูกลบออกแล้ว" + cReset
			// Kill active sessions before removing (v1: pkill -f $user).
			// pkill -u removes by UID — safer than matching cmdline.
			_ = exec.Command("pkill", "-u", rec.Name).Run()
			kickOVPN(rec.Name)
			if err := user.Remove(rec.Name); err == nil {
				deleteV1Compat(rec.Name)
				allRemoved = append(allRemoved, rec.Name)
			}
		}
		fmt.Printf("%s%-15s%s%-17s%s\n",
			cYelBold, rec.Name,
			cWhtBold, exp,
			statusCol)
		fmt.Println(cBluBold + separator + cReset)
	}

	// v1 compat: reset the expired-count file it checks on menu load.
	_ = os.MkdirAll("/etc/SSHPlus", 0o755)
	_ = os.WriteFile("/etc/SSHPlus/Exp", []byte("0\n"), 0o644)

	fmt.Println()
	if len(allRemoved) == 0 {
		fmt.Println(cYelBold + "ไม่พบผู้ใช้ที่หมดอายุ" + cReset)
	} else {
		fmt.Printf("%sลบแล้ว %s%d %suser%s\n", cGrnBold, cWhtBold, len(allRemoved), cGrnBold, cReset)
	}
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 09 เช็คบัญชีทั้งหมด
// ---------------------------------------------------------------------

func runListUsers(r *bufio.Reader) error {
	clearScreen()
	fmt.Println("\033[44;1;37m ผู้ใช้          รหัสผ่าน       จำนวนอุปกรณ์   วันหมดอายุ \033[0m")
	fmt.Println()

	records := systemUsers()
	if len(records) == 0 {
		errLine("แสดงรายชื่อผู้ใช้ไม่ได้: ไม่พบผู้ใช้ในระบบ — กรุณาสร้างผู้ใช้ก่อน")
		waitEnter(r)
		return nil
	}

	// Build online maps once, same as sshmonitor (function 04).
	// ps -u user | grep sshd doesn't work on Ubuntu 22+ because all sshd
	// processes run as root regardless of authenticated user.
	sshOnline := readSSHLogins()
	ovpnOnline := readOpenVPNUsers()

	names := make([]string, 0, len(records))
	for _, rec := range records {
		names = append(names, rec.Name)
	}
	badHash := unverifiableHashSet(names)

	tUser, tOnline, tExpired := 0, 0, 0
	for _, rec := range records {
		tUser++

		// password from /etc/SSHPlus/senha/<name> (v1 style)
		pass := "-"
		if b, err := os.ReadFile("/etc/SSHPlus/senha/" + rec.Name); err == nil {
			if s := strings.TrimSpace(string(b)); s != "" {
				pass = s
			}
		}

		// expiry from chage (authoritative — covers both v1 and v2 users)
		exp, daysLeft := chageExpiry(rec.Name)

		limit := "∞" // same label as the online monitor; a Thai word breaks the column width
		if rec.Limit > 0 {
			limit = strconv.Itoa(rec.Limit)
		}

		var expCol string
		if exp == "never" || exp == "" {
			expCol = cYelBold + "ไม่หมดอายุ" + cReset
		} else {
			if daysLeft < 0 {
				tExpired++
				expCol = cRedBold + "หมดอายุ" + cReset
			} else {
				expCol = fmt.Sprintf("%s%d %sวัน%s", cYelBold, daysLeft, cWhtBold, cReset)
			}
		}

		if sshOnline[rec.Name] > 0 || ovpnOnline[rec.Name] > 0 {
			tOnline++
		}

		nameColor := cYelBold
		if badHash[rec.Name] {
			nameColor = cRedBold
		}
		fmt.Printf("%s %-15s %s%-13s %s%-10s %s\n",
			nameColor, rec.Name,
			cWhtBold, pass,
			cWhtBold, limit,
			expCol)
		fmt.Println(cBluBold + separator + cReset)
	}

	fmt.Printf("%s• %sผู้ใช้ทั้งหมด%s %d %s• %sออนไลน์%s: %d %s• %sหมดอายุ%s: %d %s•%s\n",
		cYelBold, cCyanBold, cWhtBold, tUser,
		cYelBold, cGrnBold, cWhtBold, tOnline,
		cYelBold, cRedBold, cWhtBold, tExpired,
		cYelBold, cReset)
	if len(badHash) > 0 {
		var bad []string
		for _, n := range names {
			if badHash[n] {
				bad = append(bad, n)
			}
		}
		offerHashRepair(func(label string) (string, error) { return readLine(r, label) }, bad)
	}
	waitEnter(r)
	return nil
}

// chageExpiry returns (dateStr, daysLeft) for a user.
// dateStr is "never" if no expiry is set, or "YYYY-MM-DD" otherwise.
// daysLeft < 0 means already expired, 0 means today is the last day.
func chageExpiry(name string) (string, int) {
	out, err := exec.Command("chage", "-l", name).Output()
	if err != nil {
		return "never", 0
	}
	for _, line := range strings.Split(string(out), "\n") {
		if !strings.Contains(line, "Account expires") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) < 2 {
			continue
		}
		val := strings.TrimSpace(parts[1])
		if val == "never" || val == "ไม่มีกำหนด" {
			return "never", 0
		}
		t, err := time.Parse("Jan 02, 2006", val)
		if err != nil {
			t, err = time.Parse("2006-01-02", val)
		}
		if err != nil {
			return val, 0
		}
		return t.Format("02/01/2006"), daysLeft(t, time.Now())
	}
	return "never", 0
}

// menuPasswordProblem is the menus' password rule: at least 4 characters
// (v1's rule) plus what OpenVPN and chpasswd can take
// (user.CheckVPNPassword). It returns the message to show, or "".
func menuPasswordProblem(pw string) string {
	if len(pw) < 4 {
		return "รหัสผ่านไม่ถูกต้อง: ต้องมีอย่างน้อย 4 ตัวอักษร"
	}
	if errors.Is(user.CheckVPNPassword(pw), user.ErrPasswordTooLong) {
		return fmt.Sprintf("รหัสผ่านไม่ถูกต้อง: ยาวได้ไม่เกิน %d ไบต์ (OPENVPN รับไม่ได้)", user.MaxVPNPasswordLen)
	}
	if user.CheckVPNPassword(pw) != nil {
		return "รหัสผ่านไม่ถูกต้อง: มีอักขระที่ใช้ไม่ได้"
	}
	return ""
}

// utcDay is t as days since 1970-01-01 in UTC, the unit of the expiry field
// in /etc/shadow.
func utcDay(t time.Time) int64 { return t.Unix() / 86400 }

// daysLeft returns the full days an account expiring on expire (a shadow
// expiry date, 00:00 UTC) still has after today: 0 on its last day, -1 from
// the expiry date on. The auth script and pam_unix reject the account once
// today reaches the expiry day; rounding the remaining hours toward zero
// showed that day as "0 days left" and kept the user out of the
// expired-user cleanup.
func daysLeft(expire, now time.Time) int {
	return int(utcDay(expire) - utcDay(now) - 1)
}
