package ovpnguard

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func names(kills []Kill) []string {
	var out []string
	for _, k := range kills {
		out = append(out, k.Name+"@"+k.Instance+"#"+itoa(k.CID))
	}
	return out
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}

func TestDecideKeepsNewestUpToTheLimit(t *testing.T) {
	sessions := []Session{
		{Instance: "main", CID: 1, Name: "bob", Since: 100},
		{Instance: "main", CID: 2, Name: "bob", Since: 300}, // newest
		{Instance: "2", CID: 7, Name: "bob", Since: 200},    // another port counts too
		{Instance: "main", CID: 3, Name: "alice", Since: 50},
		{Instance: "main", CID: 4, Name: "carol", Since: 10},
		{Instance: "main", CID: 5, Name: "carol", Since: 20},
	}
	limits := map[string]int{"bob": 2, "alice": 1, "carol": 0}
	got := Decide(sessions, Policy{Limit: func(n string) int { return limits[n] }})
	// bob: keep 300 and 200, kill the oldest (100). alice: within limit.
	// carol: 0 means no limit.
	if want := []string{"bob@main#1"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("kills = %v, want %v", names(got), want)
	}
	if got[0].Reason == "" {
		t.Error("a kill without a reason")
	}
}

func TestDecideTieBreaksOnClientID(t *testing.T) {
	sessions := []Session{
		{Instance: "main", CID: 8, Name: "bob", Since: 100},
		{Instance: "main", CID: 9, Name: "bob", Since: 100},
	}
	got := Decide(sessions, Policy{Limit: func(string) int { return 1 }})
	// Same second: the higher client id connected later and stays.
	if want := []string{"bob@main#8"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("kills = %v, want %v", names(got), want)
	}
}

func TestDecideRefusedAccountsLoseEverySession(t *testing.T) {
	sessions := []Session{
		{Instance: "main", CID: 1, Name: "gone", Since: 100},
		{Instance: "2", CID: 2, Name: "gone", Since: 200},
		{Instance: "main", CID: 3, Name: "ok", Since: 100},
	}
	got := Decide(sessions, Policy{
		Limit:   func(string) int { return 5 },
		Refused: func(n string) string { return map[string]string{"gone": "deleted"}[n] },
	})
	if want := []string{"gone@2#2", "gone@main#1"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("kills = %v, want %v", names(got), want)
	}
	for _, k := range got {
		if k.Reason != "deleted" {
			t.Errorf("reason = %q", k.Reason)
		}
	}
}

func TestDecideNothingToDo(t *testing.T) {
	if got := Decide(nil, Policy{}); len(got) != 0 {
		t.Errorf("no sessions: %v", got)
	}
	s := []Session{{Instance: "main", CID: 1, Name: "", Since: 1}}
	if got := Decide(s, Policy{Limit: func(string) int { return 1 }}); len(got) != 0 {
		t.Errorf("nameless session must be ignored: %v", got)
	}
}

func TestParseStatus(t *testing.T) {
	lines := []string{
		"TITLE,OpenVPN 2.5.9",
		"HEADER,CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since,Connected Since (time_t),Username,Client ID,Peer ID,Data Channel Cipher",
		"CLIENT_LIST,bob,172.27.0.3:60670,10.8.0.2,,3870,3446,2026-10-05 01:28:20,1759627700,bob,0,0,AES-256-GCM",
		"CLIENT_LIST,carl,1.2.3.4:5,10.8.0.3,,1,1,2026-10-05 01:28:21,1759627701,UNDEF,4,1,AES-256-GCM",
		"CLIENT_LIST,bad,1.2.3.4:5,10.8.0.4,,1,1,x,1,bad,notanumber,1,AES",
		"CLIENT_LIST,short,1.2.3.4:5",
		"ROUTING_TABLE,10.8.0.2,bob,172.27.0.3:60670,2026-10-05 01:28:20,1759627700",
		"GLOBAL_STATS,Max bcast/mcast queue length,0",
	}
	got := parseStatus("main", lines)
	// carl's row has not authenticated yet (UNDEF): it is nobody's session.
	want := []Session{
		{Instance: "main", CID: 0, Name: "bob", Since: 1759627700, Real: "172.27.0.3:60670", RxBytes: 3870},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("parseStatus:\n got %+v\nwant %+v", got, want)
	}
}

