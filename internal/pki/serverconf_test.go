package pki

import (
	"strings"
	"testing"
)

// The embedded OpenVPN is built with --disable-plugins, so any "plugin"
// directive stops the daemon at startup. Authentication must go through the
// script no matter what exists on the host.
func TestAuthDirectivesUseScriptNotPlugin(t *testing.T) {
	got := authDirectives()

	for _, want := range []string{
		"script-security 2",
		"verify-client-cert none",
		"username-as-common-name",
		"auth-user-pass-verify " + AuthScriptPath + " via-file",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in:\n%s", want, got)
		}
	}
	for _, line := range strings.Split(got, "\n") {
		if strings.HasPrefix(strings.TrimSpace(line), "plugin") {
			t.Errorf("emits a plugin directive the embedded binary cannot parse: %q", line)
		}
	}
}
