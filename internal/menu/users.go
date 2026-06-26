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
	"crypto/rand"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

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
	records, err := user.List()
	if err != nil {
		return user.Record{}, err
	}
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

// ---------------------------------------------------------------------
// 01 สร้างบัญชี ผู้ใช้
// ---------------------------------------------------------------------

func runCreateUser(r *bufio.Reader) error {
	userHeader("สร้างชิ่อผู้ใช้")

	name, err := readLine(r, "createuser:")
	if err != nil {
		return err
	}
	if name == "" {
		errLine("สร้างผู้ใช้ไม่สำเร็จ: ชื่อผู้ใช้ว่างเปล่า — กรุณาพิมพ์ชื่อผู้ใช้ก่อนกด ENTER")
		waitEnter(r)
		return nil
	}
	if err := user.ValidateName(name); err != nil {
		errLine("ชื่อผู้ใช้ไม่ถูกต้อง: ต้องขึ้นต้นด้วยตัวอักษร a-z หรือ A-Z แล้วตามด้วย a-z, A-Z, 0-9, _ หรือ - และยาว 2-32 ตัวอักษร")
		waitEnter(r)
		return nil
	}

	pw, err := readLine(r, "Password:")
	if err != nil {
		return err
	}
	if len(pw) < 4 {
		errLine("รหัสผ่านไม่ถูกต้อง: ต้องมีอย่างน้อย 4 ตัวอักษร")
		waitEnter(r)
		return nil
	}

	daysStr, err := readLine(r, "Expire:")
	if err != nil {
		return err
	}
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 1 {
		errLine("จำนวนวันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 1 ขึ้นไป")
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

	host, err := readLine(r, "Remote IP ["+defaultRemoteHost()+"]:")
	if err != nil {
		return err
	}
	if host == "" {
		host = defaultRemoteHost()
	}

	res, err := user.Add(
		user.AddInput{
			Name:          name,
			Password:      pw,
			ExpiresInDays: days,
			Limit:         limit,
		},
		user.OVPNInput{
			RemoteHost: host,
			RemotePort: 1194,
			Proto:      "udp",
		},
	)
	if err != nil {
		errLine("สร้างผู้ใช้ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	ovpnPath := "/root/" + name + ".ovpn"
	if err := os.WriteFile(ovpnPath, res.OVPN, 0o600); err != nil {
		errLine("บันทึกไฟล์ .ovpn ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	clearScreen()
	fmt.Println("\033[44;1;37m       สร้างบัญชี SSH แล้ว !       \033[0m")
	fmt.Println()
	fmt.Printf("%sIP: %s%s\n", cGrnBold, cWhtBold, host)
	fmt.Printf("%sUser: %s%s\n", cGrnBold, cWhtBold, name)
	fmt.Printf("%sPassword: %s%s\n", cGrnBold, cWhtBold, pw)
	if !res.Record.ExpiresAt.IsZero() {
		fmt.Printf("%sExpire: %s%s\n", cGrnBold, cWhtBold, res.Record.ExpiresAt.Format("02/01/2006"))
	}
	fmt.Printf("%slimited connection: %s%d\n", cGrnBold, cWhtBold, limit)
	fmt.Println()
	okLine("เพิ่มผู้ใช้สำเร็จ!")
	fmt.Printf("%sที่อยู่ไฟล์: %s%s%s\n", cGrnBold, cCyanBold, ovpnPath, cReset)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 02 สร้างบัญชี ผุ้ใช้ทดลอง
// ---------------------------------------------------------------------

func runCreateTrial(r *bufio.Reader) error {
	userHeader("สร้างบัญชีผู้ใช้ทดลอง")

	ans, err := readLine(r, "จะสร้างทดลองใหม่หรือไม่? [Y/n]:")
	if err != nil {
		return err
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	if ans != "" && ans != "y" && ans != "yes" {
		fmt.Println(cYelBold + "ยกเลิก." + cReset)
		waitEnter(r)
		return nil
	}

	// 4-digit suffix via crypto/rand so duplicates are statistically
	// negligible without us tracking previous trial names.
	suffix, err := rand.Int(rand.Reader, big.NewInt(10000))
	if err != nil {
		return err
	}
	name := fmt.Sprintf("test%04d", suffix.Int64())
	pw := fmt.Sprintf("test%04d", suffix.Int64())

	host := defaultRemoteHost()
	res, err := user.Add(
		user.AddInput{
			Name:          name,
			Password:      pw,
			ExpiresInDays: 1,
			Limit:         1,
		},
		user.OVPNInput{
			RemoteHost: host,
			RemotePort: 1194,
			Proto:      "udp",
		},
	)
	if err != nil {
		errLine("สร้างผู้ใช้ทดลองไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	ovpnPath := "/root/" + name + ".ovpn"
	if err := os.WriteFile(ovpnPath, res.OVPN, 0o600); err != nil {
		errLine("บันทึกไฟล์ .ovpn ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	clearScreen()
	fmt.Println("\033[44;1;37m     สร้างบัญชีผู้ใช้ทดลอง เรียบร้อย     \033[0m")
	fmt.Println()
	fmt.Printf("%sIP: %s%s\n", cGrnBold, cWhtBold, host)
	fmt.Printf("%sผู้ใช้: %s%s\n", cGrnBold, cWhtBold, name)
	fmt.Printf("%sรหัสผ่าน: %s%s\n", cGrnBold, cWhtBold, pw)
	fmt.Printf("%sจำนวนอุปกรณ์ที่ใช้พร้อมกัน: %s%d\n", cGrnBold, cWhtBold, 1)
	fmt.Printf("%sอายุใช้งาน: %s%s\n", cGrnBold, cWhtBold, "1 วัน")
	fmt.Println()
	fmt.Printf("%sที่อยู่ไฟล์: %s%s%s\n", cGrnBold, cCyanBold, ovpnPath, cReset)
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 03 ลบชื่อ ผู้ใช้
// ---------------------------------------------------------------------

func runRemoveUser(r *bufio.Reader) error {
	userHeader("ลบชื่อผู้ใช้ SSH")

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
	confirm = strings.ToLower(strings.TrimSpace(confirm))
	if confirm != "y" && confirm != "yes" {
		fmt.Println(cYelBold + "ยกเลิก." + cReset)
		waitEnter(r)
		return nil
	}

	if err := user.Remove(rec.Name); err != nil {
		errLine("ลบผู้ใช้ไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	fmt.Println()
	fmt.Printf("\033[41;1;37m User %s ลบเรียบร้อย! \033[0m\n", rec.Name)
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

	records, err := user.List()
	if err != nil {
		errLine("อ่านรายชื่อไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

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
		limit := rec.Limit
		if limit == 0 {
			limit = 1
		}
		fmt.Printf("%s %-15s %s %-13s %-10s %s\n",
			cYelBold,
			rec.Name,
			statusText,
			fmt.Sprintf("%d/%d", conex, limit),
			"--:--:--",
			cReset)
		fmt.Println(cBluBold + separator + cReset)
	}
	waitEnter(r)
	return nil
}

// readSSHLogins returns a map[user]count of active sshd: sessions, by
// parsing `ps -eo user:32,comm` and filtering for the sshd "user@pts"
// or session-with-username form. We treat any process whose user is in
// the records list as one session.
func readSSHLogins() map[string]int {
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
		if !strings.Contains(fields[1], "sshd") {
			continue
		}
		if fields[0] == "root" || fields[0] == "sshd" {
			continue
		}
		out[fields[0]]++
	}
	return out
}

// readOpenVPNUsers parses /etc/openvpn/openvpn-status.log for CLIENT_LIST
// rows. Format: CLIENT_LIST,<cn>,<addr>,...
func readOpenVPNUsers() map[string]int {
	out := map[string]int{}
	data, err := os.ReadFile("/etc/openvpn/openvpn-status.log")
	if err != nil {
		return out
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "CLIENT_LIST,") {
			continue
		}
		parts := strings.Split(line, ",")
		if len(parts) < 2 {
			continue
		}
		out[parts[1]]++
	}
	return out
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
	fmt.Println(cRedBold + "EX:" + cYelBold + " จำนวนวันจากวันนี้ เช่น 30 (ใส่ 0 = ไม่หมดอายุ)" + cReset)
	fmt.Println()
	daysStr, err := readLine(r, fmt.Sprintf("วันหมดอายุใหม่สำหรับ %s:", rec.Name))
	if err != nil {
		return err
	}
	if daysStr == "" {
		errLine("ไม่ได้ระบุจำนวนวัน — กรุณาพิมพ์จำนวนวัน เช่น 30")
		waitEnter(r)
		return nil
	}
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 0 {
		errLine("จำนวนวันไม่ถูกต้อง: ต้องเป็นตัวเลขตั้งแต่ 0 ขึ้นไป")
		waitEnter(r)
		return nil
	}

	if err := user.UpdateExpiry(rec.Name, days); err != nil {
		errLine("เปลี่ยนวันหมดอายุไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}

	if days == 0 {
		fmt.Println()
		fmt.Printf("\033[44;1;37m เปลี่ยนวันหมดอายุของผู้ใช้ %s สำเร็จ: ไม่หมดอายุ \033[0m\n", rec.Name)
	} else {
		expDate := time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Format("02/01/2006")
		fmt.Println()
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

	limStr, err := readLine(r, fmt.Sprintf("SELECT CONNECTION FOR USER? %s:", rec.Name))
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

	pw, err := readLine(r, fmt.Sprintf("PASSWORD FOR USER %s:", rec.Name))
	if err != nil {
		return err
	}
	if len(pw) < 4 {
		errLine("รหัสผ่านไม่ถูกต้อง: ต้องมีอย่างน้อย 4 ตัวอักษร")
		waitEnter(r)
		return nil
	}

	if err := user.UpdatePassword(rec.Name, pw); err != nil {
		errLine("เปลี่ยนรหัสผ่านไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
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

	removed, err := user.CleanExpired()
	if err != nil {
		errLine("ลบผู้ใช้ที่หมดอายุไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	fmt.Printf("%sลบแล้ว %s%d %suser%s\n", cGrnBold, cWhtBold, len(removed), cGrnBold, cReset)
	for _, name := range removed {
		fmt.Printf("  %s- %s%s%s\n", cRedBold, cWhtBold, name, cReset)
	}
	waitEnter(r)
	return nil
}

// ---------------------------------------------------------------------
// 09 เช็คบัญชีทั้งหมด
// ---------------------------------------------------------------------

func runListUsers(r *bufio.Reader) error {
	clearScreen()
	fmt.Println("\033[44;1;37m ผู้ใช้          สร้างเมื่อ      หมดอายุ         Limit  สถานะ \033[0m")
	fmt.Println()

	records, err := user.List()
	if err != nil {
		errLine("อ่านรายชื่อไม่สำเร็จ: " + err.Error())
		waitEnter(r)
		return nil
	}
	if len(records) == 0 {
		errLine("แสดงรายชื่อผู้ใช้ไม่ได้: ไม่พบผู้ใช้ในระบบ — กรุณาสร้างผู้ใช้ก่อน")
		waitEnter(r)
		return nil
	}

	now := time.Now().UTC()
	for _, rec := range records {
		created := rec.CreatedAt.Format("02/01/2006")
		exp := "ไม่หมดอายุ"
		status := cGrnBold + "ใช้งานได้" + cReset
		if !rec.ExpiresAt.IsZero() {
			exp = rec.ExpiresAt.Format("02/01/2006")
			if rec.ExpiresAt.Before(now) {
				status = cRedBold + "หมดอายุ" + cReset
			}
		}
		limit := "-"
		if rec.Limit > 0 {
			limit = strconv.Itoa(rec.Limit)
		}
		fmt.Printf("%s %-15s %s%-13s %s%-15s %s%-6s %s\n",
			cYelBold, rec.Name,
			cWhtBold, created,
			cWhtBold, exp,
			cWhtBold, limit,
			status)
		fmt.Println(cBluBold + separator + cReset)
	}
	waitEnter(r)
	return nil
}
