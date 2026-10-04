package user

import (
	"reflect"
	"testing"
)

func TestUnverifiableHashUsers(t *testing.T) {
	shadow := "" +
		"root:$y$j9T$aaaa$bbbb:19000:0:99999:7:::\n" + // not in names: ignored
		"alice:$y$j9T$salt$hash:19000:0:99999:7:::\n" + // yescrypt: auth script rejects
		"bob:$6$salt$hash:19000:0:99999:7:::\n" + // SHA-512: fine
		"carol:$5$salt$hash:19000:0:99999:7:::\n" + // SHA-256: fine
		"dave:$1$salt$hash:19000:0:99999:7:::\n" + // MD5: fine
		"erin:!$y$j9T$salt$hash:19000:0:99999:7:::\n" + // locked: rejected on purpose
		"frank:*:19000:0:99999:7:::\n" + // no password login
		"gina::19000:0:99999:7:::\n" + // empty password field
		"hank:$2b$12$salt$hash:19000:0:99999:7:::\n" + // bcrypt: script cannot verify
		"ivan:$y$j9T$salt$hash\r\n" // CRLF and a short entry must not break parsing
	names := []string{"alice", "bob", "carol", "dave", "erin", "frank", "gina", "hank", "ivan", "nobody-in-shadow"}

	got := UnverifiableHashUsers(shadow, names)
	want := []string{"alice", "hank", "ivan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnverifiableHashUsers = %v, want %v", got, want)
	}
}

// The checker must flag exactly what hexplus-auth.sh rejects. This pins the
// accepted prefixes next to the script's `case "$ALG" in 6|5|1`.
func TestScriptVerifiablePrefixesMatchAuthScript(t *testing.T) {
	want := []string{"$6$", "$5$", "$1$"}
	if !reflect.DeepEqual(scriptVerifiable, want) {
		t.Errorf("scriptVerifiable = %v, want %v (keep in sync with pki.authScript)", scriptVerifiable, want)
	}
}

func TestUnverifiableHashUsersEmptyInput(t *testing.T) {
	if got := UnverifiableHashUsers("", []string{"alice"}); len(got) != 0 {
		t.Errorf("empty shadow: got %v", got)
	}
	if got := UnverifiableHashUsers("alice:$y$x$y$z:::\n", nil); len(got) != 0 {
		t.Errorf("no names: got %v", got)
	}
}
