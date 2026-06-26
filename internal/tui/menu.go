// menu.go: top-level bubble tea Model. Owns the page-routing state and
// the periodic refresh of the service-status header so the live grid at
// the top stays accurate without the user pressing a key.

package tui

import (
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/lolyhexey/hexplus/internal/service"
)

// page is the enum of the menu screens the TUI can show. Adding a new
// view = add a constant + a case in View() + a constructor in Update()
// that routes when its main-menu row is selected.
type page int

const (
	pageMain page = iota
	pageServices
	pageUsers
	pageProxies
	pagePKI
)

// model is the root bubble tea Model. Sub-views render through it so
// the header / footer / refresh ticker live in one place.
type model struct {
	page    page
	mainSel int

	// status is the live service-status grid we paint at the top of
	// every page. Refreshed once per refreshInterval via tickMsg.
	status []service.State

	// sub holds whatever sub-page Model is currently active; nil on
	// pageMain. Sub-pages return commands of their own which we relay.
	sub tea.Model

	width, height int

	// notice is a transient one-line message rendered at the bottom of
	// the main menu (e.g. "user added", "proxy started"). Cleared on
	// the next status tick so it doesn't pin forever.
	notice string
}

// refreshInterval bounds how often we re-poll systemd + /proc/net.
// Two seconds is the same cadence v1's bash status loop used and matches
// what an operator perceives as "live".
const refreshInterval = 2 * time.Second

// tickMsg fires on a timer and triggers a status refresh.
type tickMsg time.Time

// statusMsg is the result of the async service.StatusAll() lookup that
// runs in a goroutine off of tickMsg.
type statusMsg []service.State

// noticeMsg is how sub-pages tell the main view "render this one-liner
// when the user returns to the main menu".
type noticeMsg string

// NewModel returns the initial bubble tea model. The status grid is
// empty until the first tick lands - that's a frame or two on a slow
// box and not visually distracting.
func NewModel() tea.Model {
	return model{page: pageMain}
}

// Run wraps tea.NewProgram + Run for the CLI entry point so main.go
// doesn't have to know about bubble tea types.
func Run() error {
	p := tea.NewProgram(NewModel(), tea.WithAltScreen())
	_, err := p.Run()
	return err
}

func (m model) Init() tea.Cmd {
	return tea.Batch(tick(), fetchStatus())
}

func tick() tea.Cmd {
	return tea.Tick(refreshInterval, func(t time.Time) tea.Msg { return tickMsg(t) })
}

func fetchStatus() tea.Cmd {
	return func() tea.Msg {
		all, err := service.StatusAll()
		if err != nil {
			return statusMsg(nil)
		}
		return statusMsg(all)
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.sub != nil {
			next, cmd := m.sub.Update(msg)
			m.sub = next
			return m, cmd
		}
		return m, nil

	case tickMsg:
		return m, tea.Batch(tick(), fetchStatus())

	case statusMsg:
		m.status = []service.State(msg)
		return m, nil

	case noticeMsg:
		m.notice = string(msg)
		return m, nil

	case tea.KeyMsg:
		// Global quit shortcut works from any page.
		switch msg.String() {
		case "ctrl+c":
			return m, tea.Quit
		}
		if m.sub != nil {
			next, cmd := m.sub.Update(msg)
			if _, leaving := next.(returnToMain); leaving {
				m.sub = nil
				m.page = pageMain
				return m, fetchStatus()
			}
			m.sub = next
			return m, cmd
		}
		return m.updateMain(msg)
	}

	// Pass non-key, non-tick messages to the sub-page if any.
	if m.sub != nil {
		next, cmd := m.sub.Update(msg)
		m.sub = next
		return m, cmd
	}
	return m, nil
}

// mainItems is the labeled list shown in the top-level menu. Order
// mirrors the v1 conexao flow so muscle memory carries over.
var mainItems = []struct {
	label string
	page  page
}{
	{"จัดการระบบบริการ (services)", pageServices},
	{"จัดการผู้ใช้ (users)", pageUsers},
	{"จัดการ Proxy SOCKS", pageProxies},
	{"PKI (CA + server cert)", pagePKI},
}

