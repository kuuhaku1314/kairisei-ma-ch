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
)

const GiftBatchesKey = "gift_code_batches"

type GiftBatchRegistry struct {
	NextID  int                  `json:"next_id"`
	Batches []game.GiftCodeBatch `json:"batches"`
}

const giftCodeSchema = `CREATE TABLE cn_gift_code (
 code_id INTEGER PRIMARY KEY AUTOINCREMENT,
 batch_id INTEGER NOT NULL CHECK(batch_id>0),
 canonical TEXT NOT NULL UNIQUE,
 enabled INTEGER NOT NULL CHECK(enabled IN (0,1)));
 CREATE INDEX cn_gift_code_batch ON cn_gift_code(batch_id,code_id);
 CREATE TABLE cn_gift_redemption (
 code_id INTEGER NOT NULL REFERENCES cn_gift_code(code_id),
 scope INTEGER NOT NULL,
 user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id),
 batch_id INTEGER NOT NULL,
 claim_group TEXT,
 claimed_at INTEGER NOT NULL,
 present_ids TEXT NOT NULL,
 reward_revision INTEGER NOT NULL,
 PRIMARY KEY(code_id,scope), UNIQUE(claim_group,user_id));
 CREATE INDEX cn_gift_redemption_batch ON cn_gift_redemption(batch_id,claimed_at);`

type giftQuery interface {
	QueryRow(query string, args ...any) *sql.Row
}

func readGiftRegistry(q giftQuery) (GiftBatchRegistry, int, error) {
	registry := GiftBatchRegistry{NextID: 1, Batches: []game.GiftCodeBatch{}}
	var raw []byte
	var revision int
	var hash string
	err := q.QueryRow(`SELECT revision,payload_json,payload_sha256 FROM cn_global_operation WHERE operation_key=?`, GiftBatchesKey).Scan(&revision, &raw, &hash)
	if errors.Is(err, sql.ErrNoRows) {
		return registry, 0, nil
	}
	if err != nil {
		return registry, 0, err
	}
	digest := sha256.Sum256(raw)
	if hex.EncodeToString(digest[:]) != hash {
		return registry, 0, errors.New("gift batch document digest mismatch")
	}
	err = json.Unmarshal(raw, &registry)
	return registry, revision, err
}

func giftClaimGroup(batch game.GiftCodeBatch) any {
	if batch.ClaimScope == "code" {
		return nil
	}
	if batch.ClaimScope == "group" {
		return "group:" + batch.Group
	}
	return fmt.Sprintf("batch:%d", batch.ID)
}

func lookupGift(q giftQuery, userID int, codeID int64, canonical string, now time.Time) (game.GiftCodeOffer, int, error) {
	var offer game.GiftCodeOffer
	var batchID, enabled int
	query := `SELECT code_id,batch_id,enabled FROM cn_gift_code WHERE canonical=?`
	var key any = canonical
	if codeID != 0 {
		query = `SELECT code_id,batch_id,enabled FROM cn_gift_code WHERE code_id=?`
		key = codeID
	}
	err := q.QueryRow(query, key).Scan(&offer.CodeID, &batchID, &enabled)
	if errors.Is(err, sql.ErrNoRows) {
		return offer, 1, nil
	}
	if err != nil {
		return offer, 0, err
	}
	registry, _, err := readGiftRegistry(q)
	if err != nil {
		return offer, 0, err
	}
	for _, b := range registry.Batches {
		if b.ID == batchID {
			offer.Batch = b
			break
		}
	}
	b := offer.Batch
	if enabled != 1 || !b.Enabled || !b.Published || b.ID == 0 || now.Unix() < b.StartsAt {
		return offer, 1, nil
	}
	if b.EndsAt > 0 && now.Unix() >= b.EndsAt {
		return offer, 2, nil
	}
	scope := userID
	if b.Mode == "single" {
		scope = 0
	}
	var count int
	if err = q.QueryRow(`SELECT count(*) FROM cn_gift_redemption WHERE code_id=? AND scope=?`, offer.CodeID, scope).Scan(&count); err != nil {
		return offer, 0, err
	}
	if count > 0 {
		return offer, 3, nil
	}
	if err = q.QueryRow(`SELECT count(*) FROM cn_gift_redemption WHERE claim_group=? AND user_id=?`, giftClaimGroup(b), userID).Scan(&count); err != nil {
		return offer, 0, err
	}
	if count > 0 {
		return offer, 6, nil
	}
	if b.Limit > 0 {
		if err = q.QueryRow(`SELECT count(*) FROM cn_gift_redemption WHERE batch_id=?`, b.ID).Scan(&count); err != nil {
			return offer, 0, err
		}
		if count >= b.Limit {
			return offer, 3, nil
		}
	}
	return offer, 0, nil
}

