package battlecore

// 7097c sums draw bonuses and subtracts draw penalties from actor-owned
// lists 0..6; it is neither selected-card count nor current hand size.
// Normal draw effects have already resolved their replacement policy at
// registration. Do not apply another max or discard retained zero-turn entries.
func playerAIDrawChange(player *battlePlayer) int {
	var value int32
	for _, effect := range player.Effects {
		if effect.ListType < 0 || effect.ListType > 6 {
			continue
		}
		switch battleBuffCodes[effect.Function] {
		case 400:
			value += int32(effect.Value)
		case 408:
			value -= int32(effect.Value)
		}
	}
	return int(value)
}

func (engine *BattleEngine) highestUserDrawMatches(lower, upper string) bool {
	maximum := 0
	candidates := make([]int, 0, maxRoomMembers)
	for index := range engine.players {
		player := &engine.players[index]
		if player.HP <= 0 {
			continue
		}
		value := playerAIDrawChange(player)
		if len(candidates) == 0 || value > maximum {
			maximum = value
			candidates = candidates[:0]
		}
		if value == maximum {
			candidates = append(candidates, index+1)
		}
	}
	// 5645c -> 560d0 requests rank=1; 4f21c sorts descending. Bounds
	// are closed, including upper=0; tied maxima remain random candidates.
	if maximum < combatParameterInt(lower) || maximum > combatParameterInt(upper) {
		return false
	}
	return engine.retainEnemyTriggerTarget(candidates)
}
