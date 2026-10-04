package user

import (
	"os"
	"sort"
	"strings"
)

// scriptVerifiable are the crypt formats hexplus-auth.sh can check (see
// pki.authScript: it recomputes the hash with `openssl passwd -6/-5/-1`).
// Anything else, notably yescrypt ($y$) written by PAM on Ubuntu 22.04+ and
// Debian 11+, makes OpenVPN reject the user even with the right password.
var scriptVerifiable = []string{"$6$", "$5$", "$1$"}

// UnverifiableHashUsers returns, sorted, the names in names whose entry in
// shadow (the contents of /etc/shadow) holds a password hash the OpenVPN
// auth script cannot verify: a format it does not know, or a $6$/$5$ hash
// with a "rounds=" parameter. Locked accounts ("!", "*") and accounts with
// no password are skipped: the script rejects those on purpose.
func UnverifiableHashUsers(shadow string, names []string) []string {
	want := make(map[string]bool, len(names))
	for _, n := range names {
		want[n] = true
	}
	var out []string
	for _, line := range strings.Split(shadow, "\n") {
		f := strings.SplitN(line, ":", 3)
		if len(f) < 2 || !want[f[0]] {
			continue
		}
		hash := strings.TrimRight(f[1], "\r")
		if hash == "" || hash[0] == '!' || hash[0] == '*' {
			continue
		}
		ok := false
		for _, p := range scriptVerifiable {
			if strings.HasPrefix(hash, p) {
				// "$6$rounds=N$salt$..." (login.defs sets SHA_CRYPT_*_ROUNDS):
				// the script splits on '$' and takes "rounds=N" as the salt,
				// so it can never reproduce the hash.
				ok = !strings.HasPrefix(hash[len(p):], "rounds=")
				break
			}
		}
		if !ok {
			out = append(out, f[0])
		}
	}
	sort.Strings(out)
	return out
}

// ReadUnverifiableHashUsers is UnverifiableHashUsers over the live
// /etc/shadow. It needs root, like everything else in the user menus.
func ReadUnverifiableHashUsers(names []string) ([]string, error) {
	raw, err := os.ReadFile("/etc/shadow")
	if err != nil {
		return nil, err
	}
	return UnverifiableHashUsers(string(raw), names), nil
}
