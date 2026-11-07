package game

import "kairisei.local/server/internal/gamestate"

type TowerRankRecord struct {
	RankNumber     int
	UserID         int
	Level          int
	Name           string
	Floor          int
	Score          int64
	LeaderCardID   int
	LeaderCardLV   int
	LeaderCardLove int
	LeaderCardFame int
}

type TowerRankingPage struct {
	Entries []TowerRankRecord
	Self    *TowerRankRecord
	Total   int
}

type TowerRankingRepository interface {
	TowerRanking(towerID, floorCount, userID, offset, limit int) (TowerRankingPage, error)
}

// LOCAL_POLICY: a record is the highest verified cleared floor in the current
// tower. A rewound challenge floor never lowers it. Unknown historic turn data
// contributes no invented score; known turns use the existing local formula.
func TowerQuestRecord(progress gamestate.TowerQuestProgress, floorCount int) (int, int64) {
	highest := 0
	turns := make(map[int]int, len(progress.BestClearTurns))
	for _, floor := range progress.ClearedFloors {
		if floor > 0 && floor <= floorCount {
			highest = max(highest, floor)
			if best, found := progress.BestClearTurns[floor]; found {
				turns[floor] = best
			}
		}
	}
	progress.BestClearTurns = turns
	return highest, TowerQuestScore(progress)
}
