package accountstore

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

// An additive schema-3 table. No player progress or historical save is rewritten.
const battleClearStatisticsSchema = `CREATE TABLE IF NOT EXISTS cn_battle_clear_daily (
 day INTEGER NOT NULL, boss_id INTEGER NOT NULL, mode INTEGER NOT NULL,
 user_id INTEGER NOT NULL, arthur_type INTEGER NOT NULL, clear_count INTEGER NOT NULL,
 completed_at INTEGER NOT NULL, event_key TEXT NOT NULL, decks_json BLOB NOT NULL,
 decks_sha256 TEXT NOT NULL,
 PRIMARY KEY(day, boss_id, mode, user_id, arthur_type)
);
CREATE INDEX IF NOT EXISTS cn_battle_clear_recent ON cn_battle_clear_daily(boss_id, completed_at DESC);
CREATE INDEX IF NOT EXISTS cn_battle_clear_ranking ON cn_battle_clear_daily(day, boss_id, mode, arthur_type, clear_count DESC);`

func writeBattleClear(tx *sql.Tx, record gamestate.BattleClearRecord) error {
	if record.BossID <= 0 || record.UserID < PrimaryUserID || record.UserID >= SystemPartnerUserIDBase ||
		record.CompletedAt <= 0 || record.EventKey == "" || len(record.EventKey) > 256 ||
		(record.Mode != 0 && record.Mode != 1) || (record.Mode == 0 && record.ArthurType != 0) ||
		(record.Mode == 1 && (record.ArthurType < 1 || record.ArthurType > 4 || len(record.Decks) != 4)) ||
		len(record.Decks) < 1 || len(record.Decks) > 4 {
		return errors.New("invalid battle clear statistics identity")
	}
	seen := map[int]bool{}
	owner := false
	for _, deck := range record.Decks {
		alchemistSolo := record.Mode == 0 && len(record.Decks) == 1 && deck.ArthurType == 5
		if deck.ArthurType < 1 || (deck.ArthurType > 4 && !alchemistSolo) || (record.Mode == 1 && seen[deck.ArthurType]) || deck.UserID <= 0 || len(deck.HonorIDs) != 4 || !json.Valid(deck.Deck) {
			return errors.New("invalid frozen clear deck")
		}
		seen[deck.ArthurType] = true
		owner = owner || (deck.UserID == record.UserID && (record.Mode == 0 || deck.ArthurType == record.ArthurType))
	}
	if !owner {
		return errors.New("clear statistics owner is absent")
	}
	body, err := json.Marshal(record.Decks)
	if err != nil {
		return err
	}
	if len(body) > 128*1024 {
		return errors.New("clear-deck snapshot exceeds storage limit")
	}
	digest := sha256.Sum256(body)
	_, err = tx.Exec(`INSERT INTO cn_battle_clear_daily
 (day,boss_id,mode,user_id,arthur_type,clear_count,completed_at,event_key,decks_json,decks_sha256)
 VALUES(?,?,?,?,?,1,?,?,?,?) ON CONFLICT(day,boss_id,mode,user_id,arthur_type) DO UPDATE SET
 clear_count=clear_count+1,
 decks_json=CASE WHEN excluded.completed_at>=completed_at THEN excluded.decks_json ELSE decks_json END,
 decks_sha256=CASE WHEN excluded.completed_at>=completed_at THEN excluded.decks_sha256 ELSE decks_sha256 END,
 event_key=CASE WHEN excluded.completed_at>=completed_at THEN excluded.event_key ELSE event_key END,
 completed_at=MAX(completed_at,excluded.completed_at)`,
		gamestate.BattleClearDay(record.CompletedAt), record.BossID, record.Mode, record.UserID, record.ArthurType,
		record.CompletedAt, record.EventKey, body, hex.EncodeToString(digest[:]))
	return err
}

func pruneBattleClearStatistics(tx *sql.Tx, now time.Time) error {
	// Only this new rolling display table expires. Permanent boss/story progress
	// and reward/idempotency receipts are not part of this cleanup.
	_, err := tx.Exec(`DELETE FROM cn_battle_clear_daily WHERE day < ?`, gamestate.BattleClearDay(now.Unix())-29)
	return err
}

