// hexplus - single-binary HEXPLUS v2 entry point.
//
// Subcommands:
//
//	(none)         print banner + auto-install if not installed yet
//	version        print version metadata
//	install        idempotent install: extract binaries + copy self to /usr/local/bin
//	uninstall      reverse install (leaves /etc/openvpn etc. alone)
//	extract        dev-only: extract embedded assets to --lib-dir without installing
//	status         report install state + presence of each embedded binary on disk
//
// Service supervision (running openvpn/squid/dropbear), user management, and
// the TUI menu are deliberately not here yet - they come in later phases. This
// file is the install boundary; everything above runs at root once per box,
// everything else runs from the installed location.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/extract"
	"github.com/lolyhexey/hexplus/internal/install"
	"github.com/lolyhexey/hexplus/internal/menu"
	"github.com/lolyhexey/hexplus/internal/pki"
	"github.com/lolyhexey/hexplus/internal/proxy"
	"github.com/lolyhexey/hexplus/internal/service"
	"github.com/lolyhexey/hexplus/internal/user"
	"github.com/lolyhexey/hexplus/internal/version"
)

func main() {
	if len(os.Args) < 2 {
		runDefault()
		return
	}
	cmd, rest := os.Args[1], os.Args[2:]
	switch cmd {
	case "version", "-version", "--version":
		printVersion()
	case "install":
		runInstall()
	case "uninstall":
		runUninstall()
	case "extract":
		runExtract(rest)
	case "status":
		runStatus()
	case "service":
		runService(rest)
	case "logs":
		runLogs(rest)
	case "pki":
		runPKI(rest)
	case "user":
		runUser(rest)
	case "proxy":
		runProxy(rest)
	case "fileserver":
		runFileServer()
	case "menu":
		runMenu()
	case "help", "-h", "--help":
		printUsage(os.Stdout)
	default:
		fmt.Fprintln(os.Stderr, "unknown command:", cmd)
		printUsage(os.Stderr)
		os.Exit(2)
	}
}

func printVersion() {
	fmt.Println(version.Full())
	fmt.Printf("  runtime: %s/%s (%s)\n", runtime.GOOS, runtime.GOARCH, runtime.Version())
}

func printUsage(w *os.File) {
	fmt.Fprintf(w, `hexplus - single-binary SSH+VPN management

Usage:
  hexplus [subcommand]

Subcommands:
  install              extract embedded binaries to %s and copy self to %s
  uninstall            remove what 'install' put down (configs under /etc preserved)
  status               show whether install has been run and which binaries are present
  service <verb> [name]  start/stop/restart/enable/disable/status one or all services
                         (services: openvpn, squid, dropbear)
  logs <name>            tail systemd journal for one service (--follow, --tail N)
  pki init [--force]     generate OpenVPN CA + server cert + ta.key + server.conf
  pki status             show stored PKI material (subject, expiry)
  user add <name>        create a HEXPLUS account (system user + OpenVPN client cert)
  user list              list HEXPLUS users with their metadata
  user remove <name>     delete a user (system + cert + db row)
  user export <name>     re-emit the user's .ovpn file
  proxy add <name>       register a Go-native HTTP CONNECT proxy + systemd unit
  proxy list             show configured proxies
  proxy remove <name>    drop a proxy's config row + systemd unit
  proxy run <name>       foreground server (invoked by systemd)
  menu                   launch the Thai bubbletea TUI (default action when installed)
  extract              dev-only: extract embedded assets to --lib-dir without installing
  version              print version metadata
  help                 this message

With no subcommand, hexplus prints a banner and, on first run as root, auto-installs.
`, install.LibDir, install.SelfPath)
}

func runDefault() {
	runMenu()
}

// runMenu launches the v1-identical plain REPL menu (no altscreen, no
// arrow keys - just clear+print+read like Modulos/menu).
func runMenu() {
	if err := menu.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "menu:", err)
		os.Exit(1)
	}
}

func runInstall() {
	res, err := install.Install()
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	fmt.Println("hexplus installed.")
	if res.SelfCopied {
		fmt.Printf("  + %s\n", install.SelfPath)
	}
	if res.MarkerWritten {
		fmt.Printf("  + %s\n", install.MarkerFile)
	}
	fmt.Println()
	fmt.Println("ขั้นต่อไป: รัน 'hexplus' เพื่อเปิดเมนู - บริการแต่ละตัว (openvpn / squid /")
	fmt.Println("dropbear) ติดตั้งทีละตัวผ่านเมนู 'โหมดฟังชั่น' (ข้อ 10)")
}

