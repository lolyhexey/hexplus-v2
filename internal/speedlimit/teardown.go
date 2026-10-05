package speedlimit

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// IFBStatePath lists the ifb devices the learn-address hook created, one
// per line (see IFB_STATE in the script).
const IFBStatePath = paths.StateDir + "/speedlimit-ifb"

var (
	ifbName      = regexp.MustCompile(`^ifb[0-9]+$`)
	mirredTarget = regexp.MustCompile(`[Rr]edirect to device (ifb[0-9]+)`)
)

// Overridable for tests.
var (
	ifbStatePath = IFBStatePath
	scriptPath   = ScriptPath
	confPath     = ConfPath
	sysClassNet  = "/sys/class/net"
	runCmd       = func(name string, args ...string) ([]byte, error) {
		return exec.Command(name, args...).CombinedOutput()
	}
)

// Teardown removes everything the shaper put on the host: the ifb devices
// (they outlive OpenVPN and stay up until reboot), the learn-address hook,
// its state and config, and the learn-address lines in server*.conf. Call
// it while OpenVPN still runs: an ifb created by a release that did not
// record its devices is only recognisable through the redirect filter on
// its live tun device.
func Teardown() error {
	tuns, _ := filepath.Glob(filepath.Join(sysClassNet, "tun*"))
	owned := recordedIFBs()
	for _, t := range tuns {
		owned = append(owned, redirectTargets(filepath.Base(t))...)
	}
	var errs []error
	for _, ifb := range dedupe(owned) {
		if err := deleteIFB(ifb); err != nil {
			errs = append(errs, err)
		}
	}
	if err := StripServerConf(); err != nil {
		errs = append(errs, err)
	}
	for _, p := range []string{ifbStatePath, scriptPath, confPath} {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ReleaseTun deletes the ifb device that shapes uploads arriving on dev
// (tunN), for removing one OpenVPN instance. The device is ours if the
// redirect filter on dev points at it, or if the hook recorded creating
// the matching ifbN. Call it before the instance stops.
func ReleaseTun(dev string) error {
	n := strings.TrimPrefix(dev, "tun")
	if n == dev || n == "" {
		return nil
	}
	cands := redirectTargets(dev)
	for _, r := range recordedIFBs() {
		if r == "ifb"+n {
			cands = append(cands, r)
		}
	}
	var errs []error
	for _, ifb := range dedupe(cands) {
		if err := deleteIFB(ifb); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// deleteIFB removes one ifb device and its line in the state file. A
// device that no longer exists counts as removed.
func deleteIFB(ifb string) error {
	if !ifbName.MatchString(ifb) {
		return nil
	}
	if _, err := os.Stat(filepath.Join(sysClassNet, ifb)); err == nil {
		if out, err := runCmd("ip", "link", "del", ifb); err != nil {
			return errors.New("ip link del " + ifb + ": " + strings.TrimSpace(string(out)))
		}
	}
	return forgetIFB(ifb)
}

func recordedIFBs() []string {
	raw, err := os.ReadFile(ifbStatePath)
	if err != nil {
		return nil
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if l = strings.TrimSpace(l); ifbName.MatchString(l) {
			out = append(out, l)
		}
	}
	return dedupe(out)
}

func forgetIFB(ifb string) error {
	rec := recordedIFBs()
	var kept []string
	for _, r := range rec {
		if r != ifb {
			kept = append(kept, r)
		}
	}
	if len(kept) == len(rec) {
		return nil
	}
	if len(kept) == 0 {
		if err := os.Remove(ifbStatePath); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	return os.WriteFile(ifbStatePath, []byte(strings.Join(kept, "\n")+"\n"), 0o644)
}

// redirectTargets returns the ifb devices the ingress filter on dev
// redirects to, which is how the hook sends uploads through an ifb.
func redirectTargets(dev string) []string {
	out, err := runCmd("tc", "filter", "show", "dev", dev, "parent", "ffff:")
	if err != nil {
		return nil
	}
	var ifbs []string
	for _, m := range mirredTarget.FindAllStringSubmatch(string(out), -1) {
		ifbs = append(ifbs, m[1])
	}
	return ifbs
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}
