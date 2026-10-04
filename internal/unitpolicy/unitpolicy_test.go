package unitpolicy

import (
	"strings"
	"testing"
)

const skeleton = `[Unit]
Description=x
%s
[Service]
Restart=on-failure
RestartSec=%s
`

func unit(limits, restart string) string {
	return strings.Replace(strings.Replace(skeleton, "%s", limits, 1), "%s", restart, 1)
}

func TestCheck(t *testing.T) {
	cases := []struct {
		name    string
		unit    string
		wantErr bool
	}{
		{"shared constant with 5s", unit(StartLimit, "5s"), false},
		// The defect: systemd's defaults (10s window, burst 5) with 5s
		// restarts. 5 x 5s = 25s > 10s, so the limiter never trips.
		{"systemd defaults", unit("StartLimitIntervalSec=10\nStartLimitBurst=5\n", "5s"), true},
		{"no limits declared", unit("", "5s"), true},
		{"limit in wrong section", "[Unit]\nDescription=x\n\n[Service]\nRestartSec=5s\nStartLimitIntervalSec=300\nStartLimitBurst=20\n", true},
		{"no RestartSec", "[Unit]\nStartLimitIntervalSec=300\nStartLimitBurst=20\n\n[Service]\nRestart=on-failure\n", true},
		{"window exactly equal", unit("StartLimitIntervalSec=100\nStartLimitBurst=20\n", "5s"), true},
		{"no service section", "[Unit]\nStartLimitIntervalSec=300\n", true},
	}
	for _, c := range cases {
		if err := Check(c.unit); (err != nil) != c.wantErr {
			t.Errorf("%s: Check err = %v, wantErr %v", c.name, err, c.wantErr)
		}
	}
}

func TestStartLimitLeavesRoomForRestartSec(t *testing.T) {
	// A permanently failing unit must reach "failed" instead of looping,
	// but a unit that crashes once a minute must not be tripped.
	if err := Check(unit(StartLimit, "5s")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(StartLimit, "\n\n") || !strings.HasSuffix(StartLimit, "\n") {
		t.Errorf("StartLimit must be whole lines ending in a newline: %q", StartLimit)
	}
}
