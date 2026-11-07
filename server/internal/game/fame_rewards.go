package game

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"slices"

	"kairisei.local/server/internal/gamestate"
)

func TeamBattleFamePool(profile gamestate.TeamBattleRewardProfile, policy gamestate.TeamBattleFameBonusPolicy) []gamestate.Reward {
	if profile.FameRewards != nil {
		return cloneRewards(profile.FameRewards)
	}
	for _, reward := range profile.ResultRewards {
		if slices.Contains(policy.EligibleRewardTypes, reward.Type) {
			return []gamestate.Reward{cloneReward(reward)}
		}
	}
	return nil
}

func teamBattleFameReward(pool []gamestate.Reward, seed string, memberType, box int) gamestate.Reward {
	sum := sha256.Sum256([]byte(fmt.Sprintf("%s|member=%d|fame-box=%d", seed, memberType, box)))
	return cloneReward(pool[binary.BigEndian.Uint64(sum[:8])%uint64(len(pool))])
}
