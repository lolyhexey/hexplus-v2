// Package unitpolicy holds the systemd restart-rate limits shared by every
// unit template hexplus writes (OpenVPN/squid/dropbear/..., proxy,
// sslhmux, ssltunnel), plus a checker the templates' tests use.
//
// Why it exists: each template sets Restart=on-failure with RestartSec=5s
// and used to leave the rate limiter at systemd's default of 5 starts per
// 10s. Five restarts 5s apart span 25s, so the burst could never trip and
// a unit that failed permanently restarted forever (1388 restarts in one
// boot were observed on a production box). The window has to be longer than
// RestartSec x StartLimitBurst for the limiter to be reachable.
package unitpolicy

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// StartLimit goes in the [Unit] section. With RestartSec=5s, a unit that
// fails on every start reaches the "failed" state after about 100s
// (20 x 5s), well inside the 300s window. A unit that merely crashes now
// and then needs to average one start every 15s to trip it, so occasional
// crashes still self-heal.
const StartLimit = "StartLimitIntervalSec=300\nStartLimitBurst=20\n"

var (
	intervalRe = regexp.MustCompile(`(?m)^StartLimitIntervalSec=(\d+)s?$`)
	burstRe    = regexp.MustCompile(`(?m)^StartLimitBurst=(\d+)$`)
	restartRe  = regexp.MustCompile(`(?m)^RestartSec=(\d+)s?$`)
)

// Check verifies that unit declares a restart limit the unit can actually
// reach: the limits sit in [Unit], and RestartSec x StartLimitBurst fits
// inside StartLimitIntervalSec. unit may be a raw text/template source.
func Check(unit string) error {
	svc := strings.Index(unit, "\n[Service]")
	if svc < 0 {
		return fmt.Errorf("no [Service] section")
	}
	unitSec, svcSec := unit[:svc], unit[svc:]

	interval, err := number(intervalRe, unitSec, "StartLimitIntervalSec (in [Unit])")
	if err != nil {
		return err
	}
	burst, err := number(burstRe, unitSec, "StartLimitBurst (in [Unit])")
	if err != nil {
		return err
	}
	restart, err := number(restartRe, svcSec, "RestartSec (in [Service])")
	if err != nil {
		return err
	}
	if restart*burst >= interval {
		return fmt.Errorf("RestartSec(%ds) x StartLimitBurst(%d) = %ds does not fit inside "+
			"StartLimitIntervalSec(%ds): the limiter can never trip", restart, burst, restart*burst, interval)
	}
	return nil
}

func number(re *regexp.Regexp, s, what string) (int, error) {
	m := re.FindStringSubmatch(s)
	if m == nil {
		return 0, fmt.Errorf("missing %s", what)
	}
	n, err := strconv.Atoi(m[1])
	if err != nil {
		return 0, fmt.Errorf("%s: %w", what, err)
	}
	return n, nil
}
