package menu

import (
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/lolyhexey/hexplus/internal/ovpninstance"
	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// portChangeConflict refuses a new port something already listens on,
// except the ports the service itself holds (selfPorts). Without it a port
// change rewrote the config and the firewall, restarted a service that
// could not bind, and still reported success.
func portChangeConflict(proto string, next int, selfPorts ...int) error {
	for _, p := range selfPorts {
		if p == next {
			return nil
		}
	}
	return checkPortFree(next, proto)
}

// instanceOnPort finds a registered OpenVPN instance (extra or spread
// worker) on proto/port. A stopped one holds no socket, so checkPortFree
// alone would let the primary take its port.
func instanceOnPort(proto string, port int) (ovpninstance.Instance, bool) {
	insts, _ := ovpninstance.List()
	for _, i := range insts {
		if i.Proto == proto && i.Port == port {
			return i, true
		}
	}
	return ovpninstance.Instance{}, false
}

// waitListening polls for up to 4 s, like the OpenVPN install does: the
// units are Type=simple, so systemctl restart succeeds before the service
// has bound (or failed to bind) its port.
func waitListening(port int, proto string) bool {
	for i := 0; i < 8; i++ {
		time.Sleep(500 * time.Millisecond)
		if ok, _ := service.ListenStatus(port, proto); ok {
			return true
		}
	}
	return false
}

// retargetLoopback rewrites a local "host:port" address from oldPort to
// newPort. Anything that is not a loopback or wildcard address on oldPort
// is returned unchanged, so a hand-set remote target is never touched.
func retargetLoopback(addr string, oldPort, newPort int) (string, bool) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port != strconv.Itoa(oldPort) {
		return addr, false
	}
	local := host == "localhost"
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsUnspecified()) {
		local = true
	}
	if !local {
		return addr, false
	}
	return net.JoinHostPort(host, strconv.Itoa(newPort)), true
}

// followOpenVPNPort points the services that forward to the primary
// OpenVPN port at its new port: SSL TUNNEL's target and the SOCKS OPENVPN
// proxies' default host. (SSLH reads server.conf on every connection.)
// Only running units are restarted (try-restart); a failure is a warning,
// since server.conf is already the source of truth.
func followOpenVPNPort(oldPort, newPort int) {
	if cfg, err := ssltunnel.Load(); err == nil && cfg.Port > 0 {
		if t, ok := retargetLoopback(cfg.Target, oldPort, newPort); ok {
			cfg.Target = t
			if err := cfg.Save(); err != nil {
				fmt.Println(cYelBold + "คำเตือน: ปรับปลายทาง SSL TUNNEL ไม่สำเร็จ: " + err.Error() + cReset)
			} else {
				_ = ssltunnel.WriteUnit(cfg)
				_ = systemctlRun("try-restart", ssltunnel.UnitName)
				fmt.Println(cGrnBold + "ปรับปลายทาง SSL TUNNEL เป็น " + t + cReset)
			}
		}
	}

	db, err := proxy.Load()
	if err != nil {
		return
	}
	var moved []proxy.Config
	for key, c := range db.Proxies {
		if c.Name != "openvpn" && !strings.HasPrefix(c.Name, "openvpn-") {
			continue
		}
		if h, ok := retargetLoopback(c.DefaultHost, oldPort, newPort); ok {
			c.DefaultHost = h
			db.Proxies[key] = c
			moved = append(moved, c)
		}
	}
	if len(moved) == 0 {
		return
	}
	if err := db.Save(); err != nil {
		fmt.Println(cYelBold + "คำเตือน: ปรับปลายทาง PROXY ไม่สำเร็จ: " + err.Error() + cReset)
		return
	}
	for _, c := range moved {
		_, _, _, _ = proxy.WriteUnit(c)
		_ = systemctlRun("try-restart", c.UnitName())
		fmt.Println(cGrnBold + "ปรับปลายทาง PROXY " + c.Name + " เป็น " + c.DefaultHost + cReset)
	}
}
