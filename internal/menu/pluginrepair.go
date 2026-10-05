package menu

import (
	"bufio"
	"fmt"
	"path/filepath"

	"github.com/lolyhexey/hexplus/internal/ovpninstance"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/service"
)

// repairPluginConfs runs pki.RepairPluginConfs at menu start and starts the
// OpenVPN units it fixed. With the old plugin line the embedded OpenVPN
// cannot start at all, so a unit that is active is not running that config
// and is left alone (the fix applies on its next restart); a disabled unit
// stays off. It only prints, and waits, when it changed something.
func repairPluginConfs(r *bufio.Reader) {
	fixed, err := pki.RepairPluginConfs()
	if len(fixed) == 0 && err == nil {
		return
	}
	fmt.Println()
	for _, conf := range fixed {
		msg := "แก้ " + conf + " (บรรทัด plugin ของเวอร์ชันเก่าทำให้ OPENVPN เปิดไม่ขึ้น)"
		if svc, ok := serviceForConf(conf); ok {
			if st, e := service.Status(svc); e == nil && st.Enabled && st.ActiveState != "active" {
				if e := service.Restart(svc); e != nil {
					msg += " แต่เริ่ม " + svc.UnitName + " ไม่สำเร็จ: " + e.Error()
				} else {
					msg += " และเริ่ม " + svc.UnitName + " แล้ว"
				}
			}
		}
		okLine(msg)
	}
	if err != nil {
		errLine("ซ่อมไฟล์ตั้งค่า OPENVPN ไม่สำเร็จ: " + err.Error())
	}
	waitEnter(r)
}

// serviceForConf maps /etc/openvpn/server.conf to the primary OpenVPN unit
// and serverN.conf to extra instance N.
func serviceForConf(conf string) (service.Service, bool) {
	if filepath.Base(conf) == "server.conf" {
		return service.ByName("openvpn")
	}
	insts, _ := ovpninstance.List()
	for _, inst := range insts {
		if inst.ConfPath() == conf {
			return inst.Service(), true
		}
	}
	return service.Service{}, false
}
