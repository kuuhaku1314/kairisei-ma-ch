package game

import "kairisei.local/server/internal/gamestate"

// BossScoreRank is the public projection of one player's best accepted
// score for one Boss and participation mode. Name is the current display name,
// read from the account projection rather than stored in the ranking row.
type BossScoreRank struct {
	Rank       int    `json:"rank"`
	UserID     int    `json:"userid"`
	Name       string `json:"name"`
	Grade      int    `json:"grade"`
	Score      int64  `json:"score"`
	AchievedAt int64  `json:"achieved_at"`
}

type BossScoreRankingPage struct {
	Total   int             `json:"total"`
	Entries []BossScoreRank `json:"entries"`
	Self    *BossScoreRank  `json:"self"`
}

type BossScoreRankingRepository interface {
	BossScoreRanking(bossID int, mode gamestate.BattleScoreMode, userID, offset, limit int) (BossScoreRankingPage, error)
}
