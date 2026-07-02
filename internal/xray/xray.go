// Package xray wraps the embedded xray-core binary that powers the
// V2Ray web panel. All state (config.json, generated keys) lives under
// paths.XrayStateDir; the process is driven by the hexplus-xray.service
// systemd unit registered in internal/service.
//
// This package intentionally has no dependency on internal/panel:
// panel imports xray, not the other way around, so xray can be exercised
// standalone (e.g. by CLI subcommands: `hexplus xray reload`, etc.).
//
// Phase 1 ships the skeleton only. Real config generation lives in
// config.go, subprocess control in daemon.go, gRPC stats in stats.go,
// and per-client CRUD in client.go — filled in during Phase 3 (protocol
// support) and Phase 7 (traffic stats).
package xray
