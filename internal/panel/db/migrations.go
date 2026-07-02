package db

import (
	"database/sql"
	"fmt"
)

// migrations lists every schema step in application order. Adding a new
// step is a two-line change here and a bump to targetVersion. Do NOT
// edit an existing migration once it has shipped — write a new one
// that ALTERs to the desired state.
//
// Column notes worth calling out:
//   - node_id INTEGER on every user-scoped table: reserved for the
//     multi-node feature deferred to v2. Defaults to 1 so single-node
//     upgrades to master/agent by inserting node rows, not by rewriting
//     existing data.
//   - clients.protocol duplicates inbounds.protocol on purpose: xray's
//     per-client credential (uuid vs password vs key) depends on it,
//     and denormalizing here means stats/subscription code doesn't have
//     to JOIN for every render.
var migrations = []string{
	// v1: initial schema.
	`
CREATE TABLE nodes (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    name        TEXT    NOT NULL UNIQUE,
    address     TEXT    NOT NULL DEFAULT '127.0.0.1',
    is_local    INTEGER NOT NULL DEFAULT 1,
    created_at  INTEGER NOT NULL
);
INSERT INTO nodes (id, name, address, is_local, created_at)
    VALUES (1, 'local', '127.0.0.1', 1, strftime('%s','now'));

CREATE TABLE admin_users (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    username       TEXT    NOT NULL UNIQUE,
    password_hash  TEXT    NOT NULL,
    created_at     INTEGER NOT NULL,
    last_login_at  INTEGER
);

CREATE TABLE settings (
    key    TEXT PRIMARY KEY,
    value  TEXT NOT NULL
);

CREATE TABLE inbounds (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      INTEGER NOT NULL DEFAULT 1 REFERENCES nodes(id) ON DELETE RESTRICT,
    tag          TEXT    NOT NULL UNIQUE,
    protocol     TEXT    NOT NULL,
    listen       TEXT    NOT NULL DEFAULT '0.0.0.0',
    port         INTEGER NOT NULL,
    settings     TEXT    NOT NULL DEFAULT '{}',
    stream       TEXT    NOT NULL DEFAULT '{}',
    sniffing     INTEGER NOT NULL DEFAULT 1,
    enabled      INTEGER NOT NULL DEFAULT 1,
    remark       TEXT    NOT NULL DEFAULT '',
    total_up     INTEGER NOT NULL DEFAULT 0,
    total_down   INTEGER NOT NULL DEFAULT 0,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);
CREATE INDEX idx_inbounds_node ON inbounds(node_id);

CREATE TABLE clients (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    inbound_id    INTEGER NOT NULL REFERENCES inbounds(id) ON DELETE CASCADE,
    node_id       INTEGER NOT NULL DEFAULT 1 REFERENCES nodes(id) ON DELETE RESTRICT,
    email         TEXT    NOT NULL,
    protocol      TEXT    NOT NULL,
    uuid          TEXT    NOT NULL DEFAULT '',
    password      TEXT    NOT NULL DEFAULT '',
    shared_key    TEXT    NOT NULL DEFAULT '',
    flow          TEXT    NOT NULL DEFAULT '',
    quota_bytes   INTEGER NOT NULL DEFAULT 0,
    used_bytes    INTEGER NOT NULL DEFAULT 0,
    ip_limit      INTEGER NOT NULL DEFAULT 0,
    expires_at    INTEGER NOT NULL DEFAULT 0,
    enabled       INTEGER NOT NULL DEFAULT 1,
    sub_token     TEXT    NOT NULL DEFAULT '',
    created_at    INTEGER NOT NULL,
    updated_at    INTEGER NOT NULL,
    UNIQUE(inbound_id, email)
);
CREATE INDEX idx_clients_inbound ON clients(inbound_id);
CREATE INDEX idx_clients_node    ON clients(node_id);
CREATE INDEX idx_clients_sub     ON clients(sub_token) WHERE sub_token != '';

CREATE TABLE traffic_samples (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    scope         TEXT    NOT NULL,       -- 'inbound' | 'outbound' | 'user'
    ref_id        INTEGER NOT NULL,       -- inbound.id / outbound tag hash / client.id
    sampled_at    INTEGER NOT NULL,
    up_bytes      INTEGER NOT NULL,
    down_bytes    INTEGER NOT NULL
);
CREATE INDEX idx_traffic_scope_time ON traffic_samples(scope, ref_id, sampled_at);

CREATE TABLE sessions (
    token       TEXT    PRIMARY KEY,
    admin_id    INTEGER NOT NULL REFERENCES admin_users(id) ON DELETE CASCADE,
    created_at  INTEGER NOT NULL,
    expires_at  INTEGER NOT NULL,
    remote_ip   TEXT    NOT NULL DEFAULT ''
);
CREATE INDEX idx_sessions_admin ON sessions(admin_id);
CREATE INDEX idx_sessions_exp   ON sessions(expires_at);
`,
	// v2: routing + outbounds + certificates.
	//
	// - outbounds: user-configured egress channels. freedom/blackhole
	//   are always synthesized by generate.go; this table holds WARP,
	//   proxy chains, socks/http outbounds, and any custom ones.
	// - routing_rules: field-type rules with a priority ordering. Lower
	//   priority runs first; ties broken by id.
	// - certs: managed TLS material for VLESS/VMess/Trojan+TLS inbounds.
	//   Source is either "acme" (Let's Encrypt) or "manual" (uploaded).
	`
CREATE TABLE outbounds (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    node_id      INTEGER NOT NULL DEFAULT 1 REFERENCES nodes(id) ON DELETE RESTRICT,
    tag          TEXT    NOT NULL UNIQUE,
    protocol     TEXT    NOT NULL,
    settings     TEXT    NOT NULL DEFAULT '{}',
    stream       TEXT    NOT NULL DEFAULT '{}',
    remark       TEXT    NOT NULL DEFAULT '',
    enabled      INTEGER NOT NULL DEFAULT 1,
    created_at   INTEGER NOT NULL,
    updated_at   INTEGER NOT NULL
);

CREATE TABLE routing_rules (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    priority       INTEGER NOT NULL DEFAULT 100,
    outbound_tag   TEXT    NOT NULL,
    inbound_tag    TEXT    NOT NULL DEFAULT '',
    domains        TEXT    NOT NULL DEFAULT '',  -- newline-joined
    ips            TEXT    NOT NULL DEFAULT '',  -- newline-joined
    protocols      TEXT    NOT NULL DEFAULT '',
    port_range     TEXT    NOT NULL DEFAULT '',
    remark         TEXT    NOT NULL DEFAULT '',
    enabled        INTEGER NOT NULL DEFAULT 1,
    created_at     INTEGER NOT NULL,
    updated_at     INTEGER NOT NULL
);
CREATE INDEX idx_rules_priority ON routing_rules(priority, id);

CREATE TABLE certs (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    domain          TEXT    NOT NULL UNIQUE,
    source          TEXT    NOT NULL DEFAULT 'acme',  -- 'acme' | 'manual'
    cert_path       TEXT    NOT NULL,
    key_path        TEXT    NOT NULL,
    not_after       INTEGER NOT NULL DEFAULT 0,
    last_renewed_at INTEGER NOT NULL DEFAULT 0,
    remark          TEXT    NOT NULL DEFAULT '',
    created_at      INTEGER NOT NULL,
    updated_at      INTEGER NOT NULL
);
`,
	// v3: login audit trail. Used by fail2ban / operators to spot
	// break-in attempts. Kept short (username + ip + success) so the
	// table stays tiny even under sustained scraping.
	`
CREATE TABLE login_attempts (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    username    TEXT    NOT NULL,
    remote_ip   TEXT    NOT NULL,
    success     INTEGER NOT NULL,
    at          INTEGER NOT NULL
);
CREATE INDEX idx_login_at_ip ON login_attempts(remote_ip, at);
`,
}

// targetVersion is len(migrations); DBs at this version are up to date.
var targetVersion = len(migrations)

// Migrate advances the DB to targetVersion, one step per pending
// migration, each in its own transaction so a partial failure rolls
// back cleanly. Returns ErrTooNew if the DB is ahead of us.
func Migrate(sqldb *sql.DB) error {
	from, err := currentVersion(sqldb)
	if err != nil {
		return err
	}
	if from > targetVersion {
		return fmt.Errorf("%w: db=%d, supported=%d", ErrTooNew, from, targetVersion)
	}
	for v := from; v < targetVersion; v++ {
		if err := applyMigration(sqldb, v); err != nil {
			return fmt.Errorf("migrate %d -> %d: %w", v, v+1, err)
		}
	}
	return nil
}

func applyMigration(sqldb *sql.DB, index int) error {
	tx, err := sqldb.Begin()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(migrations[index]); err != nil {
		_ = tx.Rollback()
		return err
	}
	if err := setVersion(tx, index+1); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}
