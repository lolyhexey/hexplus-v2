// panel.go: `hexplus panel <verb>` and `hexplus xray <verb>` subcommand
// handlers. Kept out of main.go so the dispatcher stays a table of
// short lines.
//
// Verbs:
//   panel install [--port N] [--user NAME] [--password PW] [--keep-db]
//   panel uninstall [--wipe-db]
//   panel serve                (systemd ExecStart target)
//   panel show
//   panel port <N>
//   panel reset-password [--user NAME] [--password PW]
//   xray reload

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"github.com/lolyhexey/hexplus/internal/panel"
	"github.com/lolyhexey/hexplus/internal/service"
)

func runPanel(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus panel <install|uninstall|serve|show|port|reset-password>")
		os.Exit(2)
	}
	switch args[0] {
	case "install":
		runPanelInstall(args[1:])
	case "uninstall":
		runPanelUninstall(args[1:])
	case "serve":
		runPanelServe()
	case "show":
		runPanelShow()
	case "port":
		runPanelPort(args[1:])
	case "reset-password":
		runPanelResetPassword(args[1:])
	case "backup":
		runPanelBackup(args[1:])
	case "restore":
		runPanelRestore(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown panel verb %q\n", args[0])
		os.Exit(2)
	}
}

func runPanelInstall(args []string) {
	fs := flag.NewFlagSet("panel install", flag.ExitOnError)
	port := fs.Int("port", panel.DefaultPort, "TCP port for the panel")
	username := fs.String("user", "admin", "admin username")
	password := fs.String("password", "", "admin password (empty = random)")
	keepDB := fs.Bool("keep-db", false, "keep existing DB on reinstall (default: wipe)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel install requires root")
		os.Exit(1)
	}
	res, err := panel.Install(panel.InstallOptions{
		Port:          *port,
		AdminUsername: *username,
		AdminPassword: *password,
		KeepDB:        *keepDB,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "panel install:", err)
		os.Exit(1)
	}
	fmt.Println("V2Ray panel installed.")
	fmt.Printf("  port:     %d\n", res.Port)
	fmt.Printf("  URL:      http://<server-ip>:%d%s/\n", res.Port, res.URLPrefix)
	fmt.Printf("  user:     %s\n", res.AdminUsername)
	fmt.Printf("  password: %s\n", res.AdminPassword)
	fmt.Println()
	fmt.Println("The panel is HTTP-only. Recommended: reach it via SSH tunnel")
	fmt.Println("  ssh -L", res.Port, ":localhost:", res.Port, " <user>@<server>")
	fmt.Println("or place it behind a reverse proxy (Cloudflare, nginx) that terminates TLS.")
	fmt.Println()
	fmt.Println("Enable and start now:")
	fmt.Println("  hexplus service enable panel")
	fmt.Println("  hexplus service start  panel")
}

func runPanelUninstall(args []string) {
	fs := flag.NewFlagSet("panel uninstall", flag.ExitOnError)
	wipeDB := fs.Bool("wipe-db", false, "also remove the SQLite DB (client list, admin, sessions)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel uninstall requires root")
		os.Exit(1)
	}
	if err := panel.Uninstall(*wipeDB); err != nil {
		fmt.Fprintln(os.Stderr, "panel uninstall:", err)
		os.Exit(1)
	}
	fmt.Println("V2Ray panel removed.")
	if !*wipeDB {
		fmt.Println("  DB preserved. To also delete client/admin data: hexplus panel uninstall --wipe-db")
	}
}

// runPanelServe is the systemd ExecStart target. Blocks until SIGTERM.
func runPanelServe() {
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	if err := panel.Serve(ctx); err != nil {
		fmt.Fprintln(os.Stderr, "panel serve:", err)
		os.Exit(1)
	}
}

