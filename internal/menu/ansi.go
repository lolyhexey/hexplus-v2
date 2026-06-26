// Package menu is the v1-identical Thai TUI HEXPLUS operators expect.
//
// Why not bubble tea: v1 customers reach for `menu` and type a number.
// They don't expect altscreen takeover or arrow-key navigation. To
// preserve muscle memory we mirror the exact rendering loop v1 used:
// clear screen, print a fully-styled frame with ANSI escape codes,
// read a number from stdin, dispatch via switch.
//
// The ANSI codes throughout this package are copied byte-for-byte from
// Modulos/menu so any pixel diff vs the bash original is a bug. Don't
// "modernize" the colors here; v1 customers will notice.

package menu

import "fmt"

// ANSI bytes copied from Modulos/menu's `echo -e` calls. Comments name
// the foreground color so reviewers don't need a 256-color chart.
const (
	cReset    = "\033[0m"
	cRedBold  = "\033[1;31m"    // red bold (option indices, separators)
	cGrnBold  = "\033[1;32m"    // green bold (labels, "ON" markers)
	cYelBold  = "\033[1;33m"    // yellow bold (option text)
	cBluBold  = "\033[0;34m"    // blue (the ━━━ separator color)
	cCyanBold = "\033[1;36m"    // cyan bold (numbers inside [..])
	cWhtBold  = "\033[1;37m"    // white bold (bullets, values)
	cWhtRedBG = "\033[41;1;37m" // white on red bg (banner)

	separator = "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
)

// printSep mirrors v1's `echo -e "\033[0;34m━━━━...\033[0m"`.
func printSep() {
	fmt.Print(cBluBold, separator, cReset, "\n")
}

// printBanner is the centered "⇱ HEXPLUS SCRIPT FREE EDIT:BY LOLY ⇲"
// row. v1 uses ESC[41;1;37m which is "bright white on red background".
func printBanner() {
	// Note the literal \E vs \033: \E is bash's echo -e extension, but
	// the byte sequence is the same as \033. We emit \033 which both
	// real terminals and screen recorders interpret correctly.
	fmt.Print("\033[41;1;37m       ⇱ HEXPLUS SCRIPT FREE EDIT:BY LOLY ⇲       ", cReset, "\n")
}

// clearScreen replicates `clear`. Goes through ANSI rather than execing
// /usr/bin/clear so the menu works on a box where coreutils is trimmed.
func clearScreen() {
	fmt.Print("\033[H\033[2J")
}

// markerOn / markerOff are the per-feature ◉ / ○ indicators v1 paints
// next to options like "BAD VPN" or "ONLINE APP".
func markerOn() string  { return cGrnBold + "◉ " + cReset }
func markerOff() string { return cRedBold + "○ " + cReset }
