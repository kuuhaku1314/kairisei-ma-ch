package accountstore

import (
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestExistingVersionFivePreservesAccountsWhenAddingScoreRanking(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	storage, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	account := []byte(`{"user":{"userid":1000001,"name":"保留的玩家"}}`)
	digest := sha256.Sum256(account)
	if _, err := db.Exec(`INSERT INTO cn_local_account
	 (user_id,login_uuid,session_key,created_utc,last_login_utc)
	 VALUES(1000001,'account-uuid','session-token','2026-10-24T00:00:00Z','2026-10-25T00:00:00Z');
	 INSERT INTO cn_save_snapshot
	 (singleton,schema_version,revision,updated_utc,payload_json,payload_sha256)
	 VALUES(1,1,1,'2026-10-25T00:00:00Z',?,?);
	 INSERT INTO cn_gift_code(batch_id,canonical,enabled) VALUES(7,'KEEP-THIS-CODE',1);
	 DROP TABLE cn_boss_score_rank;
	 DROP TABLE cn_account_creation_daily; ALTER TABLE cn_local_account DROP COLUMN login_block_until_unix; PRAGMA user_version=5`, account, hex.EncodeToString(digest[:])); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if err := upgraded.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err = upgraded.Open()
	if err != nil {
		t.Fatal(err)
	}
	var version, accounts, codes, rankTables int
	var persisted []byte
	if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_local_account WHERE user_id=1000001`).Scan(&accounts); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&persisted); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_gift_code WHERE canonical='KEEP-THIS-CODE'`).Scan(&codes); err != nil {
		t.Fatal(err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='cn_boss_score_rank'`).Scan(&rankTables); err != nil {
		t.Fatal(err)
	}
	if version != SaveDatabaseSchemaVersion || accounts != 1 || codes != 1 || rankTables != 1 || string(persisted) != string(account) {
		t.Fatalf("schema upgrade changed account/gift data: version=%d accounts=%d codes=%d rank=%d", version, accounts, codes, rankTables)
	}
}

func TestCreationQuotaSchemaUpgradeAndCorruption(t *testing.T) {
	for _, test := range []struct {
		name      string
		setup     string
		wantError bool
		version   int
	}{
		{"version7", `DROP TABLE cn_account_creation_daily; PRAGMA user_version=7;`, false, 7},
		{"version4", `DROP TABLE cn_account_creation_daily; DROP TABLE cn_gift_redemption; DROP TABLE cn_gift_code; DROP TABLE cn_boss_score_rank; PRAGMA user_version=4;`, false, 4},
		{"version7-collision", `PRAGMA user_version=7;`, true, 7},
		{"version8", `PRAGMA user_version=8;`, false, 8},
		{"current-missing", `DROP TABLE cn_account_creation_daily;`, true, SaveDatabaseSchemaVersion},
		{"current-key-order", `DROP TABLE cn_account_creation_daily; CREATE TABLE cn_account_creation_daily(day TEXT NOT NULL,source_ip TEXT NOT NULL,created_count INTEGER NOT NULL,PRIMARY KEY(source_ip,day)) WITHOUT ROWID;`, true, SaveDatabaseSchemaVersion},
		{"current-type", `DROP TABLE cn_account_creation_daily; CREATE TABLE cn_account_creation_daily(day TEXT NOT NULL,source_ip TEXT NOT NULL,created_count TEXT NOT NULL,PRIMARY KEY(day,source_ip)) WITHOUT ROWID;`, true, SaveDatabaseSchemaVersion},
		{"current-nullable", `DROP TABLE cn_account_creation_daily; CREATE TABLE cn_account_creation_daily(day TEXT NOT NULL,source_ip TEXT NOT NULL,created_count INTEGER,PRIMARY KEY(day,source_ip)) WITHOUT ROWID;`, true, SaveDatabaseSchemaVersion},
	} {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "save.json")
			storage, err := OpenDatabase(path, "unused-seed.json", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = storage.Close() })
			if err := storage.EnsureSchema(); err != nil {
				t.Fatal(err)
			}
			db, _ := storage.Open()
			setup := test.setup
			if test.version < 9 {
				setup = `ALTER TABLE cn_local_account DROP COLUMN login_block_until_unix;` + setup
			}
			if _, err := db.Exec(`INSERT INTO cn_local_account (user_id,login_uuid,session_key,created_utc,last_login_utc)
				VALUES(1000001,'kept-uuid','kept-session','created','login');` + setup); err != nil {
				t.Fatal(err)
			}
			if err := storage.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := OpenDatabase(path, "unused-seed.json", nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			err = reopened.EnsureSchema()
			if (err != nil) != test.wantError {
				t.Fatalf("schema result: %v", err)
			}
			db, _ = reopened.Open()
			var version, accounts int
			if err := db.QueryRow(`PRAGMA user_version`).Scan(&version); err != nil {
				t.Fatal(err)
			}
			if err := db.QueryRow(`SELECT COUNT(*) FROM cn_local_account WHERE login_uuid='kept-uuid'`).Scan(&accounts); err != nil {
				t.Fatal(err)
			}
			wantVersion := SaveDatabaseSchemaVersion
			if test.wantError {
				wantVersion = test.version
			}
			if version != wantVersion || accounts != 1 {
				t.Fatal("migration failure altered schema version or account", version, accounts)
			}
			if !test.wantError {
				for _, value := range []any{-1, 0.5, "bad"} {
					if _, err := db.Exec(`INSERT INTO cn_account_creation_daily VALUES('2026-11-04','192.0.2.1',?)`, value); err == nil {
						t.Fatalf("invalid integer count accepted: %v", value)
					}
				}
			}
		})
	}
}

func TestVersionSixKeepsExistingScoresInMultiplayerMode(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	storage, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`INSERT INTO cn_local_account (user_id,login_uuid,session_key,created_utc,last_login_utc)
 VALUES(1000001,'uuid','session','created','login');
DROP TABLE cn_boss_score_rank;
CREATE TABLE cn_boss_score_rank(boss_id INTEGER NOT NULL,user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id),best_score INTEGER NOT NULL,best_grade INTEGER NOT NULL,best_at_unix INTEGER NOT NULL,PRIMARY KEY(boss_id,user_id));
CREATE INDEX cn_boss_score_rank_order ON cn_boss_score_rank(boss_id,best_score DESC,best_at_unix ASC,user_id ASC);
INSERT INTO cn_boss_score_rank VALUES(37,1000001,8800,7,1000);
DROP TABLE cn_account_creation_daily; ALTER TABLE cn_local_account DROP COLUMN login_block_until_unix; PRAGMA user_version=6;`)
	if err != nil {
		t.Fatal(err)
	}
	if err = storage.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if err = upgraded.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err = upgraded.Open()
	if err != nil {
		t.Fatal(err)
	}
	var mode, grade int
	var score, at int64
	if err = db.QueryRow(`SELECT mode,best_score,best_grade,best_at_unix FROM cn_boss_score_rank WHERE boss_id=37 AND user_id=1000001`).Scan(&mode, &score, &grade, &at); err != nil || mode != 1 || score != 8800 || grade != 7 || at != 1000 {
		t.Fatal("old multiplayer rank was lost or became solo", mode, score, grade, at, err)
	}
	if _, err = db.Exec(`INSERT INTO cn_boss_score_rank VALUES(37,0,1000001,9000,8,2000)`); err != nil {
		t.Fatal("migrated key cannot store independent solo score", err)
	}
}

func TestCardAcquisitionTimeMigratesUnknownDatesAndPersistsNewDates(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	storage, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Reproduce a real version-3 inventory, including its unchanged row digest.
	if _, err := db.Exec(`ALTER TABLE cn_account_card DROP COLUMN create_time;
 DROP TABLE cn_gift_redemption; DROP TABLE cn_gift_code; DROP TABLE cn_boss_score_rank;
 DROP TABLE cn_account_creation_daily; ALTER TABLE cn_local_account DROP COLUMN login_block_until_unix; PRAGMA user_version = 3;
 INSERT INTO cn_account_balance VALUES(1000001,100,0,0,0,0);
 INSERT INTO cn_account_card VALUES(1000001,7,0,5001,1,0,0,0,0,5,0,0,0,0,0,0,'[1]',1024,'old-digest');`); err != nil {
		t.Fatal(err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = upgraded.Close() })
	if err := upgraded.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err = upgraded.Open()
	if err != nil {
		t.Fatal(err)
	}
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	state := gamestate.State{CardTemplates: []gamestate.Card{{CardID: 5001, Name: "master"}}}
	state.User.UserID = 1000001
	if err := readAccountRows(tx, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Cards) != 1 || state.Cards[0].UniqueID != 7 || state.Cards[0].CreateTime != 0 || state.Cards[0].Fame != 5 || state.User.Gold != 100 {
		t.Fatalf("migration changed the old inventory: %+v", state)
	}
	newCard := state.Cards[0]
	newCard.UniqueID, newCard.CreateTime = 8, 1792368000
	state.Cards = append(state.Cards, newCard)
	if err := writeAccountRows(tx, state); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err = reopened.Open()
	if err != nil {
		t.Fatal(err)
	}
	tx, err = db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if err := readAccountRows(tx, &state); err != nil {
		t.Fatal(err)
	}
	if len(state.Cards) != 2 || state.Cards[0].CreateTime != 0 || state.Cards[1].CreateTime != newCard.CreateTime {
		t.Fatalf("reopened inventory lost acquisition dates: %+v", state.Cards)
	}
}

func TestOldDatabaseIsRejectedWithoutReset(t *testing.T) {
	for _, version := range []int{0, 1, 2} {
		storage, err := OpenDatabase(filepath.Join(t.TempDir(), "save.json"), filepath.Join("..", "..", "config", "cn602-save-template.json"), slog.New(slog.NewTextHandler(io.Discard, nil)))
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if err := storage.Close(); err != nil {
				t.Error(err)
			}
		})
		path := storage.databasePath
		database, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := database.Exec(fmt.Sprintf(`PRAGMA user_version = %d; CREATE TABLE preserved(value TEXT); INSERT INTO preserved VALUES ('keep')`, version)); err != nil {
			t.Fatal(err)
		}
		if err := database.Close(); err != nil {
			t.Fatal(err)
		}
		if _, err := storage.LoadOrImport(); err == nil || !strings.Contains(err.Error(), "unsupported") {
			t.Fatalf("old database error = %v", err)
		}
		if _, err := os.Stat(path); err != nil {
			t.Fatal(err)
		}
		database, err = sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		var value string
		if err := database.QueryRow(`SELECT value FROM preserved`).Scan(&value); err != nil || value != "keep" {
			t.Fatalf("old database was altered: %q %v", value, err)
		}
		database.Close()
	}
}

func TestCurrentSchemaIsValidatedAtStartupNotRepaired(t *testing.T) {
	path := filepath.Join(t.TempDir(), "save.json")
	storage, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = storage.Close() })
	if err := storage.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	// Current saves gain only the additive table and lookup indexes.
	if _, err := db.Exec("DROP TABLE cn_battle_clear_daily; DROP INDEX cn_local_account_recent_login; DROP INDEX cn_account_projection_recent_update"); err != nil {
		t.Fatal(err)
	}
	upgraded, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := upgraded.EnsureSchema(); err != nil {
		t.Fatal(err)
	}
	if err := upgraded.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("DROP TABLE cn_account_credentials"); err != nil {
		t.Fatal(err)
	}
	// Once initialized, requests do not scan or recreate schema behind the
	// operator's back. A fresh owner must detect the broken current schema.
	if err := storage.EnsureSchema(); err != nil {
		t.Fatalf("schema was rescanned: %v", err)
	}
	if err := storage.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := OpenDatabase(path, "unused-seed.json", nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	if err := reopened.EnsureSchema(); err == nil || !strings.Contains(err.Error(), "cn_account_credentials") {
		t.Fatalf("missing schema was accepted: %v", err)
	}
	db, err = reopened.Open()
	if err != nil {
		t.Fatal(err)
	}
	var count int
	if err := db.QueryRow("SELECT count(*) FROM sqlite_master WHERE name='cn_account_credentials'").Scan(&count); err != nil || count != 0 {
		t.Fatalf("missing table was recreated: %d %v", count, err)
	}
}

func writeAccountRows(tx *sql.Tx, state gamestate.State) error {
	return writeChangedAccountRows(tx, state, accountComponentHashes{}, accountComponentHashes{})
}
