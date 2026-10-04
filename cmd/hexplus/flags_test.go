package main

import (
	"flag"
	"io"
	"reflect"
	"testing"
)

func newUserAddFlags() (*flag.FlagSet, *string, *string, *int) {
	fs := flag.NewFlagSet("user add", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	pw := fs.String("password", "", "")
	remote := fs.String("remote", "", "")
	limit := fs.Int("limit", 0, "")
	return fs, pw, remote, limit
}

// A boolean flag never takes the next argument as its value, so a "--"
// after it is still the end-of-flags marker (logs --follow -- name).
func TestParseFlagsAnywhereBoolBeforeDoubleDash(t *testing.T) {
	fs := flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	follow := fs.Bool("follow", false, "")
	pos, err := parseFlagsAnywhere(fs, []string{"--follow", "--", "openvpn"})
	if err != nil || !*follow || !reflect.DeepEqual(pos, []string{"openvpn"}) {
		t.Errorf("got pos=%q follow=%v err=%v", pos, *follow, err)
	}
	fs = flag.NewFlagSet("logs", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	follow = fs.Bool("follow", false, "")
	pos, err = parseFlagsAnywhere(fs, []string{"openvpn", "--follow"})
	if err != nil || !*follow || !reflect.DeepEqual(pos, []string{"openvpn"}) {
		t.Errorf("flag after name: got pos=%q follow=%v err=%v", pos, *follow, err)
	}
}

// Regression guard: the documented form puts flags after the name, and a
// plain fs.Parse dropped them (user add failed with "password is required",
// user export handed out a profile for 127.0.0.1).
func TestParseFlagsAnywhere(t *testing.T) {
	cases := []struct {
		name       string
		args       []string
		wantPos    []string
		wantPw     string
		wantRemote string
		wantLimit  int
	}{
		{"flags after name", []string{"bob", "--password", "s3cret", "--remote", "203.0.113.7"}, []string{"bob"}, "s3cret", "203.0.113.7", 0},
		{"flags before name", []string{"--password=s3cret", "--limit", "2", "bob"}, []string{"bob"}, "s3cret", "", 2},
		{"flags on both sides", []string{"--limit=3", "bob", "--password", "x"}, []string{"bob"}, "x", "", 3},
		{"single-dash and = forms", []string{"bob", "-password=y", "-limit", "4"}, []string{"bob"}, "y", "", 4},
		{"no flags", []string{"bob"}, []string{"bob"}, "", "", 0},
		{"several positionals", []string{"a", "--limit", "1", "b"}, []string{"a", "b"}, "", "", 1},
		{"-- ends flag parsing", []string{"--password", "x", "--", "-weird", "--limit", "9"}, []string{"-weird", "--limit", "9"}, "x", "", 0},
		{"-- as a flag value is a value", []string{"bob", "--password", "--", "--limit", "2"}, []string{"bob"}, "--", "", 2},
		{"-- as a value before the name", []string{"--password", "--", "bob"}, []string{"bob"}, "--", "", 0},
		{"empty", nil, nil, "", "", 0},
	}
	for _, c := range cases {
		fs, pw, remote, limit := newUserAddFlags()
		pos, err := parseFlagsAnywhere(fs, c.args)
		if err != nil {
			t.Errorf("%s: unexpected error %v", c.name, err)
			continue
		}
		if !reflect.DeepEqual(pos, c.wantPos) || *pw != c.wantPw || *remote != c.wantRemote || *limit != c.wantLimit {
			t.Errorf("%s: got pos=%q pw=%q remote=%q limit=%d, want pos=%q pw=%q remote=%q limit=%d",
				c.name, pos, *pw, *remote, *limit, c.wantPos, c.wantPw, c.wantRemote, c.wantLimit)
		}
	}
}

func TestParseFlagsAnywhereReportsUnknownFlags(t *testing.T) {
	fs, _, _, _ := newUserAddFlags()
	if _, err := parseFlagsAnywhere(fs, []string{"bob", "--nope"}); err == nil {
		t.Error("an unknown flag after the name must be an error, not silently ignored")
	}
	fs, _, _, _ = newUserAddFlags()
	if _, err := parseFlagsAnywhere(fs, []string{"bob", "--limit", "notanumber"}); err == nil {
		t.Error("a bad flag value after the name must be an error")
	}
}
