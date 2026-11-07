package httpapi

import (
	"kairisei.local/server/internal/gamestate"
	"net/http"
)

// A boss closed to battles keeps its ranking, so players can still read the final standings.
func (a *API) scoreRankingBossAvailable(bossID int) bool {
	if bossID <= 0 {
		return false
	}
	for _, profile := range a.initialState.TeamBattleRewards {
		if profile.BossID == bossID && profile.StageQuestAreaID == 0 && profile.TowerID == 0 &&
			profile.ScorePolicy != nil && profile.ScorePolicy.SourceState == "LOCAL_POLICY_DAMAGE_SCORE" {
			return true
		}
	}
	return false
}

func (a *API) bossScoreRankingShow(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		Mode   gamestate.BattleScoreMode `json:"mode"`
		BossID int                       `json:"bossid"`
		Offset int                       `json:"offset"`
		Limit  int                       `json:"limit"`
	}
	if err := decodeExact(r, []string{"bossid", "mode"}, &payload, "offset", "limit"); err != nil {
		a.writeStoreError(w, err)
		return
	}
	payload.BossID = a.scoreBossID(payload.BossID)
	if !payload.Mode.Valid() || payload.Offset < 0 || payload.Limit < 0 || payload.Limit > 100 || !a.scoreRankingBossAvailable(payload.BossID) {
		writeError(w, http.StatusBadRequest, "invalid boss score ranking request")
		return
	}
	if payload.Limit == 0 {
		payload.Limit = 50
	}
	if a.bossScoreRankings == nil {
		writeError(w, http.StatusServiceUnavailable, "boss score ranking is unavailable")
		return
	}
	page, err := a.bossScoreRankings.BossScoreRanking(payload.BossID, payload.Mode, a.initialState.User.UserID, payload.Offset, payload.Limit)
	if err != nil {
		a.writeStoreError(w, err)
		return
	}
	// Keep the native account protocol envelope while the modern client owns
	// this new leaderboard presentation. No ranking state lives in the save.
	a.writeProtocol(w, map[string]any{
		"bossid": payload.BossID, "mode": payload.Mode, "total": page.Total,
		"entries": page.Entries, "self": page.Self,
	})
}
