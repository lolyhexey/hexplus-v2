// Package netbridge joins a client connection to a backend connection for the
// SSL TUNNEL and SSLH multiplexer daemons, and makes sure that when one side
// ends the other side is told, instead of lingering until it times out.
package netbridge

import (
	"io"
	"net"
	"sync"
	"time"
)

// dialTimeout bounds the backend dial so an unreachable or backlog-full
// target does not pin the accepted client socket for the OS connect timeout.
var dialTimeout = 10 * time.Second

// Dial connects to the TCP backend at target, giving up after 10 seconds.
func Dial(target string) (net.Conn, error) {
	d := net.Dialer{Timeout: dialTimeout}
	return d.Dial("tcp", target)
}

// Pipe copies bytes both ways between client and backend and closes both
// before it returns.
//
//   - When the client finishes sending cleanly (EOF, TLS close_notify) the
//     backend is half-closed (FIN) so it can end the session, and the reply
//     direction keeps running until the backend is done. Clients that send a
//     request and then wait for the answer keep working.
//   - When reading the client or writing the backend fails (RST, keepalive
//     death, backend gone) both sides are closed at once.
//   - When the backend finishes, cleanly or not, both sides are closed at
//     once; the client is never left waiting on a backend that is gone.
//
// The backend must support CloseWrite (a *net.TCPConn does); otherwise it is
// closed outright.
func Pipe(client, backend net.Conn) {
	closeBoth := func() {
		client.Close()
		backend.Close()
	}
	defer closeBoth()

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if _, err := io.Copy(backend, client); err != nil {
			closeBoth()
			return
		}
		if cw, ok := backend.(interface{ CloseWrite() error }); ok && cw.CloseWrite() == nil {
			return
		}
		closeBoth()
	}()
	go func() {
		defer wg.Done()
		io.Copy(client, backend)
		closeBoth()
	}()
	wg.Wait()
}
