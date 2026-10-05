// Package ovpnguard enforces per-user OpenVPN session limits and removes
// sessions of accounts that were deleted, expired or locked.
//
// OpenVPN itself only knows "one session per user" (no duplicate-cn) or
// "unlimited" (duplicate-cn). hexplus stores a per-user device count, so a
// small loop compares the live sessions of every instance with those limits
// and kills the surplus through OpenVPN's management interface.
//
// The management interface is used only to read status and kill sessions,
// never to authenticate (no management-client-auth): with client-auth every
// TLS renegotiation stalls the client's data channel for 13-16 s, while a
// plain management socket was measured to cause no interruption at all.
package ovpnguard

import (
	"sort"
	"strconv"
	"strings"
)

// Session is one connected client as OpenVPN's `status 2` reports it.
type Session struct {
	Instance string // "main" for server.conf, the instance id for server<N>.conf
	CID      uint64 // management client id, what client-kill takes
	Name     string // the authenticated username
	Since    int64  // connected since, unix seconds
	Real     string // client address as OpenVPN sees it
	RxBytes  uint64 // bytes received from the client so far
	// Stale marks a session that has received nothing for a while: the
	// client's keepalive pings stopped, so the device is gone (it roamed to
	// another network and reconnected, or lost power) and OpenVPN has not
	// timed the session out yet.
	Stale bool
}

// Kill is a session the guard ends, and why.
type Kill struct {
	Session
	Reason string
}

// Policy supplies what the guard knows about accounts.
type Policy struct {
	// Limit returns the device limit for name; 0 means no limit.
	Limit func(name string) int
	// Refused returns a non-empty reason when name may not have any
	// session at all (deleted, expired, locked).
	Refused func(name string) string
}

// Decide returns the sessions to kill, grouped by user in name order.
//
// When a user is over the limit, live sessions are kept before stale ones,
// and among those the newest stay and the oldest go. Stale first, because a
// device that roamed leaves a dead session behind until OpenVPN's keepalive
// timeout, and counting it would cut one of the user's devices that is still
// in use. Newest next, because killing the newest would lock out a device
// that just reconnected.
func Decide(sessions []Session, p Policy) []Kill {
	byUser := map[string][]Session{}
	for _, s := range sessions {
		if s.Name == "" {
			continue
		}
		byUser[s.Name] = append(byUser[s.Name], s)
	}
	names := make([]string, 0, len(byUser))
	for n := range byUser {
		names = append(names, n)
	}
	sort.Strings(names)

	var kills []Kill
	for _, name := range names {
		ss := byUser[name]
		sort.Slice(ss, func(i, j int) bool { // live before stale, then newest first
			if ss[i].Stale != ss[j].Stale {
				return !ss[i].Stale
			}
			if ss[i].Since != ss[j].Since {
				return ss[i].Since > ss[j].Since
			}
			if ss[i].Instance != ss[j].Instance {
				return ss[i].Instance < ss[j].Instance
			}
			return ss[i].CID > ss[j].CID
		})
		if p.Refused != nil {
			if reason := p.Refused(name); reason != "" {
				for _, s := range ss {
					kills = append(kills, Kill{Session: s, Reason: reason})
				}
				continue
			}
		}
		limit := 0
		if p.Limit != nil {
			limit = p.Limit(name)
		}
		if limit <= 0 || len(ss) <= limit {
			continue
		}
		reason := "เกินจำนวนอุปกรณ์ " + strconv.Itoa(len(ss)) + "/" + strconv.Itoa(limit)
		for _, s := range ss[limit:] {
			kills = append(kills, Kill{Session: s, Reason: reason})
		}
	}
	return kills
}

// parseStatus reads the CLIENT_LIST rows of a `status 2` reply (the same
// format as status-version 2 log files):
//
//	CLIENT_LIST,<CN>,<real>,<vip>,<vip6>,<rx>,<tx>,<since>,<since_t>,<username>,<cid>,<peer id>,<cipher>
//
// Only the username column is used. A row whose username is still UNDEF
// has not authenticated yet; it is not a session of anyone (yet).
func parseStatus(instance string, lines []string) []Session {
	var out []Session
	for _, l := range lines {
		if !strings.HasPrefix(l, "CLIENT_LIST,") {
			continue
		}
		f := strings.Split(strings.TrimRight(l, "\r"), ",")
		if len(f) < 11 {
			continue
		}
		cid, err := strconv.ParseUint(f[10], 10, 64)
		if err != nil {
			continue
		}
		since, _ := strconv.ParseInt(f[8], 10, 64)
		rx, _ := strconv.ParseUint(f[5], 10, 64)
		name := f[9]
		if name == "" || name == "UNDEF" {
			continue
		}
		out = append(out, Session{Instance: instance, CID: cid, Name: name, Since: since, Real: f[2], RxBytes: rx})
	}
	return out
}
