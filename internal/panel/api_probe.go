package panel

import (
	"net/http"
	"strconv"

	"github.com/lolyhexey/hexplus/internal/service"
)

// api_probe.go: read-only helpers the frontend uses to sanity-check a
// port before committing. Prevents the "user types 8888 but squid
// already owns it" flow we hit in early testing.
//
// The port check reads /proc/net/tcp[6]|udp[6] via internal/service,
// so it reflects any process holding the port right now — not just
// hexplus's own units.

func (s *Server) registerProbeRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/probe/port", s.auth.RequireSession(s.handleProbePort))
}

// handleProbePort — GET /api/probe/port?port=8888&proto=tcp
// Response: { port, proto, in_use, owner? }
func (s *Server) handleProbePort(w http.ResponseWriter, r *http.Request) {
	port, err := strconv.Atoi(r.URL.Query().Get("port"))
	if err != nil || port <= 0 || port > 65535 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "bad port"})
		return
	}
	proto := r.URL.Query().Get("proto")
	if proto != "udp" {
		proto = "tcp"
	}
	listening, err := service.ListenStatus(port, proto)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"port":   port,
		"proto":  proto,
		"in_use": listening,
	})
}
