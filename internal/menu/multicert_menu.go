// multicert_menu.go: Multi-Cert submenu under OPENVPN.
//
// One OpenVPN instance, one port, multiple trusted CAs. The primary CA is
// the one openvpnInstall created and signed server.crt against; extra CAs
// are bundled into /etc/openvpn/ca.crt so OpenVPN accepts client certs
// from any of them. After every add/remove the bundle is rebuilt and the
// service is restarted so the new trust store loads.
//
// Per-CA users live under /etc/openvpn/pki/extra/<ca>/clients/ and their
// Record.CA field tells Remove/Export which folder to look in.

package menu

import (
	"bufio"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/progress"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/user"
)

func multiCertMenu(r *bufio.Reader, svc service.Service) error {
	for {
		clearScreen()
		paintTitleBar("         ระบบ MULTI CERT          ")
		fmt.Println()

		cas, err := pki.ListExtraCAs()
		if err != nil {
			return err
		}
		if len(cas) == 0 {
			fmt.Println(cYelBold + "  (ยังไม่มี CA เพิ่มเติม — มีเพียง CA หลักของระบบ)" + cReset)
		} else {
			fmt.Printf("%sCA เพิ่มเติมที่กำลังใช้งาน%s: %s%d%s ใบ\n",
				cYelBold, cWhtBold, cGrnBold, len(cas), cReset)
			for i, info := range cas {
				fmt.Printf("  %s%d%s. %s%s%s  %s(หมดอายุ %s • client %d ราย)%s\n",
					cCyanBold, i+1, cWhtBold,
					cGrnBold, info.Name, cWhtBold,
					cYelBold, info.NotAfter, info.ClientCount, cReset)
			}
		}
		fmt.Println()

		fmt.Printf("%s[%s1%s] %s• %sเพิ่ม CA ใหม่ (สร้างเอง)%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s2%s] %s• %sนำเข้า CA จากไฟล์ภายนอก%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s3%s] %s• %sสร้างผู้ใช้ภายใต้ CA เพิ่มเติม%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s4%s] %s• %sลบ CA เพิ่มเติม%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s5%s] %s• %sรีบิวด์ ca.crt bundle + รีสตาร์ท%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Printf("%s[%s0%s] %s• %sย้อนกลับ%s\n", cRedBold, cCyanBold, cRedBold, cWhtBold, cYelBold, cReset)
		fmt.Println()

		choice, err := menuPrompt(r)
		if err != nil {
			return err
		}
		switch choice {
		case "0", "00":
			return nil
		case "1", "01":
			multiCertCreate(r, svc)
		case "2", "02":
			multiCertImport(r, svc)
		case "3", "03":
			multiCertAddUser(r, cas)
		case "4", "04":
			multiCertRemove(r, svc, cas)
		case "5", "05":
			multiCertRebuild(svc)
			waitEnter(r)
		default:
			fmt.Println("\n" + cRedBold + "กรุณาเลือกให้ถูกต้อง..." + cReset)
		}
	}
}

func multiCertCreate(r *bufio.Reader, svc service.Service) {
	clearScreen()
	paintTitleBar("        เพิ่ม CA ใหม่ (สร้างเอง)        ")
	fmt.Println()

	name, _ := readLine(r, "ชื่อ CA (a-z 0-9 - _):")
	if err := pki.ValidateCAName(name); err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	cn, _ := promptLineDefault(r, "CA Common Name", name)
	org, _ := promptLineDefault(r, "Organization ", "lolouch.com")
	yearsStr, _ := promptLineDefault(r, "อายุ CA (ปี) ", "100")
	years, err := strconv.Atoi(strings.TrimSpace(yearsStr))
	if err != nil || years <= 0 {
		years = 100
	}

	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "สร้าง CA + บันทึก + รีบิวด์ ca.crt bundle", Work: func() error {
			return pki.CreateExtraCA(name, strings.TrimSpace(cn), strings.TrimSpace(org), years)
		}},
		{Label: "รีสตาร์ท OPENVPN เพื่อโหลด trust store ใหม่", Work: func() error {
			return service.Restart(svc)
		}},
	}); err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	okLine("\nเพิ่ม CA " + name + " สำเร็จ")
	waitEnter(r)
}

func multiCertImport(r *bufio.Reader, svc service.Service) {
	clearScreen()
	paintTitleBar("      นำเข้า CA จากไฟล์ภายนอก      ")
	fmt.Println()
	fmt.Println(cYelBold + "ระบุ path ของไฟล์ PEM (ca.crt จำเป็น, ca.key ใส่หรือไม่ใส่ก็ได้)" + cReset)
	fmt.Println(cYelBold + "ถ้าไม่ใส่ ca.key จะ verify client ได้แต่เซ็นต์ user ใหม่ผ่าน hexplus ไม่ได้" + cReset)
	fmt.Println()

	name, _ := readLine(r, "ชื่อที่จะตั้งให้ CA นี้:")
	if err := pki.ValidateCAName(name); err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	certPath, _ := readLine(r, "path ของ ca.crt:")
	if certPath == "" {
		errLine("ต้องระบุ path ของ ca.crt")
		waitEnter(r)
		return
	}
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		errLine("อ่าน ca.crt: " + err.Error())
		waitEnter(r)
		return
	}
	keyPath, _ := readLine(r, "path ของ ca.key (ปล่อยว่างเพื่อข้าม):")
	var keyPEM []byte
	if keyPath != "" {
		keyPEM, err = os.ReadFile(keyPath)
		if err != nil {
			errLine("อ่าน ca.key: " + err.Error())
			waitEnter(r)
			return
		}
	}

	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "บันทึก CA + รีบิวด์ ca.crt bundle", Work: func() error {
			return pki.ImportExtraCA(name, certPEM, keyPEM)
		}},
		{Label: "รีสตาร์ท OPENVPN เพื่อโหลด trust store ใหม่", Work: func() error {
			return service.Restart(svc)
		}},
	}); err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	okLine("\nนำเข้า CA " + name + " สำเร็จ")
	waitEnter(r)
}

