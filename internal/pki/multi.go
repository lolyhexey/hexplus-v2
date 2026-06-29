// multi.go: extra CA management for the Multi-Cert feature.
//
// OpenVPN's `ca` directive accepts a PEM bundle: every BEGIN CERTIFICATE
// block in the file is added to the SSL trust store and any client cert
// chaining back to ANY of those CAs is accepted. We exploit this to let
// one OpenVPN instance trust multiple CAs on the same port - no extra
// service, no extra port, no config rewrite.
//
// Layout:
//
//   /etc/openvpn/pki/
//     ca.crt                       <- primary CA (untouched, used to sign server.crt)
//     ca.key                       <- primary CA private key
//     extra/
//       <name>/ca.crt              <- extra CA cert
//       <name>/ca.key              <- extra CA private key
//       <name>/clients/<u>.crt     <- users signed by this extra CA
//       <name>/clients/<u>.key
//   /etc/openvpn/ca.crt            <- bundle: primary + every extra/*/ca.crt
//
// RebuildCABundle reconstructs the top-level bundle from the primary + all
// extras and is the only thing that has to run after add/remove. The primary
// CA can't be removed (server.crt chains off it).

package pki

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// ExtraCADir holds per-CA folders for the Multi-Cert feature.
const ExtraCADir = PKIDir + "/extra"

// ValidateCAName guards against shell-meta / path-traversal in CA names
// that the menu pipes straight into directory paths.
func ValidateCAName(name string) error {
	if name == "" {
		return errors.New("ชื่อ CA ห้ามว่าง")
	}
	if len(name) > 32 {
		return errors.New("ชื่อ CA ยาวเกิน 32 ตัวอักษร")
	}
	for _, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(r >= '0' && r <= '9') || r == '-' || r == '_'
		if !ok {
			return errors.New("ชื่อ CA ใช้ได้เฉพาะ a-z A-Z 0-9 - _")
		}
	}
	return nil
}

// ExtraCAInfo summarizes one extra CA for the list view.
type ExtraCAInfo struct {
	Name        string
	Subject     string
	NotAfter    string
	ClientCount int
}

// ListExtraCAs scans ExtraCADir and returns one ExtraCAInfo per folder
// that has a parseable ca.crt. Missing dir = empty list, not an error.
func ListExtraCAs() ([]ExtraCAInfo, error) {
	entries, err := os.ReadDir(ExtraCADir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read %s: %w", ExtraCADir, err)
	}
	var out []ExtraCAInfo
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		name := e.Name()
		caPath := filepath.Join(ExtraCADir, name, "ca.crt")
		info, err := InspectCert(caPath)
		if err != nil || !info.Present {
			continue
		}
		clients := 0
		if dirEntries, err := os.ReadDir(filepath.Join(ExtraCADir, name, "clients")); err == nil {
			for _, ce := range dirEntries {
				if strings.HasSuffix(ce.Name(), ".crt") {
					clients++
				}
			}
		}
		out = append(out, ExtraCAInfo{
			Name:        name,
			Subject:     info.Subject,
			NotAfter:    info.NotAfter,
			ClientCount: clients,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// CreateExtraCA generates a brand-new CA into ExtraCADir/<name>/ and
// rebuilds the bundle so OpenVPN starts trusting it. Caller restarts
// the service to pick up the new bundle.
func CreateExtraCA(name, commonName, org string, validityYears int) error {
	if os.Geteuid() != 0 {
		return errors.New("create extra CA requires root; rerun under sudo")
	}
	if err := ValidateCAName(name); err != nil {
		return err
	}
	if !IsInitialized() {
		return errors.New("PKI ยังไม่ถูก initialize — ติดตั้ง OPENVPN ก่อน")
	}
	dir := filepath.Join(ExtraCADir, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("CA %q มีอยู่แล้ว", name)
	}
	if commonName == "" {
		commonName = name
	}
	if org == "" {
		org = "lolouch.com"
	}
	validity := time.Duration(0)
	if validityYears > 0 {
		validity = time.Duration(validityYears) * 365 * 24 * time.Hour
	}
	ca, err := GenerateCA(commonName, org, validity)
	if err != nil {
		return fmt.Errorf("generate CA: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "clients"), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), ca.CertPEM, 0o644); err != nil {
		return fmt.Errorf("write ca.crt: %w", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.key"), ca.KeyPEM, 0o600); err != nil {
		return fmt.Errorf("write ca.key: %w", err)
	}
	return RebuildCABundle()
}

// ImportExtraCA installs a CA from caller-supplied PEM bytes (key is
// optional — without a key the CA can verify clients but can't issue new
// certs through hexplus). Then rebuilds the bundle.
func ImportExtraCA(name string, certPEM, keyPEM []byte) error {
	if os.Geteuid() != 0 {
		return errors.New("import extra CA requires root; rerun under sudo")
	}
	if err := ValidateCAName(name); err != nil {
		return err
	}
	if !IsInitialized() {
		return errors.New("PKI ยังไม่ถูก initialize — ติดตั้ง OPENVPN ก่อน")
	}
	if _, err := parseCertPEM(certPEM); err != nil {
		return fmt.Errorf("ca.crt PEM ใช้ไม่ได้: %w", err)
	}
	if len(keyPEM) > 0 {
		if _, err := parseRSAKeyPEM(keyPEM); err != nil {
			return fmt.Errorf("ca.key PEM ใช้ไม่ได้: %w", err)
		}
	}
	dir := filepath.Join(ExtraCADir, name)
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("CA %q มีอยู่แล้ว", name)
	}
	if err := os.MkdirAll(filepath.Join(dir, "clients"), 0o700); err != nil {
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "ca.crt"), certPEM, 0o644); err != nil {
		return fmt.Errorf("write ca.crt: %w", err)
	}
	if len(keyPEM) > 0 {
		if err := os.WriteFile(filepath.Join(dir, "ca.key"), keyPEM, 0o600); err != nil {
			return fmt.Errorf("write ca.key: %w", err)
		}
	}
	return RebuildCABundle()
}

// RemoveExtraCA deletes the per-CA folder and rebuilds the bundle. Idempotent.
func RemoveExtraCA(name string) error {
	if os.Geteuid() != 0 {
		return errors.New("remove extra CA requires root; rerun under sudo")
	}
	if err := ValidateCAName(name); err != nil {
		return err
	}
	dir := filepath.Join(ExtraCADir, name)
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove %s: %w", dir, err)
	}
	return RebuildCABundle()
}

// LoadExtraCA reads <name>'s cert+key for client-cert signing.
func LoadExtraCA(name string) (*Cert, error) {
	if err := ValidateCAName(name); err != nil {
		return nil, err
	}
	dir := filepath.Join(ExtraCADir, name)
	certPEM, err := os.ReadFile(filepath.Join(dir, "ca.crt"))
	if err != nil {
		return nil, fmt.Errorf("read %s/ca.crt: %w", dir, err)
	}
	keyPEM, err := os.ReadFile(filepath.Join(dir, "ca.key"))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("CA %q ไม่มีไฟล์ ca.key — เซ็นต์ user ใหม่ไม่ได้ (CA นำเข้าแบบ verify-only)", name)
		}
		return nil, fmt.Errorf("read %s/ca.key: %w", dir, err)
	}
	return LoadCAFromPEM(certPEM, keyPEM)
}

