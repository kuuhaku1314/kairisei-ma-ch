package battlecore

func weaknessAdjustedPower(power int, effects []battleEffect) int {
	// 8bb40 stores WEAKNESS in buff+0x6c; 71d48 sums/caps it at 3000.
	// 927b0 multiplies the target aggregate BEFORE ATTR_DEF and elemental
	// scaling. 93461 (enchant) and 8bb40's DOT registration do not consume it.
	rate := 0
	for _, effect := range effects {
		if effect.Function == "WEAKNESS" && effect.Remaining > 0 && effect.ListType >= 0 && effect.ListType <= 6 {
			rate += effect.Rate
		}
	}
	return int(int64(power) * int64(1000+minInt(3000, rate)) / 1000)
}

func (engine *BattleEngine) registerWeakness(memberType int, role CombatSkillRole, effect battleEffect) []BattleResult {
	var targets []*[]battleEffect
	var members []int
	var destination *[]battleEffect
	baseResistance := 0
	if engine.playerMember(memberType) {
		destination = &engine.player(memberType).Effects
		party := engine.playerParty(memberType)
		for i := range party {
			player := &party[i]
			if player.MemberType != 0 {
				targets = append(targets, &player.Effects)
				members = append(members, player.MemberType)
			}
		}
	} else if memberType >= 5 && memberType < 5+engine.enemyCount {
		destination = &engine.enemies[memberType-5].Effects
		baseResistance = engine.enemies[memberType-5].Level.StatusResistances[9]
		for i := 0; i < engine.enemyCount; i++ {
			if engine.enemies[i].MemberType != 0 {
				targets = append(targets, &engine.enemies[i].Effects)
				members = append(members, engine.enemies[i].MemberType)
			}
		}
	} else {
		return nil
	}
	// 46d30 allows replacement. 8bb40 rolls only base BAD_STATUS[9] here,
	// including a zero-rate draw; no role accuracy or typed-resistance roll.
	code := battleBuffCodes["WEAKNESS"]
	if engine.rollPercent10000(baseResistance) {
		return []BattleResult{battleDebuffFailedResult(memberType, role.RoleIndex, code)}
	}
	var results []BattleResult
	// 6efdc includes all existing members on the target side, including KO.
	// 8bb40 moves the mark off the FIRST holder in NORMAL/FIELD, using 439a4
	// (grouped72 only), not 72a18 (no per-entry71, cooldown or parameter rows).
	if effect.ListType == 0 || effect.ListType == 3 {
		for i, effects := range targets {
			found := false
			for _, old := range *effects {
				if old.Function == "WEAKNESS" && old.ListType == effect.ListType {
					_, release := naturalEffectReleaseResult(members[i], old, code)
					release.Args[1] = int64(effect.ListType)
					results = append(results, release)
					found = true
					break
				}
			}
			if found {
				kept := (*effects)[:0]
				for _, old := range *effects {
					if old.Function != "WEAKNESS" || old.ListType != effect.ListType {
						kept = append(kept, old)
					}
				}
				*effects = kept
				break
			}
		}
	}
	*destination = append(*destination, effect)
	engine.recordAIStatusApplied(memberType, effect)
	return append(results, battlePersistentResult(memberType, role, code, effect))
}
