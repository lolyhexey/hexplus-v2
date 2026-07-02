package xray

// stats.go: pull counters from xray-core's stats API and roll them
// into panel-owned tables.
//
// We could import github.com/xtls/xray-core's gRPC client, but that
// would drag half of xray-core into our binary. Instead we shell out
// to the extracted xray binary, which has the same stats query built
// in as a CLI subcommand — parseable JSON out, one subprocess per
// poll interval (~10s). Cheap and avoids the dependency footprint.
//
// Counter name schema (xray convention, `>>>`-delimited):
//   user>>>email>>>traffic>>>{uplink|downlink}
//   inbound>>>tag>>>traffic>>>{uplink|downlink}
//   outbound>>>tag>>>traffic>>>{uplink|downlink}

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/lolyhexey/hexplus/internal/paths"
)

// prev tracks the last-seen absolute counter value per (scope, name,
// direction) so we can compute deltas without asking xray to reset.
// Reset-mode is destructive: a failed parse would flush the pending
// bytes into oblivion (which is what happened before the JSON fix).
// Delta-mode leaves xray as the source of truth.
var (
	prevMu sync.Mutex
	prev   = map[string]int64{}
)

// StatsAPIAddr is the loopback endpoint the generated config binds the
// API inbound to (see generate.go). Kept as a const so both the poller
// and any diagnostic code use the same address.
const StatsAPIAddr = "127.0.0.1:10085"

// PollInterval matches the Phase 0 decision — 10 seconds is a good
// tradeoff between freshness and DB write volume.
const PollInterval = 10 * time.Second

// Sample is one traffic reading for a given tag (inbound / outbound /
// per-user). Bytes are cumulative since the counter was last reset;
// -reset in the CLI arg means each poll returns and clears the
// deltas, which is what we want for time-series storage.
type Sample struct {
	Scope string // "user" | "inbound" | "outbound"
	Name  string // email, inbound tag, or outbound tag
	Up    int64
	Down  int64
}

// xrayStatResponse mirrors the JSON emitted by `xray api statsquery`.
// Value is a raw JSON number in the wire format; using json.Number lets
// us accept both the number-form emitted by modern xray-core (25.x+)
// and the older string-form that used to ship in 1.x-era protobuf-JSON.
type xrayStatResponse struct {
	Stat []struct {
		Name  string      `json:"name"`
		Value json.Number `json:"value"`
	} `json:"stat"`
}

// Poll runs one query cycle in non-destructive mode and returns the
// deltas since the last successful poll. Xray keeps counting; we
// compute (current - prev) locally and remember `current` for the
// next call. If xray restarted (current < prev) the whole current
// value is treated as a delta so we don't lose the first slice of
// post-restart traffic.
func Poll(ctx context.Context) ([]Sample, error) {
	pctx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()

	cmd := exec.CommandContext(pctx, paths.LibDir+"/xray",
		"api", "statsquery",
		"--server="+StatsAPIAddr,
		// intentionally NO -reset: we compute deltas ourselves
	)
	out, err := cmd.Output()
	if err != nil {
		if errors.Is(pctx.Err(), context.DeadlineExceeded) {
			return nil, errors.New("xray api statsquery timed out")
		}
		return nil, fmt.Errorf("xray api statsquery: %w", err)
	}
	return parseStatsJSON(out)
}

// parseStatsJSON is factored out so tests can feed it fixtures without
// spawning xray. Returns per-counter deltas relative to the last call.
// Zero-delta rows are elided.
func parseStatsJSON(raw []byte) ([]Sample, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var resp xrayStatResponse
	if err := dec.Decode(&resp); err != nil {
		return nil, fmt.Errorf("parse stats json: %w", err)
	}

	prevMu.Lock()
	defer prevMu.Unlock()

	// Bucket by (scope, name); one row combines the uplink + downlink
	// entries that xray emits separately.
	type key struct{ scope, name string }
	buckets := make(map[key]*Sample)
	for _, s := range resp.Stat {
		scope, name, direction, ok := splitStatName(s.Name)
		if !ok {
			continue
		}
		cur, err := s.Value.Int64()
		if err != nil {
			continue
		}
		fullKey := s.Name
		last := prev[fullKey]
		delta := cur - last
		if delta < 0 {
			// xray restart wiped the counter — accept current as the
			// delta so we don't lose the first slice of post-restart
			// traffic to a "went backwards" veto.
			delta = cur
		}
		prev[fullKey] = cur
		if delta == 0 {
			continue
		}

		k := key{scope, name}
		b := buckets[k]
		if b == nil {
			b = &Sample{Scope: scope, Name: name}
			buckets[k] = b
		}
		if direction == "uplink" {
			b.Up += delta
		} else if direction == "downlink" {
			b.Down += delta
		}
	}
	out := make([]Sample, 0, len(buckets))
	for _, b := range buckets {
		if b.Up == 0 && b.Down == 0 {
			continue
		}
		out = append(out, *b)
	}
	return out, nil
}

