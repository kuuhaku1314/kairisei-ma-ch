//go:build workshop

package game

import "kairisei.local/server/internal/gamestate"

// The same response fields consumed by the original result panel. A workshop
// run has no previous personal record and never grants the planned rewards.
type WorkshopScoreInfo struct {
	Damage    int64 `json:"damage"`
	Rate      int   `json:"rate"`
	Grade     int   `json:"grade"`
	HighScore int64 `json:"high_score"`
}

func WorkshopScorePreview(policy *gamestate.TeamBattleScorePolicy, damage int64, turns int) (int64, int, *WorkshopScoreInfo, error) {
	if err := gamestate.ValidateTeamBattleScorePolicy(policy); err != nil {
		return 0, 0, nil, err
	}
	progress, _, rows := planTeamBattleScore(policy, gamestate.TeamBattleScoreProgress{}, TeamBattleDropReport{ScoreVerified: true, ScoreDamage: damage, Turns: turns})
	count := 0
	for i := 0; i < 9; i++ {
		if progress.Claimed&(1<<i) != 0 {
			count++
		}
	}
	var info *WorkshopScoreInfo
	if len(rows) > 0 {
		row := rows[0].(map[string]any)
		info = &WorkshopScoreInfo{Damage: row["damage"].(int64), Rate: row["rate"].(int), Grade: row["grade"].(int), HighScore: row["high_score"].(int64)}
	}
	return progress.HighScore, count, info, nil
}

// Preview the production scoring function with an empty in-memory claim record.
// No account is involved and no returned rewards are granted.
func WorkshopScore(policy *gamestate.TeamBattleScorePolicy, damage int64, turns int) (int64, int, error) {
	score, count, _, err := WorkshopScorePreview(policy, damage, turns)
	return score, count, err
}
