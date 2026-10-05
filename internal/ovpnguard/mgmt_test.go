package ovpnguard

import (
	"bufio"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
)

// fakeMgmt is an OpenVPN management socket that speaks just enough of the
// protocol: the >INFO greeting, `status 2`, `client-kill <cid> <msg>` and
// `quit`, with an async >LOG line thrown in before each reply.
type fakeMgmt struct {
	mu       sync.Mutex
	sessions []Session
	killed   []string // "cid msg"
	ln       net.Listener
}

func startFake(t *testing.T, path, instance string, sessions []Session) *fakeMgmt {
	t.Helper()
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Skipf("unix sockets unavailable here: %v", err)
	}
	f := &fakeMgmt{sessions: sessions, ln: ln}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f
}

func (f *fakeMgmt) serve(c net.Conn) {
	defer c.Close()
	fmt.Fprint(c, ">INFO:OpenVPN Management Interface Version 3 -- type 'help' for more info\r\n")
	r := bufio.NewReader(c)
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		cmd := strings.Fields(strings.TrimSpace(line))
		if len(cmd) == 0 {
			continue
		}
		fmt.Fprint(c, ">LOG:1759627700,,some async noise\r\n")
		f.mu.Lock()
		switch {
		case cmd[0] == "status":
			fmt.Fprint(c, "TITLE,OpenVPN 2.5.9\r\nHEADER,CLIENT_LIST,Common Name,...\r\n")
			for _, s := range f.sessions {
				fmt.Fprintf(c, "CLIENT_LIST,%s,%s,10.8.0.9,,%d,1,x,%d,%s,%d,0,AES-256-GCM\r\n", s.Name, s.Real, s.RxBytes, s.Since, s.Name, s.CID)
			}
			fmt.Fprint(c, "GLOBAL_STATS,Max bcast/mcast queue length,0\r\nEND\r\n")
		case cmd[0] == "client-kill" && len(cmd) >= 2:
			found := false
			for i, s := range f.sessions {
				if fmt.Sprint(s.CID) == cmd[1] {
					f.sessions = append(f.sessions[:i], f.sessions[i+1:]...)
					found = true
					break
				}
			}
			msg := ""
			if len(cmd) >= 3 {
				msg = cmd[2]
			}
			f.killed = append(f.killed, cmd[1]+" "+msg)
			if found {
				fmt.Fprint(c, "SUCCESS: client-kill command succeeded\r\n")
			} else {
				fmt.Fprint(c, "ERROR: client-kill command failed\r\n")
			}
		case cmd[0] == "quit":
			f.mu.Unlock()
			return
		default:
			fmt.Fprint(c, "ERROR: unknown command\r\n")
		}
		f.mu.Unlock()
	}
}

func (f *fakeMgmt) kills() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := append([]string{}, f.killed...)
	sort.Strings(out)
	return out
}

func shortTempDir(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the guard runs on Linux; unix sockets here are a Linux test")
	}
	// Unix socket paths are limited to ~108 bytes; t.TempDir can be long.
	dir, err := os.MkdirTemp("/tmp", "og")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	return dir
}

// End to end against fake management sockets: the limit counts sessions
// across instances, the oldest goes, refused accounts lose everything, and
// every kill carries HALT so the client does not reconnect straight away.
func TestEnforceAcrossInstances(t *testing.T) {
	dir := shortTempDir(t)
	main := startFake(t, filepath.Join(dir, "ovpn-main.sock"), "main", []Session{
		{CID: 1, Name: "bob", Since: 100, Real: "1.1.1.1:1"},
		{CID: 2, Name: "alice", Since: 100, Real: "2.2.2.2:2"},
		{CID: 3, Name: "gone", Since: 100, Real: "3.3.3.3:3"},
	})
	inst2 := startFake(t, filepath.Join(dir, "ovpn-2.sock"), "2", []Session{
		{CID: 1, Name: "bob", Since: 200, Real: "1.1.1.1:9"}, // newer bob on another port
	})
	// A socket file with nobody listening (stopped instance) must not break the run.
	if f, err := os.Create(filepath.Join(dir, "ovpn-3.sock")); err == nil {
		f.Close()
	}

	var logs []string
	logf := func(format string, a ...any) { logs = append(logs, fmt.Sprintf(format, a...)) }
	p := Policy{
		Limit:   func(n string) int { return map[string]int{"bob": 1, "alice": 1}[n] },
		Refused: func(n string) string { return map[string]string{"gone": "deleted"}[n] },
	}
	n := enforce(sockets(dir), p, nil, logf)

	if n != 2 {
		t.Errorf("killed %d sessions, want 2 (logs: %q)", n, logs)
	}
	if got, want := main.kills(), []string{"1 HALT", "3 HALT"}; !reflect.DeepEqual(got, want) {
		t.Errorf("main kills = %v, want %v", got, want)
	}
	if got := inst2.kills(); len(got) != 0 {
		t.Errorf("the newest bob session (instance 2) was killed: %v", got)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "instance 3") {
		t.Errorf("the dead socket was not reported: %q", logs)
	}
	if !strings.Contains(joined, "kicked bob") || !strings.Contains(joined, "kicked gone") {
		t.Errorf("kicks were not logged: %q", logs)
	}
}

func TestKickInEndsEverySessionOfOneUser(t *testing.T) {
	dir := shortTempDir(t)
	main := startFake(t, filepath.Join(dir, "ovpn-main.sock"), "main", []Session{
		{CID: 1, Name: "bob", Since: 100},
		{CID: 2, Name: "alice", Since: 100},
	})
	inst2 := startFake(t, filepath.Join(dir, "ovpn-2.sock"), "2", []Session{
		{CID: 5, Name: "bob", Since: 200},
	})
	if n := kickIn(sockets(dir), "bob", func(string, ...any) {}); n != 2 {
		t.Errorf("kicked %d, want 2", n)
	}
	if got := main.kills(); !reflect.DeepEqual(got, []string{"1 HALT"}) {
		t.Errorf("main kills = %v", got)
	}
	if got := inst2.kills(); !reflect.DeepEqual(got, []string{"5 HALT"}) {
		t.Errorf("instance 2 kills = %v", got)
	}
}

func TestReadV1Limits(t *testing.T) {
	path := filepath.Join(t.TempDir(), "usuarios.db")
	if err := os.WriteFile(path, []byte("bob 2\nalice x\n\ncarol 0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := readV1Limits(path)
	if err != nil {
		t.Fatal(err)
	}
	if want := map[string]int{"bob": 2, "carol": 0}; !reflect.DeepEqual(got, want) {
		t.Errorf("readV1Limits = %v, want %v", got, want)
	}
	if got, err := readV1Limits(filepath.Join(t.TempDir(), "missing")); err != nil || len(got) != 0 {
		t.Errorf("missing file must give no limits and no error: %v %v", got, err)
	}
}
