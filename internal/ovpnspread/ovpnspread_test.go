package ovpnspread

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/lolyhexey/hexplus/internal/ovpninstance"
)

// fakeIPT keeps iptables state per table and chain, enough for -N -F -X -A
// -I -D -S, so tests can check what the rules end up as.
type fakeIPT struct {
	tables map[string]map[string][]string
	calls  []string
	failS  bool // every -S fails, as when the xtables lock cannot be taken
}

func newFakeIPT() *fakeIPT {
	return &fakeIPT{tables: map[string]map[string][]string{
		"nat":    {"PREROUTING": {"-j DOCKER"}, "OUTPUT": {}, "POSTROUTING": {}},
		"filter": {"INPUT": {"-p tcp --dport 443 -j ACCEPT"}, "FORWARD": {}, "OUTPUT": {}},
	}}
}

func (f *fakeIPT) run(args ...string) ([]byte, error) {
	if len(args) >= 2 && args[0] == "-w" {
		args = args[2:]
	}
	f.calls = append(f.calls, strings.Join(args, " "))
	table := "filter"
	if len(args) >= 2 && args[0] == "-t" {
		table, args = args[1], args[2:]
	}
	t := f.tables[table]
	op, chain, rest := args[0], args[1], args[2:]
	rules, exists := t[chain]
	switch op {
	case "-N":
		if exists {
			return []byte("Chain already exists."), fmt.Errorf("exit status 1")
		}
		t[chain] = []string{}
	case "-F":
		if !exists {
			return []byte("No chain/target/match by that name."), fmt.Errorf("exit status 1")
		}
		t[chain] = []string{}
	case "-X":
		if !exists {
			return []byte("No chain/target/match by that name."), fmt.Errorf("exit status 1")
		}
		delete(t, chain)
	case "-A":
		t[chain] = append(rules, strings.Join(rest, " "))
	case "-I":
		spec := strings.Join(rest[1:], " ") // rest[0] is the position; tests always use 1
		t[chain] = append([]string{spec}, rules...)
	case "-D":
		spec := strings.Join(rest, " ")
		for i, r := range rules {
			if r == spec {
				t[chain] = append(rules[:i:i], rules[i+1:]...)
				return nil, nil
			}
		}
		return []byte("Bad rule"), fmt.Errorf("exit status 1")
	case "-S":
		if f.failS {
			return []byte("Another app is currently holding the xtables lock."), fmt.Errorf("exit status 4")
		}
		if !exists {
			return []byte("No chain/target/match by that name."), fmt.Errorf("exit status 1")
		}
		var b strings.Builder
		fmt.Fprintf(&b, "-P %s ACCEPT\n", chain)
		for _, r := range rules {
			fmt.Fprintf(&b, "-A %s %s\n", chain, r)
		}
		return []byte(b.String()), nil
	default:
		return nil, fmt.Errorf("fake iptables: unsupported %v", args)
	}
	return nil, nil
}