func runPanelShow() {
	if !panel.IsInstalled() {
		fmt.Println("V2Ray panel: not installed (run: hexplus panel install)")
		return
	}
	cfg, username, err := panel.ShowInfo()
	if err != nil {
		fmt.Fprintln(os.Stderr, "panel show:", err)
		os.Exit(1)
	}
	fmt.Println("V2Ray panel:")
	fmt.Printf("  port:     %d\n", cfg.Port)
	fmt.Printf("  URL:      http://<server-ip>:%d%s/\n", cfg.Port, cfg.URLPrefix)
	if username != "" {
		fmt.Printf("  admin:    %s (password stored hashed; reset with 'hexplus panel reset-password')\n", username)
	}
}

func runPanelPort(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus panel port <new-port>")
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel port requires root")
		os.Exit(1)
	}
	newPort, err := strconv.Atoi(args[0])
	if err != nil {
		fmt.Fprintf(os.Stderr, "not a number: %q\n", args[0])
		os.Exit(2)
	}
	if err := panel.ChangePort(newPort); err != nil {
		fmt.Fprintln(os.Stderr, "panel port:", err)
		os.Exit(1)
	}
	fmt.Printf("panel port set to %d (restart the unit for it to take effect)\n", newPort)
	if svc, ok := service.ByName("panel"); ok {
		if err := service.Restart(svc); err != nil {
			fmt.Fprintln(os.Stderr, "  warning: restart:", err)
		} else {
			fmt.Println("  restarted hexplus-panel.service")
		}
	}
}

func runPanelResetPassword(args []string) {
	fs := flag.NewFlagSet("panel reset-password", flag.ExitOnError)
	username := fs.String("user", "admin", "admin username to update")
	password := fs.String("password", "", "new password (empty = random)")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel reset-password requires root")
		os.Exit(1)
	}
	pw, err := panel.ResetAdminPassword(*username, *password)
	if err != nil {
		fmt.Fprintln(os.Stderr, "panel reset-password:", err)
		os.Exit(1)
	}
	fmt.Printf("admin password for %q updated.\n", *username)
	fmt.Printf("  password: %s\n", pw)
}

func runPanelBackup(args []string) {
	fs := flag.NewFlagSet("panel backup", flag.ExitOnError)
	out := fs.String("out", "/var/lib/hexplus/backups", "directory to write hexplus-panel-<ts>.tar.gz")
	if err := fs.Parse(args); err != nil {
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel backup requires root")
		os.Exit(1)
	}
	dest, res, err := panel.MakeBackup(*out)
	if err != nil {
		fmt.Fprintln(os.Stderr, "panel backup:", err)
		os.Exit(1)
	}
	fmt.Println("backup written:")
	fmt.Printf("  %s\n", dest)
	for _, entry := range res.Included {
		fmt.Printf("  + %s\n", entry)
	}
}

func runPanelRestore(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus panel restore <path/to/backup.tar.gz>")
		os.Exit(2)
	}
	if os.Geteuid() != 0 {
		fmt.Fprintln(os.Stderr, "panel restore requires root")
		os.Exit(1)
	}
	// Stop the units first so restore doesn't race a running daemon.
	for _, name := range []string{"panel", "xray"} {
		if svc, ok := service.ByName(name); ok {
			_ = service.Stop(svc)
		}
	}
	if err := panel.Restore(args[0]); err != nil {
		fmt.Fprintln(os.Stderr, "panel restore:", err)
		os.Exit(1)
	}
	fmt.Println("restore complete. Start the units when ready:")
	fmt.Println("  hexplus service start xray")
	fmt.Println("  hexplus service start panel")
}

// runXray dispatches `hexplus xray <verb>`. Currently only `reload`,
// invoked by the panel when config.json is regenerated after an
// inbound/client change.
func runXray(args []string) {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: hexplus xray reload")
		os.Exit(2)
	}
	switch args[0] {
	case "reload":
		svc, ok := service.ByName("xray")
		if !ok {
			fmt.Fprintln(os.Stderr, "xray service not registered")
			os.Exit(1)
		}
		if err := service.TryReload(svc); err != nil {
			fmt.Fprintln(os.Stderr, "xray reload:", err)
			os.Exit(1)
		}
		fmt.Println("xray reloaded.")
	default:
		fmt.Fprintf(os.Stderr, "unknown xray verb %q\n", args[0])
		os.Exit(2)
	}
}
