// portcheck.go: parse /proc/net/{tcp,tcp6,udp,udp6} to answer
// "is anyone listening on this port?" without shelling out to
// netstat/ss (which may not be installed and add ~5 MB to our embed).
//
// /proc/net/tcp* and /proc/net/udp* share a column layout - we only
// need columns 1 (local_address) and 3 (state for tcp) - so a single
// scanner serves all four files.

package service

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// tcpStateListen is the proto value /proc reports for sockets in LISTEN.
// (Values from include/net/tcp_states.h: ESTABLISHED=01, ... LISTEN=0A.)
const tcpStateListen = "0A"

// ListenStatus answers whether 'port' (treated as proto/PortProto) has
// at least one socket in a state where it'd accept incoming traffic.
// For TCP that means LISTEN; for UDP any bound socket counts.
//
// We check both v4 and v6 tables - a service listening on :: only
// shows up in tcp6/udp6 but still accepts v4 traffic on most modern
// kernels.
func ListenStatus(port int, proto string) (bool, error) {
	var files []string
	switch strings.ToLower(proto) {
	case "tcp":
		files = []string{"/proc/net/tcp", "/proc/net/tcp6"}
	case "udp":
		files = []string{"/proc/net/udp", "/proc/net/udp6"}
	default:
		return false, errors.New("unknown proto: " + proto)
	}

	wantHex := strings.ToUpper(formatPortHex(port))
	wantTCP := strings.ToLower(proto) == "tcp"

	for _, path := range files {
		f, err := os.Open(path)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue // ipv6 disabled, etc.
			}
			return false, err
		}
		listening, err := scanProcNet(f, wantHex, wantTCP)
		f.Close()
		if err != nil {
			return false, err
		}
		if listening {
			return true, nil
		}
	}
	return false, nil
}

// scanProcNet does the line walk for one /proc/net file. Splits on
// runs of whitespace because the file is column-padded, not tab-
// separated, and the padding varies across kernels.
func scanProcNet(f *os.File, wantHex string, isTCP bool) (bool, error) {
	sc := bufio.NewScanner(f)
	first := true
	for sc.Scan() {
		if first {
			first = false // header row
			continue
		}
		fields := strings.Fields(sc.Text())
		if len(fields) < 4 {
			continue
		}
		// fields[1] = "local_addr:port" in hex, e.g. "0100007F:1F90"
		// (= 127.0.0.1:8080 little-endian).
		addr := fields[1]
		idx := strings.LastIndex(addr, ":")
		if idx < 0 {
			continue
		}
		gotPort := strings.ToUpper(addr[idx+1:])
		if gotPort != wantHex {
			continue
		}
		if isTCP && fields[3] != tcpStateListen {
			continue
		}
		return true, nil
	}
	return false, sc.Err()
}

// formatPortHex turns 8080 into "1F90", the layout /proc/net uses.
func formatPortHex(port int) string {
	return strings.ToUpper(strconv.FormatInt(int64(port), 16))
}
