package service

import (
	"strings"
	"testing"
	"text/template"

	"github.com/lolyhexey/hexplus/internal/unitpolicy"
)

func TestUnitTemplateRestartLimit(t *testing.T) {
	if err := unitpolicy.Check(unitTemplate); err != nil {
		t.Fatal(err)
	}
}

func TestUnitTemplatePartOf(t *testing.T) {
	tmpl := template.Must(template.New("unit").Funcs(template.FuncMap{"join": strings.Join}).Parse(unitTemplate))
	render := func(svc Service) string {
		var b strings.Builder
		if err := tmpl.Execute(&b, svc); err != nil {
			t.Fatal(err)
		}
		return b.String()
	}
	base := Service{UnitName: "hexplus-openvpn3.service", Binary: "/bin/true", After: []string{"network-online.target"}}
	if out := render(base); strings.Contains(out, "PartOf") {
		t.Errorf("PartOf rendered for a unit without it:\n%s", out)
	}
	base.PartOf = []string{"hexplus-openvpn.service"}
	out := render(base)
	unit := out[strings.Index(out, "[Unit]"):strings.Index(out, "[Service]")]
	if !strings.Contains(unit, "\nPartOf=hexplus-openvpn.service\n") {
		t.Errorf("PartOf missing from [Unit]:\n%s", out)
	}
	if err := unitpolicy.Check(out); err != nil {
		t.Error(err)
	}
}
