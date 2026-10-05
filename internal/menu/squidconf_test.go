package menu

import (
	"strings"
	"testing"
)

// buildSquidConf is the only squid.conf hexplus writes. Pin its access rules
// so nobody adds an allow rule that is not tied to a source or to this
// server's own address, which would turn the proxy into an open relay.
func TestBuildSquidConfAccessRules(t *testing.T) {
	conf := buildSquidConf(8080, "203.0.113.5")

	var rules []string
	for _, line := range strings.Split(conf, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "http_access ") {
			rules = append(rules, line)
		}
	}
	want := []string{
		"http_access allow SSH",
		"http_access allow localnet",
		"http_access allow localhost",
		"http_access deny all",
	}
	if len(rules) != len(want) {
		t.Fatalf("http_access rules = %q, want %q", rules, want)
	}
	for i := range want {
		if rules[i] != want[i] {
			t.Errorf("rule %d = %q, want %q", i, rules[i], want[i])
		}
	}
	for _, acl := range []string{"acl SSH dst 203.0.113.5/32", "acl SSH dst 127.0.0.1/32", "http_port 8080"} {
		if !strings.Contains(conf, acl+"\n") {
			t.Errorf("conf is missing %q", acl)
		}
	}
}

func TestBuildSquidConfWithoutServerIPHasNoSSHAllow(t *testing.T) {
	conf := buildSquidConf(3128, "")
	if strings.Contains(conf, "http_access allow SSH") || strings.Contains(conf, "acl SSH ") {
		t.Errorf("SSH ACL emitted without a server IP:\n%s", conf)
	}
	if !strings.Contains(conf, "http_access deny all\n") {
		t.Errorf("conf does not end its rules with deny all:\n%s", conf)
	}
}