func runUninstall() {
	if err := install.Uninstall(); err != nil {
		fmt.Fprintln(os.Stderr, "uninstall:", err)
		os.Exit(1)
	}
	fmt.Println("hexplus uninstalled (state under /etc and /var/lib preserved).")
}

func runStatus() {
	fmt.Println(version.Full())
	if install.IsInstalled() {
		fmt.Printf("installed: yes (marker at %s)\n", install.MarkerFile)
	} else {
		fmt.Println("installed: no")
	}

	fmt.Println()
	fmt.Println("embedded binaries on disk:")
	for _, name := range []string{"openvpn", "squid", "dropbearmulti"} {
		path := install.LibDir + "/" + name
		if st, err := os.Stat(path); err == nil {
			fmt.Printf("  + %s (%s, %d bytes)\n", path, st.Mode(), st.Size())
		} else {
			fmt.Printf("  - %s (missing)\n", path)
		}
	}

	// Service rows: combine systemd state with /proc/net listening check
	// so the user sees both "what systemd thinks" and "what is actually
	// bound to the port" in one place.
	fmt.Println()
	fmt.Println("services:")
	states, err := service.StatusAll()
	if err != nil {
		fmt.Fprintln(os.Stderr, "  (status:", err, ")")
		return
	}
	for _, st := range states {
		printStatusRow(st)
	}
}

// printStatusRow formats one row of the `hexplus status` services table.
// Columns: indicator + name, systemd active/sub, enable state, PID,
// expected port + actual listening result.
func printStatusRow(st service.State) {
	if !st.UnitExists {
		fmt.Printf("  ○ %-8s  unit not installed\n", st.Service.Name)
		return
	}

	enabled := "disabled"
	if st.Enabled {
		enabled = "enabled"
	}
	pid := ""
	if st.MainPID != "" && st.MainPID != "0" {
		pid = " PID " + st.MainPID
	}

	portStatus := "?"
	listening, err := service.ListenStatus(st.Service.Port, st.Service.PortProto)
	switch {
	case err != nil:
		portStatus = "(proc check: " + err.Error() + ")"
	case listening:
		portStatus = "listening"
	default:
		portStatus = "not listening"
	}

	indicator := "○"
	if st.ActiveState == "active" {
		indicator = "●"
	}
	fmt.Printf("  %s %-8s  %s/%s  %s%s  port %d/%s: %s\n",
		indicator, st.Service.Name,
		st.ActiveState, st.SubState,
		enabled, pid,
		st.Service.Port, st.Service.PortProto, portStatus,
	)
}

// runFileServer serves /root/openvpn/ over HTTP on port 82.
// Runs under systemd as hexplus-fileserver.service.
func runFileServer() {
	dir := service.OVPNDir
	_ = os.MkdirAll(dir, 0o700)
	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.Dir(dir)))
	srv := &http.Server{Addr: ":82", Handler: mux}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("fileserver: %v", err)
		}
	}()
	<-ctx.Done()
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_ = srv.Shutdown(shutCtx)
}

// runService dispatches the `hexplus service <verb> [name]` subcommand.
// Verbs that need a target service name (start, stop, restart, enable,
// disable) error if it's missing. status with no name reports all three.
func runService(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus service <verb> [name]")
		fmt.Fprintf(os.Stderr, "  verbs: start, stop, restart, enable, disable, status, list\n")
		fmt.Fprintf(os.Stderr, "  names: %s\n", strings.Join(service.Names(), ", "))
		os.Exit(2)
	}
	verb := args[0]
	target := ""
	if len(args) > 1 {
		target = args[1]
	}

	if verb == "list" {
		for _, s := range service.All() {
			fmt.Printf("  %s\t%s (%d/%s)\n", s.Name, s.UnitName, s.Port, s.PortProto)
		}
		return
	}

	if verb == "status" {
		if target == "" {
			states, err := service.StatusAll()
			if err != nil {
				fmt.Fprintln(os.Stderr, "service status:", err)
				os.Exit(1)
			}
			for _, st := range states {
				printState(st)
			}
			return
		}
		svc, ok := service.ByName(target)
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown service %q (known: %s)\n", target, strings.Join(service.Names(), ", "))
			os.Exit(2)
		}
		st, err := service.Status(svc)
		if err != nil {
			fmt.Fprintln(os.Stderr, "service status:", err)
			os.Exit(1)
		}
		printState(st)
		return
	}

	// Action verbs: must have a target.
	if target == "" {
		fmt.Fprintf(os.Stderr, "verb %q needs a service name (one of: %s)\n",
			verb, strings.Join(service.Names(), ", "))
		os.Exit(2)
	}
	svc, ok := service.ByName(target)
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown service %q (known: %s)\n", target, strings.Join(service.Names(), ", "))
		os.Exit(2)
	}

	var err error
	switch verb {
	case "start":
		err = service.Start(svc)
	case "stop":
		err = service.Stop(svc)
	case "restart":
		err = service.Restart(svc)
	case "enable":
		err = service.Enable(svc)
	case "disable":
		err = service.Disable(svc)
	case "reload":
		err = service.TryReload(svc)
	default:
		fmt.Fprintf(os.Stderr, "unknown verb %q\n", verb)
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "service:", err)
		os.Exit(1)
	}
	fmt.Printf("ok: %s %s\n", verb, svc.UnitName)
}

