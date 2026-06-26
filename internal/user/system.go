// system.go: wrap useradd/userdel/chpasswd so we don't reimplement
// /etc/passwd + /etc/shadow editing in Go.
//
// Why shell out: shadow-utils ships on every Linux distro we care about
// (Debian/Ubuntu/RHEL/Arch all default-install it; Alpine needs
// `apk add shadow`). Reimplementing useradd correctly - handling
// /etc/login.defs, PAM integration, the various locking semantics,
// /etc/skel - would be a few hundred lines for behavior that's already
// in the box. The wrapper's job is to turn useradd's status codes into
// errors the CLI can render meaningfully.
//
// Username validation mirrors the rule HEXPLUS v1's createuser landed
// on: ^[a-zA-Z][a-zA-Z0-9_-]*$, 2-32 chars. useradd applies the same
// rule but rejects with a cryptic "invalid user name"; doing the check
// up front lets us tell the user "must start with a letter, ..." which
// is what v1 customers expect.

package user

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var nameRE = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)

// ErrNameInvalid is the canonical "username doesn't match HEXPLUS rules"
// signal so the CLI can render a focused message instead of useradd's
// "invalid user name 'foo'" which doesn't say what to fix.
var ErrNameInvalid = errors.New("username must start with a letter, then letters/digits/_/-, 2-32 chars")

// ValidateName enforces the HEXPLUS naming rule before useradd ever sees
// the input. Returns ErrNameInvalid for the standard failure modes;
// callers can wrap it with the bad input for context.
func ValidateName(name string) error {
	if len(name) < 2 || len(name) > 32 {
		return ErrNameInvalid
	}
	if !nameRE.MatchString(name) {
		return ErrNameInvalid
	}
	return nil
}

// SystemUserExists reports whether `getent passwd <name>` finds the
// account in /etc/passwd. Used by Add to refuse re-adding an existing
// user.
func SystemUserExists(name string) (bool, error) {
	cmd := exec.Command("getent", "passwd", name)
	err := cmd.Run()
	if err == nil {
		return true, nil
	}
	// getent exits 2 when the key isn't found. Anything else is a real
	// error (binary missing, etc.).
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 2 {
		return false, nil
	}
	if errors.Is(err, exec.ErrNotFound) {
		return false, errors.New("getent not in PATH; install shadow-utils or glibc-utils")
	}
	return false, fmt.Errorf("getent passwd %s: %w", name, err)
}

// CreateSystemUser runs useradd with HEXPLUS's default flags. The user
// is created without a home dir, with /bin/false as the shell (we only
// need the entry for password auth in dropbear; the user never gets a
// real login session), and optionally with an absolute expiry date.
//
// Returns an error including useradd's stderr when the command fails,
// so the CLI surfaces the underlying reason (e.g. "user already exists",
// "invalid date format").
func CreateSystemUser(name string, expires time.Time) error {
	args := []string{"-M", "-s", "/bin/false"}
	if !expires.IsZero() {
		args = append(args, "-e", expires.UTC().Format("2006-01-02"))
	}
	args = append(args, name)
	cmd := exec.Command("useradd", args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("useradd not in PATH; install shadow-utils")
		}
		return fmt.Errorf("useradd %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// SetPassword feeds 'name:password\n' to `chpasswd`, which hashes via
// the system's configured crypt method (SHA-512 on every distro since
// ~2010) and writes /etc/shadow. We avoid `passwd <name>` because it
// wants a tty.
func SetPassword(name, password string) error {
	cmd := exec.Command("chpasswd")
	cmd.Stdin = strings.NewReader(name + ":" + password + "\n")
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("chpasswd not in PATH; install shadow-utils")
		}
		return fmt.Errorf("chpasswd: %w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ChageExpiry runs `chage -E <date> <name>` to update the system-side
// expiry. Pass a zero time.Time to mark the account as never expiring
// (chage -E -1).
//
// We split this out of CreateSystemUser because the "change expiry"
// flow runs against an already-created account, where useradd's -e flag
// no longer applies.
func ChageExpiry(name string, expires time.Time) error {
	val := "-1"
	if !expires.IsZero() {
		val = expires.UTC().Format("2006-01-02")
	}
	cmd := exec.Command("chage", "-E", val, name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("chage not in PATH; install shadow-utils")
		}
		return fmt.Errorf("chage %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// DeleteSystemUser runs userdel. The --force avoids "mail spool" errors
// on systems where the user never got a spool to begin with.
func DeleteSystemUser(name string) error {
	cmd := exec.Command("userdel", "--force", name)
	out, err := cmd.CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return errors.New("userdel not in PATH; install shadow-utils")
		}
		// userdel returns 6 when the user doesn't exist; treat that as
		// success so 'user remove' is idempotent.
		var ee *exec.ExitError
		if errors.As(err, &ee) && ee.ExitCode() == 6 {
			return nil
		}
		return fmt.Errorf("userdel %s: %w: %s", name, err, strings.TrimSpace(string(out)))
	}
	return nil
}
