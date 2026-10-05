package menu

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

// Fixtures below are byte-for-byte in the shape OpenVPN 2.5.9 emits. The v1
// sample is a real capture from a running server; the v2/v3 samples follow the
// binary's own format strings:
//
//	HEADER%cCLIENT_LIST%cCommon Name%cReal Address%cVirtual Address%c
//	Virtual IPv6 Address%cBytes Received%cBytes Sent%cConnected Since%c
//	Connected Since (time_t)%cUsername%cClient ID%cPeer ID%cData Channel Cipher
//	CLIENT_LIST%c%s%c%s%c%s%c%s%c%llu%c%llu%c%s%c%u%c%s%c%lu%c%u%c%s
//
// where %c is "," for status-version 2 and "\t" for status-version 3.

const statusV1 = `OpenVPN CLIENT LIST
Updated,2026-08-03 17:49:55
Common Name,Real Address,Bytes Received,Bytes Sent,Connected Since
UNDEF,127.0.0.1:50964,0,0,2026-08-03 17:49:44
HexeyNet,127.0.0.1:50062,18415521,200205038,2026-08-03 17:07:22
ROUTING TABLE
Virtual Address,Common Name,Real Address,Last Ref
10.8.0.2,HexeyNet,127.0.0.1:50062,2026-08-03 17:49:52
GLOBAL STATS
Max bcast/mcast queue length,0
END
`

// Timestamps are authored in UTC, matching the server these were captured
// from, and every time_t below is the exact epoch of the date beside it —
// TestClientListFieldOffsets enforces that so the fixture cannot drift.
const statusV2 = `TITLE,OpenVPN 2.5.9 x86_64-pc-linux-musl [SSL (OpenSSL)] [LZO] [LZ4] [EPOLL] [MH/PKTINFO] [AEAD] built on Jun 26 2026
TIME,2026-08-03 17:49:55,1785779395
HEADER,CLIENT_LIST,Common Name,Real Address,Virtual Address,Virtual IPv6 Address,Bytes Received,Bytes Sent,Connected Since,Connected Since (time_t),Username,Client ID,Peer ID,Data Channel Cipher
CLIENT_LIST,HexeyNet,127.0.0.1:50062,10.8.0.2,,18415521,200205038,2026-08-03 17:07:22,1785776842,HexeyNet,0,0,AES-256-GCM
CLIENT_LIST,HexeyNet,127.0.0.1:50999,10.8.0.3,,100,200,2026-08-03 17:40:00,1785778800,HexeyNet,1,1,AES-256-GCM
CLIENT_LIST,somchai,203.0.113.9:41000,10.8.0.4,,10,20,2026-08-03 17:45:00,1785779100,somchai,2,2,AES-256-GCM
HEADER,ROUTING_TABLE,Virtual Address,Common Name,Real Address,Last Ref,Last Ref (time_t)
ROUTING_TABLE,10.8.0.2,HexeyNet,127.0.0.1:50062,2026-08-03 17:49:52,1785779392
GLOBAL_STATS,Max bcast/mcast queue length,0
END
`

const statusV3 = "TITLE\tOpenVPN 2.5.9\n" +
	"TIME\t2026-08-03 17:49:55\t1785779395\n" +
	"HEADER\tCLIENT_LIST\tCommon Name\tReal Address\tVirtual Address\tVirtual IPv6 Address\tBytes Received\tBytes Sent\tConnected Since\tConnected Since (time_t)\tUsername\tClient ID\tPeer ID\tData Channel Cipher\n" +
	"CLIENT_LIST\tHexeyNet\t127.0.0.1:50062\t10.8.0.2\t\t18415521\t200205038\t2026-08-03 17:07:22\t1785776842\tHexeyNet\t0\t0\tAES-256-GCM\n" +
	"GLOBAL_STATS\tMax bcast/mcast queue length\t0\n" +
	"END\n"

