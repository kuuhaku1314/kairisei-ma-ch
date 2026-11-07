package battlecore

import "strings"

// Player and enemy sources use the same enemy HP, barrier and parent receiver.
func (engine *BattleEngine) executeAttackOnEnemies(source battleAttackSource, targets []int, role CombatSkillRole, roles []CombatSkillRole) []BattleResult {
	if len(targets) == 0 {
		return nil
	}
	modifiers := source.modifiers
	hits := maxInt(1, combatParameterInt(role.Parameters[4]))
	_, _, enchanted := activeEnchantEffect(source.effects())
	engine.recordSkillAttackCount(source.memberType, role.RoleIndex, hits, enchanted)
	results := make([]BattleResult, 0, len(targets)*hits*3)
	for _, targetIndex := range targets {
		var drains []BattleResult
		enemy := &engine.enemies[targetIndex]
		// FUN_000838d0 computes every hit against the same pre-commit HP and
		// commits the accumulated normal + enchant damage once after the hit
		// loop. This is observable both in ResultCmd60 arg3 and in multi-hit
		// overkill: a lethal first hit does not suppress later hit commands.
		targetHP := enemy.HP
		pendingDamage := 0
		parentSignedDamage := 0
		var parent *battleEnemy
		if enemy.Parent > 0 && enemy.Parent <= engine.enemyCount {
			parent = &engine.enemies[enemy.Parent-1]
		}
		for hit := 0; hit < hits; hit++ {
			boostFactor := int32(1000) + int32(source.boostRate) + int32(countedDamageHitRate(source.counted, hit))
			hitPower := int(int64(source.power+source.boostFixed) * int64(boostFactor) / 1000)
			rate, damageAttribute := engine.enemyAttackAttribute(enemy, role.Parameters[7])
			if modifiers.attrRateDownInvalid {
				rate = maxInt(100, rate)
			}
			critical, criticalMultiplier := engine.attackCriticalOutcome(source, role)
			attrDefenseRate, attrDefenseFixed := attributeDefenseAdjustment(enemy.Effects, damageAttribute)
			attrDefenseFixed -= enemyFixedAttributeDefense(enemy, damageAttribute)
			attrDefenseFixed = nativePiercedAttributeAdjustment(attrDefenseFixed, modifiers.attributePiercingRate)
			attributeAdjustedPower := weaknessAdjustedPower(hitPower, enemy.Effects) * (1000 + attrDefenseRate) / 1000
			if source.chainCount > 1 {
				attributeAdjustedPower = int(int64(attributeAdjustedPower) * int64(100+source.chainRate*(source.chainCount-1)) / 100)
			}
			defense := engine.enemyEffectiveDefense(enemy, role.Parameters[8])
			defense = nativePiercedDefense(defense, modifiers.piercingRate)
			damage, attributeDifference := nativeAttributeAttackDamage(attributeAdjustedPower, rate, criticalMultiplier, defense, attrDefenseFixed)
			resolution := engine.resolveIncomingDamageEffects(&enemy.Effects, damage, enemy.HP, damageAttribute, role.Parameters[8])
			damage = resolution.Damage
			if resolution.BarrierTriggered && damage == 0 {
				// 47028 clears the critical flag after an absorbed hit, retaining its RNG draw.
				critical = 0
			}
			if parent != nil {
				// FUN_000838d0 keeps the attribute-only ResultCmd field separate
				// from its damage-consumer adjustment slot. Their final aggregate
				// is exactly the signed post-consumer damage propagated to parent.
				parentSignedDamage -= damage
				recordEnemyDamageKind(parent, damage, role.Parameters[8])
			}
			pendingDamage += damage
			recordEnemyDamageKind(enemy, damage, role.Parameters[8])
			recordEnemyAIDamage(enemy, damage, combatPhysicsIndex(role.Parameters[8]))
			if source.player != nil {
				engine.turnStats.DamageHits++
				engine.turnStats.DamageHitsByPhysics[combatPhysicsIndex(role.Parameters[8])]++
			}
			engine.recordDamageEvent(enemy.MemberType, source.memberType, damage, damageAttribute, role.Parameters[8], "", false)
			engine.recordSkillAttackDamage(enemy.MemberType, damage, damageAttribute, role.Parameters[8])
			if source.player != nil {
				if strings.EqualFold(role.Parameters[8], "MAGIC") {
					engine.turnStats.Magic += maxInt(0, damage)
				} else {
					engine.turnStats.Physical += maxInt(0, damage)
				}
			}
			results = append(results,
				battleDamageResult(enemy.MemberType, role.RoleIndex, -damage, targetHP, attributeDifference, damageAttribute, rate, critical, 0, source.memberType),
			)
			results = append(results, damageEffectResults(enemy.MemberType, resolution)...)
			if resolution.Reflected > 0 && !attackIgnoresReflection(roles, role) {
				results = append(results, engine.reflectedDamageResults(source.memberType, enemy.MemberType, resolution.Reflected)...)
			}
			drains = append(drains, engine.attackDrainResults(source, role.RoleIndex, damage, modifiers)...)
			if enchantValue, enchantAttribute, active := activeEnchantEffect(source.effects()); active {
				enchantRate, enchantAttribute := engine.enemyAttackAttribute(enemy, enchantAttribute)
				enchantDefenseRate, enchantDefenseFixed := attributeDefenseAdjustment(enemy.Effects, enchantAttribute)
				enchantDefenseFixed -= enemyFixedAttributeDefense(enemy, enchantAttribute)
				enchantPower := enchantValue * (1000 + enchantDefenseRate) / 1000
				enchantDamage, enchantDifference := nativeAttributeEnchantDamage(enchantPower, enchantRate, enchantDefenseFixed)
				enchantResolution := engine.resolveEnchantDamageEffects(&enemy.Effects, enchantDamage, enemy.HP, enchantAttribute)
				enchantDamage = enchantResolution.Damage
				if parent != nil {
					parentSignedDamage -= enchantDamage
					recordEnemyDamageKind(parent, enchantDamage, "MAGIC")
				}
				pendingDamage += enchantDamage
				recordEnemyDamageKind(enemy, enchantDamage, "MAGIC")
				recordEnemyAIDamage(enemy, enchantDamage, 2)
				if source.player != nil {
					engine.turnStats.Enchant += maxInt(0, enchantDamage)
				}
				engine.recordDamageEvent(enemy.MemberType, source.memberType, enchantDamage, enchantAttribute, "", "", true)
				results = append(results, battleDamageResult(enemy.MemberType, role.RoleIndex, -enchantDamage, targetHP, enchantDifference, enchantAttribute, enchantRate, 0, 1, source.memberType))
				results = append(results, damageEffectResults(enemy.MemberType, enchantResolution)...)
				// 838d0 reflects only the normal hit, before the enchant branch.
			}
		}
		// Native receive totals and REVENGE use the positive net amount of
		// one attack role, after all normal and enchant hits (including absorption).
		recordEnemyDamageTotals(enemy, pendingDamage)
		if source.player != nil {
			engine.turnStats.Damage += maxInt(0, pendingDamage)
		}
		if engine.playerMember(source.memberType) {
			engine.turnStats.DamageByUser[source.memberType-1] += maxInt(0, pendingDamage)
		}
		enemy.HP = nativeHPCommit(targetHP, enemy.MaxHP, -pendingDamage, enemy.Effects)
		engine.applyStoryEnemyFloor(enemy)
		markEnemyPendingBreak(enemy)
		engine.addPlayerRoleHate(source.memberType, pendingDamage, source.hateRatio, role.HateLimit)
		results = append(results, BattleResult{Command: CommandHP, Args: []int64{int64(enemy.MemberType), int64(enemy.MaxHP), int64(enemy.HP), 1}})
		if parent != nil && parentSignedDamage < 0 && parent.HP > 0 && !parent.Broken {
			// Native commits the parent only once, after every normal and enchant
			// hit and after the selected part's HP row. It does not run the
			// parent's barriers or reflection a second time; 73e9f applies its HP floor.
			parentDamage := -parentSignedDamage
			parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, -parentDamage, parent.Effects)
			engine.applyStoryEnemyFloor(parent)
			markEnemyPendingBreak(parent)
			recordEnemyDamageTotals(parent, parentDamage)
			results = append(results, BattleResult{Command: CommandHP, Args: []int64{int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 1}})
		}
		results = engine.appendDrainResults(results, drains, role.RoleIndex)
	}
	return results
}
