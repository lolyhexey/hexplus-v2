// Package ovpnspread spreads the OpenVPN primary port over several OpenVPN
// processes, so a busy server uses more than one CPU core.
//
// OpenVPN 2.5 does all its work in one thread: one process saturates one
// core (measured: 100% of a core at ~1.3 Gbit/s while the host was 93%
// idle), and two processes cannot listen on the same port. So Enable adds
// "worker" instances (ovpninstance, Worker=true) on internal ports, and
// iptables REDIRECT hands each new connection for the primary port to the
// primary or one of the workers in turn:
//
//   - nat PREROUTING: connections from outside to the primary port;
//   - nat OUTPUT: connections the host itself makes to
//     127.0.0.0/8:<primary port>, which is where SSL TUNNEL, SSLH MULTIPLEX
//     and the proxies forward OpenVPN traffic.
//
// conntrack keeps every connection on the process that took its first
// packet. Workers share the primary's PKI and auth, so any client can land
// anywhere and the .ovpn files keep pointing at the primary port. A worker
// port admits only redirected connections (filter INPUT).
//
// The rules live in two chains of our own and are rebuilt from the instance
// registry and server.conf by Apply, which rc.local runs at boot.
package ovpnspread

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/firewall"
	"github.com/lolyhexey/hexplus/internal/ovpninstance"
	"github.com/lolyhexey/hexplus/internal/paths"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/speedlimit"
)

const (
	// NATChain holds the REDIRECT rules; PREROUTING and OUTPUT jump to it.
	NATChain = "HEXPLUS-OVPN-SPREAD"
	// InputChain admits redirected connections to the worker ports and
	// drops direct ones; INPUT jumps to it.
	InputChain = "HEXPLUS-OVPN-SPREAD-IN"
	// WorkerPortBase is where the search for free worker ports starts.
	WorkerPortBase = 21195
	// MaxProcs bounds the number of OpenVPN processes Enable creates.
	MaxProcs = 64
)

// Overridable for tests.
var (
	serverConf = "/etc/openvpn/server.conf"
	rcLocal    = firewall.RCLocalPath
	iptables   = func(args ...string) ([]byte, error) {
		return exec.Command("iptables", append([]string{"-w", "5"}, args...)...).CombinedOutput()
	}
	allInstances = ovpninstance.List
	portFree     = func(port int, proto string) bool {
		addr := ":" + strconv.Itoa(port)
		if proto == "udp" {
			pc, err := net.ListenPacket("udp", addr)
			if err != nil {
				return false
			}
			pc.Close()
			return true
		}
		ln, err := net.Listen("tcp", addr)
		if err != nil {
			return false
		}
		ln.Close()
		return true
	}
)

// RCLocalLine is the boot line that restores the rules.
func RCLocalLine() string { return paths.SelfPath + " ovpnspread apply >/dev/null 2>&1 || true" }

// Plan is what the rules are built from.
type Plan struct {
	Proto   string // "tcp" or "udp", the primary's
	Port    int    // the primary port
	Workers []int  // worker ports
}

// natRules are NATChain's rules. With n processes, rule i takes one in
// (n-i) of the new connections that reach it, which gives each worker and
// the primary (the fall-through) one connection in n, in turn. The nat
// table only sees the first packet of a connection.
func (p Plan) natRules() [][]string {
	n := len(p.Workers) + 1
	var out [][]string
	for i, w := range p.Workers {
		out = append(out, []string{"-p", p.Proto,
			"-m", "statistic", "--mode", "nth", "--every", strconv.Itoa(n - i), "--packet", "0",
			"-j", "REDIRECT", "--to-ports", strconv.Itoa(w)})
	}
	return out
}

// inputRules are InputChain's rules: a worker port takes redirected
// connections (conntrack marks REDIRECT as DNAT) and nothing else from
// outside the host.
func (p Plan) inputRules() [][]string {
	var out [][]string
	for _, w := range p.Workers {
		port := strconv.Itoa(w)
		out = append(out,
			[]string{"-p", p.Proto, "--dport", port, "-m", "conntrack", "--ctstate", "DNAT", "-j", "ACCEPT"},
			[]string{"!", "-i", "lo", "-p", p.Proto, "--dport", port, "-j", "DROP"})
	}
	return out
}