// ExtraCAClientsDir is the per-CA clients folder where user certs signed
// by that CA live. Used by the user package to read/write per-CA leaves.
func ExtraCAClientsDir(name string) string {
	return filepath.Join(ExtraCADir, name, "clients")
}

// RebuildCABundle concatenates the primary CA cert with every extra/*/ca.crt
// and writes the result to /etc/openvpn/ca.crt (and the PKI mirror), which
// is what server.conf's `ca` directive points at. OpenVPN must be restarted
// afterwards for the new trust store to take effect.
func RebuildCABundle() error {
	primary, err := os.ReadFile(filepath.Join(PKIDir, "ca.crt"))
	if err != nil {
		return fmt.Errorf("read primary ca.crt: %w", err)
	}
	var buf strings.Builder
	buf.Write(trimAndNewline(primary))

	entries, err := os.ReadDir(ExtraCADir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read %s: %w", ExtraCADir, err)
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			names = append(names, e.Name())
		}
	}
	sort.Strings(names)
	for _, name := range names {
		raw, err := os.ReadFile(filepath.Join(ExtraCADir, name, "ca.crt"))
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return fmt.Errorf("read extra %s/ca.crt: %w", name, err)
		}
		buf.Write(trimAndNewline(raw))
	}

	bundle := []byte(buf.String())
	if err := writeFileAtomic(filepath.Join(OpenVPNDir, "ca.crt"), bundle, 0o644); err != nil {
		return err
	}
	return writeFileAtomic(filepath.Join(PKIDir, "ca.crt"), bundle, 0o644)
}

// trimAndNewline strips trailing whitespace and guarantees exactly one
// terminating newline so concatenated PEM blocks don't run into each other.
func trimAndNewline(b []byte) []byte {
	trimmed := strings.TrimRight(string(b), "\r\n \t")
	return []byte(trimmed + "\n")
}

func writeFileAtomic(dest string, data []byte, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return fmt.Errorf("mkdir parent of %s: %w", dest, err)
	}
	tmp := dest + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return fmt.Errorf("write %s: %w", tmp, err)
	}
	if err := os.Rename(tmp, dest); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("rename %s -> %s: %w", tmp, dest, err)
	}
	return nil
}