func printState(st service.State) {
	marker := "●"
	if !st.UnitExists {
		fmt.Printf("  %s %-8s  unit not installed\n", marker, st.Service.Name)
		return
	}
	enabled := "disabled"
	if st.Enabled {
		enabled = "enabled"
	}
	pidPart := ""
	if st.MainPID != "" && st.MainPID != "0" {
		pidPart = " (PID " + st.MainPID + ")"
	}
	fmt.Printf("  %s %-8s  %s/%s  %s%s  expected %d/%s\n",
		marker, st.Service.Name, st.ActiveState, st.SubState, enabled, pidPart,
		st.Service.Port, st.Service.PortProto)
}

// runLogs handles `hexplus logs <name> [--follow] [--tail N]`.
// Streams journalctl's stdout/stderr through to the user; for --follow
// the process blocks until they Ctrl-C.
func runLogs(args []string) {
	fs := flag.NewFlagSet("logs", flag.ExitOnError)
	follow := fs.Bool("follow", false, "stream new entries (-f)")
	fs.BoolVar(follow, "f", false, "alias for --follow")
	tail := fs.Int("tail", 0, "show only the last N entries (0 = all)")
	fs.IntVar(tail, "n", 0, "alias for --tail")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus logs <service> [--follow] [--tail N]")
		fmt.Fprintf(os.Stderr, "  services: %s\n", strings.Join(service.Names(), ", "))
		os.Exit(2)
	}
	svc, ok := service.ByName(rest[0])
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown service %q (known: %s)\n", rest[0], strings.Join(service.Names(), ", "))
		os.Exit(2)
	}
	if err := service.StreamLogs(svc, service.LogsOptions{Follow: *follow, Tail: *tail}); err != nil {
		fmt.Fprintln(os.Stderr, "logs:", err)
		os.Exit(1)
	}
}

// runPKI dispatches `hexplus pki <init|status>`.
func runPKI(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus pki <init|status>")
		os.Exit(2)
	}
	switch args[0] {
	case "init":
		runPKIInit(args[1:])
	case "status":
		runPKIStatus()
	default:
		fmt.Fprintf(os.Stderr, "unknown pki verb %q (known: init, status)\n", args[0])
		os.Exit(2)
	}
}

