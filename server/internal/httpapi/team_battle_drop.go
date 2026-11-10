package httpapi

import (
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

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
	return game.TeamBattleDropReport{Turns: completed.Turns, EnemyDeadBits: completed.EnemyDeadBits, Authoritative: true, ReleasedDrops: completed.ReleasedDrops,
		FameRewardsSet: completed.FameRewardsSet, FameRewards: completed.FameRewards,
		ScoreVerified: completed.ScorePolicy != nil, ScoreDamage: completed.ScoreDamage, ScorePolicy: completed.ScorePolicy}
}
