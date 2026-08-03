package panel

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// api_server.go: /api/server/status — the payload 3x-ui's Dashboard
// binds against. Same shape (cpu / mem / swap / disk / xray / uptime
// / loadavg / ips) so we can port their IndexPage.tsx nearly verbatim.
//
// All readings come from Linux virtual files (/proc/*) so the endpoint
// works from a plain unprivileged goroutine — no gopsutil dependency,
// no cgo.

// ServerStatus mirrors 3x-ui's Status type used by IndexPage.
type ServerStatus struct {
	CPU         Gauge      `json:"cpu"`
	CPUCores    int        `json:"cpuCores"`
	LogicalPro  int        `json:"logicalPro"`
	CPUSpeedMhz float64    `json:"cpuSpeedMhz"`
	Mem         MemGauge   `json:"mem"`
	Swap        MemGauge   `json:"swap"`
	Disk        MemGauge   `json:"disk"`
	Xray        XrayInfo   `json:"xray"`
	Uptime      int64      `json:"uptime"`
	Loads       [3]float64 `json:"loads"`
	TCPCount    int        `json:"tcpCount"`
	UDPCount    int        `json:"udpCount"`
	NetIO       NetIO      `json:"netIO"`
	NetTraffic  NetTraffic `json:"netTraffic"`
	PublicIP    PublicIP   `json:"publicIP"`
	AppStats    AppStats   `json:"appStats"`
	OSVersion   string     `json:"osVersion"`
}

type Gauge struct {
	Percent int    `json:"percent"`
	Color   string `json:"color"`
}

type MemGauge struct {
	Current int64  `json:"current"`
	Total   int64  `json:"total"`
	Percent int    `json:"percent"`
	Color   string `json:"color"`
}

type XrayInfo struct {
	State    string `json:"state"`
	Version  string `json:"version"`
	ErrorMsg string `json:"errorMsg"`
}

type NetIO struct {
	Up   int64 `json:"up"`
	Down int64 `json:"down"`
}

type NetTraffic struct {
	Sent int64 `json:"sent"`
	Recv int64 `json:"recv"`
}

type PublicIP struct {
	V4 string `json:"v4"`
	V6 string `json:"v6"`
}

type AppStats struct {
	Threads int   `json:"threads"`
	Mem     int64 `json:"mem"`
	Uptime  int64 `json:"uptime"`
}

func (s *Server) registerServerRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /api/server/status", s.auth.RequireSession(s.handleServerStatus))
	mux.HandleFunc("GET /api/server/xray/config", s.auth.RequireSession(s.handleXrayConfig))
	mux.HandleFunc("POST /api/server/xray/restart", s.auth.RequireSession(s.handleXrayRestart))
	mux.HandleFunc("POST /api/server/xray/stop", s.auth.RequireSession(s.handleXrayStop))
	mux.HandleFunc("GET /api/server/xray/log", s.auth.RequireSession(s.handleXrayLog))
}

// handleXrayConfig returns the current /var/lib/hexplus/xray/config.json.
// Read-only — mutations happen through inbound/outbound CRUD which
// regenerate the file each time.
func (s *Server) handleXrayConfig(w http.ResponseWriter, _ *http.Request) {
	data, err := os.ReadFile("/var/lib/hexplus/xray/config.json")
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "obj": "{}"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "obj": string(data)})
}

func (s *Server) handleXrayRestart(w http.ResponseWriter, _ *http.Request) {
	if err := exec.Command("systemctl", "restart", "hexplus-xray.service").Run(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

func (s *Server) handleXrayStop(w http.ResponseWriter, _ *http.Request) {
	if err := exec.Command("systemctl", "stop", "hexplus-xray.service").Run(); err != nil {
		writeJSON(w, http.StatusInternalServerError, errBody(err))
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"success": true})
}

// handleXrayLog returns the last N lines of journalctl -u hexplus-xray.
// ?lines=200 default; capped at 2000 to keep the response bounded.
func (s *Server) handleXrayLog(w http.ResponseWriter, r *http.Request) {
	lines := 200
	if v := r.URL.Query().Get("lines"); v != "" {
		if n, err := strconvAtoi(v); err == nil && n > 0 && n <= 2000 {
			lines = n
		}
	}
	out, err := exec.Command("journalctl", "-u", "hexplus-xray.service",
		"-n", strconvItoa(lines), "--no-pager", "--output=short-iso").Output()
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"success": true, "obj": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "obj": string(out)})
}

