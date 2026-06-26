// users.go: the "จัดการผู้ใช้" sub-page. List + add form + remove.
//
// Phase 4 MVP: read-only list + ENTER opens an add-user form using
// the bubbles/textinput component. Submit drives user.Add, which
// creates the system account, signs the OpenVPN client cert, persists
// metadata, and writes the .ovpn file.

package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lolyhexey/hexplus/internal/user"
)

type usersView struct {
	width, height int
	rows          []user.Record
	sel           int
	mode          usersMode
	form          usersForm
	msg           string
	err           string
}

type usersMode int

const (
	usersList usersMode = iota
	usersAdd
)

// usersForm is the add-user textinput cluster. Fields render top-to-bottom;
// Tab/Shift+Tab moves focus.
type usersForm struct {
	inputs   []textinput.Model
	focusIdx int
}

const (
	uName = iota
	uPassword
	uExpireDays
	uLimit
	uRemoteHost
	uFieldCount
)

func newUsersView(w, h int) tea.Model {
	v := usersView{width: w, height: h}
	v.reloadRows()
	return v
}

func (v *usersView) reloadRows() {
	rows, err := user.List()
	if err != nil {
		v.err = err.Error()
		return
	}
	v.rows = rows
	v.err = ""
}

func (v usersView) Init() tea.Cmd { return nil }

func (v usersView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		return v, nil
	case tea.KeyMsg:
		switch v.mode {
		case usersList:
			return v.updateList(msg)
		case usersAdd:
			return v.updateAdd(msg)
		}
	}
	return v, nil
}

func (v usersView) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
	case "n", "a":
		v.mode = usersAdd
		v.form = newUsersForm()
		v.msg = ""
		v.err = ""
	case "d":
		if v.sel >= 0 && v.sel < len(v.rows) {
			rec := v.rows[v.sel]
			if err := user.Remove(rec.Name); err != nil {
				v.err = err.Error()
			} else {
				v.msg = "ลบผู้ใช้ " + rec.Name + " แล้ว"
				v.reloadRows()
				if v.sel >= len(v.rows) {
					v.sel = max0(len(v.rows) - 1)
				}
			}
		}
	}
	return v, nil
}

func (v usersView) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.mode = usersList
		v.err = ""
		return v, nil
	case "tab", "down":
		v.form.focusIdx = (v.form.focusIdx + 1) % len(v.form.inputs)
		v.form.refocus()
		return v, nil
	case "shift+tab", "up":
		v.form.focusIdx = (v.form.focusIdx - 1 + len(v.form.inputs)) % len(v.form.inputs)
		v.form.refocus()
		return v, nil
	case "enter":
		if v.form.focusIdx < len(v.form.inputs)-1 {
			// Move to next field instead of submitting until we're on
			// the last field; the operator can hit Enter on Remote
			// Host to confirm.
			v.form.focusIdx++
			v.form.refocus()
			return v, nil
		}
		return v.submitAdd()
	}

	// Otherwise let the active textinput consume the key.
	var cmd tea.Cmd
	v.form.inputs[v.form.focusIdx], cmd = v.form.inputs[v.form.focusIdx].Update(msg)
	return v, cmd
}

func (v usersView) submitAdd() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(v.form.inputs[uName].Value())
	password := v.form.inputs[uPassword].Value()
	expireDays, _ := strconv.Atoi(strings.TrimSpace(v.form.inputs[uExpireDays].Value()))
	limit, _ := strconv.Atoi(strings.TrimSpace(v.form.inputs[uLimit].Value()))
	remote := strings.TrimSpace(v.form.inputs[uRemoteHost].Value())
	if remote == "" {
		remote = "127.0.0.1"
	}

	in := user.AddInput{
		Name:          name,
		Password:      password,
		ExpiresInDays: expireDays,
		Limit:         limit,
	}
	res, err := user.Add(in, user.OVPNInput{
		RemoteHost: remote,
		RemotePort: 1194,
		Proto:      "udp",
	})
	if err != nil {
		v.err = err.Error()
		return v, nil
	}

	// Persist .ovpn beside the user's row.
	dest := "/root/" + name + ".ovpn"
	if writeErr := writeFileOnce(dest, res.OVPN, 0o600); writeErr != nil {
		v.err = "ovpn write: " + writeErr.Error()
		return v, nil
	}
	v.msg = "เพิ่มผู้ใช้ " + name + " แล้ว — " + dest
	v.mode = usersList
	v.err = ""
	v.reloadRows()
	return v, nil
}