type jump struct {
	table, chain string
	spec         []string
}

func (p Plan) jumps() []jump {
	port := strconv.Itoa(p.Port)
	return []jump{
		// Only connections addressed to this host: without addrtype, VPN
		// clients' own traffic to port <primary> elsewhere (HTTPS when the
		// primary is 443) is forwarded through PREROUTING too and would be
		// redirected into OpenVPN.
		{"nat", "PREROUTING", []string{"-p", p.Proto, "-m", "addrtype", "--dst-type", "LOCAL", "--dport", port, "-j", NATChain}},
		{"nat", "OUTPUT", []string{"-o", "lo", "-d", "127.0.0.0/8", "-p", p.Proto, "--dport", port, "-j", NATChain}},
		{"filter", "INPUT", []string{"-j", InputChain}},
	}
}

// Enabled reports whether spreading is on, i.e. any worker exists.
func Enabled() bool {
	ws, _ := Workers()
	return len(ws) > 0
}

// Workers lists the worker instances.
func Workers() ([]ovpninstance.Instance, error) {
	list, err := allInstances()
	return ovpninstance.Workers(list), err
}

// Primary reads the primary's port and protocol from server.conf, with
// OpenVPN's own defaults (1194, udp) for a missing directive.
func Primary() (proto string, port int, err error) {
	raw, err := os.ReadFile(serverConf)
	if err != nil {
		return "", 0, err
	}
	proto, port = "udp", 1194
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) < 2 {
			continue
		}
		switch f[0] {
		case "port":
			if n, err := strconv.Atoi(f[1]); err == nil && n > 0 && n < 65536 {
				port = n
			}
		case "proto":
			if strings.HasPrefix(f[1], "tcp") {
				proto = "tcp"
			} else {
				proto = "udp"
			}
		}
	}
	return proto, port, nil
}

// Apply (re)builds the rules from the registry and server.conf, and makes
// sure rc.local restores them at boot. With no workers it tears them down.
// Call it after anything that changes the primary port or the workers.
func Apply() error {
	ws, err := Workers()
	if err != nil {
		return err
	}
	if len(ws) == 0 {
		return Teardown()
	}
	proto, port, err := Primary()
	if err != nil {
		return err
	}
	p := Plan{Proto: proto, Port: port}
	for _, w := range ws {
		if w.Proto != proto {
			return fmt.Errorf("worker #%d ใช้ %s แต่พอร์ตหลักใช้ %s: ปิดแล้วเปิดการกระจายโหลดใหม่", w.ID, w.Proto, proto)
		}
		p.Workers = append(p.Workers, w.Port)
	}
	if err := applyPlan(p); err != nil {
		return err
	}
	if err := writeDropIn(ws); err != nil {
		return err
	}
	return firewall.AddRCLocalLine(rcLocal, RCLocalLine())
}

