// hexplus - single-binary HEXPLUS v2 entry point.
//
// Subcommands:
//   (none)         print banner + auto-install if not installed yet
//   version        print version metadata
//   install        idempotent install: extract binaries + copy self to /usr/local/bin
//   uninstall      reverse install (leaves /etc/openvpn etc. alone)
//   extract        dev-only: extract embedded assets to --lib-dir without installing
//   status         report install state + presence of each embedded binary on disk
//
// Service supervision (running openvpn/squid/dropbear), user management, and
// the TUI menu are deliberately not here yet - they come in later phases. This
// file is the install boundary; everything above runs at root once per box,
// everything else runs from the installed location.
package main

import (
	"flag"
	"fmt"
	"os"
	"runtime"
	"strings"

	"github.com/lolyhexey/hexplus/internal/assets"
	"github.com/lolyhexey/hexplus/internal/extract"
	"github.com/lolyhexey/hexplus/internal/install"
	"github.com/lolyhexey/hexplus/internal/pki"
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
  extract              dev-only: extract embedded assets to --lib-dir without installing
  version              print version metadata
  help                 this message

With no subcommand, hexplus prints a banner and, on first run as root, auto-installs.
`, install.LibDir, install.SelfPath)
}

func runDefault() {
	fmt.Println(version.Full())
	fmt.Printf("running on %s/%s\n\n", runtime.GOOS, runtime.GOARCH)
	if install.IsInstalled() {
		fmt.Println("hexplus is installed. The TUI menu lands in a later phase; for now")
		fmt.Println("you can poke at:")
		fmt.Println("  hexplus status")
		fmt.Println("  hexplus uninstall")
		return
	}
	fmt.Println("hexplus has not been installed on this box yet.")
	fmt.Println("Run 'sudo hexplus install' to lay down the embedded binaries.")
}

func runInstall() {
	res, err := install.Install()
	if err != nil {
		fmt.Fprintln(os.Stderr, "install:", err)
		os.Exit(1)
	}
	fmt.Println("hexplus installed.")
	for _, p := range res.BinariesWritten {
		fmt.Printf("  + %s\n", p)
	}
	for _, p := range res.BinariesSkipped {
		fmt.Printf("  = %s (already up-to-date)\n", p)
	}
	for _, p := range res.SymlinksCreated {
		fmt.Printf("  + %s -> dropbearmulti\n", p)
	}
	for _, p := range res.UnitsWritten {
		fmt.Printf("  + %s\n", p)
	}
	for _, p := range res.UnitsSkipped {
		fmt.Printf("  = %s (already up-to-date)\n", p)
	}
	for _, p := range res.ConfigsWritten {
		fmt.Printf("  + %s\n", p)
	}
	for _, p := range res.ConfigsSkipped {
		fmt.Printf("  = %s (preserved)\n", p)
	}
	if res.SelfCopied {
		fmt.Printf("  + %s\n", install.SelfPath)
	}
	if res.MarkerWritten {
		fmt.Printf("  + %s\n", install.MarkerFile)
	}
	fmt.Println()
	if len(res.UnitsWritten) == 0 && len(res.UnitsSkipped) == 0 {
		fmt.Println("note: systemd not detected; unit files were skipped.")
		fmt.Println("      binaries under " + install.LibDir + " can be run manually.")
	} else {
		if res.UnitsReloadWarning != nil {
			fmt.Printf("warning: systemctl daemon-reload failed: %v\n", res.UnitsReloadWarning)
			fmt.Println("         units are on disk; reload manually after booting into systemd.")
			fmt.Println()
		}
		fmt.Println("next: enable services to start on boot, e.g.")
		fmt.Println("      systemctl enable --now hexplus-openvpn")
	}
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