func runPKIInit(args []string) {
	fs := flag.NewFlagSet("pki init", flag.ExitOnError)
	force := fs.Bool("force", false, "overwrite an existing PKI (invalidates existing client certs)")
	caCN := fs.String("ca-cn", "", "Subject CN for the CA cert (default: HEXPLUS CA)")
	srvCN := fs.String("server-cn", "", "Subject CN for the server cert (default: server)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}

	res, err := pki.Init(pki.InitOptions{
		Force:            *force,
		CACommonName:     *caCN,
		ServerCommonName: *srvCN,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "pki init:", err)
		os.Exit(1)
	}
	fmt.Println("PKI initialized.")
	for _, p := range res.Written {
		fmt.Printf("  + %s\n", p)
	}
	fmt.Println()
	fmt.Println("next: 'hexplus service start openvpn' (after enabling on boot if you want it persistent)")
}

func runPKIStatus() {
	if !pki.IsInitialized() {
		fmt.Println("PKI: not initialized.")
		fmt.Println("run: sudo hexplus pki init")
		return
	}
	fmt.Println("PKI:")
	for _, p := range []string{
		pki.OpenVPNDir + "/ca.crt",
		pki.OpenVPNDir + "/server.crt",
	} {
		info, err := pki.InspectCert(p)
		if err != nil {
			fmt.Printf("  ! %s: %v\n", p, err)
			continue
		}
		if !info.Present {
			fmt.Printf("  - %s (missing)\n", p)
			continue
		}
		fmt.Printf("  + %s\n", p)
		fmt.Printf("      subject:  %s\n", info.Subject)
		fmt.Printf("      issuer:   %s\n", info.Issuer)
		fmt.Printf("      not after: %s\n", info.NotAfter)
	}
	for _, p := range []string{pki.OpenVPNDir + "/server.key", pki.OpenVPNDir + "/ta.key"} {
		if st, err := os.Stat(p); err == nil {
			fmt.Printf("  + %s (mode %s, %d bytes)\n", p, st.Mode(), st.Size())
		} else {
			fmt.Printf("  - %s (missing)\n", p)
		}
	}
}

// runUser dispatches `hexplus user <add|list|remove|export>`.
func runUser(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus user <add|list|remove|export> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		runUserAdd(args[1:])
	case "list":
		runUserList()
	case "remove", "delete", "rm":
		runUserRemove(args[1:])
	case "export":
		runUserExport(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown user verb %q\n", args[0])
		os.Exit(2)
	}
}

func runUserAdd(args []string) {
	fs := flag.NewFlagSet("user add", flag.ExitOnError)
	pw := fs.String("password", "", "user password (required)")
	expDays := fs.Int("expire-days", 0, "days until the account expires (0 = no expiry)")
	limit := fs.Int("limit", 0, "max concurrent connections (0 = no enforced cap)")
	remoteHost := fs.String("remote", "", "OpenVPN server's public address for the .ovpn (defaults to /etc/IP if present, else 127.0.0.1)")
	remotePort := fs.Int("remote-port", 1194, "OpenVPN server port for the .ovpn")
	proto := fs.String("proto", "udp", "OpenVPN proto (udp|tcp)")
	outFile := fs.String("out", "", "write the .ovpn to this path (default: /root/<name>.ovpn)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus user add <name> --password <pw> [--expire-days N] [--limit N]")
		os.Exit(2)
	}
	name := rest[0]
	if *remoteHost == "" {
		*remoteHost = defaultRemoteHost()
	}
	res, err := user.Add(
		user.AddInput{
			Name:          name,
			Password:      *pw,
			ExpiresInDays: *expDays,
			Limit:         *limit,
		},
		user.OVPNInput{
			RemoteHost: *remoteHost,
			RemotePort: *remotePort,
			Proto:      *proto,
		},
	)
	if err != nil {
		fmt.Fprintln(os.Stderr, "user add:", err)
		os.Exit(1)
	}
	dest := *outFile
	if dest == "" {
		dest = "/root/" + name + ".ovpn"
	}
	if err := os.WriteFile(dest, res.OVPN, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write ovpn:", err)
		os.Exit(1)
	}
	fmt.Printf("user %s created.\n", res.Record.Name)
	fmt.Printf("  ovpn config: %s\n", dest)
	if !res.Record.ExpiresAt.IsZero() {
		fmt.Printf("  expires:     %s\n", res.Record.ExpiresAt.Format("2006-01-02"))
	}
	if res.Record.Limit > 0 {
		fmt.Printf("  limit:       %d concurrent connections\n", res.Record.Limit)
	}
}

func runUserList() {
	all, err := user.List()
	if err != nil {
		fmt.Fprintln(os.Stderr, "user list:", err)
		os.Exit(1)
	}
	if len(all) == 0 {
		fmt.Println("no HEXPLUS users.")
		return
	}
	fmt.Printf("%-16s  %-12s  %-12s  %s\n", "NAME", "CREATED", "EXPIRES", "LIMIT")
	for _, r := range all {
		exp := "(none)"
		if !r.ExpiresAt.IsZero() {
			exp = r.ExpiresAt.Format("2006-01-02")
		}
		limit := "-"
		if r.Limit > 0 {
			limit = fmt.Sprintf("%d", r.Limit)
		}
		fmt.Printf("%-16s  %-12s  %-12s  %s\n",
			r.Name, r.CreatedAt.Format("2006-01-02"), exp, limit)
	}
}

