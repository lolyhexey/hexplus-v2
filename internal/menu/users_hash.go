package menu

import (
	"fmt"
	"os"
	"strings"

	"github.com/lolyhexey/hexplus/internal/user"
)

// senhaDir holds the v1-style per-user password files. The create-user,
// create-trial and change-password menus write the plaintext password here
// (mode 0600), which is also what the user list prints.
var senhaDir = "/etc/SSHPlus/senha"

// setUserPassword is user.SetPassword; a variable so tests can fake it.
var setUserPassword = user.SetPassword

// repairUnverifiableHashes re-applies each user's stored password with
// user.SetPassword, which hashes as SHA-512, so a user whose hash the OpenVPN
// auth script cannot verify (yescrypt from before the chpasswd fix) can log
// in again.
//
//	fixed    — password re-applied
//	noStored — no stored password; the operator must set one (menu: change password)
//	failed   — chpasswd refused it
//
// The stored password is the one the hexplus menus last set. A password
// changed outside hexplus (plain `passwd`) is not reflected there, so this is
// only ever run after the operator confirms.
func repairUnverifiableHashes(names []string) (fixed, noStored []string, failed map[string]error) {
	failed = map[string]error{}
	for _, name := range names {
		raw, err := os.ReadFile(senhaDir + "/" + name)
		pw := strings.TrimRight(string(raw), "\r\n")
		if err != nil || pw == "" {
			noStored = append(noStored, name)
			continue
		}
		if err := setUserPassword(name, pw); err != nil {
			failed[name] = err
			continue
		}
		fixed = append(fixed, name)
	}
	return fixed, noStored, failed
}

// unverifiableHashSet returns the set of listed users whose password hash
// the OpenVPN auth script cannot verify. Unreadable /etc/shadow (not root,
// test environment) yields an empty set: this is a hint, never a gate.
func unverifiableHashSet(names []string) map[string]bool {
	set := map[string]bool{}
	bad, err := user.ReadUnverifiableHashUsers(names)
	if err != nil {
		return set
	}
	for _, n := range bad {
		set[n] = true
	}
	return set
}

// offerHashRepair explains the problem for the users in bad and, if the
// operator answers y, repairs them. Default is no.
func offerHashRepair(readAnswer func(label string) (string, error), bad []string) {
	fmt.Println()
	fmt.Printf("%s[คำเตือน]%s ผู้ใช้สีแดง %d คน login OpenVPN ไม่ได้ (รหัสผ่านเข้ารหัสแบบ yescrypt ที่สคริปต์ตรวจสอบไม่รองรับ): %s%s\n",
		cYelBold, cWhtBold, len(bad), strings.Join(bad, ", "), cReset)
	fmt.Println(cWhtBold + "ตั้งรหัสผ่านที่เก็บไว้ใน " + senhaDir + " ใหม่เป็น SHA-512 ตอนนี้เลยไหม? (ใช้รหัสเดิมที่เมนูเคยตั้งไว้)" + cReset)
	ans, err := readAnswer("[y/N]:")
	if err != nil || !strings.EqualFold(ans, "y") {
		fmt.Println(cYelBold + "ข้าม — แก้ภายหลังได้ที่เมนู 'เปลี่ยนรหัสผ่านผู้ใช้งาน'" + cReset)
		return
	}
	fixed, noStored, failed := repairUnverifiableHashes(bad)
	if len(fixed) > 0 {
		okLine("ซ่อมแล้ว: " + strings.Join(fixed, ", "))
	}
	if len(noStored) > 0 {
		errLine("ไม่มีรหัสผ่านที่เก็บไว้ ต้องตั้งใหม่ที่เมนู 'เปลี่ยนรหัสผ่านผู้ใช้งาน': " + strings.Join(noStored, ", "))
	}
	for _, name := range bad {
		if e, ok := failed[name]; ok {
			errLine(fmt.Sprintf("ซ่อม %s ไม่สำเร็จ: %v", name, e))
		}
	}
}
