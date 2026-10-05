package install

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/lolyhexey/hexplus/internal/firewall"
	"github.com/lolyhexey/hexplus/internal/portclaim"
	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/sslhmux"
	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// removeFrontends takes down SSL TUNNEL, SSLH MULTIPLEX and the proxies the
// way their own menus' delete does: stop and disable the units, remove them
// and the services' configs, and close their ports in the firewall unless
// another hexplus service still listens there. None of them is in
// service.All(), so uninstall used to leave their units restarting a binary
// it had deleted. The units go whether or not a config can be read (a config
// may be corrupt or gone); the configs only tell which ports to close.
func removeFrontends() error {
	var errs []error
	disable := func(unit string) { _ = exec.Command("systemctl", "disable", "--now", unit).Run() }
	rm := func(path string) {
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			errs = append(errs, err)
		}
	}
	type port struct {
		owner string
		n     int
	}
	var ports []port

	// Read the ports first: the configs are deleted below.
	if cfg, err := ssltunnel.Load(); err == nil && cfg.Port > 0 {
		ports = append(ports, port{portclaim.SSLTunnel, cfg.Port})
	}
	if cfg, err := sslhmux.Load(); err == nil && cfg.Port > 0 {
		ports = append(ports, port{portclaim.SSLH, cfg.Port})
	}
	if db, err := proxy.Load(); err == nil {
		for _, c := range db.All() {
			ports = append(ports, port{portclaim.Proxy(c.Name), c.Port})
		}
	}

	disable(ssltunnel.UnitName)
	errs = append(errs, ssltunnel.RemoveUnit())
	rm(ssltunnel.CertFile)
	rm(ssltunnel.KeyFile)
	rm(ssltunnel.DBPath)

	disable(sslhmux.UnitName)
	errs = append(errs, sslhmux.RemoveUnit())
	rm(sslhmux.DBPath)

	// Every proxy unit on disk, including any whose entry is missing from a
	// corrupt or hand-edited proxies.json.
	units, _ := filepath.Glob(filepath.Join(service.SystemdUnitDir, "hexplus-proxy-*.service"))
	for _, u := range units {
		disable(filepath.Base(u))
		rm(u)
	}
	if len(units) > 0 {
		_ = exec.Command("systemctl", "daemon-reload").Run()
	}
	rm(proxy.DBPath)

	// The services no longer claim their ports now; another one still might.
	for _, p := range ports {
		if p.n > 0 {
			errs = append(errs, firewall.ClosePort("tcp", p.n, firewall.RCLocalPath, portclaim.HeldByOther(p.owner)))
		}
	}
	return errors.Join(errs...)
}