func multiCertAddUser(r *bufio.Reader, cas []pki.ExtraCAInfo) {
	clearScreen()
	paintTitleBar("    สร้างผู้ใช้ภายใต้ CA เพิ่มเติม    ")
	fmt.Println()

	if len(cas) == 0 {
		errLine("ยังไม่มี CA เพิ่มเติม — เพิ่ม CA ก่อนสร้างผู้ใช้")
		waitEnter(r)
		return
	}
	for i, info := range cas {
		fmt.Printf("  %s[%s%d%s] %s%s%s\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cGrnBold, info.Name, cReset)
	}
	fmt.Println()
	pick, _ := readLine(r, "เลือก CA (หมายเลข):")
	idx, err := strconv.Atoi(pick)
	if err != nil || idx < 1 || idx > len(cas) {
		errLine("หมายเลขไม่ถูกต้อง")
		waitEnter(r)
		return
	}
	caName := cas[idx-1].Name

	name, _ := readLine(r, "ชื่อผู้ใช้:")
	password, _ := readLine(r, "รหัสผ่าน:")
	daysStr, _ := promptLineDefault(r, "หมดอายุภายในกี่วัน (0 = ไม่หมดอายุ)", "30")
	days, _ := strconv.Atoi(strings.TrimSpace(daysStr))
	limitStr, _ := promptLineDefault(r, "จำกัด simultaneous login (0 = ไม่จำกัด)", "1")
	limit, _ := strconv.Atoi(strings.TrimSpace(limitStr))

	in := user.AddInput{
		Name:          strings.TrimSpace(name),
		Password:      password,
		ExpiresInDays: days,
		Limit:         limit,
		CA:            caName,
	}
	ovpnIn := user.OVPNInput{
		RemoteHost: defaultRemoteHost(),
		RemotePort: ovpnPort(),
		Proto:      ovpnProto(),
	}
	res, err := user.Add(in, ovpnIn)
	if err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	dest := "/root/" + res.Record.Name + ".ovpn"
	if err := os.WriteFile(dest, res.OVPN, 0o644); err != nil {
		errLine("เขียน .ovpn: " + err.Error())
		waitEnter(r)
		return
	}
	okLine("\nสร้างผู้ใช้ " + res.Record.Name + " (CA: " + caName + ") สำเร็จ")
	fmt.Println(cYelBold + "ไฟล์ .ovpn: " + cWhtBold + dest + cReset)
	waitEnter(r)
}

func multiCertRemove(r *bufio.Reader, svc service.Service, cas []pki.ExtraCAInfo) {
	clearScreen()
	paintTitleBar("           ลบ CA เพิ่มเติม           ")
	fmt.Println()

	if len(cas) == 0 {
		errLine("ยังไม่มี CA เพิ่มเติมให้ลบ")
		waitEnter(r)
		return
	}
	for i, info := range cas {
		fmt.Printf("  %s[%s%d%s] %s%s%s  %s(client %d ราย)%s\n",
			cRedBold, cCyanBold, i+1, cRedBold,
			cGrnBold, info.Name, cWhtBold,
			cYelBold, info.ClientCount, cReset)
	}
	fmt.Println()
	pick, _ := readLine(r, "เลือก CA (หมายเลข):")
	idx, err := strconv.Atoi(pick)
	if err != nil || idx < 1 || idx > len(cas) {
		errLine("หมายเลขไม่ถูกต้อง")
		waitEnter(r)
		return
	}
	caName := cas[idx-1].Name

	fmt.Print(cYelBold + "ยืนยันลบ CA " + cWhtBold + caName + cYelBold +
		" และ client cert ทั้งหมด " + cRedBold + "? " + cGrnBold + "[s/n]: " + cReset)
	confirm, _ := r.ReadString('\n')
	if strings.TrimSpace(confirm) != "s" {
		return
	}

	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "ลบโฟลเดอร์ CA + รีบิวด์ ca.crt bundle", Work: func() error {
			return pki.RemoveExtraCA(caName)
		}},
		{Label: "รีสตาร์ท OPENVPN เพื่อโหลด trust store ใหม่", Work: func() error {
			return service.Restart(svc)
		}},
	}); err != nil {
		errLine(err.Error())
		waitEnter(r)
		return
	}
	okLine("\nลบ CA " + caName + " สำเร็จ")
	fmt.Println(cYelBold + "หมายเหตุ: ผู้ใช้ที่เคยถูกเซ็นต์ด้วย CA นี้จะใช้งานต่อไม่ได้" + cReset)
	waitEnter(r)
}

func multiCertRebuild(svc service.Service) {
	fmt.Println()
	if err := progress.Run([]progress.Step{
		{Label: "รีบิวด์ /etc/openvpn/ca.crt จาก primary + extras", Work: func() error {
			return pki.RebuildCABundle()
		}},
		{Label: "รีสตาร์ท OPENVPN", Work: func() error {
			return service.Restart(svc)
		}},
	}); err != nil {
		errLine(err.Error())
		return
	}
	okLine("\nรีบิวด์สำเร็จ")
}
