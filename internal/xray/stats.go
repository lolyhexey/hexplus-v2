package xray

// stats.go: consume xray-core's gRPC stats API bound to 127.0.0.1:10085
// (see APIConfig in config.go). Poll interval is 10s per Phase 0
// decision; results are persisted to the panel DB by the caller.
//
// Skeleton for Phase 1; real gRPC client lands in Phase 7.

// Sample is one traffic reading for a given tag (inbound / outbound /
// per-user). Bytes are cumulative since xray start; deltas are computed
// by the DB layer.
type Sample struct {
	Tag      string
	Uplink   int64
	Downlink int64
}

// Poll returns the current set of counters from xray's stats API.
// Returns an error if the API is unreachable (xray down, port not
// bound yet, config drift).
//
// Placeholder — implementation lands in Phase 7.
func Poll() ([]Sample, error) {
	return nil, nil
}
