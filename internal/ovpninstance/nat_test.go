package ovpninstance

import (
	"errors"
	"reflect"
	"strings"
	"testing"
)

// fakeNat stands in for the nat table: it understands -t nat with -C, -A and
// -D on POSTROUTING, like iptables (-A always appends, -D removes one match).
type fakeNat struct {
	rules []string
	calls []string
}

func useFakeNat(t *testing.T, rules ...string) *fakeNat {
	t.Helper()
	f := &fakeNat{rules: rules}
	old := iptablesRun
	iptablesRun = func(args ...string) ([]byte, error) {
		f.calls = append(f.calls, strings.Join(args, " "))
		if len(args) < 4 || args[0] != "-t" || args[1] != "nat" || args[3] != "POSTROUTING" {
			t.Errorf("unexpected iptables call: %v", args)
			return nil, errors.New("unexpected")
		}
		rule := strings.Join(args[4:], " ")
		switch args[2] {
		case "-C":
			for _, r := range f.rules {
				if r == rule {
					return nil, nil
				}
			}
			return []byte("Bad rule"), errors.New("exit status 1")
		case "-A":
			f.rules = append(f.rules, rule)
			return nil, nil
		case "-D":
			for i, r := range f.rules {
				if r == rule {
					f.rules = append(f.rules[:i], f.rules[i+1:]...)
					return nil, nil
				}
			}
			return []byte("Bad rule"), errors.New("exit status 1")
		}
		t.Errorf("unexpected iptables verb: %v", args)
		return nil, errors.New("unexpected")
	}
	t.Cleanup(func() { iptablesRun = old })
	return f
}

const (
	rule8 = "-s 10.8.0.0/16 -j MASQUERADE"
	rule9 = "-s 10.9.0.0/16 -j MASQUERADE"
)

// A reinstall appended another copy of the rule every time.
func TestEnsureMasqueradeDoesNotStack(t *testing.T) {
	f := useFakeNat(t, rule9)
	for i := 0; i < 3; i++ {
		if err := EnsureMasquerade("10.8.0.0/16"); err != nil {
			t.Fatal(err)
		}
	}
	if want := []string{rule9, rule8}; !reflect.DeepEqual(f.rules, want) {
		t.Errorf("rules = %v, want %v", f.rules, want)
	}
}

// Uninstall deleted one copy and left the rest live.
func TestDeleteMasqueradeRemovesEveryCopyOfThatSubnetOnly(t *testing.T) {
	f := useFakeNat(t, rule8, rule9, rule8, rule8)
	if n, err := DeleteMasquerade("10.8.0.0/16"); n != 3 || err != nil {
		t.Errorf("deleted %d (%v), want 3", n, err)
	}
	if want := []string{rule9}; !reflect.DeepEqual(f.rules, want) {
		t.Errorf("rules = %v, want %v", f.rules, want)
	}
}

func TestDeleteMasqueradeWhenAbsent(t *testing.T) {
	f := useFakeNat(t, rule9)
	if n, err := DeleteMasquerade("10.8.0.0/16"); n != 0 || err != nil {
		t.Errorf("deleted %d (%v), want 0", n, err)
	}
	if want := []string{rule9}; !reflect.DeepEqual(f.rules, want) {
		t.Errorf("rules = %v, want %v", f.rules, want)
	}
}

// A -D that never fails must not loop forever.
func TestDeleteMasqueradeIsBounded(t *testing.T) {
	calls := 0
	old := iptablesRun
	iptablesRun = func(args ...string) ([]byte, error) { calls++; return nil, nil }
	t.Cleanup(func() { iptablesRun = old })
	if n, _ := DeleteMasquerade("10.8.0.0/16"); n != maxMasqueradeCopies || calls != maxMasqueradeCopies {
		t.Errorf("deleted %d with %d calls, want %d", n, calls, maxMasqueradeCopies)
	}
}

func TestEnsureMasqueradeReportsIptablesOutput(t *testing.T) {
	old := iptablesRun
	iptablesRun = func(args ...string) ([]byte, error) {
		return []byte(" Another app is currently holding the xtables lock \n"), errors.New("exit status 4")
	}
	t.Cleanup(func() { iptablesRun = old })
	err := EnsureMasquerade("10.8.0.0/16")
	if err == nil || !strings.Contains(err.Error(), "exit status 4 Another app is currently holding the xtables lock") {
		t.Errorf("err = %v", err)
	}
}

// A held xtables lock (exit 4) is not "rule absent": -C must not fall
// through to -A (that stacked the rule once the lock freed), and -D must
// report the failure instead of claiming there was nothing to delete.
func TestMasqueradeDistinguishesTheXtablesLockFromAbsence(t *testing.T) {
	locked := true
	f := useFakeNat(t, rule8)
	inner := iptablesRun
	iptablesRun = func(args ...string) ([]byte, error) {
		if locked {
			f.calls = append(f.calls, strings.Join(args, " "))
			return []byte("Another app is currently holding the xtables lock."), errors.New("exit status 4")
		}
		return inner(args...)
	}
	if err := EnsureMasquerade("10.8.0.0/16"); err == nil {
		t.Error("EnsureMasquerade ignored the lock")
	}
	for _, c := range f.calls {
		if strings.Contains(c, " -A ") {
			t.Errorf("appended while the lock was held: %s", c)
		}
	}
	if n, err := DeleteMasquerade("10.8.0.0/16"); n != 0 || err == nil {
		t.Errorf("DeleteMasquerade = %d, %v; want 0 and the lock error", n, err)
	}
	locked = false
	if n, err := DeleteMasquerade("10.8.0.0/16"); n != 1 || err != nil {
		t.Errorf("after the lock: %d, %v", n, err)
	}
}