// Real `ps -eo etimes=,args=` capture from a production box with six tunnel
// sessions live. Every one of them is PTY-less, which is the whole point: at
// the moment this was taken `who` listed exactly one line — the operator's own
// root pts/0 — so the utmp-based count reported all six users as offline.
const psSSHSessions = `  67485 /usr/local/lib/hexplus/dropbear -F -R -p 110
  67482 sshd: /usr/sbin/sshd -D [listener] 1 of 10-100 startups
  67482 sshd: test2 [priv]
  67481 sshd: root@pts/0
  67479 sshd: test2
  67477 sshd: test2 [priv]
  67475 sshd: test2
  67474 sshd: test2 [priv]
  67473 sshd: test2
  67472 sshd: test2 [priv]
  67471 sshd: test2
  67470 sshd: test2 [priv]
  67469 sshd: test2
  40412 sshd: test2 [priv]
  40409 sshd: test2@notty
    106 sshd: [accepted]
      0 sshd: root@notty
`

// Same host, `ps -eo args=`, with a mid-authentication connection and a root
// login added so the rejection paths are covered.
const psSSHArgs = `/usr/local/lib/hexplus/dropbear -F -R -p 110
sshd: /usr/sbin/sshd -D [listener] 1 of 10-100 startups
sshd: test2 [priv]
sshd: test2
sshd: test2 [priv]
sshd: test2@notty
sshd: somchai [priv]
sshd: somchai
sshd: unknown [priv]
sshd: unknown [net]
sshd: [accepted]
sshd: root [priv]
sshd: root@pts/0
`

func TestSSHPrivSessionUser(t *testing.T) {
	tests := []struct {
		args string
		want string
	}{
		{"sshd: test2 [priv]", "test2"},
		{"sshd-session: test2 [priv]", "test2"}, // OpenSSH 9.8 renamed the binary
		{"sshd: test2", ""},                     // session child, not the monitor
		{"sshd: test2@notty", ""},
		{"sshd: unknown [priv]", ""}, // still authenticating
		{"sshd: unknown [net]", ""},
		{"sshd: [accepted]", ""},
		{"sshd: /usr/sbin/sshd -D [listener] 1 of 10-100 startups", ""},
		{"/usr/local/lib/hexplus/dropbear -F -R -p 110", ""},
		{"", ""},
	}
	for _, tc := range tests {
		if got := sshPrivSessionUser(tc.args); got != tc.want {
			t.Errorf("sshPrivSessionUser(%q) = %q, want %q", tc.args, got, tc.want)
		}
	}
}

func TestCountSSHPrivSessions(t *testing.T) {
	got := map[string]int{}
	countSSHPrivSessions(psSSHArgs, got)

	want := map[string]int{"test2": 2, "somchai": 1}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for user, n := range want {
		if got[user] != n {
			t.Errorf("user %q: got %d, want %d (full: %v)", user, got[user], n, got)
		}
	}
}

func TestParseSSHTimes(t *testing.T) {
	got := parseSSHTimes(psSSHSessions)

	// Longest test2 monitor is 67482s = 18h 44m 42s. The 40412s session and
	// every non-monitor line must lose to it.
	if want := "18:44:42"; got["test2"] != want {
		t.Errorf("test2: got %q, want %q (full: %v)", got["test2"], want, got)
	}
	if _, ok := got["root"]; ok {
		t.Errorf("root should never be reported: %v", got)
	}
	if len(got) != 1 {
		t.Errorf("expected only test2, got %v", got)
	}
}

func TestCountOVPNStatus(t *testing.T) {
	tests := []struct {
		name string
		data string
		want map[string]int
	}{
		{
			// Traditional format. UNDEF is a client that has completed the TCP
			// handshake but not auth; it lands in the map but no caller ever
			// looks it up, since they all iterate systemUsers().
			name: "status-version 1",
			data: statusV1,
			want: map[string]int{"UNDEF": 1, "HexeyNet": 1},
		},
		{
			// Regression guard: before status-version 2 support, every line
			// here fell through (no bare "Common Name," header to open the v1
			// section, and "CLIENT_LIST," does not match the "CLIENT_LIST\t"
			// prefix), so the count came back empty and every user read as
			// offline. duplicate-cn means HexeyNet legitimately has 2 sessions.
			name: "status-version 2",
			data: statusV2,
			want: map[string]int{"HexeyNet": 2, "somchai": 1},
		},
		{
			name: "status-version 3",
			data: statusV3,
			want: map[string]int{"HexeyNet": 1},
		},
		{
			name: "empty file",
			data: "",
			want: map[string]int{},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := map[string]int{}
			countOVPNStatus(tc.data, got)
			if len(got) != len(tc.want) {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
			for user, n := range tc.want {
				if got[user] != n {
					t.Errorf("user %q: got %d, want %d (full: %v)", user, got[user], n, got)
				}
			}
		})
	}
}

