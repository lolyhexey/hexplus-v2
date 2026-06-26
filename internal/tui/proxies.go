// proxies.go: the "จัดการ Proxy SOCKS" sub-page. List + add form + remove.

package tui

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/lolyhexey/hexplus/internal/proxy"
)

type proxiesView struct {
	width, height int
	rows          []proxy.Config
	sel           int
	mode          proxiesMode
	form          proxiesForm
	msg           string
	err           string
}

type proxiesMode int

const (
	proxiesList proxiesMode = iota
	proxiesAdd
)

const (
	pName = iota
	pPort
	pTarget
	pPreset
	pStatusMsg
	pFieldCount
)

type proxiesForm struct {
	inputs   []textinput.Model
	focusIdx int
}

func newProxiesView(w, h int) tea.Model {
	v := proxiesView{width: w, height: h}
	v.reloadRows()
	return v
}

func (v *proxiesView) reloadRows() {
	db, err := proxy.Load()
	if err != nil {
		v.err = err.Error()
		return
	}
	v.rows = db.All()
	v.err = ""
}

func (v proxiesView) Init() tea.Cmd { return nil }

func (v proxiesView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		return v, nil
	case tea.KeyMsg:
		switch v.mode {
		case proxiesList:
			return v.updateList(msg)
		case proxiesAdd:
			return v.updateAdd(msg)
		}
	}
	return v, nil
}

func (v proxiesView) updateList(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
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
		v.mode = proxiesAdd
		v.form = newProxiesForm()
		v.msg, v.err = "", ""
	case "d":
		if v.sel >= 0 && v.sel < len(v.rows) {
			c := v.rows[v.sel]
			db, err := proxy.Load()
			if err == nil {
				delete(db.Proxies, c.Name)
				if saveErr := db.Save(); saveErr != nil {
					v.err = saveErr.Error()
					return v, nil
				}
				_, _, _, _ = proxy.RemoveUnit(c)
				v.msg = "ลบ proxy " + c.Name + " แล้ว"
				v.reloadRows()
				if v.sel >= len(v.rows) {
					v.sel = max0(len(v.rows) - 1)
				}
			} else {
				v.err = err.Error()
			}
		}
	}
	return v, nil
}

// presetByLabel maps the human label the operator types into the
// (status_code, default_msg) tuple. Matches the conexao.bash presets.
var presetByLabel = map[string]struct{ code, msg string }{
	"101":      {"101", `<font color="null">HEXPLUS</font>`},
	"200":      {"200", `Connection established\r\nContent-length: 0`},
	"400":      {"400", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`},
	"520":      {"520", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`},
	"กำหนดเอง": {"", ""},
}

func (v proxiesView) updateAdd(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "esc":
		v.mode = proxiesList
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
			v.form.focusIdx++
			v.form.refocus()
			return v, nil
		}
		return v.submitAdd()
	}
	var cmd tea.Cmd
	v.form.inputs[v.form.focusIdx], cmd = v.form.inputs[v.form.focusIdx].Update(msg)
	return v, cmd
}