// PersistSoloBattle commits the accepted outcome and cross-account rental
// inbox events atomically. A result query cannot create any of these effects.
func (accounts *Accounts) PersistSoloBattle(state gamestate.State, result game.SoloBattleCommit) error {
	userID, bossID, receiptSHA := state.User.UserID, result.BossID, result.ReceiptSHA
	if bossID <= 0 || len(receiptSHA) != 64 || (result.Clear != nil && (result.Clear.UserID != userID || result.Clear.BossID != bossID || result.Clear.Mode != 0)) {
		return errors.New("solo clear ownership mismatch")
	}
	matched := false
	for _, receipt := range state.TeamBattleSoloResultReceipts {
		if receipt.RequestSHA256 == receiptSHA && receipt.BossID == bossID {
			matched = true
			break
		}
	}
	if !matched {
		return errors.New("solo clear has no result receipt")
	}
	content, err := EncodeAccountMetadata(state)
	if err != nil {
		return err
	}
	db, err := accounts.storage.Open()
	if err != nil {
		return err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var old []byte
	if userID == PrimaryUserID {
		err = tx.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&old)
	} else {
		err = tx.QueryRow(`SELECT payload_json FROM cn_account_snapshot WHERE user_id=?`, userID).Scan(&old)
	}
	if err != nil {
		return err
	}
	var previous struct {
		Receipts []gamestate.TeamBattleSoloResultReceipt `json:"team_battle_solo_result_receipts"`
	}
	if err = json.Unmarshal(old, &previous); err != nil {
		return err
	}
	for _, receipt := range previous.Receipts {
		if receipt.RequestSHA256 == receiptSHA && receipt.BossID == bossID {
			return nil
		}
	}
	digest := sha256.Sum256(content)
	updated := time.Now().UTC().Format(time.RFC3339Nano)
	if userID == PrimaryUserID {
		err = writePrimaryAccountSnapshot(tx, state, content, hex.EncodeToString(digest[:]), updated, nil)
	} else {
		err = writeAccountSnapshot(tx, userID, state, content, hex.EncodeToString(digest[:]), updated, nil)
	}
	if err != nil {
		return err
	}
	if result.Clear != nil {
		if err = writeBattleClear(tx, *result.Clear); err != nil {
			return err
		}
		if err = pruneBattleClearStatistics(tx, time.Now()); err != nil {
			return err
		}
	}
	if len(result.RentalCredits) != 0 {
		if err = writeFriendPointRentals(tx, result.RentalEventKey, userID, result.RentalCredits, bossID); err != nil {
			return err
		}
	}
	if result.Score != nil {
		if err = writeBossScoreResult(tx, userID, gamestate.BattleScoreSolo, *result.Score); err != nil {
			return err
		}
	}
	if err = tx.Commit(); err != nil {
		return err
	}
	accounts.RememberBattle(userID, state.ActiveTeamBattle, state.StoryTeamBattleSession)
	return nil
}

// Called only for a newly inserted immutable BattleSv completion, within its
// existing transaction. Death exits and system/CPU slots never receive counts.
func writeMultiplayerClear(tx *sql.Tx, completed multiplayer.CompletedBattle) error {
	if completed.EndType == 2 {
		return nil
	}
	decks := make([]gamestate.BattleClearDeck, 0, 4)
	for _, member := range completed.Members {
		if len(member.ClearDeck) == 0 {
			return nil
		} // Pre-update in-flight room.
		var identity struct {
			UserID int `json:"userid"`
		}
		if err := json.Unmarshal(member.ClearDeck, &identity); err != nil {
			return err
		}
		decks = append(decks, gamestate.BattleClearDeck{UserID: identity.UserID, Name: member.Name, ArthurType: member.ArthurType, HonorIDs: member.DeckHonorIDs, Deck: member.ClearDeck})
	}
	for _, userID := range completed.OnlineUserIDs {
		if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
			continue
		}
		for _, member := range completed.Members {
			if member.UserID != userID {
				continue
			}
			if err := writeBattleClear(tx, gamestate.BattleClearRecord{BossID: completed.BossID, UserID: userID, Mode: 1, ArthurType: member.ArthurType,
				CompletedAt: completed.CompletedAtUnix, EventKey: fmt.Sprintf("multi:%d", completed.RoomID), Decks: decks}); err != nil {
				return err
			}
			break
		}
	}
	return pruneBattleClearStatistics(tx, time.Now())
}

func (accounts *Accounts) queryBattleClears(query string, args ...any) ([]gamestate.BattleClearRecord, error) {
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return nil, err
	}
	rows, err := db.QueryContext(context.Background(), query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := []gamestate.BattleClearRecord{}
	for rows.Next() {
		var r gamestate.BattleClearRecord
		var data []byte
		var expected string
		if err := rows.Scan(&r.BossID, &r.UserID, &r.Mode, &r.ArthurType, &r.Count, &r.CompletedAt, &r.EventKey, &data, &expected); err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data)
		if len(data) > 128*1024 || hex.EncodeToString(digest[:]) != expected {
			return nil, errors.New("clear-deck digest mismatch")
		}
		if err := json.Unmarshal(data, &r.Decks); err != nil {
			return nil, err
		}
		result = append(result, r)
	}
	return result, rows.Err()
}

const battleClearColumns = "boss_id,user_id,mode,arthur_type,clear_count,completed_at,event_key,decks_json,decks_sha256"

func (accounts *Accounts) RecentBattleClears(bossID int, now time.Time) ([]gamestate.BattleClearRecord, error) {
	return accounts.queryBattleClears(`SELECT `+battleClearColumns+` FROM cn_battle_clear_daily
 WHERE boss_id=? AND day>=? ORDER BY completed_at DESC,user_id LIMIT 100`, bossID, gamestate.BattleClearDay(now.Unix())-29)
}

func (accounts *Accounts) YesterdayBattleRanks(bossID, mode int, now time.Time) ([]gamestate.BattleClearRecord, error) {
	if mode != 0 && mode != 1 {
		return nil, errors.New("invalid ranking mode")
	}
	// Read one bounded snapshot. Ties use user ID for a stable presentation;
	// callers give equal counts the same displayed rank.
	return accounts.queryBattleClears(`SELECT `+battleClearColumns+` FROM (
 SELECT *,ROW_NUMBER() OVER(PARTITION BY arthur_type ORDER BY clear_count DESC,user_id) AS position
 FROM cn_battle_clear_daily WHERE day=? AND boss_id=? AND mode=?)
 WHERE position<=10 ORDER BY arthur_type,clear_count DESC,user_id`, gamestate.BattleClearDay(now.Unix())-1, bossID, mode)
}
