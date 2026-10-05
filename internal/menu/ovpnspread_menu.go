package menu

import (
	"bufio"
	"fmt"
	"runtime"
	"strconv"
	"strings"

	"github.com/lolyhexey/hexplus/internal/ovpnguard"
	"github.com/lolyhexey/hexplus/internal/ovpninstance"
	"github.com/lolyhexey/hexplus/internal/ovpnspread"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/progress"
)

// spreadMark is the OpenVPN menu marker for [11]: the number of OpenVPN
// processes sharing the primary port, or off.
func spreadMark() string {
	ws, _ := ovpnspread.Workers()
	if len(ws) == 0 {
		return markerOff()
	}
	return fmt.Sprintf("%s[%s%d process%s]%s", cRedBold, cGrnBold, len(ws)+1, cRedBold, cReset)
}

// toggleSpread turns CPU spreading of the primary port on or off. Turning
// it on starts worker processes and adds iptables rules; the primary keeps
// running, so connected users are not dropped. Turning it off drops the
// sessions on the workers, which reconnect to the primary.
func toggleSpread(r *bufio.Reader) {
	clearScreen()
	noLog := func(string, ...any) {}
	if ws, _ := ovpnspread.Workers(); len(ws) > 0 {
		paintTitleBar("     กระจายโหลด OPENVPN ทุก CPU (ปิด)     ")
		fmt.Println()
		fmt.Printf("%sตอนนี้พอร์ตหลักแบ่งให้ OPENVPN %d process%s\n", cWhtBold, len(ws)+1, cReset)
		fmt.Println(cWhtBold + "ลูกค้าที่อยู่บน process เสริมจะหลุด แล้วต่อใหม่เข้าพอร์ตหลัก" + cReset)
		fmt.Println()
		fmt.Print(cYelBold + "ยืนยันปิด? " + cGrnBold + "[y/N]: " + cReset)
		ans, _ := r.ReadString('\n')
		if !isYes(ans) {
			return
		}
		fmt.Println()
		if err := progress.Run([]progress.Step{
			{Label: "ปิดการกระจายโหลด + ลบ process เสริม", Work: func() error { return ovpnspread.Disable(noLog) }},
		}); err != nil {
			fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
		} else {
			fmt.Println("\n" + cGrnBold + "ปิดการกระจายโหลดแล้ว" + cReset)
		}
		waitEnter(r)
		return
	}

	paintTitleBar("     กระจายโหลด OPENVPN ทุก CPU (เปิด)     ")
	fmt.Println()
	proto, port, err := ovpnspread.Primary()
	if err != nil {
		errLine("อ่าน /etc/openvpn/server.conf ไม่ได้ (ติดตั้ง OPENVPN แล้วหรือยัง?): " + err.Error())
		waitEnter(r)
		return
	}
	cpus := runtime.NumCPU()
	fmt.Println(cWhtBold + "OPENVPN 1 process ใช้ CPU ได้แค่ 1 core ลูกค้าทุกคนบนพอร์ตเดียวจึงแย่ง core เดียวกัน" + cReset)
	fmt.Printf("%sจะเพิ่ม OPENVPN process บนพอร์ตภายใน แล้วแบ่งการเชื่อมต่อใหม่ที่เข้าพอร์ต %d/%s ให้ทุก process เท่า ๆ กัน%s\n", cWhtBold, port, proto, cReset)
	fmt.Printf("%sรวมถึงที่มาทาง SSL TUNNEL / SSLH / PROXY ซึ่งส่งต่อมาที่ 127.0.0.1:%d ด้วย%s\n", cWhtBold, port, cReset)
	fmt.Println(cWhtBold + "ไฟล์ .ovpn ไม่ต้องเปลี่ยน และลูกค้าที่ต่ออยู่ตอนนี้ไม่หลุด" + cReset)
	fmt.Printf("%sเครื่องนี้มี %d CPU%s\n", cYelBold, cpus, cReset)
	if cpus < 2 {
		fmt.Println("\n" + cRedBold + "มี CPU เดียว การกระจายโหลดไม่ช่วยอะไร" + cReset)
		waitEnter(r)
		return
	}
	if !pki.HasDuplicateCN(pki.OpenVPNDir+"/server.conf") && !ovpnguard.Enabled() {
		fmt.Println()
		fmt.Println(cYelBold + "หมายเหตุ: MULTILOGIN ปิดอยู่ แต่แต่ละ process กันการต่อซ้ำได้เฉพาะในตัวเอง" + cReset)
		fmt.Println(cYelBold + "ผู้ใช้หนึ่งคนจึงอาจต่อได้หลายเครื่อง (สูงสุดเท่าจำนวน process)" + cReset)
		fmt.Println(cYelBold + "เปิด [10] จำกัดจำนวนอุปกรณ์ต่อผู้ใช้ เพื่อนับรวมทุก process" + cReset)
	}
	fmt.Println()
	def := cpus
	if def > ovpnspread.MaxProcs {
		def = ovpnspread.MaxProcs
	}
	line, err := promptLineDefault(r, fmt.Sprintf("จำนวน OPENVPN process ทั้งหมด (2-%d)", ovpnspread.MaxProcs), strconv.Itoa(def))
	if err != nil {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(line))
	if err != nil || n < 2 || n > ovpnspread.MaxProcs {
		errLine(fmt.Sprintf("ต้องเป็นตัวเลข 2-%d", ovpnspread.MaxProcs))
		waitEnter(r)
		return
	}
	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: fmt.Sprintf("สร้าง OPENVPN %d process เสริม + กฎ iptables", n-1), Work: func() error {
			return ovpnspread.Enable(n, ovpnDNSPushLines(), noLog)
		}},
	}); err != nil {
		fmt.Println("\n" + cRedBold + "[ผิดพลาด] " + cYelBold + "เปิดไม่สำเร็จ ย้อนการตั้งค่ากลับแล้ว: " + err.Error() + cReset)
		waitEnter(r)
		return
	}
	fmt.Printf("\n%sเปิดแล้ว: พอร์ต %d/%s แบ่งให้ %d process%s\n", cGrnBold, port, proto, n, cReset)
	waitEnter(r)
}

// listExtraInstances is the operator's extra ports: every registered
// instance except the spread workers, which listen on internal ports.
func listExtraInstances() []ovpninstance.Instance {
	all, _ := ovpninstance.List()
	return ovpninstance.Extras(all)
}

// setSpreadMultilogin writes the primary's new MULTILOGIN setting into the
// spread workers' configs. Call it before restarting the primary, which
// restarts them too (PartOf). Each process can only refuse a second session
// of a user within itself; across processes the device-limit guard counts.
func setSpreadMultilogin(on bool) {
	ws, _ := ovpnspread.Workers()
	for _, w := range ws {
		_ = pki.SetDuplicateCN(w.ConfPath(), on)
	}
}