// forgetCounter drops one prev entry so ResetUserCounter's zero-out on
// xray is followed by our next poll starting a fresh delta baseline.
func forgetCounter(email string) {
	prevMu.Lock()
	defer prevMu.Unlock()
	delete(prev, "user>>>"+email+">>>traffic>>>uplink")
	delete(prev, "user>>>"+email+">>>traffic>>>downlink")
}

// splitStatName teases apart "scope>>>name>>>traffic>>>direction". A
// falsely-shaped counter (from a future xray version) is skipped, not
// treated as an error.
func splitStatName(name string) (scope, ident, direction string, ok bool) {
	parts := strings.Split(name, ">>>")
	if len(parts) != 4 || parts[2] != "traffic" {
		return "", "", "", false
	}
	return parts[0], parts[1], parts[3], true
}

// ResetUserCounter zeroes the two per-user counters for the given
// email. Used when the panel does a manual "reset used bytes" so the
// next poll doesn't re-inflate the number.
func ResetUserCounter(email string) error {
	for _, direction := range []string{"uplink", "downlink"} {
		cmd := exec.Command(paths.LibDir+"/xray",
			"api", "stats",
			"--server="+StatsAPIAddr,
			"-name=user>>>"+email+">>>traffic>>>"+direction,
			"-reset",
		)
		if err := cmd.Run(); err != nil {
			// The counter might not exist yet (user just created,
			// no traffic yet). Not an error worth surfacing.
			continue
		}
	}
	// Drop our own remembered baseline for this email so the next
	// non-reset Poll doesn't compute a giant negative delta against
	// the freshly-zeroed xray counter.
	forgetCounter(email)
	return nil
}

// Persist writes one poll's worth of samples into the panel DB:
//   - one traffic_samples row per (scope, ref_id, sampled_at) as a
//     historical breadcrumb
//   - increment inbounds.total_up/down by inbound samples
//   - increment clients.used_bytes by user samples (matched by email)
//
// A single sqldb transaction wraps the whole batch so a partial write
// on the way out doesn't half-apply an interval.
func Persist(sqldb *sql.DB, samples []Sample) error {
	if len(samples) == 0 {
		return nil
	}
	tx, err := sqldb.Begin()
	if err != nil {
		return err
	}
	now := time.Now().Unix()
	for _, s := range samples {
		switch s.Scope {
		case "inbound":
			var id int64
			err := tx.QueryRow(`SELECT id FROM inbounds WHERE tag = ?`, s.Name).Scan(&id)
			if err != nil {
				continue // orphaned counter from a since-deleted inbound
			}
			_, _ = tx.Exec(`INSERT INTO traffic_samples (scope, ref_id, sampled_at, up_bytes, down_bytes)
			                VALUES ('inbound', ?, ?, ?, ?)`, id, now, s.Up, s.Down)
			_, _ = tx.Exec(`UPDATE inbounds SET total_up = total_up + ?, total_down = total_down + ?
			                WHERE id = ?`, s.Up, s.Down, id)
		case "user":
			// email is unique per inbound (see UNIQUE index in the
			// schema) — but the same email can exist under two
			// inbounds. Update every match.
			rows, err := tx.Query(`SELECT id FROM clients WHERE email = ?`, s.Name)
			if err != nil {
				continue
			}
			var ids []int64
			for rows.Next() {
				var id int64
				if err := rows.Scan(&id); err == nil {
					ids = append(ids, id)
				}
			}
			rows.Close()
			for _, id := range ids {
				_, _ = tx.Exec(`INSERT INTO traffic_samples (scope, ref_id, sampled_at, up_bytes, down_bytes)
				                VALUES ('user', ?, ?, ?, ?)`, id, now, s.Up, s.Down)
				_, _ = tx.Exec(`UPDATE clients SET used_bytes = used_bytes + ? WHERE id = ?`,
					s.Up+s.Down, id)
			}
		case "outbound":
			// We don't track per-outbound in a table today; the counter
			// is still useful as a sample row so future dashboards can
			// query "top outbounds over time".
			hash := int64(fnv1a(s.Name))
			_, _ = tx.Exec(`INSERT INTO traffic_samples (scope, ref_id, sampled_at, up_bytes, down_bytes)
			                VALUES ('outbound', ?, ?, ?, ?)`, hash, now, s.Up, s.Down)
		}
	}
	return tx.Commit()
}

// PollAndPersist is the loop-body helper: one poll, one persist. Used
// by the panel's background goroutine so we don't repeat the (poll,
// log, persist, log) boilerplate at the call site.
func PollAndPersist(ctx context.Context, sqldb *sql.DB) (int, error) {
	samples, err := Poll(ctx)
	if err != nil {
		return 0, err
	}
	if err := Persist(sqldb, samples); err != nil {
		return 0, err
	}
	return len(samples), nil
}

// fnv1a hashes an outbound tag into a stable int64 for ref_id. Using a
// text ref_id would either force a schema change or a JOIN we don't
// need for a debug-only counter.
func fnv1a(s string) uint64 {
	const (
		offset64 = 14695981039346656037
		prime64  = 1099511628211
	)
	h := uint64(offset64)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= prime64
	}
	return h
}
