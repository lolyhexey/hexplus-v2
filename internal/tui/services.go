// services.go: the "จัดการระบบบริการ" sub-page. Lists openvpn / squid /
// dropbear with their live state and lets the operator drive them
// (start / stop / restart / enable / disable / reload) without leaving
// the TUI.

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lolyhexey/hexplus/internal/service"
)

type servicesView struct {
	width, height int
	rows          []service.State
	sel           int
	msg           string // transient one-line feedback after an action
	loading       bool
}

func newServicesView(w, h int) tea.Model {
	v := servicesView{width: w, height: h, loading: true}
	return v
}

func (v servicesView) Init() tea.Cmd {
	return tea.Batch(servicesTick(), fetchServicesStatus())
}

type servicesTickMsg time.Time
type servicesStatusMsg []service.State
type serviceActionDoneMsg struct {
	verb string
	name string
	err  error
}

func servicesTick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return servicesTickMsg(t) })
}

func fetchServicesStatus() tea.Cmd {
	return func() tea.Msg {
		all, err := service.StatusAll()
		if err != nil {
			return servicesStatusMsg(nil)
		}
		return servicesStatusMsg(all)
	}
}

func runServiceVerb(verb string, svc service.Service) tea.Cmd {
	return func() tea.Msg {
		var err error
		switch verb {
		case "start":
			err = service.Start(svc)
		case "stop":
			err = service.Stop(svc)
		case "restart":
			err = service.Restart(svc)
		case "enable":
			err = service.Enable(svc)
		case "disable":
			err = service.Disable(svc)
		case "reload":
			err = service.TryReload(svc)
		}
		return serviceActionDoneMsg{verb: verb, name: svc.Name, err: err}
	}
}

func (v servicesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		return v, nil
	case servicesTickMsg:
		return v, tea.Batch(servicesTick(), fetchServicesStatus())
	case servicesStatusMsg:
		v.rows = []service.State(msg)
		v.loading = false
		if v.sel >= len(v.rows) {
			v.sel = 0
		}
		return v, nil
	case serviceActionDoneMsg:
		if msg.err != nil {
			v.msg = fmt.Sprintf("%s %s: %v", msg.verb, msg.name, msg.err)
		} else {
			v.msg = fmt.Sprintf("%s %s: สำเร็จ", msg.verb, msg.name)
		}
		return v, fetchServicesStatus()
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "left", "h":
			return returnToMain{}, nil
		case "up", "k":
			if v.sel > 0 {
				v.sel--
			}
		case "down", "j":
			if v.sel < len(v.rows)-1 {
				v.sel++
			}
		case "s":
			if r, ok := v.current(); ok {
				v.msg = "กำลังเริ่ม " + r.Service.Name + "…"
				return v, runServiceVerb("start", r.Service)
			}
		case "x":
			if r, ok := v.current(); ok {
				v.msg = "กำลังหยุด " + r.Service.Name + "…"
				return v, runServiceVerb("stop", r.Service)
			}
		case "r":
			if r, ok := v.current(); ok {
				v.msg = "กำลังรีสตาร์ท " + r.Service.Name + "…"
				return v, runServiceVerb("restart", r.Service)
			}
		case "e":
			if r, ok := v.current(); ok {
				v.msg = "กำลังเปิด autostart " + r.Service.Name + "…"
				return v, runServiceVerb("enable", r.Service)
			}
		case "d":
			if r, ok := v.current(); ok {
				v.msg = "กำลังปิด autostart " + r.Service.Name + "…"
				return v, runServiceVerb("disable", r.Service)
			}
		}
	}
	return v, nil
}

func (v servicesView) current() (service.State, bool) {
	if v.sel < 0 || v.sel >= len(v.rows) {
		return service.State{}, false
	}
	return v.rows[v.sel], true
}

func (v servicesView) View() string {
	var lines []string
	lines = append(lines, styles.Header.Render("จัดการระบบบริการ"))
	lines = append(lines, "")
	if v.loading {
		lines = append(lines, styles.HelpDesc.Render("กำลังโหลด..."))
	}
	for i, r := range v.rows {
		marker := runeOff
		markerStyle := styles.StatusOff
		stateText := "ปิด"
		if r.UnitExists && r.ActiveState == "active" {
			marker = runeOn
			markerStyle = styles.StatusOn
			stateText = "ทำงาน"
		} else if !r.UnitExists {
			stateText = "ยังไม่ติดตั้ง"
		} else if r.SubState == "no-dbus" || r.SubState == "no-systemctl" {
			stateText = "ไม่ทราบ (" + r.SubState + ")"
		}
		enabled := "disabled"
		if r.Enabled {
			enabled = "enabled"
		}
		pid := ""
		if r.MainPID != "" && r.MainPID != "0" {
			pid = " PID " + r.MainPID
		}
		row := fmt.Sprintf("  %s  %s  %s  %s  %d/%s%s",
			markerStyle.Render(marker),
			styles.StatusLabel.Render(padRight(r.Service.Name, 10)),
			styles.StatusValue.Render(padRight(stateText, 18)),
			styles.HelpDesc.Render(padRight(enabled, 9)),
			r.Service.Port, r.Service.PortProto, pid)
		if i == v.sel {
			row = styles.ItemActive.Render("→") + row[1:]
		}
		lines = append(lines, row)
	}
	if v.msg != "" {
		lines = append(lines, "", styles.HelpDesc.Render("» "+v.msg))
	}
	lines = append(lines, "", v.footer())
	return strings.Join(lines, "\n")
}

func (v servicesView) footer() string {
	parts := []string{
		hk("↑/↓") + " เลือก",
		hk("s") + " start",
		hk("x") + " stop",
		hk("r") + " restart",
		hk("e") + " enable",
		hk("d") + " disable",
		hk("esc") + " กลับ",
	}
	return styles.Footer.Render(strings.Join(parts, "  "))
}
