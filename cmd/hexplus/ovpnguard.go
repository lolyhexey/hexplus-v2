package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lolyhexey/hexplus/internal/ovpnguard"
)

// runOVPNGuard: `hexplus ovpnguard run` is what hexplus-ovpnguard.service
// executes; `once` runs a single check; `kick <name>` ends every OpenVPN
// session of one user.
func runOVPNGuard(args []string) {
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: hexplus ovpnguard run | once | kick <name>")
		os.Exit(2)
	}
	if len(args) == 0 {
		usage()
	}
	switch args[0] {
	case "run":
		if len(args) != 1 {
			usage()
		}
		ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
		defer cancel()
		if err := ovpnguard.Run(ctx); err != nil {
			fmt.Fprintln(os.Stderr, "ovpnguard:", err)
			os.Exit(1)
		}
	case "once":
		if len(args) != 1 {
			usage()
		}
		n := ovpnguard.Once(log.Printf)
		fmt.Printf("ended %d session(s)\n", n)
	case "kick":
		if len(args) != 2 {
			usage()
		}
		n := ovpnguard.Kick(args[1], log.Printf)
		fmt.Printf("ended %d session(s) of %s\n", n, args[1])
	default:
		usage()
	}
}
