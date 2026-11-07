package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
)

const SaveDatabaseSchemaVersion = 9

const loginBlockColumn = "login_block_until_unix INTEGER CHECK (login_block_until_unix IS NULL OR (typeof(login_block_until_unix) = 'integer' AND login_block_until_unix >= 0))"

var fixedStateTableColumns = map[string][]string{
	"cn_account_creation_daily": {"day", "source_ip", "created_count"},
	"cn_boss_score_rank":        {"boss_id", "mode", "user_id", "best_score", "best_grade", "best_at_unix"},
	"cn_gift_code":              {"code_id", "batch_id", "canonical", "enabled"},
	"cn_gift_redemption":        {"code_id", "scope", "user_id", "batch_id", "claim_group", "claimed_at", "present_ids", "reward_revision"},
	"cn_battle_clear_daily":     {"day", "boss_id", "mode", "user_id", "arthur_type", "clear_count", "completed_at", "event_key", "decks_json", "decks_sha256"},
	"cn_account_balance":        {"user_id", "gold", "coin", "coin_free", "friend_point", "pvp_point"},
	"cn_account_card":           {"user_id", "unique_id", "slot", "card_id", "level", "experience", "now_exp", "next_exp", "love", "fame", "is_lock", "hp", "attack", "magic", "mind", "base_add_price", "skill_levels", "sort_order", "row_sha256", "create_time"},
	"cn_account_item":           {"user_id", "item_id", "num", "limit_time", "sort_order", "row_sha256"},
	"cn_account_stack_card":     {"user_id", "card_id", "num", "hp", "attack", "magic", "mind", "add_exp", "base_add_price", "material_type", "sort_order", "row_sha256"},
	"cn_account_present":        {"user_id", "present_id", "history", "issued_at_unix", "add_elapsed_sec", "limit_time", "state", "reason", "title", "comment", "url", "auto_fusion_used", "auto_loveup_used", "admin_key", "rewards_json", "sort_order", "row_sha256"},
	"cn_save_snapshot":          {"singleton", "schema_version", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_local_account":          {"user_id", "login_uuid", "session_key", "created_utc", "last_login_utc", "login_block_until_unix"},
	"cn_account_snapshot":       {"user_id", "schema_version", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_account_projection":     {"user_id", "schema_version", "snapshot_revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_multiplayer_runtime":    {"singleton", "next_room_id"},
	"cn_multiplayer_completion": {"room_id", "expires_unix", "payload_json", "payload_sha256"},
	"cn_global_operation":       {"operation_key", "revision", "updated_utc", "payload_json", "payload_sha256"},
	"cn_admin_audit":            {"audit_id", "created_utc", "operation", "target", "payload_json", "payload_sha256"},
	"cn_admin_action_receipt":   {"operation_key", "user_id", "request_sha256", "result_json", "result_sha256", "created_utc"},
	"cn_account_credentials":    {"user_id", "username", "password_salt", "password_hash", "iterations", "updated_utc"},
	"cn_friend_point_rental":    {"event_id", "event_key", "owner_user_id", "renter_user_id", "boss_id", "friend_point", "created_utc"},
	"cn_local_account_follow":   {"follower_user_id", "followed_user_id", "created_utc"},
}

func validateFixedStateSchema(transaction *sql.Tx, version int) error {
	for table, expectedColumns := range fixedStateTableColumns {
		if version < 5 && (table == "cn_gift_code" || table == "cn_gift_redemption") ||
			version < 6 && table == "cn_boss_score_rank" ||
			version < 8 && table == "cn_account_creation_daily" {
			continue
		}
		if version == 3 && table == "cn_account_card" {
			expectedColumns = expectedColumns[:len(expectedColumns)-1]
		}
		if version < 9 && table == "cn_local_account" {
			expectedColumns = expectedColumns[:len(expectedColumns)-1]
		}
		if version == 6 && table == "cn_boss_score_rank" {
			expectedColumns = []string{"boss_id", "user_id", "best_score", "best_grade", "best_at_unix"}
		}
		rows, err := transaction.Query(`PRAGMA table_info(` + table + `)`)
		if err != nil {
			return fmt.Errorf("inspect CN state table %s: %w", table, err)
		}
		actualColumns := make([]string, 0, len(expectedColumns))
		for rows.Next() {
			var columnID int
			var name string
			var dataType string
			var notNull int
			var defaultValue any
			var primaryKey int
			if err := rows.Scan(
				&columnID,
				&name,
				&dataType,
				&notNull,
				&defaultValue,
				&primaryKey,
			); err != nil {
				_ = rows.Close()
				return fmt.Errorf("scan CN state table %s: %w", table, err)
			}
			actualColumns = append(actualColumns, name)
			if table == "cn_account_creation_daily" {
				wantType, wantPK := "TEXT", columnID+1
				if columnID == 2 {
					wantType, wantPK = "INTEGER", 0
				}
				if dataType != wantType || notNull != 1 || primaryKey != wantPK {
					_ = rows.Close()
					return fmt.Errorf("CN account creation column %s has invalid type, nullability or primary key", name)
				}
			}
		}
		if err := rows.Err(); err != nil {
			_ = rows.Close()
			return fmt.Errorf("iterate CN state table %s: %w", table, err)
		}
		if err := rows.Close(); err != nil {
			return fmt.Errorf("close CN state table %s inspection: %w", table, err)
		}
		if !slices.Equal(actualColumns, expectedColumns) {
			return fmt.Errorf(
				"CN state table %s columns are %v, want %v",
				table,
				actualColumns,
				expectedColumns,
			)
		}
	}
	indexes := []string{
		"cn_friend_point_rental_owner_event",
		"cn_local_account_follow_inbound",
		"cn_local_account_follow_recent",
		"cn_local_account_recent_login",
		"cn_account_projection_recent_update",
	}
	if version >= 5 {
		indexes = append(indexes, "cn_gift_code_batch", "cn_gift_redemption_batch")
	}
	if version >= 6 {
		indexes = append(indexes, "cn_boss_score_rank_order")
	}
	for _, index := range indexes {
		var count int
		if err := transaction.QueryRow(
			`SELECT COUNT(*) FROM sqlite_master WHERE type = 'index' AND name = ?`,
			index,
		).Scan(&count); err != nil {
			return fmt.Errorf("inspect CN state index %s: %w", index, err)
		}
		if count != 1 {
			return fmt.Errorf("CN state index %s is missing", index)
		}
	}
	return nil
}

// EnsureSchema initializes or validates the fixed schema once per storage owner,
// during startup. Business requests must never execute DDL or schema scans.
func (storage *Database) EnsureSchema() error {
	storage.schemaOnce.Do(func() {
		storage.schemaErr = storage.initializeSchema()
	})
	return storage.schemaErr
}

func (storage *Database) initializeSchema() error {
	if err := storage.ensureCurrentDatabase(); err != nil {
		return err
	}
	db, err := storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := initializeSaveSchema(tx); err != nil {
		return err
	}
	return tx.Commit()
}

func initializeSaveSchema(transaction *sql.Tx) error {
	var userVersion int
	if err := transaction.QueryRow(`PRAGMA user_version`).Scan(&userVersion); err != nil {
		return fmt.Errorf("read CN SQLite schema version: %w", err)
	}
	if userVersion != 0 && (userVersion < 3 || userVersion > SaveDatabaseSchemaVersion) {
		return fmt.Errorf(
			"unsupported CN state database schema version %d, want %d",
			userVersion,
			SaveDatabaseSchemaVersion,
		)
	}
	if userVersion != 0 {
		if userVersion < 8 {
			var collisions int
			if err := transaction.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE name = 'cn_account_creation_daily'`).Scan(&collisions); err != nil {
				return err
			}
			if collisions != 0 {
				return errors.New("CN account creation table unexpectedly exists before schema version 8")
			}
		}
		if _, err := transaction.Exec(battleClearStatisticsSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(partnerRecencyIndexes); err != nil {
			return fmt.Errorf("create CN partner recency indexes: %w", err)
		}
		if err := validateFixedStateSchema(transaction, userVersion); err != nil {
			return err
		}
		if userVersion == 3 {
			// Old cards have no recorded acquisition date. Preserve that unknown
			// value instead of assigning the migration time to the whole inventory.
			if _, err := transaction.Exec(`ALTER TABLE cn_account_card ADD COLUMN create_time INTEGER NOT NULL DEFAULT 0 CHECK(create_time >= 0)`); err != nil {
				return fmt.Errorf("add CN card acquisition time: %w", err)
			}
		}
		if userVersion < 5 {
			if _, err := transaction.Exec(giftCodeSchema); err != nil {
				return err
			}
		}
		if userVersion < 6 {
			if _, err := transaction.Exec(bossScoreRankingSchema); err != nil {
				return err
			}
		} else if userVersion == 6 {
			if _, err := transaction.Exec(`ALTER TABLE cn_boss_score_rank RENAME TO cn_boss_score_rank_v6;
DROP INDEX cn_boss_score_rank_order;` + bossScoreRankingSchema + `
INSERT INTO cn_boss_score_rank(boss_id,mode,user_id,best_score,best_grade,best_at_unix)
 SELECT boss_id,1,user_id,best_score,best_grade,best_at_unix FROM cn_boss_score_rank_v6;
DROP TABLE cn_boss_score_rank_v6;`); err != nil {
				return fmt.Errorf("separate solo and multiplayer boss scores: %w", err)
			}
		}
		if userVersion < 8 {
			if _, err := transaction.Exec(accountCreationDailySchema); err != nil {
				return fmt.Errorf("create CN account creation quota schema: %w", err)
			}
		}
		if userVersion < 9 {
			if _, err := transaction.Exec(`ALTER TABLE cn_local_account ADD COLUMN ` + loginBlockColumn); err != nil {
				return fmt.Errorf("add CN account login block: %w", err)
			}
		}
		if userVersion < SaveDatabaseSchemaVersion {
			if _, err := transaction.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SaveDatabaseSchemaVersion)); err != nil {
				return err
			}
			return validateFixedStateSchema(transaction, SaveDatabaseSchemaVersion)
		}
		return nil
	}
	// Version zero is accepted only for a new, empty database. Never merge an
	// unversioned or older schema into the current one.
	var objects int
	if err := transaction.QueryRow(`SELECT count(*) FROM sqlite_master WHERE name NOT GLOB 'sqlite_*'`).Scan(&objects); err != nil {
		return fmt.Errorf("inspect new CN SQLite database: %w", err)
	}
	if objects != 0 {
		return errors.New("unversioned CN database is unsupported; use a fresh data directory")
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_save_snapshot (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		schema_version INTEGER NOT NULL,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN SQLite save schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_local_account (
		user_id INTEGER PRIMARY KEY CHECK (user_id > 0),
		login_uuid TEXT NOT NULL UNIQUE,
		session_key TEXT NOT NULL UNIQUE,
		created_utc TEXT NOT NULL,
		last_login_utc TEXT NOT NULL,
		` + loginBlockColumn + `
	)`); err != nil {
		return fmt.Errorf("create CN local account schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_account_snapshot (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		schema_version INTEGER NOT NULL,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN account snapshot schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_account_projection (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		schema_version INTEGER NOT NULL,
		snapshot_revision INTEGER NOT NULL CHECK (snapshot_revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN account projection schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_multiplayer_runtime (
		singleton INTEGER PRIMARY KEY CHECK (singleton = 1),
		next_room_id INTEGER NOT NULL CHECK (next_room_id > 0)
	)`); err != nil {
		return fmt.Errorf("create CN multiplayer runtime schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_multiplayer_completion (
		room_id INTEGER PRIMARY KEY CHECK (room_id > 0),
		expires_unix INTEGER NOT NULL CHECK (expires_unix > 0),
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN multiplayer completion schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_global_operation (
		operation_key TEXT PRIMARY KEY,
		revision INTEGER NOT NULL CHECK (revision > 0),
		updated_utc TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN global operation schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_admin_audit (
		audit_id INTEGER PRIMARY KEY AUTOINCREMENT,
		created_utc TEXT NOT NULL,
		operation TEXT NOT NULL,
		target TEXT NOT NULL,
		payload_json BLOB NOT NULL,
		payload_sha256 TEXT NOT NULL CHECK (length(payload_sha256) = 64)
	)`); err != nil {
		return fmt.Errorf("create CN admin audit schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_admin_action_receipt (
		operation_key TEXT NOT NULL,
		user_id INTEGER NOT NULL,
		request_sha256 TEXT NOT NULL CHECK (length(request_sha256) = 64),
		result_json BLOB NOT NULL,
		result_sha256 TEXT NOT NULL CHECK (length(result_sha256) = 64),
		created_utc TEXT NOT NULL,
		PRIMARY KEY (operation_key, user_id)
	)`); err != nil {
		return fmt.Errorf("create CN admin action receipts: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_friend_point_rental (
		event_id INTEGER PRIMARY KEY AUTOINCREMENT,
		event_key TEXT NOT NULL,
		owner_user_id INTEGER NOT NULL CHECK (owner_user_id > 0),
		renter_user_id INTEGER NOT NULL CHECK (renter_user_id > 0),
		boss_id INTEGER NOT NULL CHECK (boss_id > 0),
		friend_point INTEGER NOT NULL CHECK (friend_point > 0),
		created_utc TEXT NOT NULL,
		UNIQUE (event_key, owner_user_id)
	)`); err != nil {
		return fmt.Errorf("create CN friend-point rental schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE INDEX cn_friend_point_rental_owner_event
		ON cn_friend_point_rental (owner_user_id, event_id)`); err != nil {
		return fmt.Errorf("create CN friend-point rental owner index: %w", err)
	}
	if _, err := transaction.Exec(`CREATE TABLE cn_local_account_follow (
		follower_user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		followed_user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		created_utc TEXT NOT NULL,
		PRIMARY KEY (follower_user_id, followed_user_id),
		CHECK (follower_user_id <> followed_user_id)
	)`); err != nil {
		return fmt.Errorf("create CN local-account follow schema: %w", err)
	}
	if _, err := transaction.Exec(`CREATE INDEX cn_local_account_follow_inbound
		ON cn_local_account_follow (followed_user_id, follower_user_id)`); err != nil {
		return fmt.Errorf("create CN local-account follow inbound index: %w", err)
	}
	if userVersion == 0 {
		if _, err := transaction.Exec(accountCreationDailySchema); err != nil {
			return fmt.Errorf("create CN account creation quota schema: %w", err)
		}
		if _, err := transaction.Exec(giftCodeSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(bossScoreRankingSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(accountRowsSchema); err != nil {
			return err
		}
		if _, err := transaction.Exec(accountCredentialsSchema); err != nil {
			return fmt.Errorf("create CN account credentials schema: %w", err)
		}
		if _, err := transaction.Exec(fmt.Sprintf(`PRAGMA user_version = %d`, SaveDatabaseSchemaVersion)); err != nil {
			return fmt.Errorf("set CN SQLite schema version: %w", err)
		}
	}
	if _, err := transaction.Exec(battleClearStatisticsSchema); err != nil {
		return err
	}
	if _, err := transaction.Exec(partnerRecencyIndexes); err != nil {
		return fmt.Errorf("create CN partner recency indexes: %w", err)
	}
	return validateFixedStateSchema(transaction, SaveDatabaseSchemaVersion)
}

// Existing timestamp columns remain authoritative. These additive indexes also
// handle RFC3339 fractional seconds correctly, without rewriting account rows.
const partnerRecencyIndexes = `
CREATE INDEX IF NOT EXISTS cn_local_account_recent_login
 ON cn_local_account (julianday(last_login_utc) DESC, user_id DESC);
CREATE INDEX IF NOT EXISTS cn_account_projection_recent_update
 ON cn_account_projection (julianday(updated_utc) DESC, user_id DESC);
CREATE INDEX IF NOT EXISTS cn_local_account_follow_recent
 ON cn_local_account_follow (followed_user_id, julianday(created_utc) DESC, follower_user_id DESC);`

// These are account-owned records. Card names and growth definitions remain
// in the runtime master; changing one card never rewrites the other cards.
const accountRowsSchema = `
CREATE TABLE cn_account_balance (
 user_id INTEGER PRIMARY KEY CHECK(user_id >= 1000001),
 gold INTEGER NOT NULL CHECK(gold >= 0), coin INTEGER NOT NULL CHECK(coin >= 0),
 coin_free INTEGER NOT NULL CHECK(coin_free >= 0), friend_point INTEGER NOT NULL CHECK(friend_point >= 0),
 pvp_point INTEGER NOT NULL CHECK(pvp_point >= 0));
CREATE TABLE cn_account_card (
 user_id INTEGER NOT NULL REFERENCES cn_account_balance(user_id) ON DELETE CASCADE,
 unique_id INTEGER NOT NULL CHECK(unique_id > 0), slot INTEGER NOT NULL CHECK(slot IN (0,1)),
 card_id INTEGER NOT NULL CHECK(card_id > 0), level INTEGER NOT NULL,
 experience INTEGER NOT NULL, now_exp INTEGER NOT NULL, next_exp INTEGER NOT NULL,
 love INTEGER NOT NULL, fame INTEGER NOT NULL, is_lock INTEGER NOT NULL CHECK(is_lock IN (0,1)),
 hp INTEGER NOT NULL, attack INTEGER NOT NULL, magic INTEGER NOT NULL, mind INTEGER NOT NULL,
 base_add_price INTEGER NOT NULL, skill_levels TEXT NOT NULL, sort_order INTEGER NOT NULL, row_sha256 TEXT NOT NULL,
 create_time INTEGER NOT NULL DEFAULT 0 CHECK(create_time >= 0),
 PRIMARY KEY(user_id, unique_id));
CREATE TABLE cn_account_item (
 user_id INTEGER NOT NULL REFERENCES cn_account_balance(user_id) ON DELETE CASCADE,
 item_id INTEGER NOT NULL CHECK(item_id > 0), num INTEGER NOT NULL CHECK(num >= 0),
 limit_time INTEGER NOT NULL, sort_order INTEGER NOT NULL, row_sha256 TEXT NOT NULL, PRIMARY KEY(user_id, item_id));
CREATE TABLE cn_account_stack_card (
 user_id INTEGER NOT NULL REFERENCES cn_account_balance(user_id) ON DELETE CASCADE,
 card_id INTEGER NOT NULL CHECK(card_id > 0), num INTEGER NOT NULL CHECK(num >= 0),
 hp INTEGER NOT NULL, attack INTEGER NOT NULL, magic INTEGER NOT NULL, mind INTEGER NOT NULL,
 add_exp INTEGER NOT NULL, base_add_price INTEGER NOT NULL, material_type INTEGER NOT NULL,
 sort_order INTEGER NOT NULL, row_sha256 TEXT NOT NULL, PRIMARY KEY(user_id, card_id));
CREATE TABLE cn_account_present (
 user_id INTEGER NOT NULL REFERENCES cn_account_balance(user_id) ON DELETE CASCADE,
 present_id INTEGER NOT NULL CHECK(present_id > 0), history INTEGER NOT NULL CHECK(history IN (0,1)),
 issued_at_unix INTEGER NOT NULL, add_elapsed_sec INTEGER NOT NULL, limit_time INTEGER NOT NULL,
 state INTEGER NOT NULL, reason INTEGER NOT NULL, title TEXT NOT NULL, comment TEXT NOT NULL,
 url TEXT NOT NULL, auto_fusion_used INTEGER NOT NULL, auto_loveup_used INTEGER NOT NULL,
 admin_key TEXT NOT NULL, rewards_json TEXT NOT NULL, sort_order INTEGER NOT NULL, row_sha256 TEXT NOT NULL,
 PRIMARY KEY(user_id, present_id));`

const accountCredentialsSchema = `CREATE TABLE cn_account_credentials (
		user_id INTEGER PRIMARY KEY REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
		username TEXT NOT NULL UNIQUE,
		password_salt BLOB NOT NULL CHECK(length(password_salt) = 16),
		password_hash BLOB NOT NULL CHECK(length(password_hash) = 32),
		iterations INTEGER NOT NULL CHECK(iterations = 600000),
		updated_utc TEXT NOT NULL)`

const accountCreationDailySchema = `CREATE TABLE cn_account_creation_daily (
 day TEXT NOT NULL,
 source_ip TEXT NOT NULL,
 created_count INTEGER NOT NULL CHECK(typeof(created_count) = 'integer' AND created_count >= 0),
 PRIMARY KEY(day, source_ip)
) WITHOUT ROWID;`
