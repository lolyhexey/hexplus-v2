// Package ovpninstance manages extra OpenVPN instances beyond the primary
// /etc/openvpn/server.conf one — each with its own port, protocol, tun
// device, and /16 subnet, but sharing the primary instance's PKI and auth
// so every existing user credential works on every port.
//
// Layout per instance (id ≥ 2):
//
//	/etc/openvpn/server<id>.conf        config (dev tun<id>, 10.<8+id-1>.0.0/16)
//	hexplus-openvpn<id>.service         systemd unit
//	/var/log/openvpn-status<id>.log     status log (read by the online counter)
//
// The registry at /etc/openvpn/hexplus-instances.json is the source of
// truth for which instances exist; conf/unit files are derived from it.
package ovpninstance

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"

	"github.com/lolyhexey/hexplus/internal/paths"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/service"
)

// RegistryPath persists the instance list as JSON.
const RegistryPath = "/etc/openvpn/hexplus-instances.json"

// Instance describes one extra OpenVPN daemon.
type Instance struct {
	ID    int    `json:"id"`    // ≥ 2; primary instance is implicitly 1
	Port  int    `json:"port"`  //
	Proto string `json:"proto"` // "tcp" or "udp"
}

// UnitName returns the systemd unit for this instance.
func (i Instance) UnitName() string { return fmt.Sprintf("hexplus-openvpn%d.service", i.ID) }

// ConfPath returns the OpenVPN config path for this instance.
func (i Instance) ConfPath() string { return fmt.Sprintf("/etc/openvpn/server%d.conf", i.ID) }

// Subnet returns the instance's client subnet in CIDR form.
func (i Instance) Subnet() string { return fmt.Sprintf("10.%d.0.0/16", 8+i.ID-1) }

// svc builds the service.Service descriptor used for unit generation and
// start/stop/status calls.
func (i Instance) svc() service.Service {
	return service.Service{
		Name:        fmt.Sprintf("openvpn%d", i.ID),
		DisplayName: fmt.Sprintf("HEXPLUS OpenVPN server #%d (port %d/%s)", i.ID, i.Port, i.Proto),
		UnitName:    i.UnitName(),
		Binary:      paths.LibDir + "/openvpn",
		Args:        []string{"--config", i.ConfPath()},
		Port:        i.Port,
		PortProto:   i.Proto,
		After:       []string{"network-online.target"},
	}
}

// Service exposes the systemd descriptor so the menu can call
// service.Restart / service.Status on an instance.
func (i Instance) Service() service.Service { return i.svc() }

// List loads the registry, sorted by ID. Missing file → empty list.
func List() ([]Instance, error) {
	data, err := os.ReadFile(RegistryPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var list []Instance
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}
	sort.Slice(list, func(a, b int) bool { return list[a].ID < list[b].ID })
	return list, nil
}

func saveRegistry(list []Instance) error {
	sort.Slice(list, func(a, b int) bool { return list[a].ID < list[b].ID })
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	tmp := RegistryPath + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, RegistryPath); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// nextID returns the lowest free instance ID starting at 2.
func nextID(list []Instance) int {
	used := map[int]bool{}
	for _, i := range list {
		used[i.ID] = true
	}
	for id := 2; ; id++ {
		if !used[id] {
			return id
		}
	}
}

// Add creates a new instance: conf + unit + NAT rules + registry entry.
// It does NOT start the unit — the caller drives that through a progress
// step so failures surface in the UI.
func Add(port int, proto string, dnsPush []string) (Instance, error) {
	if proto != "tcp" && proto != "udp" {
		return Instance{}, fmt.Errorf("proto ต้องเป็น tcp หรือ udp")
	}
	list, err := List()
	if err != nil {
		return Instance{}, err
	}
	inst := Instance{ID: nextID(list), Port: port, Proto: proto}

	if err := pki.WriteInstanceConf(inst.ID, port, proto, dnsPush); err != nil {
		return Instance{}, err
	}
	if err := service.WriteUnitFor(inst.svc()); err != nil {
		return Instance{}, err
	}
	setupInstanceNAT(inst)

	list = append(list, inst)
	if err := saveRegistry(list); err != nil {
		return Instance{}, err
	}
	return inst, nil
}