func (v usersView) View() string {
	var lines []string
	lines = append(lines, styles.Header.Render("จัดการผู้ใช้ HEXPLUS"))
	lines = append(lines, "")

	if v.mode == usersAdd {
		lines = append(lines, v.form.view())
		if v.err != "" {
			lines = append(lines, "", styles.Error.Render("✘ "+v.err))
		}
		help := []string{
			hk("Tab/↑/↓") + " ย้าย",
			hk("Enter") + " ยืนยัน (กดที่ Remote)",
			hk("Esc") + " ยกเลิก",
		}
		lines = append(lines, "", styles.Footer.Render(strings.Join(help, "  ")))
		return strings.Join(lines, "\n")
	}

	// List mode.
	if v.err != "" {
		lines = append(lines, styles.Error.Render("✘ "+v.err))
	}
	if len(v.rows) == 0 {
		lines = append(lines, styles.HelpDesc.Render("ยังไม่มีผู้ใช้ — กด n เพื่อเพิ่ม"))
	} else {
		header := fmt.Sprintf("  %-16s  %-12s  %-12s  %s",
			"ชื่อ", "สร้างเมื่อ", "หมดอายุ", "Limit")
		lines = append(lines, styles.HelpDesc.Render(header))
		for i, r := range v.rows {
			exp := "(ไม่มี)"
			if !r.ExpiresAt.IsZero() {
				exp = r.ExpiresAt.Format("2006-01-02")
			}
			lim := "-"
			if r.Limit > 0 {
				lim = strconv.Itoa(r.Limit)
			}
			row := fmt.Sprintf("  %-16s  %-12s  %-12s  %s",
				r.Name, r.CreatedAt.Format("2006-01-02"), exp, lim)
			if i == v.sel {
				row = styles.ItemActive.Render("→") + row[1:]
			}
			lines = append(lines, row)
		}
	}
	if v.msg != "" {
		lines = append(lines, "", styles.HelpDesc.Render("» "+v.msg))
	}

	parts := []string{
		hk("↑/↓") + " เลือก",
		hk("n") + " เพิ่ม",
		hk("d") + " ลบ",
		hk("esc") + " กลับ",
	}
	lines = append(lines, "", styles.Footer.Render(strings.Join(parts, "  ")))
	return strings.Join(lines, "\n")
}

// --- form helpers ---

func newUsersForm() usersForm {
	mk := func(label, placeholder string, mask bool) textinput.Model {
		ti := textinput.New()
		ti.Placeholder = placeholder
		ti.Prompt = label + ": "
		ti.CharLimit = 64
		ti.Width = 40
		if mask {
			ti.EchoMode = textinput.EchoPassword
			ti.EchoCharacter = '•'
		}
		return ti
	}
	f := usersForm{
		inputs: make([]textinput.Model, uFieldCount),
	}
	f.inputs[uName] = mk("ชื่อผู้ใช้", "เช่น hexpat", false)
	f.inputs[uPassword] = mk("รหัสผ่าน", "อย่างน้อย 4 ตัวอักษร", true)
	f.inputs[uExpireDays] = mk("จำนวนวัน (หมดอายุ)", "30 หรือ 0 = ไม่หมด", false)
	f.inputs[uLimit] = mk("จำกัดการเชื่อมต่อพร้อมกัน", "0 = ไม่จำกัด", false)
	f.inputs[uRemoteHost] = mk("Remote (IP สำหรับ .ovpn)", "/etc/IP หรือกรอกเอง", false)
	f.inputs[uName].Focus()
	return f
}

func (f *usersForm) refocus() {
	for i := range f.inputs {
		if i == f.focusIdx {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
}

func (f usersForm) view() string {
	var lines []string
	for i := range f.inputs {
		lines = append(lines, f.inputs[i].View())
	}
	return styles.Form.Render(strings.Join(lines, "\n"))
}

func max0(n int) int {
	if n < 0 {
		return 0
	}
	return n
}
