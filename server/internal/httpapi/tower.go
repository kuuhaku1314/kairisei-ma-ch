package httpapi

import (
	"encoding/json"
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) towerQuestShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TowerID int `json:"towerid"`
	}
	if err := decodeExact(request, []string{"towerid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, progress, consumed, err := a.account.TowerQuestShowState(payload.TowerID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if consumed && !a.persistOrError(writer) {
		return
	}

	wins := []any{}
	loses := []any{}
	if progress.LastResult == "win" {
		wins = append(wins, map[string]any{
			"is_rank_up": game.BoolInt(progress.LastRankUp),
			"lose_count": progress.LastResultLoseCount,
		})
	} else if progress.LastResult == "lose" {
		loses = append(loses, map[string]any{"lose_count": progress.LastResultLoseCount})
	}
	// The client excludes the challenge rank to build its lower-rank replay
	// list, so this projection must not include ranks the player has not reached.
	challengeRank := 0
	if progress.Floor != 0 {
		challengeRank = profile.Floors[progress.Floor-1].Rank
	}
	ranks := make([]any, 0, len(profile.Ranks))
	for _, rank := range profile.Ranks {
		if challengeRank != 0 && rank.Rank > challengeRank {
			continue
		}
		ranks = append(ranks, map[string]any{"rank": rank.Rank, "rank_name": rank.RankName})
	}
	floors := make([]any, 0, len(profile.Floors))
	for _, floor := range profile.Floors {
		var source map[string]any
		if err := json.Unmarshal(floor.Boss, &source); err != nil {
			a.writeStoreError(writer, err)
			return
		}
		boss, err := teamBattleBossNamedDTO(source)
		if err != nil {
			a.writeStoreError(writer, err)
			return
		}
		trimQuestBossDecorations(boss)
		clearRewards := make([]any, 0, len(floor.ClearRewards))
		for _, reward := range floor.ClearRewards {
			clearRewards = append(clearRewards, towerRewardWire(reward))
		}
		floors = append(floors, map[string]any{
			"rank": floor.Rank, "floor": floor.Floor,
			"boss": boss, "story": floor.Story,
			"clear_rewards": clearRewards,
			"itemid":        floor.ItemID, "item_use": floor.ItemUse,
			"bp_use": floor.BPUse, "clear_rating_flag": []int{progress.ClearRatings[floor.Floor]},
		})
	}
	a.writeProtocol(writer, map[string]any{
		"user": a.userPayload(), "win": wins, "lose": loses,
		"floor": progress.Floor, "last_battle_floor": progress.LastBattleFloor,
		"lose_count_now": progress.LoseCount, "lose_count_max": profile.LoseCountMax,
		"ranks": ranks, "floors": floors,
		"is_all_clear": game.BoolInt(progress.Floor == 0), "all_clear_text": profile.AllClearText,
	})
}

type towerRankWire struct {
	RankNumber     int    `json:"rank_number"`
	UserID         int    `json:"userid"`
	Level          int    `json:"lv"`
	Name           string `json:"name"`
	Rank           int    `json:"rank"`
	Floor          int    `json:"floor"`
	Score          int64  `json:"score"`
	RankName       string `json:"rank_name"`
	LeaderCardID   int    `json:"leader_cardid"`
	LeaderCardLV   int    `json:"leader_card_lv"`
	LeaderCardLove int    `json:"leader_card_love"`
	LeaderCardFame int    `json:"leader_card_fame"`
}

func (a *API) towerRankingShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TowerID int `json:"towerid"`
	}
	if err := decodeExact(request, []string{"towerid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, _, err := a.account.TowerQuestRankingState(payload.TowerID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if a.towerRankings == nil {
		writeError(writer, http.StatusServiceUnavailable, "tower ranking is unavailable")
		return
	}
	// The original request has no paging parameters. LOCAL_POLICY: show the
	// top ten plus the requesting player's actual rank when outside that page.
	page, err := a.towerRankings.TowerRanking(payload.TowerID, len(profile.Floors), a.initialState.User.UserID, 0, 10)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	entries := page.Entries
	if page.Self != nil && page.Self.RankNumber > 10 {
		entries = append(entries, *page.Self)
	}
	ranking := make([]towerRankWire, 0, len(entries))
	for _, row := range entries {
		rank := profile.Floors[row.Floor-1].Rank
		ranking = append(ranking, towerRankWire{RankNumber: row.RankNumber, UserID: row.UserID, Level: row.Level,
			Name: row.Name, Rank: rank, Floor: row.Floor, Score: row.Score, RankName: profile.Ranks[rank-1].RankName,
			LeaderCardID: row.LeaderCardID, LeaderCardLV: row.LeaderCardLV, LeaderCardLove: row.LeaderCardLove, LeaderCardFame: row.LeaderCardFame})
	}
	a.writeProtocol(writer, struct {
		Ranking []towerRankWire `json:"ranking"`
	}{ranking})
}

func towerRewardWire(reward gamestate.Reward) map[string]any {
	return map[string]any{
		"type": reward.Type, "num": reward.Num,
		"reward_typeid": reward.RewardTypeID,
		"card_lv":       reward.CardLevel, "card_fame": reward.CardFame,
		"card_love":     reward.CardLove,
		"card_skill_lv": append([]int16(nil), reward.CardSkillLevels...),
	}
}