func (m model) updateMain(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "q", "esc":
		return m, tea.Quit
	case "up", "k":
		if m.mainSel > 0 {
			m.mainSel--
		}
	case "down", "j":
		if m.mainSel < len(mainItems)-1 {
			m.mainSel++
		}
	case "enter":
		choice := mainItems[m.mainSel]
		switch choice.page {
		case pageServices:
			m.sub = newServicesView(m.width, m.height)
			m.page = pageServices
		case pageUsers:
			m.sub = newUsersView(m.width, m.height)
			m.page = pageUsers
		case pageProxies:
			m.sub = newProxiesView(m.width, m.height)
			m.page = pageProxies
		case pagePKI:
			m.sub = newPKIView(m.width, m.height)
			m.page = pagePKI
		}
		m.notice = ""
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	if m.sub != nil {
		// Sub-pages own their own framing; we just render their view
		// behind the same banner so the operator never loses context.
		return joinVertical(m.headerView(), m.sub.View())
	}
	return joinVertical(
		m.headerView(),
		m.mainMenuView(),
		m.footerView(),
	)
}

func (m model) headerView() string {
	title := styles.Title.Render("  HEXPLUS v2 — เมนูหลัก  ")
	rule := styles.HeaderRule.Render(strings.Repeat("━", lipgloss.Width(title)+4))
	grid := m.statusGridView()
	return joinVertical(title, rule, grid)
}

func (m model) statusGridView() string {
	if len(m.status) == 0 {
		return styles.HelpDesc.Render("  (กำลังโหลดสถานะบริการ…)") + "\n"
	}
	var lines []string
	for _, st := range m.status {
		marker := runeOff
		markerStyle := styles.StatusOff
		stateText := "ปิด"
		if st.UnitExists && st.ActiveState == "active" {
			marker = runeOn
			markerStyle = styles.StatusOn
			stateText = "ทำงาน"
		} else if !st.UnitExists {
			stateText = "ยังไม่ติดตั้ง"
		} else if st.SubState == "no-dbus" || st.SubState == "no-systemctl" {
			stateText = "ไม่ทราบ (" + st.SubState + ")"
		}
		row := fmt.Sprintf(
			"  %s  %s  %s  พอร์ต %d/%s",
			markerStyle.Render(marker),
			styles.StatusLabel.Render(padRight(st.Service.Name, 10)),
			styles.StatusValue.Render(padRight(stateText, 18)),
			st.Service.Port,
			st.Service.PortProto,
		)
		lines = append(lines, row)
	}
	return joinVertical(lines...) + "\n"
}

func (m model) mainMenuView() string {
	var lines []string
	lines = append(lines, styles.Header.Render("เลือกเมนู:"))
	for i, it := range mainItems {
		prefix := "  "
		styled := styles.ItemNormal
		if i == m.mainSel {
			prefix = styles.ItemActive.Render("→ ")
			styled = styles.ItemActive
		}
		lines = append(lines, prefix+styled.Render(it.label))
	}
	if m.notice != "" {
		lines = append(lines, "", styles.HelpDesc.Render(m.notice))
	}
	return joinVertical(lines...)
}

func (m model) footerView() string {
	help := []string{
		hk("↑/↓") + " เลือก",
		hk("Enter") + " ยืนยัน",
		hk("q") + " ออก",
		hk("Ctrl+C") + " ออกฉุกเฉิน",
	}
	return styles.Footer.Render(strings.Join(help, "   "))
}

func hk(key string) string { return styles.HelpKey.Render(key) }

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func joinVertical(parts ...string) string {
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// returnToMain is the sentinel sub-pages return from Update() when the
// user has pressed esc/q to back out. The root Update() unwraps it and
// drops back to pageMain.
type returnToMain struct{}

func (returnToMain) Init() tea.Cmd                         { return nil }
func (r returnToMain) Update(tea.Msg) (tea.Model, tea.Cmd) { return r, nil }
func (returnToMain) View() string                          { return "" }