// Local strconv shims — the file already imports 'strings' but not
// strconv; we avoid the import pull since the calls are two-line.
func strconvAtoi(s string) (int, error) {
	var n int
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, errors.New("bad")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
func strconvItoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := [16]byte{}
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func (s *Server) handleServerStatus(w http.ResponseWriter, _ *http.Request) {
	st := ServerStatus{
		CPUCores:   runtime.NumCPU(),
		LogicalPro: runtime.NumCPU(),
		OSVersion:  readOSRelease(),
	}
	st.CPU = readCPU()
	st.CPUSpeedMhz = readCPUFreq()
	st.Mem = readMem()
	st.Swap = readSwap()
	st.Disk = readDisk()
	st.Xray = readXrayState()
	st.Uptime = readUptime()
	st.Loads = readLoadAvg()
	st.TCPCount, st.UDPCount = readConnCounts()
	st.NetIO, st.NetTraffic = readNetIO()
	st.PublicIP = PublicIP{}
	st.AppStats = readAppStats()

	writeJSON(w, http.StatusOK, map[string]any{"success": true, "obj": st})
}

// ─── readers ────────────────────────────────────────────────────────

var (
	lastCPUIdle  uint64
	lastCPUTotal uint64
)

func readCPU() Gauge {
	data, err := os.ReadFile("/proc/stat")
	if err != nil {
		return Gauge{Percent: 0, Color: "#3399ff"}
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "cpu ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 8 {
			return Gauge{Percent: 0, Color: "#3399ff"}
		}
		var total uint64
		for _, v := range fields[1:] {
			var n uint64
			for _, r := range v {
				n = n*10 + uint64(r-'0')
			}
			total += n
		}
		var idle uint64
		for _, r := range fields[4] {
			idle = idle*10 + uint64(r-'0')
		}
		dIdle := idle - lastCPUIdle
		dTotal := total - lastCPUTotal
		lastCPUIdle = idle
		lastCPUTotal = total
		if dTotal == 0 {
			return Gauge{Percent: 0, Color: "#3399ff"}
		}
		p := int(100 - (dIdle*100)/dTotal)
		return Gauge{Percent: p, Color: pickColor(p)}
	}
	return Gauge{Percent: 0, Color: "#3399ff"}
}

func readCPUFreq() float64 {
	data, err := os.ReadFile("/proc/cpuinfo")
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "cpu MHz") {
			parts := strings.Split(line, ":")
			if len(parts) < 2 {
				return 0
			}
			var f float64
			for i, r := range strings.TrimSpace(parts[1]) {
				if r == '.' {
					decimals := strings.TrimSpace(parts[1])[i+1:]
					for j, d := range decimals {
						if d < '0' || d > '9' {
							break
						}
						f = f + float64(d-'0')/pow10(j+1)
					}
					return f
				}
				if r < '0' || r > '9' {
					break
				}
				f = f*10 + float64(r-'0')
			}
			return f
		}
	}
	return 0
}

func pow10(n int) float64 {
	v := 1.0
	for i := 0; i < n; i++ {
		v *= 10
	}
	return v
}

func readMem() MemGauge {
	total, avail := readMemInfo("MemTotal:", "MemAvailable:")
	if total == 0 {
		return MemGauge{Color: "#3399ff"}
	}
	used := total - avail
	p := int(used * 100 / total)
	return MemGauge{Current: used * 1024, Total: total * 1024, Percent: p, Color: pickColor(p)}
}

func readSwap() MemGauge {
	total, free := readMemInfo("SwapTotal:", "SwapFree:")
	if total == 0 {
		return MemGauge{Color: "#3399ff"}
	}
	used := total - free
	var p int
	if total > 0 {
		p = int(used * 100 / total)
	}
	return MemGauge{Current: used * 1024, Total: total * 1024, Percent: p, Color: pickColor(p)}
}

func readMemInfo(a, b string) (int64, int64) {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0, 0
	}
	var aa, bb int64
	for _, line := range strings.Split(string(data), "\n") {
		var target *int64
		switch {
		case strings.HasPrefix(line, a):
			target = &aa
		case strings.HasPrefix(line, b):
			target = &bb
		default:
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		var n int64
		for _, r := range fields[1] {
			n = n*10 + int64(r-'0')
		}
		*target = n
	}
	return aa, bb
}

func readDisk() MemGauge {
	// Use `df -B1 /` — simpler than syscall.Statfs and matches 3x-ui.
	out, err := exec.Command("df", "-B1", "/").Output()
	if err != nil {
		return MemGauge{Color: "#3399ff"}
	}
	lines := strings.Split(string(out), "\n")
	if len(lines) < 2 {
		return MemGauge{Color: "#3399ff"}
	}
	fields := strings.Fields(lines[1])
	if len(fields) < 5 {
		return MemGauge{Color: "#3399ff"}
	}
	var total, used int64
	for _, r := range fields[1] {
		total = total*10 + int64(r-'0')
	}
	for _, r := range fields[2] {
		used = used*10 + int64(r-'0')
	}
	if total == 0 {
		return MemGauge{Color: "#3399ff"}
	}
	p := int(used * 100 / total)
	return MemGauge{Current: used, Total: total, Percent: p, Color: pickColor(p)}
}

