package panel

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// api_reality.go: /api/reality/scan — probe a list of candidate hosts
// to see which ones are valid Reality decoys. A decoy is "good" if it
//   • completes a TLS 1.3 handshake
//   • offers h2 in ALPN
//   • does NOT redirect to a different SNI (matches its own cert)
//
// Runs the probes in parallel with a per-host timeout, so a list of 30
// hosts finishes in ~3 seconds instead of 30.

func (s *Server) registerRealityRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/reality/scan", s.auth.RequireSession(s.handleRealityScan))
}

type realityScanInput struct {
	Targets []string `json:"targets"`
}

type realityScanResult struct {
	Host    string `json:"host"`
	OK      bool   `json:"ok"`
	Reason  string `json:"reason,omitempty"`
	TLSVer  string `json:"tls_version,omitempty"`
	ALPN    string `json:"alpn,omitempty"`
	RTTms   int64  `json:"rtt_ms,omitempty"`
}

func (s *Server) handleRealityScan(w http.ResponseWriter, r *http.Request) {
	var in realityScanInput
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, errBody(err))
		return
	}
	if len(in.Targets) == 0 {
		in.Targets = defaultRealityTargets()
	}
	if len(in.Targets) > 40 {
		in.Targets = in.Targets[:40] // cap so a bad payload can't fan out huge
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	results := make([]realityScanResult, len(in.Targets))
	var wg sync.WaitGroup
	for i, host := range in.Targets {
		wg.Add(1)
		go func(i int, host string) {
			defer wg.Done()
			results[i] = probeReality(ctx, host)
		}(i, host)
	}
	wg.Wait()

	// Sort: OK first, then by lowest RTT.
	for i := 0; i < len(results); i++ {
		for j := i + 1; j < len(results); j++ {
			a, b := results[i], results[j]
			if (a.OK != b.OK && b.OK) || (a.OK == b.OK && a.RTTms > b.RTTms && b.RTTms > 0) {
				results[i], results[j] = results[j], results[i]
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true, "results": results,
	})
}

func probeReality(ctx context.Context, host string) realityScanResult {
	res := realityScanResult{Host: host}
	// Default to :443 when caller didn't include a port.
	target := host
	if !strings.Contains(host, ":") {
		target = host + ":443"
	}
	sni, _, err := net.SplitHostPort(target)
	if err != nil {
		res.Reason = "bad host"
		return res
	}

	start := time.Now()
	dialer := &net.Dialer{Timeout: 4 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", target)
	if err != nil {
		res.Reason = "connect: " + err.Error()
		return res
	}
	defer conn.Close()

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName: sni,
		NextProtos: []string{"h2", "http/1.1"},
		MinVersion: tls.VersionTLS13,
	})
	if err := tlsConn.HandshakeContext(ctx); err != nil {
		res.Reason = "tls: " + err.Error()
		return res
	}
	res.RTTms = time.Since(start).Milliseconds()

	state := tlsConn.ConnectionState()
	if state.Version != tls.VersionTLS13 {
		res.Reason = "not TLS 1.3"
		res.TLSVer = tlsVerString(state.Version)
		return res
	}
	res.TLSVer = "TLS 1.3"
	res.ALPN = state.NegotiatedProtocol
	if res.ALPN != "h2" {
		res.Reason = "no h2 (got '" + res.ALPN + "')"
		return res
	}
	res.OK = true
	return res
}

func tlsVerString(v uint16) string {
	switch v {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	}
	return "unknown"
}

// defaultRealityTargets — same short list of decoys shipped by 3x-ui's
// scanner as the starter set.
func defaultRealityTargets() []string {
	return []string{
		"www.microsoft.com",
		"www.apple.com",
		"www.samsung.com",
		"www.nvidia.com",
		"www.cloudflare.com",
		"aws.amazon.com",
		"gateway.icloud.com",
		"www.tesla.com",
		"www.samsung.com",
		"www.lovelive-anime.jp",
		"time.cloudflare.com",
	}
}
