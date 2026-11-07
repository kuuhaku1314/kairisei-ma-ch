package battlecore

// registerRetainedStatus dispatches statuses with distinct native registration
// rules, including healing cut. General stat buffs keep their own stacking path.
func (engine *BattleEngine) registerRetainedStatus(member int, effects *[]battleEffect, role CombatSkillRole, incoming battleEffect) ([]BattleResult, bool) {
	code := battleBuffCodes[role.Function]
	if code == 312 {
		for _, previous := range *effects {
			if previous.ListType == incoming.ListType && previous.Function == "HEAL_CUT" && previous.Rate >= 0 && previous.Remaining > 1 {
				return nil, true
			}
		}
		// Native keeps the base bad-status resistance draw even at zero.
		// Typed DEBUFF_REGIST does not block this BAD_STATUS family.
		engine.rollPercent10000(0)
		*effects = append(*effects, incoming)
		engine.recordAIStatusApplied(member, incoming)
		return []BattleResult{battlePersistentResult(member, role, code, incoming)}, true
	}
	if code == 20 {
		return engine.registerEnchantStatus(member, effects, role, incoming), true
	}
	switch code {
	case 200, 203, 204, 205, 206, 207, 208, 209, 210, 211, 212, 213, 214, 215, 216, 217, 218, 409:
	default:
		return nil, false
	}
	var previous *battleEffect
	for i := range *effects {
		current := &(*effects)[i]
		if current.ListType == incoming.ListType && battleBuffCodes[current.Function] == code {
			previous = current
			break // 46cd2's first matching record controls the comparison.
		}
	}
	var rows []BattleResult
	if previous != nil && incoming.ListType != 2 && incoming.ListType != 4 {
		weaker := true
		switch code {
		case 200, 203, 207, 216:
			weaker = incoming.Value <= previous.Value
		case 204:
			weaker = incoming.Remaining <= previous.Remaining
		case 205:
			weaker = false // A new barrier replaces even a stronger old barrier.
		case 206:
			weaker = incoming.Rate <= previous.Rate
		case 208:
			weaker = combatPhysicsIndex(incoming.DamageKind) == combatPhysicsIndex(previous.DamageKind) && incoming.Rate <= previous.Rate
		case 209:
			weaker = incoming.Rate <= previous.Rate // HP fraction, not revival count.
		case 217, 218:
			weaker = false // A new count status replaces this code even when weaker.
		case 210, 211, 212, 213, 214, 215:
			weaker = incoming.Attribute == previous.Attribute && incoming.Parameter == previous.Parameter &&
				incoming.DamageKind == previous.DamageKind && incoming.Rate <= previous.Rate
		}
		if weaker && previous.Remaining > 1 {
			return nil, true // Native silently rejects; not DEBUFF_FAILED.
		}
		_, release := naturalEffectReleaseResult(member, *previous, code)
		release.Args[1] = int64(incoming.ListType)
		rows = append(rows, release)
		kept := (*effects)[:0]
		for _, current := range *effects {
			if current.ListType != incoming.ListType || battleBuffCodes[current.Function] != code {
				kept = append(kept, current)
			}
		}
		*effects = kept // 439a4 removes all same-code records in this one list.
	}
	*effects = append(*effects, incoming)
	engine.recordAIStatusApplied(member, incoming)
	if incoming.ListType == 0 || incoming.ListType == 1 || incoming.ListType == 5 || incoming.ListType == 6 {
		rows = append(rows, battlePersistentResult(member, role, code, incoming))
	}
	return rows, true
}

// 88a20 case20 compares the first enchant in the receiving list. Matching
// attributes stack; a different attribute removes that list's old enchant
// records and publishes 72 before the new 62. Other lists remain independent.
func (engine *BattleEngine) registerEnchantStatus(member int, effects *[]battleEffect, role CombatSkillRole, incoming battleEffect) []BattleResult {
	var rows []BattleResult
	for _, previous := range *effects {
		if previous.ListType != incoming.ListType || previous.Function != "ENCHANT" {
			continue
		}
		if previous.Attribute != incoming.Attribute {
			_, release := naturalEffectReleaseResult(member, previous, 20)
			release.Args[1] = int64(incoming.ListType)
			rows = append(rows, release)
			kept := (*effects)[:0]
			for _, current := range *effects {
				if current.ListType != incoming.ListType || current.Function != "ENCHANT" {
					kept = append(kept, current)
				}
			}
			*effects = kept
		}
		break
	}
	*effects = append(*effects, incoming)
	engine.recordAIStatusApplied(member, incoming)
	return append(rows, battlePersistentResult(member, role, 20, incoming))
}
