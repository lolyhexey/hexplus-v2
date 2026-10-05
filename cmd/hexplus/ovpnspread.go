package main

import (
	"fmt"
	"os"

	"github.com/lolyhexey/hexplus/internal/ovpnspread"
)

// runOVPNSpread: `hexplus ovpnspread apply` rebuilds the iptables rules that
// spread the OpenVPN primary port over the worker processes (rc.local runs
// it at boot); `status` shows what is configured. Turning the feature on or
// off is done from the OpenVPN menu.
func runOVPNSpread(args []string) {
	usage := func() {
		fmt.Fprintln(os.Stderr, "usage: hexplus ovpnspread apply | status")
		os.Exit(2)
	}
	if len(args) != 1 {
		usage()
	}
	switch args[0] {
	case "apply":
		if err := ovpnspread.Apply(); err != nil {
			fmt.Fprintln(os.Stderr, "ovpnspread:", err)
			os.Exit(1)
		}
	case "status":
		ws, err := ovpnspread.Workers()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ovpnspread:", err)
			os.Exit(1)
		}
		if len(ws) == 0 {
			fmt.Println("off")
			return
		}
		proto, port, err := ovpnspread.Primary()
		if err != nil {
			fmt.Fprintln(os.Stderr, "ovpnspread:", err)
			os.Exit(1)
		}
		fmt.Printf("on: %d processes behind %d/%s\n", len(ws)+1, port, proto)
		for _, w := range ws {
			fmt.Printf("  worker #%d  %d/%s  %s\n", w.ID, w.Port, w.Proto, w.UnitName())
		}
	default:
		usage()
	}
}
