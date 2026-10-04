package service

import (
	"reflect"
	"strings"
	"testing"
)

// After a crash loop trips StartLimitBurst, systemd refuses start/restart
// until reset-failed. Start and Restart must clear it first; Stop must not
// care.
func TestStartRestartResetFailedFirst(t *testing.T) {
	svc := Service{UnitName: "hexplus-squid.service"}
	cases := []struct {
		name string
		call func(Service) error
		want []string
	}{
		{"start", Start, []string{"reset-failed hexplus-squid.service", "start hexplus-squid.service"}},
		{"restart", Restart, []string{"reset-failed hexplus-squid.service", "restart hexplus-squid.service"}},
		{"stop", Stop, []string{"stop hexplus-squid.service"}},
	}
	for _, c := range cases {
		var calls []string
		old := systemctl
		systemctl = func(args ...string) ([]byte, error) {
			calls = append(calls, strings.Join(args, " "))
			return nil, nil
		}
		err := c.call(svc)
		systemctl = old
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if !reflect.DeepEqual(calls, c.want) {
			t.Errorf("%s: systemctl calls = %q, want %q", c.name, calls, c.want)
		}
	}
}
