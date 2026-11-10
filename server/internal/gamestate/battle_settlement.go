package gamestate

import (
	"slices"
)

// TeamBattleSettlementPlan is the published reward contract accepted by a
// room. It belongs to that run, independently of later content publication.
type TeamBattleSettlementPlan struct {
	Profile    TeamBattleRewardProfile
	EnemyTypes []int8
}

func CloneTeamBattleSettlementPlan(plan TeamBattleSettlementPlan) TeamBattleSettlementPlan {
	plan.EnemyTypes = slices.Clone(plan.EnemyTypes)
	plan.Profile.ResultRewards = cloneRewards(plan.Profile.ResultRewards)
	plan.Profile.FirstClearRewards = cloneRewards(plan.Profile.FirstClearRewards)
	plan.Profile.FameRewards = cloneRewards(plan.Profile.FameRewards)
	plan.Profile.EnemyDrops = slices.Clone(plan.Profile.EnemyDrops)
	for i := range plan.Profile.EnemyDrops {
		drop := &plan.Profile.EnemyDrops[i]
		drop.Reward.CardSkillLevels = slices.Clone(drop.Reward.CardSkillLevels)
		if drop.ChancePerMillion != nil {
			chance := *drop.ChancePerMillion
			drop.ChancePerMillion = &chance
		}
	}
	plan.Profile.ScorePolicy = CloneTeamBattleScorePolicy(plan.Profile.ScorePolicy)
	return plan
}
