package game

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

const (
	teamBattleFameMaximum    = 100
	TeamBattleRewardFame     = 0
	TeamBattleRewardFullFame = 1
	TeamBattleRewardHost     = 5
)

func teamBattleBonusRewardType(kind int) bool {
	switch kind {
	case 6, 8, 13, 15, 19:
		return true
	default:
		return false
	}
}

func TeamBattleFamePool(profile gamestate.TeamBattleRewardProfile) []gamestate.Reward {
	if profile.FameRewards != nil {
		return cloneRewards(profile.FameRewards)
	}
	for _, reward := range profile.ResultRewards {
		if teamBattleBonusRewardType(reward.Type) {
			return []gamestate.Reward{cloneReward(reward)}
		}
	}
	return nil
}

func teamBattleFameReward(pool []gamestate.Reward, seed string, memberType, box int) gamestate.Reward {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|member=%d|fame-box=%d", seed, memberType, box)))
	return cloneReward(pool[binary.BigEndian.Uint64(sum[:8])%uint64(len(pool))])
}
