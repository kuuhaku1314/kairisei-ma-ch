package accountstore

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// TowerRanking derives a small public view directly from committed progression.
// It shares the existing read pool and one SQLite snapshot with public profile
// and leader-card rows. No Account is loaded/locked, no inventory is decoded,
// and no independent ranking state, backfill or first-achievement time exists.
func (accounts *Accounts) TowerRanking(towerID, floorCount, userID, offset, limit int) (game.TowerRankingPage, error) {
	page := game.TowerRankingPage{Entries: []game.TowerRankRecord{}}
	if towerID <= 0 || floorCount < 1 || userID < PrimaryUserID || offset < 0 || limit < 1 || limit > 100 {
		return page, errors.New("invalid tower ranking request")
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
	rows, err := tx.Query(`WITH players AS (
 SELECT a.user_id, CAST(p.payload_json AS TEXT) AS profile,
 CAST(CASE WHEN a.user_id=? THEN primary_save.payload_json ELSE s.payload_json END AS TEXT) AS snapshot
 FROM cn_local_account a
 JOIN cn_account_projection p ON p.user_id=a.user_id
 LEFT JOIN cn_account_snapshot s ON s.user_id=a.user_id
 LEFT JOIN cn_save_snapshot primary_save ON a.user_id=? AND primary_save.singleton=1
 WHERE a.user_id>=? AND a.user_id<?
)
SELECT p.user_id, json_extract(p.profile,'$.user.name'), json_extract(p.profile,'$.user.level'),
 json_extract(p.profile,'$.user.leader_card_id'), COALESCE(c.level,1), COALESCE(c.love,0), COALESCE(c.fame,1), progress.value
 FROM players p
 JOIN json_each(p.snapshot,'$.tower_quest_progress') progress
 LEFT JOIN cn_account_card c ON c.user_id=p.user_id AND c.unique_id=json_extract(p.profile,'$.user.leader_card_unique_id')
 WHERE json_extract(progress.value,'$.tower_id')=?`, PrimaryUserID, PrimaryUserID, PrimaryUserID, SystemPartnerUserIDBase, towerID)
	if err != nil {
		return page, fmt.Errorf("read tower ranking: %w", err)
	}
	all := []game.TowerRankRecord{}
	for rows.Next() {
		var record game.TowerRankRecord
		var raw []byte
		if err = rows.Scan(&record.UserID, &record.Name, &record.Level, &record.LeaderCardID,
			&record.LeaderCardLV, &record.LeaderCardLove, &record.LeaderCardFame, &raw); err != nil {
			rows.Close()
			return page, err
		}
		var progress gamestate.TowerQuestProgress
		if err = json.Unmarshal(raw, &progress); err != nil {
			rows.Close()
			return page, err
		}
		record.Floor, record.Score = game.TowerQuestRecord(progress, floorCount)
		if record.Floor > 0 {
			all = append(all, record)
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return page, err
	}
	if err = tx.Commit(); err != nil {
		return page, err
	}
	// LOCAL_POLICY: no official ordering or attainment timestamp was recovered.
	sort.Slice(all, func(i, j int) bool {
		if all[i].Floor != all[j].Floor {
			return all[i].Floor > all[j].Floor
		}
		if all[i].Score != all[j].Score {
			return all[i].Score > all[j].Score
		}
		return all[i].UserID < all[j].UserID
	})
	page.Total = len(all)
	for i := range all {
		all[i].RankNumber = i + 1
		if all[i].UserID == userID {
			self := all[i]
			page.Self = &self
		}
	}
	start := min(offset, len(all))
	page.Entries = append(page.Entries, all[start:min(start+limit, len(all))]...)
	return page, nil
}
