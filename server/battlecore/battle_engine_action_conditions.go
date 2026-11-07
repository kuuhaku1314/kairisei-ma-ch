package battlecore

import "strings"

func (engine *BattleEngine) branchAttackCondition(condition string, actor int, parameters [5]string) bool {
	return engine.branchSelectedMember(actor, parameters[0], func(member int) bool {
		if condition == "USER_ATTACK_NUM" {
			stats := engine.memberSkillStatistics(member)
			return stats != nil && int64(int32(stats.AttackCount)) >= int64(combatParameterInt(parameters[1]))
		}
		attribute, physics := strings.ToUpper(parameters[1]), strings.ToUpper(parameters[2])
		if physics == "" {
			physics = "PHYSICS"
		}
		var total int64
		// d1f66 -> 7049c reads normal attack buckets on current enemy members,
		// without excluding destroyed parts. Enchant/DOT/reflection/HP transfer
		// have separate buckets and are not part of this query.
		for _, event := range engine.turnStats.DamageEvents {
			if event.Source != member || event.Target < 5 || event.Target >= 5+engine.enemyCount || event.Enchant || event.Special || event.DOT != "" {
				continue
			}
			if (combatNullValue(attribute) || strings.EqualFold(event.Attribute, attribute)) &&
				(physics == "ALL" || strings.EqualFold(event.Physics, "ALL") || strings.EqualFold(event.Physics, physics)) {
				total += int64(event.Value)
			}
		}
		return total >= int64(combatParameterInt(parameters[3]))
	})
}

func (engine *BattleEngine) branchEnemyDiedThisTurn(selector string) bool {
	member := combatEnemyMemberType(selector)
	if member >= 5 && member <= 8 {
		return member < 5+engine.enemyCount && engine.enemies[member-5].TurnSkillStats.Died
	}
	// d2132's unspecified selector checks any enemy; a named but absent
	// member is still resolved to its slot and fails the later lookup.
	for i := 0; i < engine.enemyCount; i++ {
		if engine.enemies[i].TurnSkillStats.Died {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) branchTargetChargeStarted(target int) bool {
	return target >= 5 && target < 5+engine.enemyCount && engine.enemies[target-5].ChargedActionCount > 0
}

func (engine *BattleEngine) recordSkillAttackCount(source, roleIndex, hits int, enchanted bool) {
	if context := engine.skillTargets; context != nil && context.source == source && roleIndex >= 0 && roleIndex < len(context.attackCount) {
		if enchanted {
			hits *= 2
		}
		context.attackCount[roleIndex] = uint32(hits)
	}
}
