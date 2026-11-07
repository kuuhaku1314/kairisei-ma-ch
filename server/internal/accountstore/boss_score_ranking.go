package accountstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

// Solo and multiplayer have separate best scores and first-attainment times.
// Schema 7 retains existing schema-6 entries as multiplayer records. Old
// account high scores have no trustworthy attainment time to backfill.
const bossScoreRankingSchema = `CREATE TABLE cn_boss_score_rank (
 boss_id INTEGER NOT NULL CHECK(boss_id > 0),
 mode INTEGER NOT NULL CHECK(mode IN (0,1)),
 user_id INTEGER NOT NULL REFERENCES cn_local_account(user_id) ON DELETE CASCADE,
 best_score INTEGER NOT NULL CHECK(best_score > 0),
 best_grade INTEGER NOT NULL CHECK(best_grade BETWEEN 0 AND 8),
 best_at_unix INTEGER NOT NULL CHECK(best_at_unix > 0),
 PRIMARY KEY(boss_id,mode,user_id)
);
CREATE INDEX cn_boss_score_rank_order
 ON cn_boss_score_rank(boss_id,mode,best_score DESC,best_at_unix ASC,user_id ASC);`

func writeBossScoreResult(tx *sql.Tx, userID int, mode gamestate.BattleScoreMode, result gamestate.BossScoreResult) error {
	if !mode.Valid() || result.BossID <= 0 || userID < PrimaryUserID || userID >= SystemPartnerUserIDBase ||
		result.Score <= 0 || result.Grade < 0 || result.Grade > 8 || result.AchievedAt <= 0 {
		return errors.New("invalid boss score result")
	}
	_, err := tx.Exec(`INSERT INTO cn_boss_score_rank
 (boss_id,mode,user_id,best_score,best_grade,best_at_unix) VALUES(?,?,?,?,?,?)
 ON CONFLICT(boss_id,mode,user_id) DO UPDATE SET
 best_score=excluded.best_score,best_grade=excluded.best_grade,best_at_unix=excluded.best_at_unix
 WHERE excluded.best_score>best_score OR
 (excluded.best_score=best_score AND excluded.best_at_unix<best_at_unix)`,
		result.BossID, mode, userID, result.Score, result.Grade, result.AchievedAt)
	if err != nil {
		return fmt.Errorf("record boss score for user %d: %w", userID, err)
	}
	return nil
}

// Called only for the first insertion of an immutable room completion, inside
// its SQLite transaction. An accepted score counts even when the boss was not
// defeated; the battle's frozen policy and server damage are authoritative.
func writeBossScoreRanking(tx *sql.Tx, completed multiplayer.CompletedBattle) error {
	policy := completed.ScorePolicy
	if policy == nil || policy.SourceState != "LOCAL_POLICY_DAMAGE_SCORE" || completed.ScoreDamage <= 0 {
		return nil
	}
	score, grade := game.VerifiedTeamBattleDamageScore(policy, completed.ScoreDamage)
	if score <= 0 {
		return nil
	}
	for _, userID := range completed.OnlineUserIDs {
		if userID < PrimaryUserID || userID >= SystemPartnerUserIDBase {
			continue
		}
		if err := writeBossScoreResult(tx, userID, gamestate.BattleScoreMultiplayer, gamestate.BossScoreResult{
			BossID: completed.BossID, Score: score, Grade: grade, AchievedAt: completed.CompletedAtUnix,
		}); err != nil {
			return err
		}
	}
	return nil
}

const bossScoreRankName = `COALESCE(json_extract(CAST(p.payload_json AS TEXT),'$.user.name'),'')`

func (accounts *Accounts) BossScoreRanking(bossID int, mode gamestate.BattleScoreMode, userID, offset, limit int) (game.BossScoreRankingPage, error) {
	page := game.BossScoreRankingPage{Entries: []game.BossScoreRank{}}
	if bossID <= 0 || !mode.Valid() || userID < PrimaryUserID || offset < 0 || limit < 1 || limit > 100 {
		return page, errors.New("invalid boss score ranking request")
	}
	db, err := accounts.storage.OpenRead()
	if err != nil {
		return page, err
	}
	tx, err := db.BeginTx(context.Background(), &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return page, err
	}
	defer tx.Rollback()
	if err = tx.QueryRow(`SELECT COUNT(*) FROM cn_boss_score_rank WHERE boss_id=? AND mode=?`, bossID, mode).Scan(&page.Total); err != nil {
		return page, err
	}
	rows, err := tx.Query(`SELECT r.user_id,`+bossScoreRankName+`,r.best_grade,r.best_score,r.best_at_unix
 FROM cn_boss_score_rank r LEFT JOIN cn_account_projection p ON p.user_id=r.user_id
 WHERE r.boss_id=? AND r.mode=? ORDER BY r.best_score DESC,r.best_at_unix ASC,r.user_id ASC
 LIMIT ? OFFSET ?`, bossID, mode, limit, offset)
	if err != nil {
		return page, err
	}
	for rows.Next() {
		entry := game.BossScoreRank{Rank: offset + len(page.Entries) + 1}
		if err = rows.Scan(&entry.UserID, &entry.Name, &entry.Grade, &entry.Score, &entry.AchievedAt); err != nil {
			rows.Close()
			return page, err
		}
		page.Entries = append(page.Entries, entry)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	self := &game.BossScoreRank{}
	err = tx.QueryRow(`SELECT r.user_id,`+bossScoreRankName+`,r.best_grade,r.best_score,r.best_at_unix
 FROM cn_boss_score_rank r LEFT JOIN cn_account_projection p ON p.user_id=r.user_id
 WHERE r.boss_id=? AND r.mode=? AND r.user_id=?`, bossID, mode, userID).Scan(
		&self.UserID, &self.Name, &self.Grade, &self.Score, &self.AchievedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return page, tx.Commit()
	}
	if err != nil {
		return page, err
	}
	err = tx.QueryRow(`SELECT COUNT(*)+1 FROM cn_boss_score_rank WHERE boss_id=? AND mode=? AND
 (best_score>? OR best_score=? AND best_at_unix<? OR
 best_score=? AND best_at_unix=? AND user_id<?)`,
		bossID, mode, self.Score, self.Score, self.AchievedAt, self.Score, self.AchievedAt, userID).Scan(&self.Rank)
	if err != nil {
		return page, err
	}
	page.Self = self
	return page, tx.Commit()
}
