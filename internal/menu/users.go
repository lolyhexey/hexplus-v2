// users.go: user-management menu sub-flows (options 01-09).
//
// Each function takes the menu's stdin reader and drives a Thai-labeled
// prompt sequence that ends in a call into internal/user.*. v1 customers
// type a number, walk through prompts, get an .ovpn file back.
//
// Implementations land in a follow-up; the stubs surface "ยังไม่พร้อม"
// + the CLI equivalent so testers can still drive the underlying flow
// while we wire the prompts.

package menu

import (
	"bufio"
)

func runCreateUser(r *bufio.Reader) error    { return notImplemented(r, "01 createuser") }
func runCreateTrial(r *bufio.Reader) error   { return notImplemented(r, "02 criarteste") }
func runRemoveUser(r *bufio.Reader) error    { return notImplemented(r, "03 remover") }
func runSSHMonitor(r *bufio.Reader) error    { return notImplemented(r, "04 sshmonitor") }
func runChangeExpiry(r *bufio.Reader) error  { return notImplemented(r, "05 mudardata") }
func runChangeLimit(r *bufio.Reader) error   { return notImplemented(r, "06 alterarlimite") }
func runChangePassword(r *bufio.Reader) error { return notImplemented(r, "07 alterarsenha") }
func runCleanExpired(r *bufio.Reader) error  { return notImplemented(r, "08 expcleaner") }
func runListUsers(r *bufio.Reader) error     { return notImplemented(r, "09 infousers") }