// setup points the package at a fake host: server.conf with the given
// directives, an rc.local, the instance registry, and fakeIPT.
func setup(t *testing.T, conf string, insts []ovpninstance.Instance) *fakeIPT {
	t.Helper()
	dir := t.TempDir()
	f := newFakeIPT()
	old := []any{serverConf, rcLocal, iptables, allInstances, dropInPath, daemonReload}
	serverConf = filepath.Join(dir, "server.conf")
	rcLocal = filepath.Join(dir, "rc.local")
	dropInPath = filepath.Join(dir, "hexplus-openvpn.service.d", "hexplus-ovpnspread.conf")
	reloads = 0
	daemonReload = func() error { reloads++; return nil }
	iptables = f.run
	allInstances = func() ([]ovpninstance.Instance, error) { return insts, nil }
	t.Cleanup(func() {
		serverConf, rcLocal = old[0].(string), old[1].(string)
		iptables = old[2].(func(...string) ([]byte, error))
		allInstances = old[3].(func() ([]ovpninstance.Instance, error))
		dropInPath, daemonReload = old[4].(string), old[5].(func() error)
	})
	if err := os.WriteFile(serverConf, []byte(conf), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rcLocal, []byte("#!/bin/sh -e\nexit 0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return f
}

var reloads int

var fourProcs = []ovpninstance.Instance{
	{ID: 2, Port: 8443, Proto: "tcp"}, // the operator's extra port: not a worker
	{ID: 3, Port: 21195, Proto: "tcp", Worker: true},
	{ID: 4, Port: 21196, Proto: "tcp", Worker: true},
	{ID: 5, Port: 21197, Proto: "tcp", Worker: true},
}

func TestApplyBuildsRoundRobinRules(t *testing.T) {
	f := setup(t, "port 443\nproto tcp\n", fourProcs)
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	nat := f.tables["nat"]
	// One in 4, one in 3 of the rest, one in 2 of the rest: each of the
	// four processes gets every fourth new connection.
	want := []string{
		"-p tcp -m statistic --mode nth --every 4 --packet 0 -j REDIRECT --to-ports 21195",
		"-p tcp -m statistic --mode nth --every 3 --packet 0 -j REDIRECT --to-ports 21196",
		"-p tcp -m statistic --mode nth --every 2 --packet 0 -j REDIRECT --to-ports 21197",
	}
	if !reflect.DeepEqual(nat[NATChain], want) {
		t.Errorf("%s =\n%s\nwant\n%s", NATChain, strings.Join(nat[NATChain], "\n"), strings.Join(want, "\n"))
	}
	// addrtype LOCAL: forwarded traffic (VPN clients browsing HTTPS when the
	// primary is 443) must not be redirected into OpenVPN.
	if got := nat["PREROUTING"][0]; got != "-p tcp -m addrtype --dst-type LOCAL --dport 443 -j "+NATChain {
		t.Errorf("PREROUTING[0] = %q", got)
	}
	if got := nat["OUTPUT"][0]; got != "-o lo -d 127.0.0.0/8 -p tcp --dport 443 -j "+NATChain {
		t.Errorf("OUTPUT[0] = %q (SSL TUNNEL and SSLH forward to 127.0.0.1:<port>)", got)
	}
	if nat["PREROUTING"][1] != "-j DOCKER" {
		t.Error("an existing PREROUTING rule was lost")
	}
	in := f.tables["filter"][InputChain]
	if len(in) != 6 || in[0] != "-p tcp --dport 21195 -m conntrack --ctstate DNAT -j ACCEPT" || in[1] != "! -i lo -p tcp --dport 21195 -j DROP" {
		t.Errorf("%s = %q", InputChain, in)
	}
	for _, r := range in {
		if strings.Contains(r, "8443") {
			t.Error("the operator's extra port must not be closed")
		}
	}
	if f.tables["filter"]["INPUT"][0] != "-j "+InputChain {
		t.Errorf("INPUT[0] = %q", f.tables["filter"]["INPUT"][0])
	}
	if b, _ := os.ReadFile(rcLocal); !strings.Contains(string(b), RCLocalLine()+"\nexit 0") {
		t.Errorf("rc.local does not restore the rules at boot:\n%s", b)
	}
}

// Applying again (boot, port change, re-run) must not stack rules, and a
// new primary port must replace the old jumps, or connections to the old
// port would still be redirected.
func TestApplyIsIdempotentAndFollowsThePort(t *testing.T) {
	f := setup(t, "port 443\nproto tcp\n", fourProcs)
	for i := 0; i < 3; i++ {
		if err := Apply(); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(serverConf, []byte("port 1194\nproto tcp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	nat := f.tables["nat"]
	if !reflect.DeepEqual(nat["PREROUTING"], []string{"-p tcp -m addrtype --dst-type LOCAL --dport 1194 -j " + NATChain, "-j DOCKER"}) {
		t.Errorf("PREROUTING = %q", nat["PREROUTING"])
	}
	if len(nat["OUTPUT"]) != 1 || !strings.Contains(nat["OUTPUT"][0], "--dport 1194 ") {
		t.Errorf("OUTPUT = %q", nat["OUTPUT"])
	}
	if len(nat[NATChain]) != 3 || len(f.tables["filter"][InputChain]) != 6 {
		t.Errorf("rules stacked: nat %d, input %d", len(nat[NATChain]), len(f.tables["filter"][InputChain]))
	}
	if n := strings.Count(strings.Join(f.tables["filter"]["INPUT"], "\n"), InputChain); n != 1 {
		t.Errorf("%d INPUT jumps", n)
	}
	if b, _ := os.ReadFile(rcLocal); strings.Count(string(b), "ovpnspread apply") != 1 {
		t.Errorf("rc.local:\n%s", b)
	}
}

func TestTeardownRemovesEverythingOfOurs(t *testing.T) {
	f := setup(t, "port 443\nproto udp\n", []ovpninstance.Instance{{ID: 3, Port: 21195, Proto: "udp", Worker: true}})
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	if err := Teardown(); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.tables["nat"][NATChain]; ok {
		t.Error("nat chain still exists")
	}
	if _, ok := f.tables["filter"][InputChain]; ok {
		t.Error("input chain still exists")
	}
	if !reflect.DeepEqual(f.tables["nat"]["PREROUTING"], []string{"-j DOCKER"}) || len(f.tables["nat"]["OUTPUT"]) != 0 ||
		!reflect.DeepEqual(f.tables["filter"]["INPUT"], []string{"-p tcp --dport 443 -j ACCEPT"}) {
		t.Errorf("leftovers: %v", f.tables)
	}
	if b, _ := os.ReadFile(rcLocal); strings.Contains(string(b), "ovpnspread") {
		t.Errorf("rc.local still restores the rules:\n%s", b)
	}
	// Nothing there: still fine.
	if err := Teardown(); err != nil {
		t.Errorf("second teardown: %v", err)
	}
}

// With no workers (feature off, or the last one removed) Apply cleans up
// instead of redirecting into nothing.
func TestApplyWithoutWorkersTearsDown(t *testing.T) {
	f := setup(t, "port 443\nproto tcp\n", fourProcs)
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	allInstances = func() ([]ovpninstance.Instance, error) { return fourProcs[:1], nil }
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.tables["nat"][NATChain]; ok || len(f.tables["nat"]["PREROUTING"]) != 1 {
		t.Errorf("rules left behind: %v", f.tables["nat"])
	}
}

func TestApplyRefusesMixedProtocols(t *testing.T) {
	f := setup(t, "port 443\nproto udp\n", fourProcs) // workers are tcp
	if err := Apply(); err == nil {
		t.Fatal("tcp workers behind a udp primary must be refused")
	}
	if _, ok := f.tables["nat"][NATChain]; ok {
		t.Error("rules were written anyway")
	}
}

func TestPrimary(t *testing.T) {
	cases := []struct {
		conf, proto string
		port        int
	}{
		{"port 443\nproto tcp\n", "tcp", 443},
		{"proto tcp6\nport 8080\n", "tcp", 8080},
		{"proto udp6\n", "udp", 1194},
		{"# nothing\n", "udp", 1194}, // OpenVPN's defaults
		{"port abc\nproto tcp-server\n", "tcp", 1194},
	}
	for _, c := range cases {
		setup(t, c.conf, nil)
		proto, port, err := Primary()
		if err != nil || proto != c.proto || port != c.port {
			t.Errorf("%q: got %s %d %v, want %s %d", c.conf, proto, port, err, c.proto, c.port)
		}
	}
	setup(t, "", nil)
	_ = os.Remove(serverConf)
	if _, _, err := Primary(); err == nil {
		t.Error("a missing server.conf must be an error, not a guess")
	}
}

func TestPickPorts(t *testing.T) {
	old := portFree
	t.Cleanup(func() { portFree = old })
	busy := map[int]bool{WorkerPortBase + 1: true}
	portFree = func(p int, _ string) bool { return !busy[p] }
	list := []ovpninstance.Instance{{ID: 2, Port: WorkerPortBase + 2, Proto: "udp"}} // stopped: no socket, still owned
	got, err := pickPorts(3, "udp", WorkerPortBase+3, list)
	if err != nil {
		t.Fatal(err)
	}
	if want := []int{WorkerPortBase, WorkerPortBase + 4, WorkerPortBase + 5}; !reflect.DeepEqual(got, want) {
		t.Errorf("pickPorts = %v, want %v", got, want)
	}
}

// The primary wants the workers (drop-in) and the workers are PartOf it, so
// a stop, start or restart of OpenVPN from any menu or CLI path covers all.
func TestApplyWritesThePrimaryDropIn(t *testing.T) {
	setup(t, "port 443\nproto tcp\n", fourProcs)
	for i := 0; i < 2; i++ {
		if err := Apply(); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(dropInPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), "[Unit]\nWants=hexplus-openvpn3.service hexplus-openvpn4.service hexplus-openvpn5.service\n") {
		t.Errorf("drop-in:\n%s", b)
	}
	if reloads != 1 {
		t.Errorf("daemon-reload ran %d times, want once (unchanged drop-in)", reloads)
	}
	if err := Teardown(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dropInPath); !os.IsNotExist(err) {
		t.Error("drop-in left after teardown")
	}
	if reloads != 2 {
		t.Errorf("daemon-reload after removing the drop-in: %d", reloads)
	}
	for _, w := range fourProcs {
		if got := w.Service().PartOf; w.Worker != (len(got) == 1 && got[0] == ovpninstance.PrimaryUnit) {
			t.Errorf("instance #%d (worker %v): PartOf = %v", w.ID, w.Worker, got)
		}
	}
}

// If the rules cannot be removed, the workers must stay: removing them
// would leave connections redirected to ports nobody listens on.
func TestDisableKeepsWorkersWhenTeardownFails(t *testing.T) {
	f := setup(t, "port 443\nproto tcp\n", fourProcs)
	if err := Apply(); err != nil {
		t.Fatal(err)
	}
	f.failS = true
	listed := 0
	allInstances = func() ([]ovpninstance.Instance, error) { listed++; return fourProcs, nil }
	if err := Disable(func(string, ...any) {}); err == nil {
		t.Fatal("Disable reported success although the rules are still there")
	}
	if listed != 0 {
		t.Error("Disable went on to the workers after the teardown failed")
	}
}
