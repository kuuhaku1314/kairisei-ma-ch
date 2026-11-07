package battlecore

import "strings"

// These predicates reuse the same hold, gauge and modifier state as enemy AI.
// Side-wide status queries enumerate living members. JP ENEMY_TRIBAL instead
// includes every registered enemy, including a destroyed part; direct queries
// use the supplied member. Those native guards are implementation details, not editor options.
func (engine *BattleEngine) branchHoldStateCondition(condition string, parameters [5]string, action battleAction, actions []battleAction) bool {
	switch condition {
	case "SELF_ARTHUR_TYPE":
		if !engine.playerMember(action.memberType) {
			return false
		}
		actor := engine.player(action.memberType)
		wanted := combatArthurType(parameters[0])
		return actor.ArthurType == wanted
	case "TARGET_JOB_TYPE":
		if !engine.playerMember(action.target) {
			return false
		}
		p := engine.player(action.target)
		for _, value := range parameters[:2] {
			if wanted := combatArthurType(value); wanted != 0 && p.MemberType != 0 && p.ArthurType == wanted {
				return true
			}
		}
		return false
	case "TARGET_TRIBAL":
		return engine.branchTribalMatches(action.target, parameters)
	case "ENEMY_TRIBAL":
		for i := 0; i < engine.enemyCount; i++ {
			if e := &engine.enemies[i]; e.MemberType != 0 && engine.branchTribalMatches(e.MemberType, parameters) {
				return true
			}
		}
		return false
	case "OWNER_CARD_CARD_BUFF":
		if !engine.playerMember(action.memberType) {
			return false
		}
		actor := engine.player(action.memberType)
		if action.cardType < 1 || action.cardType > len(actor.Deck) || actor.Deck[action.cardType-1].CardID == 0 {
			return false
		}
		return engine.playerCardHasBuff(actor, parameters[:4], action.cardType)
	}
	member := action.memberType
	family := strings.TrimPrefix(condition, "SELF_")
	if strings.HasPrefix(condition, "TARGET_") {
		member, family = action.target, strings.TrimPrefix(condition, "TARGET_")
	} else if strings.HasPrefix(condition, "USER_SIDE_") {
		family = strings.TrimPrefix(condition, "USER_SIDE_")
		for _, p := range engine.playerParty(action.memberType) {
			if p.MemberType != 0 && p.HP > 0 && engine.branchMemberHoldState(family, p.MemberType, parameters) {
				return true
			}
		}
		return false
	} else if !strings.HasPrefix(condition, "SELF_") {
		return false
	}
	return engine.branchMemberHoldState(family, member, parameters)
}

func (engine *BattleEngine) branchTribalMatches(member int, parameters [5]string) bool {
	if _, _, ok := engine.battleMemberHP(member); !ok {
		return false
	}
	for _, value := range parameters {
		wanted := combatParameterInt(value)
		if engine.playerMember(member) && wanted == 0 ||
			!engine.playerMember(member) && member > maxRoomMembers && enemyHasTribe(&engine.enemies[member-maxRoomMembers-1], wanted) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) branchFriendSelectedCount(condition string, member int, parameters [5]string, actions []battleAction) bool {
	if combatNullValue(parameters[0]) {
		return false
	}
	count := 0
	for _, p := range engine.playerParty(member) {
		if p.MemberType == 0 || p.HP <= 0 {
			continue
		}
		_, registered := engine.selectedPlays[p.MemberType]
		for _, other := range actions {
			if other.memberType != p.MemberType {
				continue
			}
			registered = true
			if other.cardType == 0 || other.sphereSlot != 0 {
				continue
			}
			// cfdb0/d03f0 read the selected card in its owner's class.
			skill := other.skill
			if resolved, _, err := engine.catalog.CardSkill(other.cardID, p.ArthurType); err == nil {
				skill = resolved
			}
			if condition == "FRIEND_PLAY_ATTR_NUM" && combatAttributeMatches(skill.Attribute, parameters[0]) || condition == "FRIEND_PLAY_SKILL_KIND_NUM" && strings.EqualFold(skill.Kind, parameters[0]) {
				count++
			}
		}
		if registered && count >= combatParameterInt(parameters[1]) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) branchMemberHoldState(family string, member int, parameters [5]string) bool {
	if _, _, ok := engine.battleMemberHP(member); !ok {
		return false
	}
	var player *battlePlayer
	if engine.playerMember(member) {
		player = engine.player(member)
	}
	switch family {
	case "HOLD_NUM":
		count := 0
		if player != nil {
			count = len(player.CardHolds)
			if combatParameterInt(parameters[0]) != 0 {
				count += len(player.BlessHolds)
			}
		}
		return combatValueInRange(count, parameters[1], parameters[2])
	case "CURSE", "BLESS":
		count := 0
		if player != nil {
			cardType := 21
			if family == "BLESS" {
				cardType = 22
			}
			count = playerAppendCardCount(player, cardType, parameters[0])
		}
		return combatValueInRange(count, parameters[1], parameters[2])
	case "EXEC_CURSE", "EXEC_BLESS":
		if player == nil {
			return false
		}
		cardType := 21
		if family == "EXEC_BLESS" {
			cardType = 22
		}
		return player.hasAppendExecution(cardType, parameters[0], parameters[1])
	case "BURST_GAUGE_STATE":
		state, exists := burstGaugeStateCode(parameters[0])
		return player != nil && exists && player.BurstState == state
	case "BURST_GAUGE_VALUE":
		value := 0
		if player != nil {
			value = player.Burst
		}
		return combatValueInRange(value, parameters[0], parameters[1])
	case "BURST_BUFF":
		last := 5
		if combatParameterInt(parameters[0]) != 0 {
			last = 6
		}
		return enemyAIHasStatusInLists(engine.battleMemberEffects(member), true, parameters[1:], 5, last, false)
	case "CARD_BUFF":
		if player != nil {
			return engine.playerHasCardBuff(player, parameters[:4])
		}
		return enemyAIHasStatusInLists(engine.battleMemberEffects(member), true, parameters[:4], 7, 7, false)
	}
	return false
}
