package panel

import (
	"context"
	"database/sql"
	"log"
	"time"

	"github.com/lolyhexey/hexplus/internal/xray"
)

// enforcer.go: background loop that disables clients past their quota
// or expiry, and re-enables ones whose ban condition has been cleared
// (quota reset, expiry extended). Runs every EnforceInterval and only
// triggers an xray reload when at least one row actually flipped state.
//
// Kept as a separate loop from the stats poller so a slow xray reload
// during enforcement doesn't backpressure the stats loop.

// EnforceInterval controls how often the sweep runs. 30s is fast
// enough that a quota-blown user gets kicked within one billing tick
// yet slow enough not to hammer the DB.
const EnforceInterval = 30 * time.Second

// runEnforcerLoop drives the sweep. Returns when ctx is done.
func runEnforcerLoop(ctx context.Context, sqldb *sql.DB) {
	t := time.NewTicker(EnforceInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			changed, err := enforceOnce(sqldb)
			if err != nil {
				log.Printf("panel: enforcer: %v", err)
				continue
			}
			if changed > 0 {
				if _, err := xray.Reload(sqldb); err != nil {
					log.Printf("panel: enforcer reload: %v", err)
				}
			}
		}
	}
}

// enforceOnce runs the two disable rules and one re-enable rule in a
// single transaction. Returns the number of rows whose `enabled` flag
// flipped, so callers know whether to reload xray.
func enforceOnce(sqldb *sql.DB) (int, error) {
	tx, err := sqldb.Begin()
	if err != nil {
		return 0, err
	}
	now := time.Now().Unix()
	var total int64

	// Disable expired clients that are still marked enabled.
	res, err := tx.Exec(`
		UPDATE clients
		SET enabled = 0, updated_at = ?
		WHERE enabled = 1 AND expires_at > 0 AND expires_at <= ?
	`, now, now)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	n, _ := res.RowsAffected()
	total += n

	// Disable clients that have burned through their quota.
	res, err = tx.Exec(`
		UPDATE clients
		SET enabled = 0, updated_at = ?
		WHERE enabled = 1 AND quota_bytes > 0 AND used_bytes >= quota_bytes
	`, now)
	if err != nil {
		_ = tx.Rollback()
		return 0, err
	}
	n, _ = res.RowsAffected()
	total += n

	// Re-enable clients whose bans have been lifted by a reset/extend.
	// A client is eligible if it's currently disabled AND (no expiry
	// OR expiry in the future) AND (no quota OR used < quota) AND
	// no admin-side kill switch has been persisted (which we model
	// today as "not enabled" — future manual pauses will need a
	// separate paused_by_admin flag; noted for Phase 5.1).
	//
	// For now the re-enable rule stays conservative: we only touch
	// rows we can prove were disabled by enforcement, not by an admin
	// click. Absent a paused_by_admin column, we don't attempt
	// re-enable automatically — an operator resetting a quota or
	// extending an expiry can call the /toggle endpoint to bring the
	// row back. Skipping the auto-re-enable prevents surprising an
	// operator who paused a user manually.

	return int(total), tx.Commit()
}