func (accounts *Accounts) PrepareGiftCode(userID int, code string, now time.Time) (game.GiftCodeOffer, int, error) {
	accounts.giftMu.Lock()
	if previous, ok := accounts.giftAttempts[userID]; ok && now.Sub(previous) < 3*time.Second {
		accounts.giftMu.Unlock()
		return game.GiftCodeOffer{}, 9, nil
	}
	if accounts.giftAttempts == nil {
		accounts.giftAttempts = map[int]time.Time{}
	}
	if len(accounts.giftAttempts) >= 4096 {
		for id, stamp := range accounts.giftAttempts {
			if now.Sub(stamp) >= 3*time.Second {
				delete(accounts.giftAttempts, id)
			}
		}
	}
	if len(accounts.giftAttempts) >= 4096 {
		accounts.giftMu.Unlock()
		return game.GiftCodeOffer{}, 9, nil
	}
	accounts.giftAttempts[userID] = now
	accounts.giftMu.Unlock()
	canonical, err := game.NormalizeGiftCode(code)
	if err != nil {
		return game.GiftCodeOffer{}, 1, nil
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return game.GiftCodeOffer{}, 0, err
	}
	return lookupGift(db, userID, 0, canonical, now)
}

// The account coordinator already holds this player's lock. Recheck policy,
// global limits and code use inside the single SQLite writer transaction;
// never call an account handler while holding a database transaction.
func (accounts *Accounts) CommitGiftCode(offer game.GiftCodeOffer, state gamestate.State, presentIDs []int64, now time.Time) (int, error) {
	content, err := EncodeAccountMetadata(state)
	if err != nil {
		return 0, err
	}
	db, err := accounts.storage.Open()
	if err != nil {
		return 0, err
	}
	tx, err := db.BeginTx(context.Background(), nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	current, code, err := lookupGift(tx, state.User.UserID, offer.CodeID, "", now)
	if err != nil || code != 0 {
		return code, err
	}
	if current.Batch.Revision != offer.Batch.Revision {
		return 0, errors.New("兑换配置已更新，请重试")
	}
	if len(presentIDs) != len(offer.Batch.Rewards) {
		return 0, errors.New("gift present plan is incomplete")
	}
	scope := state.User.UserID
	if current.Batch.Mode == "single" {
		scope = 0
	}
	rawIDs, err := json.Marshal(presentIDs)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(`INSERT INTO cn_gift_redemption(code_id,scope,user_id,batch_id,claim_group,claimed_at,present_ids,reward_revision) VALUES(?,?,?,?,?,?,?,?)`, offer.CodeID, scope, state.User.UserID, current.Batch.ID, giftClaimGroup(current.Batch), now.Unix(), string(rawIDs), current.Batch.Revision); err != nil {
		return 0, err
	}
	digest := sha256.Sum256(content)
	stamp := now.UTC().Format(time.RFC3339Nano)
	if state.User.UserID == PrimaryUserID {
		err = writePrimaryAccountSnapshot(tx, state, content, hex.EncodeToString(digest[:]), stamp, nil)
	} else {
		err = writeAccountSnapshot(tx, state.User.UserID, state, content, hex.EncodeToString(digest[:]), stamp, nil)
	}
	if err != nil {
		return 0, err
	}
	return 0, tx.Commit()
}
