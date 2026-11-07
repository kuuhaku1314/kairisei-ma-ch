package battlecore

// FUN_0008bb40 rejects a duplicate before any RNG, then rolls temporary
// resistance, master resistance and role accuracy in that order. Its STAN
// switch exits before the DOT-only typed DEBUFF_REGIST roll. Zero rates still
// consume their draws. STAN is the active control
// family; SILENCE/CHARM share native indices 2/3 but have no active role here.
func (engine *BattleEngine) stunHits(effects []battleEffect, cooldown, baseResistance int, role CombatSkillRole, level int) bool {
	if hasActiveCombatEffect(effects, "STAN") {
		return false
	}
	return !engine.rollPercent10000(minInt(4, maxInt(0, cooldown))*25) &&
		!engine.rollPercent10000(baseResistance) &&
		engine.persistentEffectHits(role, level)
}

// 72a18 writes four on an enemy status's per-entry removal (71), even when
// grouped 72 is suppressed by another retained entry. Silent FIELD/CARD
// expiry and default wave cleanup do not enter this function.
func (enemy *battleEnemy) recordReleasedStatusCooldown(effects []battleEffect) {
	for _, effect := range effects {
		index := -1
		switch effect.Function {
		case "STAN":
			index = 1
		default:
			index = combatDOTStatusResistanceIndex(effect.Function)
		}
		if index >= 0 && index < len(enemy.StatusCooldown) {
			enemy.StatusCooldown[index] = 4
		}
	}
}

func (enemy *battleEnemy) tickStatusCooldown() {
	// 5eb20 calls 6ee36 after this member's list expiry. A newly expired stun
	// therefore becomes three (75%) before the next player turn, not four.
	for i := range enemy.StatusCooldown {
		if enemy.StatusCooldown[i] > 0 {
			enemy.StatusCooldown[i]--
		}
	}
}
