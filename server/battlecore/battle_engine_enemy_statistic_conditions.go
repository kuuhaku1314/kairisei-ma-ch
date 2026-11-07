package battlecore

import "strings"

func (engine *BattleEngine) attackAndEnchantMatches(values []string) bool {
	if !nativeAIDamageFiltersSupported(values[3], values[4], "NULL") {
		return false
	}
	target := 0
	if !combatNullValue(values[0]) {
		enemy, ok := engine.enemyByTriggerName(values[0])
		if !ok {
			target = -1
		} else {
			target = enemy.MemberType
		}
	}
	var total int64
	for _, event := range engine.turnStats.DamageEvents {
		enemy, ok := engine.enemyByMemberType(event.Target)
		if !ok || enemy.HP <= 0 || target != 0 && event.Target != target || event.Special || event.DOT != "" {
			continue
		}
		if nativeAIDamageEventMatches(event, values[3], values[4], "NULL", false) {
			total += int64(event.Value)
		}
	}
	lower, upper := int64(combatParameterInt(values[1])), int64(combatParameterInt(values[2]))
	return total >= lower && (upper == 0 || total <= upper)
}

func anyPlayerStatisticAtLeast(values []int64, lower string) bool {
	threshold := int64(combatParameterInt(lower))
	for _, value := range values {
		if value >= threshold {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) executedAttributeMatches(values []string) bool {
	for _, value := range values {
		attribute := combatAttributeCode(value)
		if attribute > 0 && engine.turnStats.ExecutedAttributes&(uint32(1)<<(uint(attribute)&31)) != 0 {
			return true
		}
	}
	return false
}

// 838d0 fills a skill-local table for ordinary attacks only; enchant,
// reflection, DOT and parent HP propagation do not enter it. 7adb0 commits
// per-attribute maxima after all roles, before Guts and death reactions.
func (engine *BattleEngine) recordSkillAttackDamage(target, damage int, attribute, physics string) {
	if engine.skillTargets == nil || target < 5 || target >= 5+engine.enemyCount {
		return
	}
	attr, physical := combatAttributeCode(attribute), combatPhysicsIndex(physics)
	if attr >= 0 && attr < 10 && physical >= 0 && physical < 3 {
		engine.skillTargets.attackDamage[target-5][attr][physical] += int64(damage)
	}
}

func (engine *BattleEngine) commitSkillAttackDamage(context *battleSkillTargets) {
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		if enemy.HP <= 0 {
			continue
		}
		for attr, values := range context.attackDamage[i] {
			values[2] += values[0] + values[1]
			for physics, damage := range values {
				if damage > enemy.AITurn.MaxSkillDamage[attr][physics] {
					enemy.AITurn.MaxSkillDamage[attr][physics] = damage
				}
			}
		}
	}
}

func enemyMaxSkillDamageMatches(enemy *battleEnemy, attribute, physics, lower, upper string) bool {
	if !nativeAIDamageFiltersSupported(attribute, physics, "NULL") {
		return false
	}
	physical := combatPhysicsIndex(physics)
	for attr, values := range enemy.AITurn.MaxSkillDamage {
		if !combatNullValue(attribute) && attr != combatAttributeCode(strings.ToUpper(attribute)) {
			continue
		}
		// NULL tests individual attribute buckets. JP keeps a zero upper
		// bound open, using the same interval as other damage totals.
		if statisticRange(values[physical], lower, upper) {
			return true
		}
	}
	return false
}
