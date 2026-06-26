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
	"github.com/lolyhexey/hexplus/internal/service"
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
	fmt.Println("embedded binaries on disk:")
	for _, name := range []string{"openvpn", "squid", "dropbearmulti"} {
		path := install.LibDir + "/" + name
		if st, err := os.Stat(path); err == nil {
			fmt.Printf("  + %s (%s, %d bytes)\n", path, st.Mode(), st.Size())
		} else {
			fmt.Printf("  - %s (missing)\n", path)
		}
	}
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
