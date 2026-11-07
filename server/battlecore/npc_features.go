package battlecore

// These slots are the original CN ab9bc decision features, not effect codes.
// A bit identifies the source role, preserving primary/secondary eligibility
// and ROLE_OP_COMBO's shift. Skill execution remains in the ordinary core.
type npcFeatures [254]uint32

func npcSkillFeatures(skill CombatSkillDefinition, roles []CombatSkillRole) npcFeatures {
	var f npcFeatures
	npcAccumulateFeatures(skill, roles, &f)
	return f
}

func npcAccumulateFeatures(skill CombatSkillDefinition, roles []CombatSkillRole, f *npcFeatures) {
	attack := npcAttackFeatures(skill, roles, f[103] != 0)
	for i, value := range attack {
		f[i] |= value
	}
	for _, role := range roles {
		index := role.RoleIndex
		if index > 0 && f[103] != 0 {
			index--
		}
		bit := uint32(1) << uint(index&31)
		selected := role.Target == "SELECT"
		selfHeader := skill.Target == "SELF" || skill.Target == "USER_ONE"
		self := selfHeader && (selected || role.Target == "SELF")
		one := skill.Target == "USER_ONE" && selected || role.Target == "TEAMBATTLE_FRIEND_ONE" || role.Target == "PVP_FRIEND_ONE"
		all := skill.Target == "USER_ALL" && selected || role.Target == "FRIEND_ALL" && (selfHeader || skill.Target == "USER_ALL") || role.Target == "TEAMBATTLE_FRIEND_ALL" || role.Target == "PVP_FRIEND_ALL"
		enemyOne := skill.Target == "ENEMY_ONE" && selected || role.Target == "TEAMBATTLE_ENEMY_ONE" || role.Target == "PVP_ENEMY_ONE"
		enemyAll := skill.Target == "ENEMY_ALL" && selected || role.Target == "ENEMY_ALL" || role.Target == "TEAMBATTLE_ENEMY_ALL" || role.Target == "PVP_ENEMY_ALL"
		set := func(slot int, enabled bool) {
			if enabled {
				f[slot] |= bit
			}
		}
		friendly := func(a, b, c int) { set(a, self); set(b, one); set(c, all) }
		hostile := func(a, b int) { set(a, enemyOne); set(b, enemyAll) }
		healing := func(start int) {
			set(start, selfHeader && selected || role.Target == "SELF")
			set(start+1, skill.Target == "USER_ONE" && selected)
			set(start+2, skill.Target == "USER_ALL" && selected || role.Target == "FRIEND_ALL")
		}
		turnBuff := func(a, b, c int) {
			if combatParameterInt(role.Parameters[0]) < 2 {
				a++
			}
			friendly(a, b, c)
		}
		switch role.Function {
		case "ROLE_OP_COMBO":
			set(103, true)
		case "HEAL_FIXED", "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP", "HEAL_BY_SELF_PARAM":
			healing(6)
		case "CARD_SEAL_RECOVERY":
			healing(9)
		case "REGENERATE_FIXED", "REGENERATE_BY_TARGET_HP", "REGENERATE_BY_TARGET_MAXHP", "REGENERATE_BY_SELF_PARAM":
			healing(12)
		case "ATK_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_FIXED", "DEF_UP_BY_SELF_PARAM", "DEF_UP_BY_TARGET_PARAM", "DEF_UP_FIXED", "ATK_UP_FIXED_ATTR_BONUS", "DEF_UP_FIXED_ATTR_BONUS":
			switch role.Parameters[1] {
			case "ATK":
				turnBuff(15, 33, 44)
			case "MAX_HP":
				friendly(17, 34, 45)
			case "INT":
				turnBuff(18, 35, 46)
			case "MND":
				friendly(20, 36, 47)
			case "DEF":
				turnBuff(21, 37, 48)
			case "MDEF":
				turnBuff(23, 38, 49)
			}
		case "DAMAGE_UP":
			turnBuff(25, 39, 50)
		case "DAMAGE_CUT":
			turnBuff(27, 40, 51)
		case "CRITICAL_UP":
			turnBuff(29, 41, 52)
		case "ENCHANT":
			friendly(31, 42, 53)
		case "ATTR_DEF_UP":
			friendly(32, 43, 54)
		case "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_FIXED", "GUARD_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_FIXED":
			switch role.Parameters[1] {
			case "ATK":
				hostile(55, 63)
			case "MAX_HP":
				hostile(56, 64)
			case "INT":
				hostile(57, 65)
			case "MND":
				hostile(58, 66)
			case "DEF":
				hostile(59, 67)
			case "MDEF":
				hostile(60, 68)
			}
		case "DAMAGE_DOWN":
			hostile(61, 69)
		case "ATTR_DEF_DOWN":
			hostile(62, 70)
		case "STEAL":
			set(71, true)
			for i, slot := range []int{56, 55, 57, 58} {
				set(72+i, int32(f[slot]) > 0)
			}
		case "REWRITE":
			friendly(76, 77, 78)
		case "COVERING":
			set(79, self)
		case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
			friendly(80, 81, 82)
		case "REFLECTION":
			friendly(83, 84, 85)
		case "UNDERMINE":
			hostile(86, 95)
		case "STAN":
			hostile(87, 96)
		case "SILENCE":
			hostile(88, 97)
		case "CHARM":
			hostile(89, 98)
		case "POISON":
			hostile(90, 99)
		case "BURN":
			hostile(91, 100)
		case "FREEZE":
			hostile(92, 101)
		case "BLEED":
			hostile(93, 102)
		case "WEAKNESS":
			set(94, enemyOne)
		case "ELECTRIC":
			hostile(119, 120)
		case "DEBUFF_RELEASE":
			friendly(104, 105, 106)
			for _, start := range []int{9, 123, 130, 133, 136, 139, 142, 145, 148, 151, 154} {
				friendly(start, start+1, start+2)
			}
		case "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM":
			for _, value := range role.Parameters[2:4] {
				// The original release-specific feature paths use the narrower
				// healing target predicates, unlike DEBUFF_RELEASE above.
				slot, ok := map[int]int{15: 9, 23: 123, 24: 130, 26: 133, 21: 136, 7: 139, 8: 142, 9: 145, 10: 148, 22: 151, 28: 154}[combatTriggerDebuffKind(value)]
				if ok {
					healing(slot)
				}
			}
		case "ATTR_SEE":
			hostile(107, 108)
		case "DOT_VALUE_UP":
			slot, ok := map[string]int{"NULL": 117, "POISON": 109, "BURN": 111, "FREEZE": 113, "BLEED": 115, "ELECTRIC": 121}[role.Parameters[5]]
			if ok {
				hostile(slot, slot+1)
			}
		case "CARD_TRAP_DOT":
			hostile(126, 127)
		case "CARD_TRAP_DAMAGE":
			hostile(128, 129)
		case "CURSE_RELEASE", "CURSE_TURN_DOWN":
			start, param := 157, 2
			if role.Function == "CURSE_TURN_DOWN" {
				start, param = 172, 1
			}
			attribute := role.Parameters[param]
			for i, a := range []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"} {
				if combatNullValue(attribute) || damageAttributeMatches(attribute, a) {
					friendly(start+i*3, start+i*3+1, start+i*3+2)
				}
			}
		case "BLESS":
			friendly(187, 188, 189)
		case "PARAM_LIMIT_BREAK_FIXED", "PARAM_LIMIT_BREAK_BY_SELF_PARAM":
			switch role.Parameters[1] {
			case "ATK":
				friendly(190, 191, 192)
			case "INT":
				friendly(193, 194, 195)
			case "MND":
				friendly(196, 197, 198)
			}
		case "GUTS":
			friendly(199, 200, 201)
		case "ATTR_CUT_ATTACK":
			friendly(202, 203, 204)
		case "ATTR_CUT_ENCHANT":
			friendly(205, 206, 207)
		case "ATTR_DRAIN_ATTACK":
			friendly(223, 224, 225)
		case "ATTR_DRAIN_ENCHANT":
			friendly(226, 227, 228)
		case "ATTR_CUT_DOT", "ATTR_DRAIN_DOT":
			base := 208
			if role.Function == "ATTR_DRAIN_DOT" {
				base = 229
			}
			for i, dot := range []string{"POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC"} {
				if role.Parameters[4] == dot {
					friendly(base+i*3, base+i*3+1, base+i*3+2)
				}
			}
		case "NEED_COST_DOWN_BURST":
			friendly(244, 245, 246)
		case "BURST_GAUGE_QUICK_UP":
			friendly(247, 248, 249)
		case "BURST_GAUGE_REGENE_UP":
			friendly(250, 251, 252)
		}
	}
	if int32(f[253]) < int32(skill.HateRatio) {
		f[253] = uint32(skill.HateRatio)
	}
}
