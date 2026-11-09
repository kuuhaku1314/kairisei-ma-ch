package masterdata

import (
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

// NativeBattleEnemyDropSlots is the managed EnemyData.drop1..drop8 capacity.
// The limit is per wave and enemy, not the total number or quantity of rewards.
const NativeBattleEnemyDropSlots = 8

func ValidateBattleDropSlots(drops []gamestate.TeamBattleEnemyDrop) error {
	counts := make(map[[2]int]int)
	for _, drop := range drops {
		if drop.ChancePerMillion != nil && *drop.ChancePerMillion == 0 {
			continue
		}
		key := [2]int{drop.BattleIndex, drop.EnemyIndex}
		counts[key]++
		if counts[key] > NativeBattleEnemyDropSlots {
			return fmt.Errorf("第%d波目标%d超过%d项掉落（含固定金币），请分配到其他目标或合并同类奖励数量", key[0]+1, key[1]+1, NativeBattleEnemyDropSlots)
		}
	}
	return nil
}
