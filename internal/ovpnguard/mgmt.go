package ovpnguard

import (
	"bufio"
	"errors"
	"fmt"
	"net"
	"strings"
	"time"
)

// mgmtTimeout bounds every exchange with an OpenVPN management socket, so a
// stuck or restarting instance cannot hang the guard.
const mgmtTimeout = 5 * time.Second

type mgmtConn struct {
	conn net.Conn
	r    *bufio.Reader
}

func dialMgmt(path string) (*mgmtConn, error) {
	c, err := net.DialTimeout("unix", path, mgmtTimeout)
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(mgmtTimeout))
	return &mgmtConn{conn: c, r: bufio.NewReader(c)}, nil
}

func (m *mgmtConn) Close() error {
	_, _ = m.conn.Write([]byte("quit\n"))
	return m.conn.Close()
}

// readLine returns the next line that is not an asynchronous notification
// (those start with '>', e.g. the >INFO greeting or >LOG lines).
func (m *mgmtConn) readLine() (string, error) {
	for {
		l, err := m.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		l = strings.TrimRight(l, "\r\n")
		if strings.HasPrefix(l, ">") {
			continue
		}
		return l, nil
	}
}

// status returns the body of `status 2` (up to, not including, END).
func (m *mgmtConn) status() ([]string, error) {
	if _, err := m.conn.Write([]byte("status 2\n")); err != nil {
		return nil, err
	}
	var lines []string
	for {
		l, err := m.readLine()
		if err != nil {
			return nil, err
		}
		if l == "END" {
			return lines, nil
		}
		if strings.HasPrefix(l, "ERROR:") {
			return nil, errors.New(l)
		}
		lines = append(lines, l)
	}
}

// clientKill ends one session. HALT tells the client to stop instead of
// reconnecting straight away (the default RESTART made a kicked device come
// back within seconds, which turns a limit into a ping-pong).
func (m *mgmtConn) clientKill(cid uint64) error {
	if _, err := fmt.Fprintf(m.conn, "client-kill %d HALT\n", cid); err != nil {
		return err
	}
	l, err := m.readLine()
	if err != nil {
		return err
	}
	if strings.HasPrefix(l, "SUCCESS:") {
		return nil
	}
	return errors.New(l)
}
