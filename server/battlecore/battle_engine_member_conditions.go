package battlecore

import "strings"

// Member selectors query the player side: the actor's party in PVP and the
// fixed player side for PvE player/BOSS callers. Dead members are omitted.
func (engine *BattleEngine) branchSelectedMember(actor int, selector string, matches func(int) bool) bool {
	selector = strings.ToUpper(strings.TrimSpace(selector))
	alive := func(member int) bool {
		hp, _, exists := engine.battleMemberHP(member)
		return exists && hp > 0
	}
	switch selector {
	case "", "SELF":
		return alive(actor) && matches(actor)
	case "USER_ANYONE", "USER_ALL":
		all := selector == "USER_ALL"
		for _, p := range engine.playerParty(actor) {
			if p.MemberType == 0 || !alive(p.MemberType) {
				continue
			}
			if match := matches(p.MemberType); all && !match || !all && match {
				return !all
			}
		}
		return all
	case "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		wanted := combatArthurType(selector)
		for _, p := range engine.playerParty(actor) {
			if p.MemberType != 0 && p.ArthurType == wanted {
				return alive(p.MemberType) && matches(p.MemberType)
			}
		}
	}
	return false
}

func (engine *BattleEngine) branchMemberCondition(condition string, actor int, parameters [5]string) bool {
	return engine.branchSelectedMember(actor, parameters[0], func(member int) bool {
		if condition == "USER_PARAM" {
			if skillStatisticsParameter(parameters[1]) < 0 && parameters[1] != "" {
				return false
			}
			var player *battlePlayer
			if engine.playerMember(member) {
				player = engine.player(member)
			} else {
				player = playerFromEnemy(&engine.enemies[member-5])
			}
			parameter := parameters[1]
			if parameter == "" {
				parameter = "HP"
			}
			value := int64(combatStatValue(player, parameter))
			// d1c20: inclusive signed range. Zero is an actual bound.
			return value >= int64(combatParameterInt(parameters[2])) && value <= int64(combatParameterInt(parameters[3]))
		}
		effects := engine.battleMemberEffects(member)
		if condition == "USER_ENCHANT" {
			// 45b88 sums all seven lists with int32 arithmetic, retaining the
			// last entry's attribute in native list order.
			var value int32
			attribute, found := "", false
			for list := 0; list < 7; list++ {
				for _, effect := range effects {
					if effect.ListType == list && effect.Function == "ENCHANT" {
						value += int32(effect.Value)
						attribute, found = effect.Attribute, true
					}
				}
			}
			return found && damageAttributeMatches(parameters[1], attribute) && int64(value) >= int64(combatParameterInt(parameters[2]))
		}
		minimum := int64(combatParameterInt(parameters[1]))
		for _, effect := range effects {
			if effect.ListType < 0 || effect.ListType > 6 {
				continue
			}
			// 457ec returns individual entries; no cross-entry sum here.
			switch condition {
			case "USER_COVERING":
				if effect.Function == "COVERING" && int64(int32(effect.Rate)) >= minimum {
					return true
				}
			case "USER_REFLECTION":
				if effect.Function == "REFLECTION" && int64(int32(effect.Rate)) >= minimum {
					return true
				}
			case "USER_REGENERATE":
				if (effect.Function == "REGENERATE_FIXED" || effect.Function == "REGENERATE_BY_SELF_PARAM") && int64(int32(effect.Value)) >= minimum {
					return true
				}
			}
		}
		return false
	})
}
