// stats.go: collect the system-info numbers v1 paints under the banner
// (OS / RAM / CPU / time / online users / expired users / total users).
//
// v1 ran a separate awk/free/top/grep for each. We do the same in Go
// without subprocess for everything except `free` and `top` (no
// portable Go equivalent that doesn't depend on /proc parsing math).
// Acceptable trade-off: subprocesses run in ~1 ms each on a real box,
// well below the 2-second user-perceptible threshold.

package menu

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"time"
)

// SysStats is the populated set passed to the banner renderer.
type SysStats struct {
	OS         string
	RAMTotal   string
	CPUCores   int
	Time       string
	MemUsedPct string
	CPUUsedPct string
	OnlineNow  int
	ExpiredCt  int
	TotalUsers int
}

// CollectStats fills a SysStats by reading /etc/issue.net, /proc/cpuinfo,
// /etc/passwd, and shelling to `free` + `top` for the live percentages.
// Quiet on errors: empty fields render as the v1 fallback (blank space).
func CollectStats() SysStats {
	s := SysStats{
		OS:       readOSName(),
		CPUCores: runtime.NumCPU(),
		Time:     time.Now().Format("15:04:05"),
	}
	s.RAMTotal, s.MemUsedPct = readRAM()
	s.CPUUsedPct = readCPU()
	s.TotalUsers = countUsers()
	s.OnlineNow, s.ExpiredCt = countOnlineExpired()
	return s
}

// countOnlineExpired reuses the same data the users-list page uses
// (readSSHLogins + readOpenVPNUsers + chageExpiry against systemUsers)
// so the main-menu header can't disagree with the list — one user
// online there means at least one online here.
func countOnlineExpired() (online, expired int) {
	sshOnline := readSSHLogins()
	ovpnOnline := readOpenVPNUsers()
	for _, rec := range systemUsers() {
		if sshOnline[rec.Name] > 0 || ovpnOnline[rec.Name] > 0 {
			online++
		}
		exp, daysLeft := chageExpiry(rec.Name)
		if exp != "never" && exp != "" && daysLeft < 0 {
			expired++
		}
	}
	return online, expired
}

// readOSName mirrors the cut -d' ' lookups v1 does against /etc/issue.net.
// We're tolerant: missing file or weird format -> empty string, banner
// just shows blank where the OS would go.
func readOSName() string {
	data, err := os.ReadFile("/etc/issue.net")
	if err != nil {
		return ""
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return ""
	}
	// Ubuntu 22.04.5 LTS -> "Ubuntu 22"
	// Debian GNU/Linux 12 -> "Debian 12"
	switch fields[0] {
	case "Ubuntu":
		if len(fields) > 1 {
			return "Ubuntu " + strings.Split(fields[1], ".")[0]
		}
	case "Debian":
		if len(fields) > 2 {
			return "Debian " + fields[2]
		}
	}
	return fields[0]
}

// readRAM returns ("total human-readable", "used %") via `free` and
// `awk`-style math. Shelling is fine - free is in coreutils everywhere.
func readRAM() (string, string) {
	totalHum := ""
	usedPct := ""
	if out, err := exec.Command("free", "-h").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Mem:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					totalHum = fields[1]
				}
				break
			}
		}
	}
	if out, err := exec.Command("free", "-m").Output(); err == nil {
		for _, line := range strings.Split(string(out), "\n") {
			if strings.HasPrefix(line, "Mem:") {
				fields := strings.Fields(line)
				if len(fields) >= 3 {
					tot, _ := strconv.ParseFloat(fields[1], 64)
					used, _ := strconv.ParseFloat(fields[2], 64)
					if tot > 0 {
						usedPct = fmt.Sprintf("%.2f%%", used*100/tot)
					}
				}
				break
			}
		}
	}
	return totalHum, usedPct
}

// readCPU runs top in batch mode and extracts the same field v1's awk
// does (100 - idle %). Returns "" if top isn't installed (Alpine-slim).
func readCPU() string {
	cmd := exec.Command("top", "-bn1")
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "Cpu") {
			fields := strings.Fields(line)
			// v1 awk: $8 is the idle percentage; 100-$8 = used
			if len(fields) >= 8 {
				idle, err := strconv.ParseFloat(strings.TrimSuffix(fields[7], ","), 64)
				if err == nil {
					return fmt.Sprintf("%.0f%%", 100-idle)
				}
			}
			break
		}
	}
	return ""
}

// countUsers replicates v1: awk -F: '$3>=1000 {print $1}' /etc/passwd
// | grep -v nobody | wc -l. We open /etc/passwd directly so we don't
// fork awk + grep + wc just to count entries.
func countUsers() int {
	f, err := os.Open("/etc/passwd")
	if err != nil {
		return 0
	}
	defer f.Close()
	count := 0
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.SplitN(sc.Text(), ":", 7)
		if len(parts) < 3 {
			continue
		}
		uid, err := strconv.Atoi(parts[2])
		if err != nil || uid < 1000 {
			continue
		}
		if parts[0] == "nobody" {
			continue
		}
		count++
	}
	return count
}
