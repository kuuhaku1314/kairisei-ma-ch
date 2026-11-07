package game

import (
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"fmt"
	"slices"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/gamestate"
)

func cloneTeamBattleDropPlan(source []gamestate.TeamBattleEnemyDrop) []gamestate.TeamBattleEnemyDrop {
	if source == nil {
		return nil
	}
	result := make([]gamestate.TeamBattleEnemyDrop, len(source))
	copy(result, source)
	for index := range result {
		result[index].Reward.CardSkillLevels = slices.Clone(result[index].Reward.CardSkillLevels)
		if chance := result[index].ChancePerMillion; chance != nil {
			copy := *chance
			result[index].ChancePerMillion = &copy
		}
	}
	return result
}

func PlanTeamBattleDrops(profile gamestate.TeamBattleRewardProfile, enemyTypes []int8, seed string) ([]gamestate.TeamBattleEnemyDrop, error) {
	if len(enemyTypes) == 0 {
		return nil, errors.New("drop plan has no battle segments")
	}
	rules := cloneTeamBattleDropPlan(profile.EnemyDrops)
	if profile.EnemyDrops == nil {
		// Explicit legacy policy: aggregate tangible rewards belong to the last
		// mandatory body. Do not invent unknown official part probabilities.
		last := -1
		for index, kind := range enemyTypes {
			if kind != 4 {
				last = index
			}
		}
		if last < 0 {
			return nil, nil
		}
		for _, reward := range profile.ResultRewards {
			if isTangibleTeamBattleReward(reward) {
				rules = append(rules, gamestate.TeamBattleEnemyDrop{BattleIndex: last, Reward: reward})
			}
		}
	}
	plan := make([]gamestate.TeamBattleEnemyDrop, 0, len(rules))
	for index, rule := range rules {
		if rule.BattleIndex < 0 || rule.BattleIndex >= len(enemyTypes) || rule.EnemyIndex < 0 || rule.EnemyIndex >= 4 || enemyTypes[rule.BattleIndex] == 4 || !isTangibleTeamBattleReward(rule.Reward) {
			return nil, errors.New("configured enemy drop has an invalid wave, enemy or reward")
		}
		if rule.ChancePerMillion != nil {
			chance := *rule.ChancePerMillion
			if chance < 0 || chance > 1000000 {
				return nil, errors.New("configured enemy drop chance is invalid")
			}
			sum := sha256.Sum256([]byte(fmt.Sprintf("%s|drop=%d|wave=%d|enemy=%d", seed, index, rule.BattleIndex, rule.EnemyIndex)))
			if int(binary.BigEndian.Uint64(sum[:8])%1000000) >= chance {
				continue
			}
		}
		rule.ChancePerMillion = nil
		plan = append(plan, rule)
	}
	return plan, nil
}

func isTangibleTeamBattleReward(reward gamestate.Reward) bool {
	return battlecore.ValidBattleDrop(battlecore.BattleDrop{RewardType: reward.Type, RewardTypeID: reward.RewardTypeID, Num: reward.Num})
}

// A native solo report provides dead bits per wave. A completed authoritative
// room instead provides exactly the drops its engine released (DESTRUCT can
// destroy an enemy without releasing a drop). Never re-roll either at result.
type TeamBattleDropReport struct {
	// Set by the solo result route to select its separate grade progress.
	SoloChallenge  bool
	ScoreVerified  bool
	ScoreDamage    int64
	ScorePolicy    *gamestate.TeamBattleScorePolicy
	FameRewardsSet bool
	FameRewards    []gamestate.Reward
	Turns          int
	EnemyDeadBits  []int
	Authoritative  bool
	ReleasedDrops  []gamestate.TeamBattleEnemyDrop
}

func SettledTeamBattleRewards(profile gamestate.TeamBattleRewardProfile, context TeamBattleContext, report TeamBattleDropReport) []gamestate.Reward {
	if !context.DropPlanSet && !report.Authoritative {
		return append([]gamestate.Reward(nil), profile.ResultRewards...)
	}
	rewards := make([]gamestate.Reward, 0, len(profile.ResultRewards))
	for _, reward := range profile.ResultRewards {
		if !isTangibleTeamBattleReward(reward) {
			rewards = append(rewards, reward)
		}
	}
	if report.Authoritative {
		for _, entry := range report.ReleasedDrops {
			rewards = append(rewards, entry.Reward)
		}
	} else {
		for _, entry := range context.DropPlan {
			released := entry.BattleIndex < len(report.EnemyDeadBits) && report.EnemyDeadBits[entry.BattleIndex]&(1<<entry.EnemyIndex) != 0
			if entry.EnemyIndex == 0 && !released {
				for wave := entry.BattleIndex + 1; wave < len(context.BattleEnemyTypes) && context.BattleEnemyTypes[wave] == 4; wave++ {
					if wave < len(report.EnemyDeadBits) && report.EnemyDeadBits[wave]&1 != 0 {
						released = true
					}
				}
			}
			if released {
				rewards = append(rewards, entry.Reward)
			}
		}
	}
	return rewards
}
