package battlecore

// Shared member queries have identical PvE meaning for card and BOSS callers.
// Card selection/deck predicates remain with the player action context.
func commonMemberConditionSupported(condition string) bool {
	switch condition {
	case "BUFF_EXEC", "ENEMY_SIDE_BUFF", "ENEMY_TRIBAL", "TARGET_TRIBAL", "TARGET_JOB_TYPE",
		"SELF_CRITICAL", "TARGET_CRITICAL", "FRIEND_CRITICAL", "TARGET_HP_FIXED", "FRIEND_HP_FIXED", "FRIEND_HP_PER",
		"TARGET_BLESS", "TARGET_CURSE", "USER_SIDE_CURSE", "TARGET_EXEC_BLESS", "TARGET_EXEC_CURSE", "USER_SIDE_EXEC_BLESS", "USER_SIDE_EXEC_CURSE",
		"TARGET_BURST_GAUGE_STATE", "TARGET_BURST_GAUGE_VALUE", "USER_SIDE_BURST_GAUGE_STATE", "USER_SIDE_BURST_GAUGE_VALUE",
		"TARGET_BURST_BUFF", "USER_SIDE_BURST_BUFF", "TARGET_CARD_BUFF", "USER_SIDE_CARD_BUFF":
		return true
	}
	return false
}

func (engine *BattleEngine) commonMemberCondition(condition string, actor, target int, parameters [5]string) bool {
	switch condition {
	case "BUFF_EXEC":
		var values *[skillRoleBuffKindCount]uint32
		if engine.playerMember(actor) {
			values = &engine.player(actor).ExecutedBuffKinds
		} else if actor >= 5 && actor < 5+engine.enemyCount {
			values = &engine.enemies[actor-5].ExecutedBuffKinds
		}
		if values == nil {
			return false
		}
		for _, value := range parameters {
			kind := combatTriggerBuffKind(value)
			if kind > 0 && kind < len(values) && int32(values[kind]) > 0 {
				return true
			}
		}
		return false
	case "ENEMY_SIDE_BUFF":
		return engine.branchSideHasStatus(actor, false, true, parameters)
	case "SELF_CRITICAL", "TARGET_CRITICAL", "FRIEND_CRITICAL":
		return engine.branchCriticalMatches(condition, actor, target, parameters)
	case "TARGET_HP_FIXED":
		hp, _, ok := engine.battleMemberHP(target)
		return ok && combatValueInRange(hp, parameters[0], parameters[1])
	case "FRIEND_HP_FIXED", "FRIEND_HP_PER":
		for _, player := range engine.playerParty(actor) {
			value := player.HP
			if condition == "FRIEND_HP_PER" {
				value = player.HP * 100 / maxInt(1, player.MaxHP)
			}
			if player.MemberType != 0 && player.HP > 0 && combatValueInRange(value, parameters[0], parameters[1]) {
				return true
			}
		}
		return false
	default:
		return engine.branchHoldStateCondition(condition, parameters, battleAction{memberType: actor, target: target}, nil)
	}
}