func (v proxiesView) submitAdd() (tea.Model, tea.Cmd) {
	name := strings.TrimSpace(v.form.inputs[pName].Value())
	portStr := strings.TrimSpace(v.form.inputs[pPort].Value())
	target := strings.TrimSpace(v.form.inputs[pTarget].Value())
	preset := strings.TrimSpace(v.form.inputs[pPreset].Value())
	customMsg := v.form.inputs[pStatusMsg].Value()

	if err := proxy.ValidateName(name); err != nil {
		v.err = err.Error()
		return v, nil
	}
	port, err := strconv.Atoi(portStr)
	if err != nil || port <= 0 || port > 65535 {
		v.err = "พอร์ตไม่ถูกต้อง"
		return v, nil
	}
	if target == "" {
		target = "127.0.0.1:22"
	}

	p, ok := presetByLabel[preset]
	if !ok {
		// Treat unknown labels as a literal status code so power users
		// who want '418' can type it directly.
		p.code = preset
	}
	if customMsg != "" {
		p.msg = customMsg
	}
	if p.msg == "" {
		p.msg = `<font color="null">HEXPLUS</font>`
	}

	cfg := proxy.Config{
		Name:        name,
		Port:        port,
		DefaultHost: target,
		StatusCode:  p.code,
		StatusMsg:   p.msg,
	}
	if _, vErr := proxy.NewHandler(cfg); vErr != nil {
		v.err = vErr.Error()
		return v, nil
	}
	db, dbErr := proxy.Load()
	if dbErr != nil {
		v.err = dbErr.Error()
		return v, nil
	}
	db.Proxies[name] = cfg
	if saveErr := db.Save(); saveErr != nil {
		v.err = saveErr.Error()
		return v, nil
	}
	_, _, _, _ = proxy.WriteUnit(cfg)
	v.msg = "เพิ่ม proxy " + name + " (พอร์ต " + portStr + ") แล้ว"
	v.mode = proxiesList
	v.err = ""
	v.reloadRows()
	return v, nil
}

func (v proxiesView) View() string {
	var lines []string
	lines = append(lines, styles.Header.Render("จัดการ Proxy SOCKS"))
	lines = append(lines, "")

	if v.mode == proxiesAdd {
		lines = append(lines, v.form.view())
		if v.err != "" {
			lines = append(lines, "", styles.Error.Render("✘ "+v.err))
		}
		help := []string{
			hk("Tab/↑/↓") + " ย้าย",
			hk("Enter") + " ยืนยัน",
			hk("Esc") + " ยกเลิก",
		}
		lines = append(lines, "", styles.Footer.Render(strings.Join(help, "  ")))
		return strings.Join(lines, "\n")
	}

	if v.err != "" {
		lines = append(lines, styles.Error.Render("✘ "+v.err))
	}
	if len(v.rows) == 0 {
		lines = append(lines, styles.HelpDesc.Render("ยังไม่มี proxy — กด n เพื่อเพิ่ม"))
	} else {
		header := fmt.Sprintf("  %-12s  %-6s  %-22s  %-5s",
			"ชื่อ", "PORT", "DEFAULT_HOST", "CODE")
		lines = append(lines, styles.HelpDesc.Render(header))
		for i, c := range v.rows {
			row := fmt.Sprintf("  %-12s  %-6d  %-22s  %-5s",
				c.Name, c.Port, c.DefaultHost, c.StatusCode)
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

func newProxiesForm() proxiesForm {
	mk := func(label, placeholder string) textinput.Model {
		ti := textinput.New()
		ti.Prompt = label + ": "
		ti.Placeholder = placeholder
		ti.CharLimit = 96
		ti.Width = 48
		return ti
	}
	f := proxiesForm{inputs: make([]textinput.Model, pFieldCount)}
	f.inputs[pName] = mk("ชื่อ proxy", "เช่น ws, ssh, openvpn")
	f.inputs[pPort] = mk("พอร์ต", "เช่น 80, 8080, 2082")
	f.inputs[pTarget] = mk("Default host:port", "เช่น 127.0.0.1:22")
	f.inputs[pPreset] = mk("Preset", "101 (แนะนำ) / 200 / 400 / 520 / กำหนดเอง")
	f.inputs[pStatusMsg] = mk("ข้อความสถานะ (ไม่บังคับ)", `เช่น <font color="null">MY</font>\r\nContent-length: 0`)
	f.inputs[pPreset].SetValue("101")
	f.inputs[pName].Focus()
	return f
}

func (f *proxiesForm) refocus() {
	for i := range f.inputs {
		if i == f.focusIdx {
			f.inputs[i].Focus()
		} else {
			f.inputs[i].Blur()
		}
	}
}

func (f proxiesForm) view() string {
	var lines []string
	for i := range f.inputs {
		lines = append(lines, f.inputs[i].View())
	}
	return styles.Form.Render(strings.Join(lines, "\n"))
}
