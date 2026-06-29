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
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/sslhmux"
	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// runConexao paints the sub-menu and dispatches. Loops until the user
// picks 09 (back) or 00 (exit).
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
			os.Exit(0)
		case "1", "01":
			if err := opensshMenu(r); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "2", "02":
			if err := serviceMenu(r, "squid"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "3", "03":
			if err := serviceMenu(r, "dropbear"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "4", "04":
			if err := serviceMenu(r, "openvpn"); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "5", "05":
			if err := runProxies(r); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "6", "06":
			if err := sslTunnelMenu(r); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "7", "07":
			if err := sslhMuxMenu(r); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "9", "09":
			return nil
		default:
			fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
			waitEnter(r)
		}
	}
}

func paintConexaoHeader() {
	paintTitleBar("           โหมดฟังชั่นเพิ่มเติม             ")
	fmt.Println()
	// Show running services with ports (mirrors v1 conexao top-of-screen block)
	if isSSHActive() {
		sshPorts := readSSHPorts()
		strs := make([]string, len(sshPorts))
		for i, p := range sshPorts {
			strs[i] = strconv.Itoa(p)
		}
		fmt.Printf("%sบริการ: %sOPENSSH %sพอร์ต: %s%s%s\n",
			cWhtBold, cYelBold, cWhtBold, cCyanBold, strings.Join(strs, " "), cReset)
	}
	for _, pair := range []struct{ label, key string }{
		{"OPENVPN", "openvpn"},
		{"DROPBEAR", "dropbear"},
		{"SQUID PROXY", "squid"},
	} {
		svc, ok := service.ByName(pair.key)
		if !ok {
			continue
		}
		st, _ := service.Status(svc)
		if st.ActiveState != "active" {
			continue
		}
		port, _ := readPersistedPort(svc)
		if port == 0 {
			port = svc.Port
		}
		fmt.Printf("%sบริการ: %s%s %sพอร์ต: %s%d%s\n",
			cWhtBold, cYelBold, pair.label, cWhtBold, cCyanBold, port, cReset)
	}
	// Proxy SOCKS: show all active proxy ports (may be multiple)
	if proxyPorts := activeProxyPorts(); len(proxyPorts) > 0 {
		strs := make([]string, len(proxyPorts))
		for i, p := range proxyPorts {
			strs[i] = strconv.Itoa(p)
		}
		fmt.Printf("%sบริการ: %sPROXY SOCKS %sพอร์ต: %s%s%s\n",
			cWhtBold, cYelBold, cWhtBold, cCyanBold, strings.Join(strs, " "), cReset)
	}
	printSep()
}

// activeProxyPorts returns ports of all active hexplus-proxy-* units,
// sorted ascending. Used by paintConexaoHeader to show live proxy ports.
func activeProxyPorts() []int {
	db, err := proxy.Load()
	if err != nil {
		return nil
	}
	var ports []int
	for _, cfg := range db.Proxies {
		if exec.Command("systemctl", "is-active", "--quiet", cfg.UnitName()).Run() == nil {
			ports = append(ports, cfg.Port)
		}
	}
	sort.Ints(ports)
	return ports
}

