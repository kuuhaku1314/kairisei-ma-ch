package battlecore

import "strings"

func (engine *BattleEngine) executeAttackOnPlayers(source battleAttackSource, targets []int, role CombatSkillRole, roles []CombatSkillRole) []BattleResult {
	modifiers := source.modifiers
	hits := maxInt(1, combatParameterInt(role.Parameters[4]))
	if len(targets) == 0 {
		return nil
	}
	_, _, enchanted := activeEnchantEffect(source.effects())
	engine.recordSkillAttackCount(source.memberType, role.RoleIndex, hits, enchanted)
	results := make([]BattleResult, 0, len(targets)*hits*2)
	for _, memberType := range targets {
		var drains []BattleResult
		player := engine.player(memberType)
		pendingDamage := 0
		for hit := 0; hit < hits; hit++ {
			boost := int32(1000) + int32(source.boostRate) + int32(countedDamageHitRate(source.counted, hit))
			hitPower := int(int64(source.power+source.boostFixed) * int64(boost) / 1000)
			defense := player.Defense
			if strings.EqualFold(role.Parameters[8], "MAGIC") {
				defense = player.MDefense
			} else if strings.EqualFold(role.Parameters[8], "ALL") {
				defense += player.MDefense
			}
			defense = nativePiercedDefense(defense, modifiers.piercingRate)
			rate, damageAttribute := engine.playerAttackAttribute(player, role.Parameters[7])
			if modifiers.attrRateDownInvalid {
				rate = maxInt(100, rate)
			}
			critical, criticalMultiplier := engine.attackCriticalOutcome(source, role)
			attrDefenseRate, attrDefenseFixed := attributeDefenseAdjustment(player.Effects, damageAttribute)
			attrDefenseFixed = nativePiercedAttributeAdjustment(attrDefenseFixed, modifiers.attributePiercingRate)
			attributeAdjustedPower := weaknessAdjustedPower(hitPower, player.Effects) * (1000 + attrDefenseRate) / 1000
			if source.chainCount > 1 {
				attributeAdjustedPower = int(int64(attributeAdjustedPower) * int64(100+source.chainRate*(source.chainCount-1)) / 100)
			}
			damage, attributeDifference := nativeAttributeAttackDamage(attributeAdjustedPower, rate, criticalMultiplier, defense, 0)
			damage = playerCoveringDamage(player, damage, damageAttribute, role.Parameters[8])
			// 927b0 applies COVERING after ordinary defense, then adds the
			// fixed attribute adjustment. That fixed armor is not cut again.
			damage = maxInt(1, damage+attrDefenseFixed)
			resolution := engine.resolveIncomingDamageEffects(&player.Effects, damage, player.HP, damageAttribute, role.Parameters[8])
			damage = resolution.Damage
			if resolution.BarrierTriggered && damage == 0 {
				// Shared 47028 clears the flag only when the barrier absorbs the hit.
				critical = 0
			}
			pendingDamage += damage
			if source.player != nil {
				engine.recordDamageEvent(player.MemberType, source.memberType, damage, damageAttribute, role.Parameters[8], "", false)
			} else {
				engine.recordSkillDamage(player.MemberType, damage, damageAttribute, role.Parameters[8], false)
			}
			results = append(results,
				battleDamageResult(player.MemberType, role.RoleIndex, -damage, player.HP, attributeDifference, damageAttribute, rate, critical, 0, source.memberType),
			)
			results = append(results, damageEffectResults(player.MemberType, resolution)...)
			if resolution.Reflected > 0 && !attackIgnoresReflection(roles, role) {
				results = append(results, engine.reflectedDamageResults(source.memberType, player.MemberType, resolution.Reflected)...)
			}
			drains = append(drains, engine.attackDrainResults(source, role.RoleIndex, damage, modifiers)...)
			if enchantValue, enchantAttribute, active := activeEnchantEffect(source.effects()); active {
				enchantRate, enchantAttribute := engine.playerAttackAttribute(player, enchantAttribute)
				enchantDefenseRate, enchantDefenseFixed := attributeDefenseAdjustment(player.Effects, enchantAttribute)
				enchantPower := enchantValue * (1000 + enchantDefenseRate) / 1000
				enchantDamage, enchantDifference := nativeAttributeEnchantDamage(enchantPower, enchantRate, enchantDefenseFixed)
				enchantResolution := engine.resolveEnchantDamageEffects(&player.Effects, enchantDamage, player.HP, enchantAttribute)
				enchantDamage = enchantResolution.Damage
				pendingDamage += enchantDamage
				if source.player != nil {
					engine.recordDamageEvent(player.MemberType, source.memberType, enchantDamage, enchantAttribute, "", "", true)
				} else {
					engine.recordSkillDamage(player.MemberType, enchantDamage, enchantAttribute, "ALL", true)
				}
				results = append(results, battleDamageResult(player.MemberType, role.RoleIndex, -enchantDamage, player.HP, enchantDifference, enchantAttribute, enchantRate, 0, 1, source.memberType))
				results = append(results, damageEffectResults(player.MemberType, enchantResolution)...)
				// 838d0 reflects only the normal hit, before the enchant branch.
			}
		}
		recordPlayerDamage(player, pendingDamage)
		player.HP = engine.playerHPAfterDamage(player, pendingDamage)
		results = append(results, BattleResult{Command: CommandHP, Args: []int64{int64(player.MemberType), int64(player.MaxHP), int64(player.HP), 1}})
		results = append(results, engine.burstDamageGaugeResults(player, pendingDamage)...)
		results = engine.appendDrainResults(results, drains, role.RoleIndex)
	}

	return results
}
