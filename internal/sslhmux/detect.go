package sslhmux

import (
	"net"
	"os"
	"strconv"
	"strings"

	"github.com/lolyhexey/hexplus/internal/ssltunnel"
)

// The multiplexer does not keep backend addresses of its own. Each one is
// derived from the config of the service behind it (sshd, Squid, OpenVPN,
// SSL TUNNEL), read when a connection arrives, so a port changed through the
// menus or by hand takes effect without restarting the multiplexer and
// dropping its live sessions.

// Overridable for tests.
var (
	sshdConfigPath   = "/etc/ssh/sshd_config"
	squidConfPath    = "/etc/squid/squid.conf"
	openvpnConfPaths = []string{"/etc/openvpn/server.conf", "/etc/openvpn/server/server.conf"}
	sslTunnelPort    = func() int {
		cfg, err := ssltunnel.Load()
		if err != nil {
			return 0
		}
		return cfg.Port
	}
)

const loopbackHost = "127.0.0.1"

// Detect returns the live backend of every protocol; Port is left zero.
func Detect() Config {
	return Config{
		SSH:     DetectSSH(),
		SSL:     DetectSSL(),
		HTTP:    DetectHTTP(),
		OpenVPN: DetectOpenVPN(),
	}
}

// DetectSSH returns the first Port of sshd_config, 127.0.0.1:22 when none.
func DetectSSH() string {
	port := 22
	if data, err := os.ReadFile(sshdConfigPath); err == nil {
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || !strings.EqualFold(f[0], "Port") {
				continue
			}
			if n, ok := parsePort(f[1]); ok {
				port = n
				break
			}
		}
	}
	return net.JoinHostPort(loopbackHost, strconv.Itoa(port))
}

// DetectSSL returns the SSL TUNNEL listener, or "" when SSL TUNNEL is not
// installed: nothing else terminates TLS here, so a TLS client is dropped
// instead of being handed to a service that cannot speak it.
func DetectSSL() string {
	port := sslTunnelPort()
	if port <= 0 {
		return ""
	}
	return net.JoinHostPort(loopbackHost, strconv.Itoa(port))
}

// DetectHTTP returns the first plain forward-proxy http_port of squid.conf,
// or "" when Squid has none. A wildcard bind address is dialled on loopback.
func DetectHTTP() string {
	data, err := os.ReadFile(squidConfPath)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] != "http_port" || reverseProxyOnly(f[2:]) {
			continue
		}
		host, portStr := loopbackHost, f[1]
		if h, p, err := net.SplitHostPort(f[1]); err == nil {
			host, portStr = h, p
		}
		port, ok := parsePort(portStr)
		if !ok {
			continue
		}
		if ip := net.ParseIP(host); host == "" || (ip != nil && ip.IsUnspecified()) {
			host = loopbackHost
		}
		return net.JoinHostPort(host, strconv.Itoa(port))
	}
	return ""
}

// reverseProxyOnly reports http_port options that make the port unusable as
// the forward proxy clients of the multiplexer expect.
func reverseProxyOnly(opts []string) bool {
	for _, o := range opts {
		switch strings.SplitN(o, "=", 2)[0] {
		case "intercept", "tproxy", "transparent", "accel":
			return true
		}
	}
	return false
}

// DetectOpenVPN returns the primary instance's port from server.conf (the
// last valid "port" wins, as in OpenVPN), 127.0.0.1:1194 when it has none.
func DetectOpenVPN() string {
	port := 1194
	for _, path := range openvpnConfPaths {
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		for _, line := range strings.Split(string(data), "\n") {
			f := strings.Fields(line)
			if len(f) < 2 || f[0] != "port" {
				continue
			}
			if n, ok := parsePort(f[1]); ok {
				port = n
			}
		}
		break
	}
	return net.JoinHostPort(loopbackHost, strconv.Itoa(port))
}

func parsePort(s string) (int, bool) {
	n, err := strconv.Atoi(s)
	return n, err == nil && n >= 1 && n <= 65535
}

// backendFor returns the live address for a protocol kind from detect.
func backendFor(kind string) string {
	switch kind {
	case "ssh":
		return DetectSSH()
	case "ssl":
		return DetectSSL()
	case "http":
		return DetectHTTP()
	default:
		return DetectOpenVPN()
	}
}

// pointsAtSelf reports whether target is a loopback address on the
// multiplexer's own port. Following such a target would make the multiplexer
// connect to itself, once per connection, until it ran out of descriptors;
// the port-change menus do not stop an operator from reusing it.
func pointsAtSelf(target string, port int) bool {
	host, portStr, err := net.SplitHostPort(target)
	if err != nil || portStr != strconv.Itoa(port) {
		return false
	}
	ip := net.ParseIP(host)
	return host == "localhost" || (ip != nil && (ip.IsLoopback() || ip.IsUnspecified()))
}
