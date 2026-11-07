package battlecore

// JP507 publishes negative ATK/INT contributions and whether the complete
// parameter aggregate crossed its lower clamp. JP508 excludes base resistances.
type battleStatusProjection struct {
	attack, magic               int
	attackMaximum, magicMaximum bool
	attributes                  [5]int
}

func retainedStatusProjection(effects []battleEffect, enemy bool) battleStatusProjection {
	var state battleStatusProjection
	attributes := [...]string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"}
	for _, effect := range effects {
		if enemy && effect.Delta < 0 {
			switch effect.Parameter {
			case "ATK":
				state.attack += effect.Delta
			case "INT":
				state.magic += effect.Delta
			}
		}
		if effect.Function != "ATTR_DEF_DOWN" && (enemy || effect.Function != "ATTR_DEF_UP") {
			continue
		}
		for index, attribute := range attributes {
			if supportAttributeMatches(effect.Attribute, attribute) {
				state.attributes[index] -= effect.Parameters[1]
			}
		}
	}
	return state
}

func (engine *BattleEngine) memberStatusProjection(member int) battleStatusProjection {
	if player := engine.parameterState(member); player != nil {
		return retainedStatusProjection(player.Effects, false)
	}
	e := &engine.enemies[member-5]
	state := retainedStatusProjection(e.Effects, true)
	deltas := retainedBattleParameterDeltas(e.Effects)
	state.attackMaximum = e.BaseAttack+deltas.attack < 0
	state.magicMaximum = e.BaseMagic+deltas.magic < 0
	return state
}

func (state battleStatusProjection) results(member int, enemy bool) []BattleResult {
	var rows []BattleResult
	if enemy {
		rows = append(rows, BattleResult{Command: CommandEnemyDebuff, Args: []int64{int64(member), int64(state.attack), boolInt64(state.attackMaximum), int64(state.magic), boolInt64(state.magicMaximum)}})
	}
	values := []int64{int64(member)}
	for _, value := range state.attributes {
		values = append(values, int64(value))
	}
	return append(rows, BattleResult{Command: CommandFixedAttrDefense, Args: values})
}

func (engine *BattleEngine) publishMemberStatusProjection(member int) []BattleResult {
	state := engine.memberStatusProjection(member)
	var previous *battleStatusProjection
	if player := engine.parameterState(member); player != nil {
		previous = &player.statusDisplay
	} else {
		previous = &engine.enemies[member-5].statusDisplay
	}
	if state == *previous {
		return nil
	}
	*previous = state
	return state.results(member, engine.parameterState(member) == nil)
}

func (engine *BattleEngine) publishStatusProjections() []BattleResult {
	var rows []BattleResult
	// Original complete-skill publication flushes enemies before player panels.
	for i := 0; i < engine.enemyCount; i++ {
		rows = append(rows, engine.publishMemberStatusProjection(i+5)...)
	}
	for _, p := range engine.playerStates() {
		if p.MemberType != 0 && p.HP > 0 {
			rows = append(rows, engine.publishMemberStatusProjection(p.MemberType)...)
		}
	}
	return rows
}
