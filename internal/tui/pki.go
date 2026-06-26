// pki.go: the "PKI" sub-page. Read-only inspection of the on-disk CA +
// server cert + ta.key, plus an "Initialize" affordance when nothing is
// there yet.

package tui

import (
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/lolyhexey/hexplus/internal/pki"
)

type pkiView struct {
	width, height int
	ca            pki.CertInfo
	server        pki.CertInfo
	serverKey     os.FileInfo
	taKey         os.FileInfo
	loadErr       string
	actionMsg     string
}

func newPKIView(w, h int) tea.Model {
	v := pkiView{width: w, height: h}
	v.reload()
	return v
}

func (v *pkiView) reload() {
	caInfo, err := pki.InspectCert(pki.OpenVPNDir + "/ca.crt")
	if err != nil {
		v.loadErr = err.Error()
	}
	v.ca = caInfo
	srvInfo, err := pki.InspectCert(pki.OpenVPNDir + "/server.crt")
	if err != nil && v.loadErr == "" {
		v.loadErr = err.Error()
	}
	v.server = srvInfo

	if st, err := os.Stat(pki.OpenVPNDir + "/server.key"); err == nil {
		v.serverKey = st
	} else {
		v.serverKey = nil
	}
	if st, err := os.Stat(pki.OpenVPNDir + "/ta.key"); err == nil {
		v.taKey = st
	} else {
		v.taKey = nil
	}
}

func (v pkiView) Init() tea.Cmd { return nil }

func (v pkiView) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		v.width, v.height = msg.Width, msg.Height
		return v, nil
	case tea.KeyMsg:
		switch msg.String() {
		case "esc", "q", "left", "h":
			return returnToMain{}, nil
		case "i":
			res, err := pki.Init(pki.InitOptions{})
			if err != nil {
				v.actionMsg = "init ล้มเหลว: " + err.Error()
			} else {
				v.actionMsg = fmt.Sprintf("init สำเร็จ — เขียน %d ไฟล์", len(res.Written))
				v.reload()
			}
		case "r":
			v.reload()
		}
	}
	return v, nil
}

func (v pkiView) View() string {
	var lines []string
	lines = append(lines, styles.Header.Render("PKI สำหรับ OpenVPN"))
	lines = append(lines, "")

	if !pki.IsInitialized() {
		lines = append(lines, styles.StatusOff.Render("ยังไม่ได้สร้าง PKI"))
		lines = append(lines, "")
		lines = append(lines, "  รัน "+styles.HelpKey.Render("hexplus pki init")+" หรือกด "+styles.HelpKey.Render("i")+" เพื่อสร้างที่นี่")
	} else {
		lines = append(lines, certBlock("ca.crt", v.ca))
		lines = append(lines, certBlock("server.crt", v.server))
		lines = append(lines, fileBlock("server.key", v.serverKey))
		lines = append(lines, fileBlock("ta.key", v.taKey))
	}

	if v.loadErr != "" {
		lines = append(lines, "", styles.Error.Render("✘ "+v.loadErr))
	}
	if v.actionMsg != "" {
		lines = append(lines, "", styles.HelpDesc.Render("» "+v.actionMsg))
	}
	parts := []string{
		hk("i") + " init",
		hk("r") + " refresh",
		hk("esc") + " กลับ",
	}
	lines = append(lines, "", styles.Footer.Render(strings.Join(parts, "  ")))
	return strings.Join(lines, "\n")
}

func certBlock(label string, info pki.CertInfo) string {
	if !info.Present {
		return "  " + styles.StatusOff.Render(runeOff) + "  " + styles.StatusLabel.Render(label) + "  " + styles.HelpDesc.Render("(missing)")
	}
	return strings.Join([]string{
		"  " + styles.StatusOn.Render(runeOn) + "  " + styles.StatusLabel.Render(label),
		"      subject:   " + styles.StatusValue.Render(info.Subject),
		"      issuer:    " + styles.StatusValue.Render(info.Issuer),
		"      not after: " + styles.StatusValue.Render(info.NotAfter),
	}, "\n")
}

func fileBlock(label string, st os.FileInfo) string {
	if st == nil {
		return "  " + styles.StatusOff.Render(runeOff) + "  " + styles.StatusLabel.Render(label) + "  " + styles.HelpDesc.Render("(missing)")
	}
	return fmt.Sprintf("  %s  %s  mode %s, %d bytes",
		styles.StatusOn.Render(runeOn),
		styles.StatusLabel.Render(label),
		st.Mode(),
		st.Size(),
	)
}