func runUserRemove(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus user remove <name>")
		os.Exit(2)
	}
	name := args[0]
	if err := user.Remove(name); err != nil {
		fmt.Fprintln(os.Stderr, "user remove:", err)
		os.Exit(1)
	}
	fmt.Printf("user %s removed.\n", name)
}

func runUserExport(args []string) {
	fs := flag.NewFlagSet("user export", flag.ExitOnError)
	remoteHost := fs.String("remote", "", "OpenVPN server's public address (defaults like 'user add')")
	remotePort := fs.Int("remote-port", 1194, "OpenVPN server port")
	proto := fs.String("proto", "udp", "OpenVPN proto (udp|tcp)")
	outFile := fs.String("out", "", "write to this path (default: stdout)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus user export <name> [--remote host] [--out path]")
		os.Exit(2)
	}
	name := rest[0]
	if *remoteHost == "" {
		*remoteHost = defaultRemoteHost()
	}
	ovpn, err := user.Export(name, user.OVPNInput{
		RemoteHost: *remoteHost,
		RemotePort: *remotePort,
		Proto:      *proto,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "user export:", err)
		os.Exit(1)
	}
	if *outFile == "" {
		os.Stdout.Write(ovpn)
		return
	}
	if err := os.WriteFile(*outFile, ovpn, 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write ovpn:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %s\n", *outFile)
}

// defaultRemoteHost reads /etc/IP if HEXPLUS v1 wrote it (still set up
// by `Install/list`), otherwise falls back to 127.0.0.1. The caller can
// always override via --remote.
func defaultRemoteHost() string {
	if data, err := os.ReadFile("/etc/IP"); err == nil {
		ip := strings.TrimSpace(string(data))
		if ip != "" {
			return ip
		}
	}
	return "127.0.0.1"
}

// runProxy dispatches `hexplus proxy <add|list|remove|run>`.
func runProxy(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus proxy <add|list|remove|run> [...]")
		os.Exit(2)
	}
	switch args[0] {
	case "add":
		runProxyAdd(args[1:])
	case "list":
		runProxyList()
	case "remove", "delete", "rm":
		runProxyRemove(args[1:])
	case "run":
		runProxyRun(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown proxy verb %q\n", args[0])
		os.Exit(2)
	}
}

