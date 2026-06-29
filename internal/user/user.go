// user.go: the orchestration that ties system user creation, PKI
// signing, metadata DB persistence, and .ovpn export into the verbs
// the CLI calls.
//
// Why split this out from main.go: the verbs need to back out of a
// half-finished add (e.g. system user created but cert signing failed)
// or a half-finished remove. Keeping the rollback logic next to the
// happy path makes the invariants obvious.

package user

import (
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/lolyhexey/hexplus/internal/pki"
)

// AddInput drives Add. Password is required - we don't auto-generate
// because v1 sellers tell us the password they negotiated with the
// customer and expect to type it in. ExpiresInDays of 0 means "no
// expiry" (typical for a lifetime account).
type AddInput struct {
	Name          string
	Password      string
	ExpiresInDays int
	Limit         int
}

// AddResult is what Add reports back: bytes of the generated .ovpn so
// the CLI can choose to print or write them, plus the metadata stored
// in the DB.
type AddResult struct {
	Record Record
	OVPN   []byte
}

// Add creates the system user, signs a client cert, exports the .ovpn,
// and persists the metadata. Each step is the responsibility of one
// helper; this function owns the rollback when a later step fails.
func Add(in AddInput, ovpnIn OVPNInput) (AddResult, error) {
	var res AddResult

	if os.Geteuid() != 0 {
		return res, errors.New("user add requires root; rerun under sudo")
	}
	if err := ValidateName(in.Name); err != nil {
		return res, fmt.Errorf("%s: %w", in.Name, err)
	}
	if in.Password == "" {
		return res, errors.New("password is required")
	}

	exists, err := SystemUserExists(in.Name)
	if err != nil {
		return res, err
	}
	if exists {
		return res, fmt.Errorf("system user %q already exists", in.Name)
	}

	// PKI must be initialized first. We load the CA early so we can
	// fail before touching /etc/passwd if pki init hasn't been run.
	ca, err := pki.LoadCA()
	if err != nil {
		return res, err
	}

	// Compute expiry. Now+N days, then truncate to start-of-day so the
	// stored value matches what useradd -e sees.
	var expiresAt time.Time
	if in.ExpiresInDays > 0 {
		expiresAt = time.Now().UTC().Add(time.Duration(in.ExpiresInDays) * 24 * time.Hour).Truncate(24 * time.Hour)
	}

	// 1. System user. Earliest rollback point - delete it if any later
	// step fails so we don't leave half-installed entries in /etc/passwd.
	if err := CreateSystemUser(in.Name, expiresAt); err != nil {
		return res, fmt.Errorf("create system user: %w", err)
	}
	// rollback path: if anything below errors, delete the user we just
	// added. Defer keeps the cleanup near the creation so future
	// maintainers don't accidentally drop it.
	committed := false
	defer func() {
		if !committed {
			_ = DeleteSystemUser(in.Name)
		}
	}()

	if err := SetPassword(in.Name, in.Password); err != nil {
		return res, fmt.Errorf("set password: %w", err)
	}

	// 2. Sign client cert against the CA.
	clientCert, err := pki.GenerateClientCert(ca, in.Name)
	if err != nil {
		return res, fmt.Errorf("sign client cert: %w", err)
	}
	if err := os.MkdirAll(pki.ClientsDir, 0o700); err != nil {
		return res, fmt.Errorf("mkdir %s: %w", pki.ClientsDir, err)
	}
	certPath := pki.ClientsDir + "/" + in.Name + ".crt"
	keyPath := pki.ClientsDir + "/" + in.Name + ".key"
	if err := os.WriteFile(certPath, clientCert.CertPEM, 0o644); err != nil {
		return res, fmt.Errorf("write %s: %w", certPath, err)
	}
	if err := os.WriteFile(keyPath, clientCert.KeyPEM, 0o600); err != nil {
		return res, fmt.Errorf("write %s: %w", keyPath, err)
	}

	// 3. Build the .ovpn export.
	ovpnIn.Username = in.Name
	ovpn, err := BuildOVPN(ovpnIn)
	if err != nil {
		return res, fmt.Errorf("build ovpn: %w", err)
	}
	res.OVPN = ovpn

	// 4. Persist HEXPLUS metadata.
	db, err := Load()
	if err != nil {
		return res, fmt.Errorf("load user db: %w", err)
	}
	rec := Record{
		Name:      in.Name,
		CreatedAt: time.Now().UTC().Truncate(time.Second),
		ExpiresAt: expiresAt,
		Limit:     in.Limit,
	}
	db.Users[in.Name] = rec
	if err := db.Save(); err != nil {
		return res, fmt.Errorf("save user db: %w", err)
	}
	res.Record = rec

	committed = true
	return res, nil
}

