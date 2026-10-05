package ovpnguard

import (
	"context"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/user"
)

// Interval is how often the guard checks: a session over the limit, or of a
// refused account, lives at most about this long. Sessions are read from the
// management sockets, not from the status log files.
const Interval = 15 * time.Second

// staleAfter: hexplus clients send a keepalive ping every 10 s (keepalive
// 10 120), so a session whose received-bytes counter has not moved for this
// long has lost its device.
const staleAfter = 45 * time.Second

// Variables so tests can point them elsewhere.
var (
	shadowPath = "/etc/shadow"
	v1DBPath   = "/root/usuarios.db"
	now        = time.Now
	// primaryConf carries the MULTILOGIN switch (duplicate-cn).
	primaryConf = "/etc/openvpn/server.conf"
)

// sockets returns instance name -> management socket path for every socket
// present in dir.
func sockets(dir string) map[string]string {
	out := map[string]string{}
	matches, _ := filepath.Glob(filepath.Join(dir, "ovpn-*.sock"))
	for _, m := range matches {
		inst := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(m), "ovpn-"), ".sock")
		if inst != "" {
			out[inst] = m
		}
	}
	return out
}

// pass is one look at the instances: one management connection per
// instance, used for `status 2` and then for every kill on that instance.
// Using the same connection means a kill can never reach an instance that
// restarted after the status was read (client ids start again at 0 after a
// restart, so a fresh connection could end a stranger's session).
type pass struct {
	conns    map[string]*mgmtConn
	sessions []Session
}

func open(socks map[string]string, logf func(string, ...any)) *pass {
	p := &pass{conns: map[string]*mgmtConn{}}
	for inst, path := range socks {
		m, err := dialMgmt(path)
		if err != nil {
			logf("ovpnguard: instance %s: %v", inst, err)
			continue
		}
		lines, err := m.status()
		if err != nil {
			logf("ovpnguard: instance %s: status: %v", inst, err)
			m.Close()
			continue
		}
		p.conns[inst] = m
		p.sessions = append(p.sessions, parseStatus(inst, lines)...)
	}
	return p
}

func (p *pass) close() {
	for _, m := range p.conns {
		m.Close()
	}
}

func (p *pass) kill(kills []Kill, logf func(string, ...any)) int {
	done := 0
	for _, k := range kills {
		m, ok := p.conns[k.Instance]
		if !ok {
			continue
		}
		if err := m.clientKill(k.CID); err != nil {
			logf("ovpnguard: kick %s (instance %s, cid %d): %v", k.Name, k.Instance, k.CID, err)
			continue
		}
		logf("ovpnguard: kicked %s (instance %s, cid %d, from %s): %s", k.Name, k.Instance, k.CID, k.Real, k.Reason)
		done++
	}
	return done
}

// activity remembers when each session's received-bytes counter last moved,
// across passes of the Run loop.
type activity struct {
	seen map[string]seenAt
}

type seenAt struct {
	rx      uint64
	changed time.Time
}

// mark sets Stale on sessions whose counter has not moved for staleAfter.
// A session seen for the first time is live.
func (a *activity) mark(sessions []Session, t time.Time) {
	if a.seen == nil {
		a.seen = map[string]seenAt{}
	}
	next := make(map[string]seenAt, len(sessions))
	for i, s := range sessions {
		key := s.Instance + "/" + strconv.FormatUint(s.CID, 10)
		prev, ok := a.seen[key]
		if ok && prev.rx == s.RxBytes {
			next[key] = prev
			if t.Sub(prev.changed) >= staleAfter {
				sessions[i].Stale = true
			}
			continue
		}
		next[key] = seenAt{rx: s.RxBytes, changed: t}
	}
	a.seen = next
}

// enforce runs one pass and returns how many sessions it ended. act may be
// nil (no liveness history: every session counts as live).
func enforce(socks map[string]string, pol Policy, act *activity, logf func(string, ...any)) int {
	unlock := lockRunDir(logf)
	defer unlock()
	p := open(socks, logf)
	defer p.close()
	if act != nil {
		act.mark(p.sessions, now())
	}
	if len(p.sessions) == 0 {
		return 0
	}
	return p.kill(Decide(p.sessions, pol), logf)
}

// Once runs a single check and returns how many sessions it ended. Without
// history it cannot tell a dead session from an idle one, so it treats all
// as live; the Run loop does not have that blind spot.
func Once(logf func(string, ...any)) int {
	socks := sockets(RunDir)
	if len(socks) == 0 {
		return 0
	}
	return enforce(socks, loadPolicy(logf), nil, logf)
}

// Run checks every Interval until ctx ends.
func Run(ctx context.Context) error {
	logf := log.Printf
	act := &activity{}
	t := time.NewTicker(Interval)
	defer t.Stop()
	for {
		if socks := sockets(RunDir); len(socks) > 0 {
			enforce(socks, loadPolicy(logf), act, logf)
		}
		select {
		case <-ctx.Done():
			return nil
		case <-t.C:
		}
	}
}

