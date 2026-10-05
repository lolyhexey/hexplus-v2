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
		"jack:$6$rounds=10000$salt$hash:19000:0:99999:7:::\n" + // SHA_CRYPT_*_ROUNDS set: the script handles it
		"kate:$5$rounds=5000$salt$hash:19000:0:99999:7:::\n" + // same for SHA-256
		"liam:$6$roundsalt$hash:19000:0:99999:7:::\n" + // a salt that merely starts with "rounds": fine
		"ivan:$y$j9T$salt$hash\r\n" // CRLF and a short entry must not break parsing
	names := []string{"alice", "bob", "carol", "dave", "erin", "frank", "gina", "hank", "ivan", "jack", "kate", "liam", "nobody-in-shadow"}

	got := UnverifiableHashUsers(shadow, names)
	want := []string{"alice", "hank", "ivan"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("UnverifiableHashUsers = %v, want %v", got, want)
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