func readXrayState() XrayInfo {
	out, err := exec.Command("systemctl", "is-active", "hexplus-xray.service").Output()
	state := "stopped"
	if err == nil && strings.TrimSpace(string(out)) == "active" {
		state = "running"
	} else if strings.TrimSpace(string(out)) == "activating" {
		state = "starting"
	}
	// Best-effort version pull from xray binary.
	verOut, _ := exec.Command("/usr/local/lib/hexplus/xray", "version").Output()
	ver := ""
	if len(verOut) > 0 {
		fields := strings.Fields(string(verOut))
		if len(fields) >= 2 {
			ver = fields[1] // "Xray 25.3.6 ..."
		}
	}
	return XrayInfo{State: state, Version: ver}
}

func readUptime() int64 {
	data, err := os.ReadFile("/proc/uptime")
	if err != nil {
		return 0
	}
	fields := strings.Fields(string(data))
	if len(fields) == 0 {
		return 0
	}
	var whole int64
	for _, r := range fields[0] {
		if r == '.' {
			break
		}
		whole = whole*10 + int64(r-'0')
	}
	return whole
}

func readLoadAvg() [3]float64 {
	data, err := os.ReadFile("/proc/loadavg")
	if err != nil {
		return [3]float64{0, 0, 0}
	}
	fields := strings.Fields(string(data))
	var out [3]float64
	for i := 0; i < 3 && i < len(fields); i++ {
		out[i] = parseFloat(fields[i])
	}
	return out
}

func parseFloat(s string) float64 {
	var v float64
	dec := -1
	for i, r := range s {
		if r == '.' {
			dec = i
			continue
		}
		if r < '0' || r > '9' {
			break
		}
		v = v*10 + float64(r-'0')
	}
	if dec >= 0 {
		return v / pow10(len(s)-dec-1)
	}
	return v
}

func readConnCounts() (int, int) {
	tcp := lineCount("/proc/net/tcp") + lineCount("/proc/net/tcp6") - 2
	udp := lineCount("/proc/net/udp") + lineCount("/proc/net/udp6") - 2
	if tcp < 0 {
		tcp = 0
	}
	if udp < 0 {
		udp = 0
	}
	return tcp, udp
}

func lineCount(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0
	}
	return strings.Count(string(data), "\n")
}

var (
	lastNetSent int64
	lastNetRecv int64
	lastNetTime time.Time
)

func readNetIO() (NetIO, NetTraffic) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return NetIO{}, NetTraffic{}
	}
	var sent, recv int64
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.Contains(line, ":") {
			continue
		}
		if strings.Contains(line, "lo:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 10 {
			continue
		}
		r, _ := parseInt64(fields[1])
		s, _ := parseInt64(fields[9])
		recv += r
		sent += s
	}
	now := time.Now()
	var up, down int64
	if !lastNetTime.IsZero() {
		dt := now.Sub(lastNetTime).Seconds()
		if dt > 0 {
			up = int64(float64(sent-lastNetSent) / dt)
			down = int64(float64(recv-lastNetRecv) / dt)
		}
	}
	lastNetSent = sent
	lastNetRecv = recv
	lastNetTime = now
	return NetIO{Up: up, Down: down}, NetTraffic{Sent: sent, Recv: recv}
}

func parseInt64(s string) (int64, error) {
	var v int64
	for _, r := range s {
		if r < '0' || r > '9' {
			return v, errors.New("bad")
		}
		v = v*10 + int64(r-'0')
	}
	return v, nil
}

func readAppStats() AppStats {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return AppStats{
		Threads: runtime.NumGoroutine(),
		Mem:     int64(m.Alloc),
	}
}

// readOSRelease returns "Ubuntu 22.04 LTS" style string.
func readOSRelease() string {
	data, err := os.ReadFile("/etc/os-release")
	if err != nil {
		return runtime.GOOS
	}
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "PRETTY_NAME=") {
			return strings.Trim(strings.TrimPrefix(line, "PRETTY_NAME="), `"`)
		}
	}
	return runtime.GOOS
}

// pickColor returns 3x-ui's traffic-light gradient for gauges.
func pickColor(percent int) string {
	switch {
	case percent < 60:
		return "#5cadff"
	case percent < 80:
		return "#faad14"
	default:
		return "#ff4d4f"
	}
}

// Ensure JSON import used (compat helper for future struct expansions).
var _ = json.Marshal