func paintConexaoMenu() {
	states, _ := service.StatusAll()
	stateOf := map[string]service.State{}
	for _, s := range states {
		stateOf[s.Service.Name] = s
	}

	items := []struct{ idx, name, key string }{
		{"01", "OPENSSH", "ssh"},
		{"02", "SQUID PROXY", "squid"},
		{"03", "DROPBEAR", "dropbear"},
		{"04", "OPENVPN", "openvpn"},
		{"05", "PROXY SOCKS", "proxy"},
		{"06", "SSL TUNNEL", "ssltunnel"},
		{"07", "SSLH MULTIPLEX", "sslhmux"},
	}
	fmt.Println()
	for _, it := range items {
		var marker string
		switch it.key {
		case "":
			marker = markerOff()
		case "sslhmux":
			if exec.Command("systemctl", "is-active", "--quiet", sslhmux.UnitName).Run() == nil {
				marker = markerOn()
			} else {
				marker = markerOff()
			}
		case "ssltunnel":
			if exec.Command("systemctl", "is-active", "--quiet", ssltunnel.UnitName).Run() == nil {
				marker = markerOn()
			} else {
				marker = markerOff()
			}
		case "ssh":
			if isSSHActive() {
				marker = markerOn()
			} else {
				marker = markerOff()
			}
		case "proxy":
			if isAnyProxyActive() {
				marker = markerOn()
			} else {
				marker = markerOff()
			}
		default:
			if stateOf[it.key].ActiveState == "active" {
				marker = markerOn()
			} else {
				marker = markerOff()
			}
		}
		fmt.Printf("%s[%s%s%s] %s• %s%-16s%s%s\n",
			cRedBold, cCyanBold, it.idx, cRedBold,
			cWhtBold, cYelBold, it.name, marker, cReset)
	}
	fmt.Printf("%s[%s09%s] %s• %sย้อนกลับ <<<%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Printf("%s[%s00%s] %s• %sออก <<<%s\n",
		cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
	fmt.Println()
	printSep()
	fmt.Println()
}

// readSSHPorts reads all active Port lines from sshd_config.
// OpenSSH supports multiple Port directives; v1 uses this feature for
// "add port" / "delete port" instead of a single replace.
func readSSHPorts() []int {
	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return []int{22}
	}
	re := regexp.MustCompile(`(?m)^\s*Port\s+(\d+)`)
	matches := re.FindAllSubmatch(data, -1)
	var out []int
	for _, m := range matches {
		if p, err := strconv.Atoi(string(m[1])); err == nil && p > 0 {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return []int{22}
	}
	return out
}

// readSSHPort returns the first SSH port (for header display).
func readSSHPort() int {
	return readSSHPorts()[0]
}

// isSSHActive returns true when sshd is running under any common unit name.
func isSSHActive() bool {
	for _, unit := range []string{"ssh", "sshd", "openssh-server"} {
		if exec.Command("systemctl", "is-active", "--quiet", unit).Run() == nil {
			return true
		}
	}
	return false
}

// isAnyProxyActive returns true when at least one hexplus-proxy-* unit is active.
func isAnyProxyActive() bool {
	out, err := exec.Command("systemctl", "list-units", "--state=active",
		"--no-legend", "hexplus-proxy-*.service").Output()
	return err == nil && len(bytes.TrimSpace(out)) > 0
}

// opensshMenu mirrors v1's fun_openssh: add port / delete port.
// OpenSSH can listen on multiple ports via multiple Port lines in sshd_config.
func opensshMenu(r *bufio.Reader) error {
	for {
		clearScreen()
		paintTitleBar("            OPENSSH           ")
		ports := readSSHPorts()
		portStrs := make([]string, len(ports))
		for i, p := range ports {
			portStrs[i] = strconv.Itoa(p)
		}
		fmt.Println()
		fmt.Printf("%sพอร์ตที่ใช้งาน: %s%s%s\n\n",
			cWhtBold, cYelBold, strings.Join(portStrs, " "), cReset)
		paintOptions([][2]string{
			{"1", "เพิ่มพอร์ต SSH"},
			{"2", "ลบพอร์ต SSH"},
			{"3", "รีสตาร์ท SSH"},
			{"4", "ดู log SSH"},
			{"0", "ย้อนกลับ"},
		})
		fmt.Println()
		printSep()
		fmt.Println()
		choice, err := menuPrompt(r)
		if err != nil {
			return err
		}
		switch choice {
		case "1", "01":
			if err := sshAddPort(r, ports); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "2", "02":
			if err := sshDeletePort(r, ports); err != nil {
				fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
		case "3", "03":
			restartSSH()
			fmt.Println("\n" + cGrnBold + "รีสตาร์ท SSH สำเร็จ" + cReset)
			waitEnter(r)
		case "4", "04":
			showUnitLog(r, "ssh", "sshd")
		case "0", "00":
			return nil
		}
	}
}

// sshAddPort appends a new Port line to sshd_config and restarts sshd.
func sshAddPort(r *bufio.Reader, current []int) error {
	clearScreen()
	paintTitleBar("         เพิ่มพอร์ต SSH         ")
	fmt.Println()
	fmt.Print(cGrnBold + "พอร์ตที่ต้องการเพิ่ม" + cYelBold + ": " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	portStr := strings.TrimSpace(line)
	newPort, convErr := strconv.Atoi(portStr)
	if convErr != nil || newPort < 1 || newPort > 65535 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}
	for _, p := range current {
		if p == newPort {
			fmt.Printf("%s[ผิดพลาด] พอร์ต %d มีอยู่แล้ว%s\n", cRedBold, newPort, cReset)
			waitEnter(r)
			return nil
		}
	}
	if err := checkPortFree(newPort, "tcp"); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		waitEnter(r)
		return nil
	}
	data, rdErr := os.ReadFile("/etc/ssh/sshd_config")
	if rdErr != nil {
		return fmt.Errorf("อ่าน sshd_config ไม่ได้: %w", rdErr)
	}
	// If sshd_config has NO explicit Port lines yet the default 22 is implicit.
	// Writing Port <new> alone would drop 22 — so preserve it explicitly first.
	hasExplicit := regexp.MustCompile(`(?m)^\s*Port\s+\d+`).MatchString(string(data))
	portLine := fmt.Sprintf("Port %d", newPort)
	if !hasExplicit {
		portLine = "Port 22\n" + portLine
	}
	// Insert BEFORE any Match block — appending after Match puts the Port
	// directive inside the block, which sshd rejects at startup.
	conf := string(data)
	matchIdx := strings.Index(conf, "\nMatch ")
	var newConf string
	if matchIdx >= 0 {
		newConf = strings.TrimRight(conf[:matchIdx], "\n") +
			"\n" + portLine + "\n" +
			conf[matchIdx+1:]
	} else {
		newConf = strings.TrimRight(conf, "\n") + "\n" + portLine + "\n"
	}
	if err := os.WriteFile("/etc/ssh/sshd_config", []byte(newConf), 0o644); err != nil {
		return fmt.Errorf("เขียน sshd_config ไม่ได้: %w", err)
	}
	restartSSH()
	// Verify the new port is listening (mirrors v1 netstat check).
	time.Sleep(700 * time.Millisecond)
	listening, _ := service.ListenStatus(newPort, "tcp")
	if listening {
		fmt.Printf("\n%sเพิ่มพอร์ต SSH %d สำเร็จ%s\n", cGrnBold, newPort, cReset)
	} else {
		fmt.Printf("\n%s[ผิดพลาด] เพิ่มพอร์ตไม่สำเร็จ — ตรวจสอบ journalctl -u ssh%s\n", cRedBold, cReset)
	}
	waitEnter(r)
	return nil
}

// sshDeletePort removes one Port line from sshd_config and restarts sshd.
func sshDeletePort(r *bufio.Reader, current []int) error {
	clearScreen()
	paintTitleBar("          ลบพอร์ต SSH          ")
	portStrs := make([]string, len(current))
	for i, p := range current {
		portStrs[i] = strconv.Itoa(p)
	}
	fmt.Printf("\n%s[!] Default port 22 — ระวังอย่าลบพอร์ตสุดท้าย%s\n", cYelBold, cReset)
	fmt.Printf("%sพอร์ตปัจจุบัน: %s%s%s\n\n", cWhtBold, cYelBold, strings.Join(portStrs, " "), cReset)
	fmt.Print(cGrnBold + "พอร์ตที่ต้องการลบ" + cYelBold + ": " + cReset)
	line, err := r.ReadString('\n')
	if err != nil {
		return err
	}
	portStr := strings.TrimSpace(line)
	delPort, convErr := strconv.Atoi(portStr)
	if convErr != nil || delPort < 1 {
		fmt.Println(cRedBold + "[ผิดพลาด] พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}
	found := false
	for _, p := range current {
		if p == delPort {
			found = true
			break
		}
	}
	if !found {
		fmt.Printf("%s[ผิดพลาด] ไม่พบพอร์ต %d ใน sshd_config%s\n", cRedBold, delPort, cReset)
		waitEnter(r)
		return nil
	}
	data, rdErr := os.ReadFile("/etc/ssh/sshd_config")
	if rdErr != nil {
		return fmt.Errorf("อ่าน sshd_config ไม่ได้: %w", rdErr)
	}
	// Remove all "Port <delPort>" lines (sed -i "/Port $pt/d" equivalent).
	re := regexp.MustCompile(fmt.Sprintf(`(?m)^\s*Port\s+%d\s*\n?`, delPort))
	newConf := re.ReplaceAllString(string(data), "")
	if err := os.WriteFile("/etc/ssh/sshd_config", []byte(newConf), 0o644); err != nil {
		return fmt.Errorf("เขียน sshd_config ไม่ได้: %w", err)
	}
	restartSSH()
	fmt.Printf("\n%sลบพอร์ต SSH %d สำเร็จ%s\n", cGrnBold, delPort, cReset)
	waitEnter(r)
	return nil
}

// ensureSSHConfig enforces the four directives that v1 writes during
// changeroot + dropbear install. Called once when hexplus starts so
// every fresh VPS has the right sshd_config without extra steps.
//
// Uses the same idempotent pattern as v1:
//   remove any existing line (commented or not) → append the wanted value
//
// Silently no-ops when not root or sshd_config is unreadable.
// Restarts sshd only when at least one directive was changed.
func ensureSSHConfig() {
	if os.Geteuid() != 0 {
		return
	}
	data, err := os.ReadFile("/etc/ssh/sshd_config")
	if err != nil {
		return
	}
	conf := string(data)
	changed := false
	for _, kv := range [][2]string{
		{"PasswordAuthentication", "yes"},
		{"PermitRootLogin", "yes"},
		{"PermitTunnel", "yes"},
		{"UsePAM", "yes"},
	} {
		conf, changed = ensureSSHDirective(conf, kv[0], kv[1], changed)
	}
	// /bin/false must be in /etc/shells so PAM (pam_shells) lets users with
	// that shell log in via SSH. v1 conexao:481 does the same on dropbear install.
	ensureFalseShell()

	// Migrate existing hexplus users from /bin/false to /bin/sh so ForceCommand
	// works (sshd runs ForceCommand via "shell -c cmd"; /bin/false ignores -c).
	ensureUserShells()

	// Match block must live at the end of sshd_config.
	conf, changed = ensureSSHMatchBlock(conf, changed)

	if !changed {
		return
	}
	if err := os.WriteFile("/etc/ssh/sshd_config", []byte(conf), 0o644); err != nil {
		return
	}
	restartSSH()
}

// ensureSSHMatchBlock appends (or re-writes) a Match User *,!root block at the
// end of sshd_config that forces /bin/sleep infinity as the command.
// Without this, SSH tunnelling apps (HTTP Injector, etc.) that open a session
// channel alongside the SOCKS5 channel see the /bin/false exit and immediately
// tear down the SOCKS5 channel, making traffic stop flowing.
// /bin/sleep infinity doesn't read stdin (avoiding the EOF-exit problem of
// /bin/cat in non-interactive sessions) and runs until sshd terminates it on
// disconnect, keeping the session channel alive for the duration of the tunnel.
func ensureSSHMatchBlock(conf string, alreadyChanged bool) (string, bool) {
	const (
		matchLine = "Match User *,!root"
		forceLine = "    ForceCommand /bin/sleep infinity"
	)
	// Strip any existing Match User *,!root block we previously wrote so we
	// can re-append a canonical version without duplicates.
	lines := strings.Split(conf, "\n")
	var kept []string
	skipUntilNextMatch := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == matchLine {
			skipUntilNextMatch = true
			continue
		}
		if skipUntilNextMatch {
			// A new "Match" keyword ends the block we're skipping.
			if strings.HasPrefix(trimmed, "Match ") {
				skipUntilNextMatch = false
			} else {
				continue
			}
		}
		kept = append(kept, line)
	}
	rebuilt := strings.TrimRight(strings.Join(kept, "\n"), "\n")

	// Check whether the block was already correct in the original conf.
	alreadyHad := strings.Contains(conf, matchLine) &&
		strings.Contains(conf, forceLine)

	rebuilt += "\n" + matchLine + "\n" + forceLine + "\n"
	if alreadyHad {
		// Block was correct already; just return the rebuilt text unchanged.
		return rebuilt, alreadyChanged
	}
	return rebuilt, true
}

// ensureUserShells migrates hexplus users whose shell is /bin/false to /bin/sh.
// ForceCommand in sshd_config (Match User *,!root) prevents real shell access.
func ensureUserShells() {
	data, err := os.ReadFile("/etc/passwd")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		parts := strings.Split(line, ":")
		if len(parts) < 7 {
			continue
		}
		name, shell := parts[0], parts[6]
		if name == "root" || strings.TrimSpace(shell) != "/bin/false" {
			continue
		}
		// Only migrate users created by hexplus (uid >= 1000, non-system).
		uid, _ := strconv.Atoi(parts[2])
		if uid < 1000 {
			continue
		}
		_ = exec.Command("usermod", "-s", "/bin/sh", name).Run()
	}
}

// ensureFalseShell adds /bin/false to /etc/shells when absent.
func ensureFalseShell() {
	data, err := os.ReadFile("/etc/shells")
	if err != nil {
		return
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "/bin/false" {
			return
		}
	}
	f, err := os.OpenFile("/etc/shells", os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString("/bin/false\n")
}

// ensureSSHDirective guarantees that exactly one line "key val" exists in
// the sshd_config text. Removes commented variants and duplicates.
//
// When the directive needs to be inserted, it lands BEFORE the first Match
// block: directives that fall inside a Match block are scoped to it and
// sshd refuses to start when global keywords (Port, PasswordAuthentication)
// appear inside Match.
func ensureSSHDirective(conf, key, val string, alreadyChanged bool) (string, bool) {
	want := key + " " + val
	var kept []string
	found := false
	changed := alreadyChanged
	for _, line := range strings.Split(conf, "\n") {
		bare := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
		if strings.HasPrefix(bare, key+" ") || bare == key {
			if strings.TrimSpace(line) == want && !found {
				kept = append(kept, line)
				found = true
			} else {
				changed = true
			}
			continue
		}
		kept = append(kept, line)
	}
	if !found {
		insertAt := -1
		for i, line := range kept {
			if strings.HasPrefix(strings.TrimSpace(line), "Match ") {
				insertAt = i
				break
			}
		}
		if insertAt < 0 {
			kept = append(kept, want)
		} else {
			merged := make([]string, 0, len(kept)+1)
			merged = append(merged, kept[:insertAt]...)
			merged = append(merged, want)
			merged = append(merged, kept[insertAt:]...)
			kept = merged
		}
		changed = true
	}
	return strings.Join(kept, "\n"), changed
}

// restartSSH tries common systemd unit names for the SSH daemon.
func restartSSH() {
	for _, unit := range []string{"ssh", "sshd", "openssh-server"} {
		if exec.Command("systemctl", "restart", unit).Run() == nil {
			return
		}
	}
}

// serviceMenu routes to the per-service sub-menus that mirror v1's
// fun_squid / fun_drop / fun_openvpn layouts byte-for-byte (lives in
// service_menus.go). The previous generic start/stop/restart grid was
// removed - it didn't match v1 and confused operators who came from
// the bash script.
func serviceMenu(r *bufio.Reader, name string) error {
	svc, ok := service.ByName(name)
	if !ok {
		return fmt.Errorf("unknown service %q", name)
	}
	switch name {
	case "squid":
		return squidMenu(r, svc)
	case "dropbear":
		return dropbearMenu(r, svc)
	case "openvpn":
		return openvpnMenu(r, svc)
	}
	// Fallback for any future service that's wired into service.All()
	// but doesn't have a custom menu yet: minimal generic flow.
	for {
		clearScreen()
		st, _ := service.Status(svc)
		paintServiceHeader(name)
		paintServiceState(st)
		paintServiceActions(st, name)
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

			// Match HEXPLUS v1 conexao: after install, ask whether to start
			// immediately + verify with a port-listening check. Default Y
			// because v1's apt-install path used to leave services
			// auto-started and customers expect "install → working".
			fmt.Print("\n" + cGrnBold + "เริ่มใช้งานเลยไหม? [Y/n]: " + cReset)
			line, _ := r.ReadString('\n')
			ans := strings.TrimSpace(line)
			startNow := ans == "" || strings.HasPrefix(strings.ToLower(ans), "y")
			if startNow {
				if enErr := service.Enable(svc); enErr != nil {
					fmt.Println(cYelBold + "คำเตือน: เปิด autostart ไม่สำเร็จ: " + enErr.Error() + cReset)
				}
				if stErr := service.Start(svc); stErr != nil {
					fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
						" เริ่ม " + svc.Name + " ไม่สำเร็จ: " + stErr.Error() +
						" — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
				} else {
					time.Sleep(700 * time.Millisecond)
					listening, _ := service.ListenStatus(svc.Port, svc.PortProto)
					if listening {
						fmt.Println(cGrnBold + "เปิดใช้งาน " + svc.Name + " สำเร็จแล้ว — พอร์ต " +
							strconv.Itoa(svc.Port) + cReset)
					} else {
						fmt.Println(cRedBold + "[ผิดพลาด]" + cYelBold +
							" " + svc.Name + " เริ่มทำงานแต่ไม่พบ socket ที่พอร์ต " +
							strconv.Itoa(svc.Port) + " — ตรวจสอบ journalctl -u " + svc.UnitName + cReset)
					}
				}
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
		case "7": // change port
			if !st.UnitExists {
				fmt.Println("\n" + cYelBold + "ติดตั้งก่อนจึงเปลี่ยนพอร์ตได้" + cReset)
				waitEnter(r)
				continue
			}
			if err := changeServicePort(r, svc); err != nil {
				return err
			}
		case "8": // openvpn-only: payload editor
			if name != "openvpn" || !st.UnitExists {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด]" + cYelBold + " ตัวเลือกไม่ถูกต้อง" + cReset)
				waitEnter(r)
				continue
			}
			if err := runPayload(r); err != nil {
				fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
				waitEnter(r)
			}
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

func paintServiceActions(st service.State, name string) {
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
			{"7", "เปลี่ยนพอร์ต"},
		}
		// openvpn gets one extra action: rewrite a user's .ovpn with
		// a carrier-portal `remote` line for HTTP Injector / KPN Tunnel
		// use. Squid and Dropbear don't ship .ovpn files, so we skip
		// the option there to keep the grid honest.
		if name == "openvpn" {
			actions = append(actions, struct{ idx, label string }{"8", "ปรับแต่ง Payload"})
		}
		actions = append(actions, struct{ idx, label string }{"9", "ถอนการติดตั้ง"})
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

// changeServicePort drives the "พอร์ตปัจจุบัน NN. พอร์ตใหม่ ?" prompt
// for one service. We persist into the service's own config (openvpn,
// squid) or a systemd drop-in (dropbear, which v1 had no config file
// for) and then daemon-reload + restart so the new port is live.
func changeServicePort(r *bufio.Reader, svc service.Service) error {
	clearScreen()
	printSep()
	fmt.Printf("\033[44;1;37m            เปลี่ยนพอร์ต %s            \033[0m\n", svc.Name)
	printSep()
	fmt.Println()

	currentPort, _ := readPersistedPort(svc)
	if currentPort == 0 {
		currentPort = svc.Port
	}

	fmt.Printf("%sพอร์ตปัจจุบัน: %s%d%s\n", cWhtBold, cYelBold, currentPort, cReset)
	in, err := promptLine(r, "พอร์ตใหม่ (1-65535, 0 = ยกเลิก): ")
	if err != nil {
		return err
	}
	if in == "" || in == "0" {
		return nil
	}
	newPort, err := strconv.Atoi(in)
	if err != nil || newPort < 1 || newPort > 65535 {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + "พอร์ตไม่ถูกต้อง" + cReset)
		waitEnter(r)
		return nil
	}

	needReload := false
	switch svc.Name {
	case "openvpn":
		if err := rewriteConfPort("/etc/openvpn/server.conf", `(?m)^\s*port\s+\d+\b`, fmt.Sprintf("port %d", newPort)); err != nil {
			return err
		}
	case "squid":
		if err := rewriteConfPort("/etc/squid/squid.conf", `(?m)^\s*http_port\s+\d+\b`, fmt.Sprintf("http_port %d", newPort)); err != nil {
			return err
		}
	case "dropbear":
		if err := writeDropbearPortDropIn(newPort); err != nil {
			return err
		}
		needReload = true
	default:
		return fmt.Errorf("ไม่รองรับการเปลี่ยนพอร์ตของ %s", svc.Name)
	}

	if needReload {
		if out, err := exec.Command("systemctl", "daemon-reload").CombinedOutput(); err != nil {
			fmt.Println("\n" + cYelBold + "คำเตือน: systemctl daemon-reload: " + string(out) + cReset)
		}
	}
	if err := service.Restart(svc); err != nil {
		fmt.Println("\n" + cYelBold + "คำเตือน: รีสตาร์ทไม่สำเร็จ: " + err.Error() + cReset)
	}
	fmt.Println("\n" + cGrnBold + fmt.Sprintf("เปลี่ยนพอร์ตเป็น %d สำเร็จ", newPort) + cReset)
	waitEnter(r)
	return nil
}

// readPersistedPort tries to extract the currently-configured port from
// disk. Returns 0 + nil when the file doesn't exist (caller falls back
// to svc.Port).
func readPersistedPort(svc service.Service) (int, error) {
	switch svc.Name {
	case "openvpn":
		return scanFilePort("/etc/openvpn/server.conf", `(?m)^\s*port\s+(\d+)\b`)
	case "squid":
		return scanFilePort("/etc/squid/squid.conf", `(?m)^\s*http_port\s+(\d+)\b`)
	case "dropbear":
		path := filepath.Join(service.SystemdUnitDir, "hexplus-dropbear.service.d", "port.conf")
		return scanFilePort(path, `(?m)DROPBEAR_PORT=(\d+)`)
	}
	return 0, nil
}

func scanFilePort(path, pattern string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	re := regexp.MustCompile(pattern)
	m := re.FindSubmatch(data)
	if len(m) < 2 {
		return 0, nil
	}
	return strconv.Atoi(string(m[1]))
}

// rewriteConfPort rewrites the first `port`/`http_port` line in a
// service config. Idempotent: writes are skipped when the file already
// matches the desired output. Replacement is anchored with regexp so
// commented-out lines (#port 1194) are untouched.
func rewriteConfPort(path, pattern, replacement string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("อ่าน %s: %w", path, err)
	}
	re := regexp.MustCompile(pattern)
	if !re.Match(data) {
		// Fall through to append at the end — the line wasn't there.
		out := bytes.TrimRight(data, "\n")
		out = append(out, '\n')
		out = append(out, []byte(replacement+"\n")...)
		return writeIfChanged(path, data, out)
	}
	out := re.ReplaceAll(data, []byte(replacement))
	return writeIfChanged(path, data, out)
}

func writeIfChanged(path string, prev, next []byte) error {
	if bytes.Equal(prev, next) {
		return nil
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, next, 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", path, err)
	}
	return nil
}

// writeDropbearPortDropIn rewrites
// /etc/systemd/system/hexplus-dropbear.service.d/port.conf with a fresh
// ExecStart= override. Two ExecStart= lines: the blank one clears the
// inherited ExecStart from the parent unit, the second line is what we
// actually want systemd to run. (systemd quirk: appending one
// ExecStart= without first blanking would queue *both*.)
func writeDropbearPortDropIn(port int) error {
	dir := filepath.Join(service.SystemdUnitDir, "hexplus-dropbear.service.d")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// v1 always adds port 110 as extra (DROPBEAR_EXTRA_ARGS="-p 110").
	extra := " -p 110"
	if port == 110 {
		extra = ""
	}
	content := fmt.Sprintf(`# Generated by hexplus menu (port change). Safe to delete to revert.
[Service]
Environment=DROPBEAR_PORT=%d
ExecStart=
ExecStart=/usr/local/lib/hexplus/dropbear -F -R -p %d%s
ReadWritePaths=/etc/dropbear
`, port, port, extra)
	dest := filepath.Join(dir, "port.conf")
	prev, _ := os.ReadFile(dest)
	if bytes.Equal(prev, []byte(content)) {
		return nil
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, []byte(content), 0o644); err != nil {
		return fmt.Errorf("เขียน %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s: %w", dest, err)
	}
	return nil
}
