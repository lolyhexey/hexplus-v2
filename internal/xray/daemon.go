package xray

// daemon.go: control the hexplus-xray.service unit and coordinate
// config.json regen + reload.
//
// We don't spawn xray directly — systemd owns the process lifecycle.
// This file provides the panel with a thin API to:
//   - write a fresh config.json (see Generate in config.go, TBD)
//   - call `systemctl reload-or-restart hexplus-xray.service` to pick
//     it up (xray honors SIGUSR1 for reload of routing; a full restart
//     is required for inbound changes)
//
// Skeleton for Phase 1; real reload wiring arrives with Phase 3.

// Reload writes the current desired config.json and signals xray to
// re-read it. Returns any error from the systemctl call so the caller
// can surface it to the panel UI.
//
// Placeholder — implementation lands in Phase 3.
func Reload() error {
	return nil
}