func TestRefusal(t *testing.T) {
	const today = 20366
	shadow := parseShadow("" +
		"alive:$6$s$h:20000:0:99999:7:::\n" +
		"never:$6$s$h:20000:0:99999:7::-1:\n" +
		"tomorrow:$6$s$h:20000:0:99999:7::20367:\n" +
		"today:$6$s$h:20000:0:99999:7::20366:\n" +
		"yesterday:$6$s$h:20000:0:99999:7::20365:\n" +
		"locked:!$6$s$h:20000:0:99999:7:::\n" +
		"star:*:20000:0:99999:7:::\n" +
		"empty::20000:0:99999:7:::\n" +
		"weird:$6$s$h:20000:0:99999:7::abc:\n" +
		"short:$6$s$h\r\n")
	cases := map[string]bool{ // name -> refused
		"alive": false, "never": false, "tomorrow": false, "weird": false, "short": false,
		"today": true, "yesterday": true, "locked": true, "star": true, "empty": true, "deleted": true,
	}
	for name, want := range cases {
		if got := refusal(name, shadow, today) != ""; got != want {
			t.Errorf("%s: refused = %v, want %v", name, got, want)
		}
	}
	// No shadow data: never refuse (ending sessions on doubt is worse).
	if r := refusal("deleted", nil, today); r != "" {
		t.Errorf("nil shadow refused: %q", r)
	}
}

func TestLimitFor(t *testing.T) {
	// A users.json row with Limit 0 (the menu's change-expiry creates one for
	// a v1-only user) must not hide that user's v1 limit.
	v2 := map[string]int{"a": 2, "zero": 0, "cli": 0}
	v1 := map[string]int{"a": 9, "old": 3, "zero": 4}
	cases := map[string]int{"a": 2, "zero": 4, "old": 3, "cli": 0, "nobody": 0}
	for name, want := range cases {
		if got := limitFor(name, v2, v1); got != want {
			t.Errorf("limitFor(%s) = %d, want %d", name, got, want)
		}
	}
}

func TestInjectAndStripConf(t *testing.T) {
	ours := confLines("2")
	base := "port 443\nmanagement 127.0.0.1 7505\n"
	once := injectLines(base, ours)
	if injectLines(once, ours) != once {
		t.Error("injectLines is not idempotent")
	}
	for _, l := range ours {
		if !contains(once, l) {
			t.Errorf("missing %q in\n%s", l, once)
		}
	}
	back := stripLines(once, ours)
	if !contains(back, "management 127.0.0.1 7505") {
		t.Error("stripped the operator's own management line")
	}
	for _, l := range ours {
		if contains(back, l) {
			t.Errorf("left %q behind", l)
		}
	}
}

