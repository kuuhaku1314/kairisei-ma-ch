package httpapi

import (
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func teamBattleDropPlanSpecs(plan []gamestate.TeamBattleEnemyDrop, battleIndex int) []multiplayer.BattleDrop {
	drops := make([]multiplayer.BattleDrop, 0, len(plan))
	for _, entry := range plan {
		if entry.BattleIndex == battleIndex {
			drops = append(drops, multiplayer.BattleDrop{EnemyIndex: entry.EnemyIndex, RewardType: entry.Reward.Type, Num: entry.Reward.Num, RewardTypeID: entry.Reward.RewardTypeID})
		}
	}
	return drops
}

func teamBattleDropPlanWire(plan []gamestate.TeamBattleEnemyDrop, enemyTypes []int8, battleIndex int) []any {
	plan = gamestate.TeamBattleWaveDrops(plan, enemyTypes, battleIndex)
	rows := make([]any, 0, 4)
	for enemy := 0; enemy < 4; enemy++ {
		drops := make([]gamestate.Reward, 0)
		for _, entry := range plan {
			if entry.BattleIndex == battleIndex && entry.EnemyIndex == enemy {
				drops = append(drops, entry.Reward)
			}
		}
		if len(drops) > 0 {
			rows = append(rows, map[string]any{"enemy_idx": enemy, "drops": drops})
		}
	}
	return rows
}

func multiplayerDropReport(completed multiplayer.CompletedBattle) game.TeamBattleDropReport {
	if completed.DropLedgerVersion == 1 {
		return game.TeamBattleDropReport{Turns: completed.Turns, EnemyDeadBits: completed.EnemyDeadBits, Authoritative: true, ReleasedDrops: completed.ReleasedDrops,
			FameRewardsSet: completed.FameRewardsSet, FameRewards: completed.FameRewards,
			ScoreVerified: completed.ScorePolicy != nil, ScoreDamage: completed.ScoreDamage, ScorePolicy: completed.ScorePolicy}
	}
	// Pre-upgrade completed records represent victories, with the historical
	// aggregate body-only policy. They have no bit field; do not erase rewards.
	return game.TeamBattleDropReport{EnemyDeadBits: []int{1}}
}