// Remove tears an instance down: stop unit, remove unit + conf + runtime
// files + NAT rules, drop from registry. Idempotent on missing files.
func Remove(id int) error {
	list, err := List()
	if err != nil {
		return err
	}
	idx := -1
	for n, i := range list {
		if i.ID == id {
			idx = n
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("ไม่พบ instance %d", id)
	}
	inst := list[idx]

	_ = exec.Command("systemctl", "disable", "--now", inst.UnitName()).Run()
	_ = service.RemoveUnitFor(inst.svc())
	_ = os.Remove(inst.ConfPath())
	_ = os.Remove(fmt.Sprintf("/etc/openvpn/ipp%d.txt", inst.ID))
	_ = os.Remove(fmt.Sprintf("/var/log/openvpn-status%d.log", inst.ID))
	removeInstanceNAT(inst)

	list = append(list[:idx], list[idx+1:]...)
	return saveRegistry(list)
}

// setupInstanceNAT adds the MASQUERADE rule for the instance subnet and
// persists it in rc.local — same pattern setupNetworking uses for the
// primary 10.8.0.0/16. Best-effort: NAT problems shouldn't abort an add,
// the operator can fix iptables by hand and the instance still runs.
func setupInstanceNAT(inst Instance) {
	rule := []string{"-t", "nat", "-A", "POSTROUTING", "-s", inst.Subnet(), "-j", "MASQUERADE"}
	// -C checks existence; only append when absent so re-adds don't stack.
	check := []string{"-t", "nat", "-C", "POSTROUTING", "-s", inst.Subnet(), "-j", "MASQUERADE"}
	if exec.Command("iptables", check...).Run() != nil {
		_ = exec.Command("iptables", rule...).Run()
	}
	persistRCLocal("iptables -t nat -A POSTROUTING -s " + inst.Subnet() + " -j MASQUERADE")
}

func removeInstanceNAT(inst Instance) {
	_ = exec.Command("iptables", "-t", "nat", "-D", "POSTROUTING",
		"-s", inst.Subnet(), "-j", "MASQUERADE").Run()
	unpersistRCLocal("iptables -t nat -A POSTROUTING -s " + inst.Subnet() + " -j MASQUERADE")
}

const rcLocalPath = "/etc/rc.local"

func persistRCLocal(line string) {
	if _, err := os.Stat(rcLocalPath); errors.Is(err, os.ErrNotExist) {
		_ = os.WriteFile(rcLocalPath, []byte("#!/bin/sh -e\nexit 0\n"), 0o755)
	}
	raw, err := os.ReadFile(rcLocalPath)
	if err != nil {
		return
	}
	content := string(raw)
	if strings.Contains(content, line) {
		return
	}
	// Insert before the final "exit 0" like setupNetworking does; append
	// if there's no exit line.
	lines := strings.Split(strings.TrimRight(content, "\n"), "\n")
	out := make([]string, 0, len(lines)+1)
	inserted := false
	for _, l := range lines {
		if !inserted && strings.TrimSpace(l) == "exit 0" {
			out = append(out, line)
			inserted = true
		}
		out = append(out, l)
	}
	if !inserted {
		out = append(out, line)
	}
	_ = os.WriteFile(rcLocalPath, []byte(strings.Join(out, "\n")+"\n"), 0o755)
}

func unpersistRCLocal(line string) {
	raw, err := os.ReadFile(rcLocalPath)
	if err != nil {
		return
	}
	var out []string
	for _, l := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(l) == line {
			continue
		}
		out = append(out, l)
	}
	_ = os.WriteFile(rcLocalPath, []byte(strings.Join(out, "\n")), 0o755)
}