func contains(s, line string) bool {
	for _, l := range splitLines(s) {
		if l == line {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == '\n' {
			out = append(out, cur)
			cur = ""
			continue
		}
		cur += string(r)
	}
	return append(out, cur)
}

func TestInstanceOfConf(t *testing.T) {
	cases := map[string]string{
		"/etc/openvpn/server.conf":   "main",
		"/etc/openvpn/server2.conf":  "2",
		"/etc/openvpn/server12.conf": "12",
	}
	for path, want := range cases {
		if got, ok := InstanceOfConf(path); !ok || got != want {
			t.Errorf("%s -> %q %v, want %q", path, got, ok, want)
		}
	}
	for _, bad := range []string{"/etc/openvpn/client.conf", "/etc/openvpn/server.conf.bak", "/etc/openvpn/serverx.conf"} {
		if _, ok := InstanceOfConf(bad); ok {
			t.Errorf("%s accepted", bad)
		}
	}
	if SocketPath("main") != RunDir+"/ovpn-main.sock" {
		t.Errorf("SocketPath = %s", SocketPath("main"))
	}
}

// A device that roamed leaves its old session behind until OpenVPN's
// keepalive timeout. That dead session must not cost a live device its
// place: here the oldest session (B) is live and must stay.
func TestDecideKillsStaleBeforeLive(t *testing.T) {
	sessions := []Session{
		{Instance: "main", CID: 1, Name: "bob", Since: 100},              // device B, live, oldest
		{Instance: "main", CID: 2, Name: "bob", Since: 200, Stale: true}, // device A before it roamed
		{Instance: "main", CID: 3, Name: "bob", Since: 300},              // device A again
	}
	got := Decide(sessions, Policy{Limit: func(string) int { return 2 }})
	if want := []string{"bob@main#2"}; !reflect.DeepEqual(names(got), want) {
		t.Errorf("kills = %v, want %v (the dead session, not live device B)", names(got), want)
	}
}

func TestActivityMarksSessionsWhoseCounterStopped(t *testing.T) {
	t0 := time.Unix(1_000_000, 0)
	a := &activity{}
	live := Session{Instance: "main", CID: 1, Name: "bob", RxBytes: 100}
	dead := Session{Instance: "main", CID: 2, Name: "bob", RxBytes: 500}

	tick := func(at time.Time, s ...Session) []Session {
		a.mark(s, at)
		return s
	}
	got := tick(t0, live, dead)
	if got[0].Stale || got[1].Stale {
		t.Fatal("a session seen for the first time must count as live")
	}
	live.RxBytes = 200 // keepalives keep arriving
	got = tick(t0.Add(30*time.Second), live, dead)
	if got[0].Stale || got[1].Stale {
		t.Fatal("30 s without traffic is not yet stale")
	}
	live.RxBytes = 300
	got = tick(t0.Add(60*time.Second), live, dead)
	if got[0].Stale {
		t.Error("a session whose counter moves was marked stale")
	}
	if !got[1].Stale {
		t.Error("a session with no traffic for 60 s was not marked stale")
	}
	// A restarted instance reuses client ids; a different counter is a new session.
	reused := Session{Instance: "main", CID: 2, Name: "carl", RxBytes: 42}
	if got = tick(t0.Add(75*time.Second), reused); got[0].Stale {
		t.Error("a reused client id inherited the old session's staleness")
	}
}

// The fail-safe: when the guard cannot read its inputs it must enforce
// less, never more.
func TestLoadPolicyIsLenientWhenInputsAreMissing(t *testing.T) {
	oldShadow, oldV1, oldConf := shadowPath, v1DBPath, primaryConf
	t.Cleanup(func() { shadowPath, v1DBPath, primaryConf = oldShadow, oldV1, oldConf })
	dir := t.TempDir()
	shadowPath = filepath.Join(dir, "no-shadow")
	v1DBPath = filepath.Join(dir, "no-usuarios.db")
	primaryConf = filepath.Join(dir, "no-server.conf") // unreadable: MULTILOGIN unknown, no cap

	var logs []string
	p := loadPolicy(func(f string, a ...any) { logs = append(logs, f) })
	if r := p.Refused("anyone"); r != "" {
		t.Errorf("unreadable shadow refused a user: %q", r)
	}
	if l := p.Limit("anyone"); l != 0 {
		t.Errorf("no limit data gave limit %d", l)
	}
	if len(logs) == 0 {
		t.Error("an unreadable /etc/shadow was not logged")
	}

	if err := os.WriteFile(v1DBPath, []byte("bob 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if l := loadPolicy(func(string, ...any) {}).Limit("bob"); l != 2 {
		t.Errorf("v1 limit = %d, want 2", l)
	}
}

// MULTILOGIN off: OpenVPN refuses a second session only inside one
// process, so with extra ports or CPU spreading the guard has to hold every
// user to one session across all of them.
func TestLoadPolicyCapsAtOneWhenMultiloginIsOff(t *testing.T) {
	oldShadow, oldV1, oldConf := shadowPath, v1DBPath, primaryConf
	t.Cleanup(func() { shadowPath, v1DBPath, primaryConf = oldShadow, oldV1, oldConf })
	dir := t.TempDir()
	shadowPath = filepath.Join(dir, "no-shadow")
	v1DBPath = filepath.Join(dir, "usuarios.db")
	primaryConf = filepath.Join(dir, "server.conf")
	if err := os.WriteFile(v1DBPath, []byte("bob 3\nann 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	limits := func(conf string) [3]int {
		if err := os.WriteFile(primaryConf, []byte(conf), 0o644); err != nil {
			t.Fatal(err)
		}
		p := loadPolicy(func(string, ...any) {})
		return [3]int{p.Limit("bob"), p.Limit("ann"), p.Limit("nolimit")}
	}
	if got := limits("port 443\nproto tcp\n"); got != [3]int{1, 1, 1} {
		t.Errorf("MULTILOGIN off: limits = %v, want every user at 1", got)
	}
	if got := limits("port 443\nduplicate-cn\n"); got != [3]int{3, 1, 0} {
		t.Errorf("MULTILOGIN on: limits = %v, want the users' own limits", got)
	}
}

func TestForeignManagementIsRefused(t *testing.T) {
	ours := confLines("main")
	if f := foreignManagement("port 1194\n"+ours[0]+"\n"+ours[1]+"\n", ours); f != "" {
		t.Errorf("our own lines reported as foreign: %q", f)
	}
	for _, conf := range []string{
		"port 1194\nmanagement 127.0.0.1 7505 /etc/openvpn/mgmt.pw\n",
		"management-hold\n",
		"  management-client-auth\n",
	} {
		if foreignManagement(conf, ours) == "" {
			t.Errorf("not refused: %q", conf)
		}
	}
	if foreignManagement("# management 127.0.0.1 7505\n", ours) != "" {
		t.Error("a comment was treated as a directive")
	}
}

// v1 limits live in /root/usuarios.db. The shared unit template sets
// ProtectHome=true unless AllowHome is set, which hid /root from the guard:
// it then enforced nothing for test users and v1 users, while a manual run
// from a root shell looked fine.
func TestServiceCanReadRootHome(t *testing.T) {
	if !Service().AllowHome {
		t.Error("the guard unit must not set ProtectHome: it reads /root/usuarios.db")
	}
}