// proxyPresets are the response shapes the v1 conexao menu offered.
// Picking a preset just fills the StatusCode + StatusMsg defaults.
var proxyPresets = map[string]struct {
	code string
	msg  string
}{
	"101": {"101", `<font color="null">HEXPLUS</font>`},
	"200": {"200", `Connection established\r\nContent-length: 0`},
	"400": {"400", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`},
	"520": {"520", `<font color="null">HEXPLUS</font>\r\nContent-length: 0`},
}

func runProxyAdd(args []string) {
	fs := flag.NewFlagSet("proxy add", flag.ExitOnError)
	port := fs.Int("port", 0, "TCP port to listen on (required)")
	target := fs.String("target", "127.0.0.1:22", "default upstream when X-Real-Host is absent (host:port)")
	preset := fs.String("preset", "101", "status preset: 101 (WebSocket spoof, default), 200, 400, 520")
	code := fs.String("status-code", "", "override status code (digits only)")
	msg := fs.String("status-msg", "", `override status message (literal \r\n becomes CRLF)`)
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	rest := fs.Args()
	if len(rest) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus proxy add --port=N [--target host:port] [--preset 101|200|400|520] <name>")
		os.Exit(2)
	}
	name := rest[0]
	if err := proxy.ValidateName(name); err != nil {
		fmt.Fprintln(os.Stderr, "proxy add:", err)
		os.Exit(2)
	}
	if *port <= 0 || *port > 65535 {
		fmt.Fprintln(os.Stderr, "proxy add: --port is required (1-65535)")
		os.Exit(2)
	}

	chosenCode := *code
	chosenMsg := *msg
	if chosenCode == "" || chosenMsg == "" {
		p, ok := proxyPresets[*preset]
		if !ok {
			fmt.Fprintf(os.Stderr, "unknown --preset %q (try: 101 200 400 520)\n", *preset)
			os.Exit(2)
		}
		if chosenCode == "" {
			chosenCode = p.code
		}
		if chosenMsg == "" {
			chosenMsg = p.msg
		}
	}

	cfg := proxy.Config{
		Name:        name,
		Port:        *port,
		DefaultHost: *target,
		StatusCode:  chosenCode,
		StatusMsg:   chosenMsg,
	}
	// Validate by trying to construct a Handler now - catches port
	// range, etc. before we touch disk.
	if _, err := proxy.NewHandler(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "proxy add:", err)
		os.Exit(1)
	}

	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "proxy add requires root (writes /var/lib/hexplus and /etc/systemd/system)")
		os.Exit(1)
	}
	db, err := proxy.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy add:", err)
		os.Exit(1)
	}
	db.Proxies[name] = cfg
	if err := db.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "proxy add:", err)
		os.Exit(1)
	}
	unitPath, written, reloadErr, err := proxy.WriteUnit(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy add (unit):", err)
		os.Exit(1)
	}
	fmt.Printf("proxy %s registered.\n", cfg.Name)
	fmt.Printf("  config: %s\n", proxy.DBPath)
	if written {
		fmt.Printf("  unit:   %s\n", unitPath)
	} else {
		fmt.Printf("  unit:   %s (already up-to-date)\n", unitPath)
	}
	if reloadErr != nil {
		fmt.Printf("  warning: systemctl daemon-reload: %v\n", reloadErr)
	}
	fmt.Println()
	fmt.Printf("next: systemctl enable --now %s\n", cfg.UnitName())
}

func runProxyList() {
	db, err := proxy.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy list:", err)
		os.Exit(1)
	}
	all := db.All()
	if len(all) == 0 {
		fmt.Println("no proxies configured.")
		return
	}
	fmt.Printf("%-12s  %-6s  %-22s  %-5s  %s\n", "NAME", "PORT", "DEFAULT_HOST", "CODE", "STATUS_MSG")
	for _, c := range all {
		msg := c.StatusMsg
		if len(msg) > 60 {
			msg = msg[:57] + "..."
		}
		fmt.Printf("%-12s  %-6d  %-22s  %-5s  %s\n", c.Name, c.Port, c.DefaultHost, c.StatusCode, msg)
	}
}

func runProxyRemove(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus proxy remove <name>")
		os.Exit(2)
	}
	name := args[0]
	db, err := proxy.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy remove:", err)
		os.Exit(1)
	}
	cfg, ok := db.Proxies[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "no proxy named %q\n", name)
		os.Exit(1)
	}
	delete(db.Proxies, name)
	if err := db.Save(); err != nil {
		fmt.Fprintln(os.Stderr, "proxy remove:", err)
		os.Exit(1)
	}
	unitPath, removed, reloadErr, err := proxy.RemoveUnit(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy remove (unit):", err)
		os.Exit(1)
	}
	fmt.Printf("proxy %s removed.\n", cfg.Name)
	if removed {
		fmt.Printf("  unit: %s removed\n", unitPath)
	} else {
		fmt.Printf("  unit: %s (already gone)\n", unitPath)
	}
	if reloadErr != nil {
		fmt.Printf("  warning: systemctl daemon-reload: %v\n", reloadErr)
	}
}

func runProxyRun(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus proxy run <name>")
		os.Exit(2)
	}
	name := args[0]
	db, err := proxy.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy run:", err)
		os.Exit(1)
	}
	cfg, ok := db.Proxies[name]
	if !ok {
		fmt.Fprintf(os.Stderr, "no proxy named %q\n", name)
		os.Exit(1)
	}
	h, err := proxy.NewHandler(cfg)
	if err != nil {
		fmt.Fprintln(os.Stderr, "proxy run:", err)
		os.Exit(1)
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := h.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "proxy run:", err)
		os.Exit(1)
	}
}

func runExtract(args []string) {
	fs := flag.NewFlagSet("extract", flag.ExitOnError)
	libDir := fs.String("lib-dir", install.LibDir, "where to extract embedded assets")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	res, err := extract.All(assets.Binaries(), *libDir)
	if err != nil {
		fmt.Fprintln(os.Stderr, "extract:", err)
		os.Exit(1)
	}
	fmt.Printf("extracted into %s:\n", *libDir)
	for _, p := range res.Written {
		fmt.Printf("  + %s\n", p)
	}
	for _, p := range res.Skipped {
		fmt.Printf("  = %s (already up-to-date)\n", p)
	}
	if len(res.Written)+len(res.Skipped) == 0 {
		fmt.Println("  (nothing embedded yet)")
	}
}