// countOVPNStatus merges into a shared map across every status log, so an
// extra OpenVPN instance's file must add to the primary's counts rather than
// replace them.
func TestCountOVPNStatusMergesInstances(t *testing.T) {
	got := map[string]int{}
	countOVPNStatus(statusV2, got)
	countOVPNStatus(statusV3, got)
	if got["HexeyNet"] != 3 {
		t.Errorf("HexeyNet across two instances: got %d, want 3 (full: %v)", got["HexeyNet"], got)
	}
}

// The header row must never be counted as a connected client.
func TestCountOVPNStatusIgnoresHeader(t *testing.T) {
	got := map[string]int{}
	countOVPNStatus(statusV2, got)
	if n, ok := got["Common Name"]; ok {
		t.Errorf("header row counted as a user (%d)", n)
	}
}

// readOpenVPNTimes reads hardcoded /var/log paths, so it is not unit-testable
// without a refactor that is out of scope. What carries the actual risk is the
// field offsets it indexes into — Common Name at [1] and the epoch at [8] —
// so lock those against the format string read out of the OpenVPN 2.5.9
// binary. If a future build reorders CLIENT_LIST, this fails instead of the
// menu silently showing wrong connection times.
func TestClientListFieldOffsets(t *testing.T) {
	var row string
	for _, line := range strings.Split(statusV2, "\n") {
		if strings.HasPrefix(line, "CLIENT_LIST,") {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal("fixture has no CLIENT_LIST row")
	}
	parts := strings.Split(row, ",")
	if len(parts) != 13 {
		t.Fatalf("CLIENT_LIST row has %d fields, want 13", len(parts))
	}
	if parts[1] != "HexeyNet" {
		t.Errorf("field [1] (Common Name): got %q, want %q", parts[1], "HexeyNet")
	}
	epoch, err := strconv.ParseInt(parts[8], 10, 64)
	if err != nil {
		t.Fatalf("field [8] (Connected Since time_t) not an integer: %q", parts[8])
	}
	if want := time.Unix(epoch, 0).UTC().Format("2006-01-02 15:04:05"); parts[7] != want {
		t.Errorf("field [7] %q disagrees with epoch [8] rendered as %q", parts[7], want)
	}
}

func TestMenuPasswordProblem(t *testing.T) {
	cases := []struct {
		pw, wantSub string
	}{
		{"abcd", ""},
		{"abc", "อย่างน้อย 4"},
		{strings.Repeat("a", 127), ""},
		{strings.Repeat("a", 128), "ไม่เกิน 127"},
		{"abcd\x00", "อักขระที่ใช้ไม่ได้"},
	}
	for _, c := range cases {
		got := menuPasswordProblem(c.pw)
		if (c.wantSub == "") != (got == "") || !strings.Contains(got, c.wantSub) {
			t.Errorf("menuPasswordProblem(%d bytes) = %q, want containing %q", len(c.pw), got, c.wantSub)
		}
	}
}

// shadow semantics: an account expiring on day E works through day E-1 and
// is rejected from 00:00 UTC on day E (hexplus-auth.sh, pam_unix). The menus
// rounded the remaining hours toward zero, so on day E itself they showed
// "0 days left" and menu 07 did not remove the account.
func TestDaysLeftFollowsShadowSemantics(t *testing.T) {
	expire := time.Date(2026, 10, 10, 0, 0, 0, 0, time.UTC)
	at := func(day, hour int) time.Time { return time.Date(2026, 10, day, hour, 30, 0, 0, time.UTC) }
	cases := []struct {
		now  time.Time
		want int
	}{
		{at(8, 0), 1},
		{at(8, 23), 1},
		{at(9, 0), 0},   // last day
		{at(9, 23), 0},  // still the last day
		{at(10, 0), -1}, // expiry day: the VPN already refuses
		{at(10, 12), -1},
		{at(11, 12), -2},
	}
	for _, c := range cases {
		if got := daysLeft(expire, c.now); got != c.want {
			t.Errorf("daysLeft(at %s) = %d, want %d", c.now.Format(time.RFC3339), got, c.want)
		}
	}
}
