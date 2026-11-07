package battlecore

// a4ac0 executes every producer, including a275b's CALL lookup even when
// another role owns the displayed number. The outer card/sphere preview owns
// RNG rollback; each CALL lookup uses preview=0 and card type22, preventing
// recursive BLESS display. All7982a lookups disable RANDOM independently of
// RNG rollback. Actual registration performs the same lookup once.
func (engine *BattleEngine) actionDisplayPower(player *battlePlayer, action *battleAction) (int, error) {
	power := engine.cardDisplayPower(player, action.cardLevel, action.skill, action.roles)
	if action.cardType == 22 || action.callSkill.ID == 0 {
		return power, nil
	}
	for index, role := range action.roles {
		if role.Function != "BLESS" {
			continue
		}
		hold := battleBlessHold{CardType: 22, SourceMember: player.MemberType,
			Skill: action.callSkill, Roles: action.callRoles, CardLevel: action.callExecutionLevel()}
		state, err := engine.resolveHoldDisplayState(player, hold, engine.appendChainCounts(player.MemberType, action.callSkill), false)
		if err != nil {
			return 0, err
		}
		action.callProducerQueried[role.RoleIndex] = true
		if action.skill.DisplayRole == index+1 {
			power = state.Power
		}
	}
	return power, nil
}