// Remove deletes the system user, removes the per-user PKI material,
// and drops the DB row. Missing pieces are tolerated so the call is
// idempotent (you can re-run remove on a half-removed user).
func Remove(name string) error {
	if os.Geteuid() != 0 {
		return errors.New("user remove requires root; rerun under sudo")
	}
	if err := DeleteSystemUser(name); err != nil {
		return err
	}
	for _, p := range []string{
		pki.ClientsDir + "/" + name + ".crt",
		pki.ClientsDir + "/" + name + ".key",
	} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("remove %s: %w", p, err)
		}
	}
	db, err := Load()
	if err != nil {
		return err
	}
	delete(db.Users, name)
	return db.Save()
}

// List returns the sorted DB rows so the CLI can render them without
// reaching into the user package's internals.
func List() ([]Record, error) {
	db, err := Load()
	if err != nil {
		return nil, err
	}
	return db.All(), nil
}

// Export reads the on-disk PKI artifacts for an existing user and
// re-builds the .ovpn. Used when the seller needs to re-send the
// config after creation.
func Export(name string, ovpnIn OVPNInput) ([]byte, error) {
	ovpnIn.Username = name
	return BuildOVPN(ovpnIn)
}

// UpdateExpiry resets the account's expiry to (today UTC + days). Passing
// days <= 0 marks the account as never expiring (chage -E -1) and clears
// the ExpiresAt on the JSON record. We sync the system + DB pair so the
// menu's "หมดอายุ" column never disagrees with /etc/shadow.
func UpdateExpiry(name string, days int) error {
	if os.Geteuid() != 0 {
		return errors.New("user update-expiry requires root; rerun under sudo")
	}
	var expires time.Time
	if days > 0 {
		expires = time.Now().UTC().Add(time.Duration(days) * 24 * time.Hour).Truncate(24 * time.Hour)
	}
	if err := ChageExpiry(name, expires); err != nil {
		return err
	}
	db, err := Load()
	if err != nil {
		return err
	}
	rec, ok := db.Users[name]
	if !ok {
		// No DB row (user predates HEXPLUS, or imported manually). Create
		// one so the menu list keeps the new expiry.
		rec = Record{Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	}
	rec.ExpiresAt = expires
	db.Users[name] = rec
	return db.Save()
}

// UpdateLimit changes only the Limit field on the JSON record. The system
// account doesn't get touched - HEXPLUS enforces the cap at the proxy/SSH
// layer, not via /etc/passwd.
func UpdateLimit(name string, limit int) error {
	db, err := Load()
	if err != nil {
		return err
	}
	rec, ok := db.Users[name]
	if !ok {
		rec = Record{Name: name, CreatedAt: time.Now().UTC().Truncate(time.Second)}
	}
	rec.Limit = limit
	db.Users[name] = rec
	return db.Save()
}

// UpdatePassword resets the system password via chpasswd. No DB row
// changes - we deliberately don't store the password, only the hash in
// /etc/shadow that SetPassword puts there.
func UpdatePassword(name, password string) error {
	if os.Geteuid() != 0 {
		return errors.New("user passwd requires root; rerun under sudo")
	}
	if password == "" {
		return errors.New("password is required")
	}
	return SetPassword(name, password)
}

// CleanExpired walks user.List() and removes anyone whose ExpiresAt is
// strictly before now. Returns the names that were dropped so the caller
// can show "ลบแล้ว N user".
//
// Records with a zero ExpiresAt are "never expire" and skipped.
func CleanExpired() ([]string, error) {
	if os.Geteuid() != 0 {
		return nil, errors.New("user clean-expired requires root; rerun under sudo")
	}
	all, err := List()
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	var removed []string
	for _, r := range all {
		if r.ExpiresAt.IsZero() || !r.ExpiresAt.Before(now) {
			continue
		}
		if err := Remove(r.Name); err != nil {
			return removed, fmt.Errorf("remove %s: %w", r.Name, err)
		}
		removed = append(removed, r.Name)
	}
	return removed, nil
}
