package user

import (
	"io"
	"reflect"
	"testing"
)

// The OpenVPN auth script can only verify $6$/$5$/$1$ hashes. Bare
// chpasswd defers to PAM, which writes yescrypt on Ubuntu 22.04+, so the
// command SetPassword runs must pin the algorithm.
func TestChpasswdCmdPinsSHA512(t *testing.T) {
	cmd := chpasswdCmd("alice", "s3cret")

	want := []string{"chpasswd", "-c", "SHA512"}
	if !reflect.DeepEqual(cmd.Args, want) {
		t.Fatalf("args = %v, want %v", cmd.Args, want)
	}

	in, err := io.ReadAll(cmd.Stdin)
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(in), "alice:s3cret\n"; got != want {
		t.Fatalf("stdin = %q, want %q", got, want)
	}
}
