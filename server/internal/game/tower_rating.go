package game

import (
	"errors"

	"kairisei.local/server/internal/gamestate"
)

// PLACEHOLDER / LOCAL_POLICY: the original rating thresholds and Season 6
// formula are not available. The local tower policy rewards earlier clears;
// fallen summoned knights do not themselves decide victory or star count.
const (
	towerClearTurnLimit = 20
	towerTwoStarTurns   = 15
	towerThreeStarTurns = 10
	towerScorePerTurn   = 500
)

type TeamBattleClearRating struct {
	OldFlag int `json:"old_flag"`
	NewFlag int `json:"new_flag"`
}

func (s *Account) planTowerQuestRatingLocked(context TeamBattleContext, turns int) (*TeamBattleClearRating, error) {
	if context.TowerID == 0 {
		return nil, nil
	}
	profile, exists := s.towerQuestProfiles[context.TowerID]
	if !exists || context.TowerFloor <= 0 || context.TowerFloor > len(profile.Floors) ||
		profile.Floors[context.TowerFloor-1].Floor != context.TowerFloor {
		return nil, errors.New("local tower battle context is invalid")
	}
	progress, exists := s.towerQuestProgress[context.TowerID]
	if !exists {
		return nil, errors.New("local tower progress is unavailable")
	}
	if turns < 1 || turns > towerClearTurnLimit {
		return nil, errors.New("tower clear exceeds the accepted turn limit")
	}
	flag := 1
	if turns <= towerTwoStarTurns {
		flag |= 2
	}
	if turns <= towerThreeStarTurns {
		flag |= 4
	}
	old := progress.ClearRatings[context.TowerFloor]
	return &TeamBattleClearRating{OldFlag: old, NewFlag: old | flag}, nil
}

func (s *Account) recordTowerQuestRatingLocked(context TeamBattleContext, rating *TeamBattleClearRating, turns int) {
	progress := s.towerQuestProgress[context.TowerID]
	if progress.ClearRatings == nil {
		progress.ClearRatings = make(map[int]int)
	}
	if progress.BestClearTurns == nil {
		progress.BestClearTurns = make(map[int]int)
	}
	progress.ClearRatings[context.TowerFloor] = rating.NewFlag
	if best := progress.BestClearTurns[context.TowerFloor]; best == 0 || turns < best {
		progress.BestClearTurns[context.TowerFloor] = turns
	}
	s.towerQuestProgress[context.TowerID] = progress
}

// TowerQuestScore derives the ranking total from best clears. Historical
// floors without a recorded turn count contribute no invented score.
func TowerQuestScore(progress gamestate.TowerQuestProgress) int64 {
	var score int64
	for _, turns := range progress.BestClearTurns {
		if turns >= 1 && turns <= towerClearTurnLimit {
			score += int64(towerClearTurnLimit+1-turns) * towerScorePerTurn
		}
	}
	return score
}
