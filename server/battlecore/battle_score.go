package battlecore

// ScoreDamage follows original CN battle_total_diff_hp (x86 4d79a):
// depleted HP of each enemy, counting a broken part's full maximum HP.
func (engine *BattleEngine) ScoreDamage() int64 {
	var total int64
	for i := 0; i < engine.enemyCount; i++ {
		e := &engine.enemies[i]
		if e.Broken {
			total += int64(e.MaxHP)
		} else {
			total += int64(e.MaxHP - e.HP)
		}
	}
	return max(0, total)
}