func applyPlan(p Plan) error {
	for _, c := range []struct {
		table, chain string
		rules        [][]string
	}{{"nat", NATChain, p.natRules()}, {"filter", InputChain, p.inputRules()}} {
		if !chainExists(c.table, c.chain) {
			if out, err := iptables("-t", c.table, "-N", c.chain); err != nil {
				return fmt.Errorf("iptables -N %s: %v: %s", c.chain, err, strings.TrimSpace(string(out)))
			}
		}
		if out, err := iptables("-t", c.table, "-F", c.chain); err != nil {
			return fmt.Errorf("iptables -F %s: %v: %s", c.chain, err, strings.TrimSpace(string(out)))
		}
		for _, r := range c.rules {
			if out, err := iptables(append([]string{"-t", c.table, "-A", c.chain}, r...)...); err != nil {
				return fmt.Errorf("iptables -A %s %s: %v: %s", c.chain, strings.Join(r, " "), err, strings.TrimSpace(string(out)))
			}
		}
	}
	// Jumps from an earlier primary port must go, or connections to the
	// old port would still be redirected.
	if err := removeJumps(); err != nil {
		return err
	}
	for _, j := range p.jumps() {
		if out, err := iptables(append([]string{"-t", j.table, "-I", j.chain, "1"}, j.spec...)...); err != nil {
			return fmt.Errorf("iptables -I %s %s: %v: %s", j.chain, strings.Join(j.spec, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}

func chainExists(table, chain string) bool {
	_, err := iptables("-t", table, "-S", chain)
	return err == nil
}

// removeJumps deletes every rule in PREROUTING, OUTPUT and INPUT that jumps
// to one of our chains, whatever else it matches.
func removeJumps() error {
	var errs []error
	for _, c := range []struct{ table, chain, target string }{
		{"nat", "PREROUTING", NATChain}, {"nat", "OUTPUT", NATChain}, {"filter", "INPUT", InputChain},
	} {
		out, err := iptables("-t", c.table, "-S", c.chain)
		if err != nil {
			errs = append(errs, fmt.Errorf("iptables -S %s: %v", c.chain, err))
			continue
		}
		for _, line := range strings.Split(string(out), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || f[0] != "-A" || f[1] != c.chain || !jumpsTo(f, c.target) {
				continue
			}
			if out, err := iptables(append([]string{"-t", c.table, "-D"}, f[1:]...)...); err != nil {
				errs = append(errs, fmt.Errorf("iptables -D %s: %v: %s", strings.Join(f[1:], " "), err, strings.TrimSpace(string(out))))
			}
		}
	}
	return errors.Join(errs...)
}

func jumpsTo(fields []string, target string) bool {
	for i := 0; i+1 < len(fields); i++ {
		if fields[i] == "-j" && fields[i+1] == target {
			return true
		}
	}
	return false
}

// Teardown removes the rules and the rc.local line. It leaves the workers
// alone (Disable removes them); without the rules they get no new
// connections.
func Teardown() error {
	errs := []error{removeJumps()}
	for _, c := range []struct{ table, chain string }{{"nat", NATChain}, {"filter", InputChain}} {
		if !chainExists(c.table, c.chain) {
			continue
		}
		if out, err := iptables("-t", c.table, "-F", c.chain); err != nil {
			errs = append(errs, fmt.Errorf("iptables -F %s: %v: %s", c.chain, err, strings.TrimSpace(string(out))))
		}
		if out, err := iptables("-t", c.table, "-X", c.chain); err != nil {
			errs = append(errs, fmt.Errorf("iptables -X %s: %v: %s", c.chain, err, strings.TrimSpace(string(out))))
		}
	}
	line := RCLocalLine()
	errs = append(errs, firewall.RemoveRCLocalLines(rcLocal, func(l string) bool { return l == line }))
	errs = append(errs, removeDropIn())
	return errors.Join(errs...)
}

// pickPorts returns n ports, from WorkerPortBase up, that are free for proto
// and not taken by the primary or any registered instance (a stopped one
// holds no socket but still owns its port).
func pickPorts(n int, proto string, primary int, list []ovpninstance.Instance) ([]int, error) {
	taken := map[int]bool{primary: true}
	for _, i := range list {
		taken[i.Port] = true
	}
	var out []int
	for p := WorkerPortBase; p < 65536 && len(out) < n; p++ {
		if !taken[p] && portFree(p, proto) {
			out = append(out, p)
		}
	}
	if len(out) < n {
		return nil, fmt.Errorf("หาพอร์ตว่างได้ %d จาก %d พอร์ต", len(out), n)
	}
	return out, nil
}

// Enable spreads the primary port over total OpenVPN processes: the
// primary plus total-1 workers on the primary's protocol. Workers inherit
// the primary's speed limit. On any failure everything is rolled back.
// logf reports progress.
func Enable(total int, dnsPush []string, logf func(string, ...any)) error {
	if total < 2 || total > MaxProcs {
		return fmt.Errorf("จำนวน process ต้องอยู่ระหว่าง 2-%d", MaxProcs)
	}
	if Enabled() {
		return errors.New("เปิดการกระจายโหลดอยู่แล้ว")
	}
	proto, port, err := Primary()
	if err != nil {
		return err
	}
	list, err := allInstances()
	if err != nil {
		return err
	}
	ports, err := pickPorts(total-1, proto, port, list)
	if err != nil {
		return err
	}
	if err := enable(proto, ports, dnsPush, logf); err != nil {
		if rerr := Disable(logf); rerr != nil {
			return fmt.Errorf("%w (ย้อนกลับไม่ครบ: %v)", err, rerr)
		}
		return err
	}
	return nil
}

func enable(proto string, ports []int, dnsPush []string, logf func(string, ...any)) error {
	var made []ovpninstance.Instance
	for _, p := range ports {
		inst, err := ovpninstance.AddWorker(p, proto, dnsPush)
		if err != nil {
			return err
		}
		made = append(made, inst)
	}
	// Every worker gets the primary's limit, 0 included: a key left behind
	// by a removed extra port with the same id would otherwise apply.
	if _, err := SyncSpeedLimit(); err != nil {
		return err
	}
	// Workers are not enabled on their own: the primary's drop-in wants
	// them, so they boot, stop and restart with OpenVPN (PartOf).
	if err := writeDropIn(made); err != nil {
		return err
	}
	for _, w := range made {
		if err := service.Start(w.Service()); err != nil {
			return err
		}
	}
	// A worker that is not listening would refuse its share of the
	// connections, so wait for all of them before redirecting anything.
	for _, w := range made {
		if !waitListening(w.Port, w.Proto, 10*time.Second) {
			return fmt.Errorf("worker #%d (พอร์ต %d/%s) ไม่ขึ้น", w.ID, w.Port, w.Proto)
		}
		logf("worker #%d listening on %d/%s", w.ID, w.Port, w.Proto)
	}
	return Apply()
}

func waitListening(port int, proto string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if ok, _ := service.ListenStatus(port, proto); ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
}

// Disable removes the rules first, so new connections go to the primary,
// then every worker; sessions on workers end and reconnect to the primary.
func Disable(logf func(string, ...any)) error {
	// If the rules could not be removed, the workers must stay: removing
	// them would leave connections redirected to ports nobody listens on.
	if err := Teardown(); err != nil {
		return fmt.Errorf("ลบกฎ iptables ไม่สำเร็จ (ยังไม่ได้ลบ process เสริม): %w", err)
	}
	var errs []error
	ws, err := Workers()
	if err != nil {
		return errors.Join(append(errs, err)...)
	}
	limits := speedlimit.LoadAll()
	for _, w := range ws {
		if key := strconv.Itoa(w.ID); limits[key] > 0 {
			errs = append(errs, speedlimit.SetLimit(key, 0))
		}
		if err := ovpninstance.Remove(w.ID); err != nil {
			errs = append(errs, err)
			continue
		}
		logf("worker #%d removed", w.ID)
	}
	return errors.Join(errs...)
}

// SyncSpeedLimit gives every worker the primary's speed limit (the main
// key; 0 clears theirs) and returns the workers whose limit changed. Like
// the primary after a change, they need a restart for sessions already
// connected to get the new limit.
func SyncSpeedLimit() ([]ovpninstance.Instance, error) {
	ws, err := Workers()
	if err != nil {
		return nil, err
	}
	limits := speedlimit.LoadAll()
	main := limits[speedlimit.MainKey]
	var changed []ovpninstance.Instance
	var errs []error
	for _, w := range ws {
		key := strconv.Itoa(w.ID)
		if limits[key] == main {
			continue
		}
		if err := speedlimit.SetLimit(key, main); err != nil {
			errs = append(errs, err)
			continue
		}
		changed = append(changed, w)
	}
	return changed, errors.Join(errs...)
}