// Kick ends every session of name on every instance, for when an account is
// deleted or expired from the menu. It returns how many sessions it ended.
func Kick(name string, logf func(string, ...any)) int {
	return kickIn(sockets(RunDir), name, logf)
}

func kickIn(socks map[string]string, name string, logf func(string, ...any)) int {
	unlock := lockRunDir(logf)
	defer unlock()
	p := open(socks, logf)
	defer p.close()
	var mine []Kill
	for _, s := range p.sessions {
		if s.Name == name {
			mine = append(mine, Kill{Session: s, Reason: "สั่งตัดจากเมนู"})
		}
	}
	return p.kill(mine, logf)
}

// loadPolicy builds the account view. Anything it cannot read makes it more
// lenient, never stricter: an unreadable /etc/shadow refuses nobody, an
// unreadable user DB caps nobody.
func loadPolicy(logf func(string, ...any)) Policy {
	v2 := map[string]int{}
	if db, err := user.Load(); err == nil {
		for name, rec := range db.Users {
			v2[name] = rec.Limit
		}
	} else {
		logf("ovpnguard: user DB: %v (no limits from it)", err)
	}
	v1, err := readV1Limits(v1DBPath)
	if err != nil {
		logf("ovpnguard: %v (no v1 limits)", err)
	}

	var shadow map[string][2]string
	if raw, err := os.ReadFile(shadowPath); err == nil {
		shadow = parseShadow(string(raw))
	} else {
		logf("ovpnguard: %v (expired and deleted accounts are not checked)", err)
	}
	today := now().Unix() / 86400

	// MULTILOGIN off (no duplicate-cn) means one session per user. OpenVPN
	// enforces that only inside one process, so with extra ports or CPU
	// spreading a user could hold one session per process; the guard holds
	// it to one across all of them. Unreadable config: no cap.
	single := false
	if raw, err := os.ReadFile(primaryConf); err == nil {
		single = !hasDuplicateCN(string(raw))
	}
	return Policy{
		Limit: func(name string) int {
			l := limitFor(name, v2, v1)
			if single && l != 1 {
				return 1
			}
			return l
		},
		Refused: func(name string) string { return refusal(name, shadow, today) },
	}
}

// limitFor: the first positive limit wins, users.json before v1's
// usuarios.db; no positive limit means none. A 0 in users.json does not
// hide a v1 limit, because the menu's "change expiry" creates a users.json
// row with Limit 0 for users that only existed in usuarios.db.
func limitFor(name string, v2, v1 map[string]int) int {
	if l := v2[name]; l > 0 {
		return l
	}
	if l := v1[name]; l > 0 {
		return l
	}
	return 0
}

// readV1Limits parses usuarios.db ("name limit" per line). A missing file
// is not an error.
func readV1Limits(path string) (map[string]int, error) {
	out := map[string]int{}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, err
	}
	for _, l := range strings.Split(string(raw), "\n") {
		f := strings.Fields(l)
		if len(f) >= 2 {
			if n, err := strconv.Atoi(f[1]); err == nil {
				out[f[0]] = n
			}
		}
	}
	return out, nil
}

// parseShadow maps user -> {hash, expiry field}.
func parseShadow(content string) map[string][2]string {
	out := map[string][2]string{}
	for _, l := range strings.Split(content, "\n") {
		f := strings.Split(strings.TrimRight(l, "\r"), ":")
		if len(f) < 2 || f[0] == "" {
			continue
		}
		exp := ""
		if len(f) >= 8 {
			exp = f[7]
		}
		out[f[0]] = [2]string{f[1], exp}
	}
	return out
}

// refusal explains why name may have no session, or returns "". With no
// shadow data it refuses nobody. The expiry rule matches hexplus-auth.sh and
// pam_unix (expired once today reaches the expiry day); a malformed expiry
// is left alone here, because ending sessions on doubt is worse than letting
// the next login be rejected by the auth script.
func refusal(name string, shadow map[string][2]string, today int64) string {
	if shadow == nil {
		return ""
	}
	e, ok := shadow[name]
	if !ok {
		return "บัญชีถูกลบแล้ว"
	}
	if e[0] == "" || strings.HasPrefix(e[0], "!") || strings.HasPrefix(e[0], "*") {
		return "บัญชีถูกล็อก"
	}
	if e[1] == "" || e[1] == "-1" {
		return ""
	}
	exp, err := strconv.ParseInt(e[1], 10, 64)
	if err != nil {
		return ""
	}
	if today >= exp {
		return "บัญชีหมดอายุ"
	}
	return ""
}

func hasDuplicateCN(conf string) bool {
	for _, l := range strings.Split(conf, "\n") {
		if strings.TrimSpace(l) == "duplicate-cn" {
			return true
		}
	}
	return false
}