// 7982a selects extend.skill_value_role_no (CSV column5), then the chosen
// producer's info+0x50. It never searches for the first recognizable effect.
// Previews use 743f6(...,0,0): its third argument clips negative values, not
// parameter limits. Player limits still apply; native enemy sources are uncapped.
// Target armor/critical/EX damage consumers are not previews.
func (engine *BattleEngine) cardDisplayPower(player *battlePlayer, level int, skill CombatSkillDefinition, roles []CombatSkillRole) int {
	if skill.DisplayRole < 1 || skill.DisplayRole > 5 || skill.DisplayRole > len(roles) {
		return 0
	}
	role := roles[skill.DisplayRole-1]
	role, _ = burstParameterRole(role)
	view := *player
	stat := func(parameter string) int { return maxInt(0, combatStatValue(&view, parameter)) }
	switch role.Function {
	case "STAN":
		// The original producer publishes raw level-scaled accuracy, including
		// negative and over100 values. Hit resolution owns its own comparison.
		return retainedRateRoleValue(role, level)
	case "CARD_SEARCH":
		return int(int32(combatParameterInt(role.Parameters[0])))
	case "DAMAGE_BOOST_COUNT2":
		return countedDamageRate(role, level)
	case "ATTR_DEF_DOWN_BOOST":
		_, rate := fixedRatePassiveValues(role, level)
		return rate
	case "ATK_BREAK_CUT", "GUARD_BREAK_CUT":
		_, rate := fixedRatePassiveValues(role, level)
		return rate / 10
	case "NEED_COST_DOWN":
		return combatParameterInt(role.Parameters[1])
	case "DAMAGE_BOOST":
		_, fixed := damageBoostRoleValues(role, level)
		return fixed
	case "ATK_UP_BOOST", "DEF_UP_BOOST", "ATK_BREAK_BOOST", "GUARD_BREAK_BOOST", "HEAL_BOOST", "DAMAGE_CUT2":
		return combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level/1000
	case "CRITICAL_BOOST":
		return linearRoleValue(role, 1, 2, level)
	case "ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT":
		return attributeGuardPower(role.Function, attributeGuardRate(role, level))
	case "ATTACK_AA":
		value := attackRolePowerScaled(role, level, stat(role.Parameters[5]), engine.roleScale(role))
		// 7982a adds the ordinary REVENGE operator and all DAMAGE_INCREASE
		// operators only when the selected role is an attack (not any buff).
		modifiers := collectAttackModifiers(roles, &view, level, 1, engine.pvp)
		return value + modifiers.damageIncrease + attackRevengeBonus(view.DamageTaken, &view, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	case "DEF_UP_FIXED", "ATK_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "PARAM_LIMIT_BREAK_FIXED",
		"ATK_UP_FIXED_ENEMY", "DEF_UP_FIXED_ENEMY", "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY":
		return fixedBuffRoleValueScaled(role, level, 1, engine.roleScale(role))
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM":
		return selfScaledParameterValueScaled(role, level, 1, stat(role.Parameters[2]), engine.roleScale(role))
	case "HEAL_FIXED":
		return fixedHealRoleValueScaled(role, level, 1, stat(role.Parameters[4]), engine.roleScale(role))
	case "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP":
		return targetHPHealRoleValue(role, level, 1, 0)
	case "HEAL_CUT":
		return 0
	case "HEAL_REVERSE":
		return retainedRateRoleValue(role, level)
	case "DEBUFF_REGIST":
		return combatParameterInt(role.Parameters[1])
	case "HEAL_BY_SELF_PARAM":
		return selfScaledHealRoleValueScaled(role, level, 1, stat(role.Parameters[0]), engine.roleScale(role))
	case "REGENERATE_FIXED":
		return fixedRegenerateRoleValueScaled(role, level, 1, stat(role.Parameters[5]), engine.roleScale(role))
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		_, fixed := attributeDefenseRoleValues(role, level, 1)
		if engine.pvp {
			fixed = int(engine.roleScale(role).fixed(int32(fixed)))
		}
		if role.Function == "ATTR_DEF_DOWN" {
			return fixed
		}
		return absInt(fixed)
	case "ENCHANT":
		return enchantRoleValueScaled(role, level, 1, engine.roleScale(role))
	case "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC":
		_, _, _, value := dotRoleValuesScaled(role, level, 1, stat(role.Parameters[7]), engine.roleScale(role))
		return value
	case "CARD_TRAP_DAMAGE":
		// a354a publishes fixed damage plus the selected source coefficient.
		return cardTrapDamageRoleValue(role, level, 1, stat(role.Parameters[7]))
	case "COVERING":
		return coveringRoleValueScaled(role, level, 1, engine.roleScale(role)) / 10
	case "WEAKNESS", "CRITICAL_UP", "CRITICAL_DOWN", "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN":
		return retainedRateRoleValue(role, level) / 10
	case "REFLECTION":
		return retainedRateRoleValue(role, level) / 100
	case "DOT_VALUE_UP":
		return retainedRateRoleValue(role, level)
	case "CARD_SEAL_REGIST":
		// 95d2d publishes the level-scaled resistance percentage to info+0x50.
		return retainedRateRoleValue(role, level)
	case "ENDURE", "DEAL_BONUS", "DEAL_PENALTY", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		// 976ab publishes the positive reduction count for DEAL_PENALTY,
		// just as DEAL_BONUS publishes its increase count.
		return combatParameterInt(role.Parameters[0])
	case "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN":
		return linearRoleValue(role, 1, 2, level)
	case "GUTS":
		// 95fe9 writes the restored HP percentage to info+0x50.
		return combatParameterInt(role.Parameters[2])
	case "COST_BLOCK", "NEED_COST_UP":
		return combatParameterInt(role.Parameters[1])
	case "BURST_GAUGE_QUICK_UP":
		return burstGaugeQuickValue(role, level)
	case "DEBUFF_RELEASE", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_OLD",
		"BUFF_RELEASE", "BUFF_RELEASE_ONE", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_OLD", "HP_CUT", "CURSE_RELEASE", "BLESS_RELEASE":
		return linearRoleValue(role, 0, 1, level)
	case "CARD_TOKEN":
		return combatParameterInt(role.Parameters[1])
	case "PARAM_UP_SKILL_BONUS", "LIMIT_BREAK_BONUS":
		// The producer exposes its flat per-card component even when the
		// percentage mode is active. Authoring normally displays its consumer.
		return int(int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(level))
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		// Both barrier producers publish p3 (uses), not the damage threshold.
		return combatParameterInt(role.Parameters[3])
	case "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE":
		// Their info+0x50 is explicitly zero; modifiers only raise ATTACK display.
		return 0
	default:
		return 0
	}
}
