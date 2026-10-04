package menu

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// openvpnInstallTools are the host commands an OpenVPN install depends on:
// ip (egress detection for the FORWARD rules), iptables (NAT and FORWARD)
// and openssl (hexplus-auth.sh verifies every login with `openssl passwd`;
// without it every login is rejected).
var openvpnInstallTools = []string{"ip", "iptables", "openssl"}

// sbinDirs are where ip and iptables live on Debian and RHEL. A root shell
// opened with plain `su` on Debian keeps the caller's PATH, which lacks
// them; every exec.Command("iptables") in the installer would fail as well,
// so the tool's directory is added to PATH instead of reporting a package
// as missing when it is installed.
var sbinDirs = []string{"/usr/local/sbin", "/usr/sbin", "/sbin"}

// lookPath and statFile are exec.LookPath and os.Stat; variables so tests
// can fake them.
var (
	lookPath = exec.LookPath
	statFile = os.Stat
)

// missingTools returns the tools that cannot be found, in the given order.
// A tool found only in an sbin directory that is not on PATH is not
// missing: that directory is appended to this process's PATH.
func missingTools(tools []string) []string {
	var missing []string
	for _, t := range tools {
		if _, err := lookPath(t); err == nil {
			continue
		}
		if dir := sbinDirOf(t); dir != "" {
			appendToPath(dir)
			continue
		}
		missing = append(missing, t)
	}
	return missing
}

func sbinDirOf(tool string) string {
	for _, dir := range sbinDirs {
		st, err := statFile(filepath.Join(dir, tool))
		if err == nil && !st.IsDir() && st.Mode().Perm()&0o111 != 0 {
			return dir
		}
	}
	return ""
}

func appendToPath(dir string) {
	path := os.Getenv("PATH")
	for _, p := range filepath.SplitList(path) {
		if p == dir {
			return
		}
	}
	if path == "" {
		os.Setenv("PATH", dir)
		return
	}
	os.Setenv("PATH", strings.TrimRight(path, string(os.PathListSeparator))+string(os.PathListSeparator)+dir)
}
