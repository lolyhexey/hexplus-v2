package speedlimit

import (
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// fakeHost points the teardown at a temp /sys/class/net with the given
// devices and records the ip/tc commands it runs. redirects maps a tun to
// the `tc filter show ... parent ffff:` output for it.
func fakeHost(t *testing.T, devs []string, recorded string, redirects map[string]string) (dir string, deleted *[]string) {
	t.Helper()
	dir = t.TempDir()
	net := filepath.Join(dir, "net")
	for _, d := range devs {
		if err := os.MkdirAll(filepath.Join(net, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	files := map[*string]string{&ifbStatePath: "speedlimit-ifb", &scriptPath: "hexplus-learn-address", &confPath: "hexplus-speedlimit.conf"}
	for ptr, name := range files {
		old := *ptr
		*ptr = filepath.Join(dir, name)
		t.Cleanup(func() { *ptr = old })
	}
	oldNet, oldRun := sysClassNet, runCmd
	sysClassNet = net
	t.Cleanup(func() { sysClassNet, runCmd = oldNet, oldRun })
	if recorded != "" {
		if err := os.WriteFile(ifbStatePath, []byte(recorded), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_ = os.WriteFile(scriptPath, []byte("#!/bin/sh\n"), 0o755)
	_ = os.WriteFile(confPath, []byte("main=10\n"), 0o644)

	var del []string
	runCmd = func(name string, args ...string) ([]byte, error) {
		cmd := name + " " + strings.Join(args, " ")
		switch {
		case strings.HasPrefix(cmd, "tc filter show dev "):
			return []byte(redirects[args[3]]), nil
		case strings.HasPrefix(cmd, "ip link del "):
			del = append(del, args[2])
			return nil, os.RemoveAll(filepath.Join(net, args[2]))
		}
		t.Errorf("unexpected command: %s", cmd)
		return nil, nil
	}
	return dir, &del
}

// iproute2's rendering of the hook's ingress filter.
func mirredTo(ifb string) string {
	return "filter parent ffff: protocol ip pref 49152 u32 chain 0 fh 800::800 order 2048 key ht 800 bkt 0 terminal flowid ??? not_in_hw\n" +
		"  match 00000000/00000000 at 0\n" +
		"\taction order 1: mirred (Egress Redirect to device " + ifb + ") stolen\n"
}

// The shaper's ifb devices outlived OpenVPN uninstall until reboot, and the
// hook script stayed in /usr/local/bin.
func TestTeardownRemovesOnlyOurDevices(t *testing.T) {
	_, deleted := fakeHost(t,
		[]string{"tun0", "tun2", "ifb0", "ifb2", "ifb7", "ifb9", "eth0"},
		"ifb7\nifb7\nifb9\n", // recorded by the hook (ifb9 still exists, ifb7 too)
		map[string]string{"tun0": mirredTo("ifb0"), "tun2": mirredTo("ifb2")})
	// ifb5 recorded but already gone: not an error.
	_ = os.WriteFile(ifbStatePath, []byte("ifb7\nifb7\nifb9\nifb5\n"), 0o644)

	if err := Teardown(); err != nil {
		t.Fatal(err)
	}
	sort.Strings(*deleted)
	if want := []string{"ifb0", "ifb2", "ifb7", "ifb9"}; !reflect.DeepEqual(*deleted, want) {
		t.Errorf("deleted %v, want %v", *deleted, want)
	}
	for _, p := range []string{ifbStatePath, scriptPath, confPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Errorf("%s still exists", filepath.Base(p))
		}
	}
}

// An ifb nobody can tie to the hook (another tool's ifb0, no redirect from
// a tun, not recorded) is left alone.
func TestTeardownLeavesForeignIFB(t *testing.T) {
	_, deleted := fakeHost(t, []string{"tun0", "ifb0", "ifb1"}, "", map[string]string{"tun0": "" /* no shaping filter */})
	if err := Teardown(); err != nil {
		t.Fatal(err)
	}
	if len(*deleted) != 0 {
		t.Errorf("deleted %v", *deleted)
	}
}

func TestReleaseTunRemovesOneInstance(t *testing.T) {
	_, deleted := fakeHost(t, []string{"tun0", "tun3", "ifb0", "ifb3"}, "ifb0\nifb3\n",
		map[string]string{"tun0": mirredTo("ifb0")})
	if err := ReleaseTun("tun3"); err != nil {
		t.Fatal(err)
	}
	if want := []string{"ifb3"}; !reflect.DeepEqual(*deleted, want) {
		t.Errorf("deleted %v, want %v", *deleted, want)
	}
	if got := recordedIFBs(); !reflect.DeepEqual(got, []string{"ifb0"}) {
		t.Errorf("state file now lists %v, want [ifb0]", got)
	}
	for _, bad := range []string{"eth0", "tun", "tunx; rm -rf /"} {
		*deleted = nil
		_ = ReleaseTun(bad)
		if len(*deleted) != 0 {
			t.Errorf("ReleaseTun(%q) deleted %v", bad, *deleted)
		}
	}
}
