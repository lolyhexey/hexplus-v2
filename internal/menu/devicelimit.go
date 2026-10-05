package menu

import (
	"bufio"
	"errors"
	"fmt"

	"github.com/lolyhexey/hexplus/internal/ovpnguard"
	"github.com/lolyhexey/hexplus/internal/ovpnspread"
	"github.com/lolyhexey/hexplus/internal/service"
)

// toggleDeviceLimit switches the OpenVPN device-limit guard on or off. Both
// directions change every server*.conf (the management socket lines) and so
// restart every OpenVPN instance, which drops everyone once; the operator
// confirms first.
func toggleDeviceLimit(r *bufio.Reader, primary service.Service) {
	clearScreen()
	enabling := !ovpnguard.Enabled()
	if enabling {
		paintTitleBar("     จำกัดจำนวนอุปกรณ์ต่อผู้ใช้ (เปิด)     ")
		fmt.Println()
		fmt.Println(cWhtBold + "ตรวจทุก ~15 วินาที นับเฉพาะการเชื่อมต่อ OPENVPN รวมทุกพอร์ต" + cReset)
		fmt.Println(cWhtBold + "ผู้ใช้ที่ต่อเกินจำนวนอุปกรณ์ที่ตั้งไว้ จะถูกตัดเครื่องที่ต่อนานที่สุด" + cReset)
		fmt.Println(cWhtBold + "(เครื่องที่หลุดค้างไว้ถูกตัดก่อน) เครื่องที่ต่อใหม่ล่าสุดใช้ต่อได้" + cReset)
		fmt.Println(cWhtBold + "บัญชีที่ถูกลบ หมดอายุ หรือถูกล็อก จะถูกตัดทุกเครื่อง" + cReset)
		fmt.Println(cWhtBold + "ผู้ใช้ที่ไม่ได้ตั้งจำนวนอุปกรณ์ = ไม่จำกัด" + cReset)
		if ovpnConfContains("duplicate-cn") {
			fmt.Println()
		} else {
			fmt.Println(cYelBold + "หมายเหตุ: MULTILOGIN ปิดอยู่ พอร์ตหลักจึงยังจำกัด 1 เครื่องต่อผู้ใช้อยู่แล้ว" + cReset)
		}
	} else {
		paintTitleBar("     จำกัดจำนวนอุปกรณ์ต่อผู้ใช้ (ปิด)      ")
		fmt.Println()
	}
	fmt.Println()
	fmt.Print(cYelBold + "OPENVPN ทุกพอร์ตจะรีสตาร์ท ลูกค้าที่ต่ออยู่จะหลุดแล้วต่อใหม่ ยืนยัน? " + cGrnBold + "[y/N]: " + cReset)
	ans, _ := r.ReadString('\n')
	if !isYes(ans) {
		return
	}

	var err error
	if enabling {
		err = enableDeviceLimit(primary)
	} else {
		err = disableDeviceLimit(primary)
	}
	fmt.Println()
	switch {
	case err != nil && enabling:
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + "เปิดไม่สำเร็จ ย้อนการตั้งค่ากลับแล้ว: " + err.Error() + cReset)
	case err != nil:
		fmt.Println(cRedBold + "[ผิดพลาด] " + cYelBold + err.Error() + cReset)
	case enabling:
		fmt.Println(cGrnBold + "เปิดการจำกัดจำนวนอุปกรณ์แล้ว" + cReset)
	default:
		fmt.Println(cGrnBold + "ปิดการจำกัดจำนวนอุปกรณ์แล้ว" + cReset)
		if ovpnspread.Enabled() && !ovpnConfContains("duplicate-cn") {
			fmt.Println(cYelBold + "หมายเหตุ: กระจายโหลดเปิดอยู่ ผู้ใช้หนึ่งคนจึงต่อได้สูงสุด 1 เครื่องต่อ process (ไม่ใช่ 1 เครื่องรวม)" + cReset)
		}
	}
	waitEnter(r)
}

// enableDeviceLimit turns the guard on. Any failure rolls everything back,
// so the menu never shows "on" for a guard that is half set up.
func enableDeviceLimit(primary service.Service) error {
	if err := setupDeviceLimit(); err != nil {
		_ = disableDeviceLimit(primary)
		return err
	}
	if err := restartAllOpenVPN(primary); err != nil {
		_ = disableDeviceLimit(primary)
		return err
	}
	return nil
}

func setupDeviceLimit() error {
	for _, conf := range ovpnguard.ServerConfs() {
		if err := ovpnguard.InjectConf(conf); err != nil {
			return err
		}
	}
	if err := ovpnguard.SetEnabled(true); err != nil {
		return err
	}
	guard := ovpnguard.Service()
	if err := service.WriteUnitFor(guard); err != nil {
		return err
	}
	if err := service.Enable(guard); err != nil {
		return err
	}
	return service.Start(guard)
}

func disableDeviceLimit(primary service.Service) error {
	ovpnguard.Teardown()
	var errs []error
	for _, conf := range ovpnguard.ServerConfs() {
		if err := ovpnguard.StripConf(conf); err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", conf, err))
		}
	}
	if err := restartAllOpenVPN(primary); err != nil {
		errs = append(errs, err)
	}
	return errors.Join(errs...)
}

func restartAllOpenVPN(primary service.Service) error {
	var errs []error
	if err := service.Restart(primary); err != nil {
		errs = append(errs, err)
	}
	// Spread workers restart with the primary (PartOf); only the
	// operator's extra ports need their own restart.
	for _, inst := range listExtraInstances() {
		if err := service.Restart(inst.Service()); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// kickOVPN ends every OpenVPN session of name when the guard is on (the
// management sockets only exist then). Deleting or expiring an account from
// the menu used to leave its OpenVPN sessions running: pkill -u only reaches
// processes the user owns, and an OpenVPN session is not one.
func kickOVPN(name string) {
	if !ovpnguard.Enabled() {
		fmt.Printf("%s  - การเชื่อมต่อ OPENVPN ที่ค้างอยู่ของ %s จะหลุดเมื่อต่อใหม่ (เปิด \"จำกัดจำนวนอุปกรณ์\" เพื่อตัดทันที)%s\n", cYelBold, name, cReset)
		return
	}
	if n := ovpnguard.Kick(name, func(string, ...any) {}); n > 0 {
		fmt.Printf("%s  - ตัดการเชื่อมต่อ OPENVPN ของ %s แล้ว %d เครื่อง%s\n", cYelBold, name, n, cReset)
	}
}
