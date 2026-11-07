package battlecore

import (
	"strings"
)

func (engine *BattleEngine) selectCombatSkillBranch(action battleAction, actions []battleAction, chainCounts map[string]int) CombatSkillDefinition {
	selected, _ := engine.selectCombatSkillBranchWithIndex(action, actions, chainCounts)
	return selected
}

func (engine *BattleEngine) selectCombatSkillBranchWithIndex(action battleAction, actions []battleAction, chainCounts map[string]int) (CombatSkillDefinition, int) {
	return engine.selectCombatSkillBranchMode(action, actions, chainCounts, true)
}

func (engine *BattleEngine) selectCombatSkillBranchMode(action battleAction, actions []battleAction, chainCounts map[string]int, allowRandom bool) (CombatSkillDefinition, int) {
	variants := engine.catalog.Skills[action.skill.ID]
	return engine.selectCombatSkillVariantsMode(variants, action, actions, chainCounts, allowRandom)
}

func (engine *BattleEngine) selectCombatSkillVariantsMode(variants []CombatSkillDefinition, action battleAction, actions []battleAction, chainCounts map[string]int, allowRandom bool) (CombatSkillDefinition, int) {
	actions = engine.partyActions(actions, action.memberType)
	if len(variants) == 0 {
		return CombatSkillDefinition{}, 0
	}
	var selected CombatSkillDefinition
	selectedIndex := 0
	matched := false
	for index, variant := range variants {
		// d3f50 rejects RANDOM in 7982a's mode0 lookup without a roll.
		// This switch is independent of 7982a's RNG save/restore argument.
		if !allowRandom && (variant.BranchCondition == "RANDOM" || variant.BranchCondition2 == "RANDOM") {
			continue
		}
		if variant.BranchCondition != "" && !engine.branchConditionSatisfied(variant.BranchCondition, variant.BranchParameters, action, actions, chainCounts) {
			continue
		}
		if variant.BranchCondition2 != "" && !engine.branchConditionSatisfied(variant.BranchCondition2, variant.BranchParameters2, action, actions, chainCounts) {
			continue
		}
		if !matched || variant.BranchPriority > selected.BranchPriority {
			selected = variant
			selectedIndex = index
			matched = true
		}
	}
	return selected, selectedIndex
}

func (engine *BattleEngine) branchConditionSatisfied(condition string, parameters [5]string, action battleAction, actions []battleAction, chainCounts map[string]int) bool {
	if commonMemberConditionSupported(condition) {
		return engine.commonMemberCondition(condition, action.memberType, action.target, parameters)
	}
	actor := engine.player(action.memberType)
	switch condition {
	case "", "NONE":
		return true
	case "OWNER_CARD_DECK_INDEX", "CARD_DECK_INDEX":
		return engine.branchDeckIndex(condition, parameters, action)
	case "OWNER_CARD_FAME":
		// e1e59 resolves this action's owned card (3fe70), not its master
		// definition or another selected card. Bounds remain signed64;
		// zero only omits the upper bound. Non-card actions have no owner.
		card, ok := conditionCardByType(actor, action.cardType)
		return action.sphereSlot == 0 && ok && combatValueInRange(card.Fame, parameters[0], parameters[1])
	case "FRIEND_PLAY_ATTR_NUM", "FRIEND_PLAY_SKILL_KIND_NUM", "FRIEND_PLAY_TAG", "FRIEND_PLAY_COST_NUM", "FRIEND_PLAY_COST_TOTAL":
		return engine.branchFriendSelectedCondition(condition, action.memberType, parameters, actions)
	case "DECK_COMBO_COUNT":
		if action.sphereSlot != 0 {
			return combatValueInRange(0, parameters[0], parameters[1])
		}
		return combatValueInRange(maxCombatChain(action.skill.Attribute, chainCounts), parameters[0], parameters[1])
	case "TURN":
		return combatValueInRange(engine.turn, parameters[0], parameters[1])
	case "SUMMONS_EVERY_TURN":
		period := int64(combatParameterInt(parameters[0]))
		return period > 0 && int64(engine.turn-actor.SummonTurn+1)%period == 0
	case "SELF_HP_PER":
		return combatValueInRange(actor.HP*100/maxInt(1, actor.MaxHP), parameters[0], parameters[1])
	case "SELF_HP_FIXED":
		return nativeOptionalRange(actor.HP, parameters[0], parameters[1])
	case "TARGET_HP_PER":
		hp, maxHP, ok := engine.battleMemberHP(action.target)
		if !ok {
			return false
		}
		hp = maxInt(0, hp) * 100 / maxInt(1, maxHP)
		return combatValueInRange(hp, parameters[0], parameters[1])
	case "TARGET_ATTR":
		return engine.branchTargetAttributeMatches(action.target, parameters)
	case "ENEMY_TRANCE_GAUGE_STATE", "TARGET_TRANCE_GAUGE_STATE":
		return engine.branchEnemyTranceState(condition, action.memberType, action.target, parameters[0])
	case "SELF_BUFF", "SELF_DEBUFF":
		return branchHasStatus(actor.Effects, condition == "SELF_BUFF", parameters)
	case "SELF_BLESS":
		// d2aab -> d2976 -> 43034 selects BLESS, not every append card.
		return combatValueInRange(playerAppendCardCount(actor, 22, parameters[0]), parameters[1], parameters[2])
	case "TARGET_BUFF", "TARGET_DEBUFF":
		return branchHasStatus(engine.battleMemberEffects(action.target), condition == "TARGET_BUFF", parameters)
	case "USER_SIDE_DEBUFF", "USER_SIDE_BUFF":
		return engine.branchSideHasStatus(action.memberType, true, condition == "USER_SIDE_BUFF", parameters)
	case "ENEMY_SIDE_DEBUFF":
		return engine.branchSideHasStatus(action.memberType, false, false, parameters)
	case "SELF_OTHER_PLAY_TAG":
		return engine.branchOtherTagCount(action, actions, parameters) >= combatParameterInt(parameters[3])
	case "TARGET_CARD_ONLY_SELF_PLAY":
		return engine.branchOnlyPlayedCards(action.memberType, actions, parameters)
	case "SELF_MAIN_DECK_TAG", "SELF_SUPPORT_DECK_TAG", "SELF_ALL_DECK_TAG":
		return engine.branchDeckTagCount(actor, condition, parameters) >= combatParameterInt(parameters[3])
	case "SELF_PLAY_MOST_HIGH_COST", "FRIEND_PLAY_MOST_HIGH_COST":
		return combatValueInRange(engine.branchSelectedCost(condition, action, actions), parameters[0], parameters[1])
	case "SELF_OTHER_PLAY_ATTR", "SELF_OTHER_PLAY_RARITY", "SELF_OTHER_PLAY_SKILL_KIND":
		if combatNullValue(parameters[0]) {
			return false
		}
		return engine.countSelectedActions(actions, action, condition, parameters[0], false) >= combatParameterInt(parameters[1])
	case "SELF_OTHER_PLAY_NUM":
		return combatValueInRange(engine.countSelectedActions(actions, action, "", "", false), parameters[0], parameters[1])
	case "SELF_PLAY_MOST_LOW_COST":
		// Hand/NPC previews temporarily submit the candidate on its own.
		// Their provisional action is not a committed selectedPlays entry.
		// A direct query with no selection still differs from a registered
		// PASS, whose minimum remains INT_MAX.
		selected := selectedActionsByMember(actions, action.memberType)
		if _, registered := engine.selectedPlays[action.memberType]; !registered && len(selected) == 0 {
			return false
		}
		minimum := 2147483647 // d2866 retains INT_MAX when no card is selected.
		for _, other := range selected {
			minimum = minInt(minimum, other.skill.Cost)
		}
		return combatValueInRange(minimum, parameters[0], parameters[1])
	case "SELF_PLAY_COST_TOTAL":
		total := 0
		for _, other := range selectedActionsByMember(actions, action.memberType) {
			total += other.skill.Cost
		}
		return combatValueInRange(total, parameters[0], parameters[1])
	case "SELF_PLAY_COST_NUM":
		count := 0
		for _, other := range selectedActionsByMember(actions, action.memberType) {
			if combatValueInRange(other.skill.Cost, parameters[0], parameters[1]) {
				count++
			}
		}
		return combatValueInRange(count, parameters[2], parameters[3])
	case "SELF_NOT_PLAY_HAND_NUM":
		return combatValueInRange(engine.unplayedHandCount(actor, actions), parameters[0], parameters[1])
	case "DEBUFF_EXEC", "SELF_PARAM_UP_VALUE", "TARGET_PARAM_UP_VALUE", "USER_SIDE_PARAM_UP_VALUE":
		return engine.branchSkillStatistic(condition, action.memberType, action.target, parameters)
	case "USER_PARAM", "USER_ENCHANT", "USER_COVERING", "USER_REFLECTION", "USER_REGENERATE":
		return engine.branchMemberCondition(condition, action.memberType, parameters)
	case "USER_ATTACK", "USER_ATTACK_NUM":
		return engine.branchAttackCondition(condition, action.memberType, parameters)
	case "TARGET_ENEMY_CHARGE_START":
		return engine.branchTargetChargeStarted(action.target)
	case "ENEMY_DEAD_NOW_TURN":
		return engine.branchEnemyDiedThisTurn(parameters[0])
	case "SELF_BUFF_RELEASE", "TARGET_BUFF_RELEASE", "USER_SIDE_BUFF_RELEASE", "SELF_DEBUFF_RELEASE", "TARGET_DEBUFF_RELEASE", "USER_SIDE_DEBUFF_RELEASE":
		return engine.branchReleaseStatistic(condition, action.memberType, action.target, parameters)
	case "SELF_DAMAGE", "FRIEND_DAMAGE", "SELF_ENCHANT_DAMAGE", "RECOVERY_ACTION":
		return engine.branchDamageStatistic(condition, action.memberType, parameters)
	case "SELF_BUFF_FROM_FRIEND", "SELF_DEBUFF_FROM_ENEMY":
		return branchHasSourcedStatus(actor.Effects, actor.MemberType, condition == "SELF_BUFF_FROM_FRIEND", parameters)
	case "TARGET_BUFF_KIND_NUM", "TARGET_DEBUFF_KIND_NUM":
		_, _, ok := engine.battleMemberHP(action.target)
		return ok && combatValueInRange(branchStatusKindCount(engine.battleMemberEffects(action.target), condition == "TARGET_BUFF_KIND_NUM"), parameters[0], parameters[1])
	case "FRIEND_PLAY_NUM":
		count := 0
		for _, other := range actions {
			if other.cardType != 0 && other.sphereSlot == 0 {
				count++
			}
		}
		return combatValueInRange(count, parameters[0], parameters[1])
	case "SELF_PLAYED_SKILL_KIND", "FRIEND_PLAYED_SKILL_KIND", "SELF_PLAYED_TAG", "FRIEND_PLAYED_TAG":
		return engine.playedCardCondition(actor, condition, parameters)
	case "FRIEND_PLAY_MOST_LOW_COST":
		minimum := 2147483647
		for _, other := range actions {
			if other.cardType != 0 && other.sphereSlot == 0 {
				minimum = minInt(minimum, other.skill.Cost)
			}
		}
		return combatValueInRange(minimum, parameters[0], parameters[1])
	case "SELF_MAIN_DECK_ATTR", "SELF_MAIN_DECK_SKILL_KIND":
		count := 0
		for _, card := range actor.Deck {
			skill, _, err := engine.catalog.CardSkill(card.CardID, actor.ArthurType)
			if err != nil {
				continue
			}
			if condition == "SELF_MAIN_DECK_SKILL_KIND" && (strings.EqualFold(skill.Kind, parameters[0]) || strings.TrimSpace(parameters[1]) != "" && !strings.EqualFold(parameters[1], "NULL") && strings.EqualFold(skill.Kind, parameters[1])) {
				count++
			} else if condition == "SELF_MAIN_DECK_ATTR" && (combatAttributeMatches(skill.Attribute, parameters[0]) || strings.TrimSpace(parameters[1]) != "" && !strings.EqualFold(parameters[1], "NULL") && combatAttributeMatches(skill.Attribute, parameters[1])) {
				count++
			}
		}
		return count >= maxInt(1, combatParameterInt(parameters[2]))
	case "RANDOM":
		return int(engine.rng.next()%10000) < combatParameterInt(parameters[0])*100
	default:
		return engine.branchHoldStateCondition(condition, parameters, action, actions)
	}
}

func combatValueInRange(value int, lowerText string, upperText string) bool {
	lower := combatParameterInt(lowerText)
	upper := combatParameterInt(upperText)
	if lowerText != "" && value < lower {
		return false
	}
	return upperText == "" || upper == 0 || value <= upper
}

func combatAttributeMatches(attributes string, wanted string) bool {
	for _, attribute := range splitCombatAttributes(attributes) {
		if strings.EqualFold(attribute, wanted) {
			return true
		}
	}
	return false
}

func combatEffectCount(effects []battleEffect, function string, attribute string) int {
	count := 0
	for _, effect := range effects {
		if (function == "" || strings.EqualFold(effect.Function, function)) && (attribute == "" || strings.EqualFold(effect.Attribute, attribute)) {
			count++
		}
	}
	return count
}

func containsInt(values []int, wanted int) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) countSelectedActions(actions []battleAction, current battleAction, condition string, value string, friends bool) int {
	count := 0
	for _, other := range actions {
		// 40908 stores selected cards at +8/count+30, and spheres in a
		// separate +34/count+3c list. These predicates read only cards.
		if other.cardType == 0 || other.sphereSlot != 0 {
			continue
		}
		if other.cardType == current.cardType && other.memberType == current.memberType {
			continue
		}
		if friends && other.memberType == current.memberType || !friends && other.memberType != current.memberType {
			continue
		}
		matches := condition == ""
		switch condition {
		case "SELF_OTHER_PLAY_ATTR":
			for _, component := range splitCombatAttributes(value) {
				matches = matches || combatAttributeMatches(other.skill.Attribute, component)
			}
		case "SELF_OTHER_PLAY_RARITY":
			rarity := 1 // Original generated CardData uses NORMAL, independently of its visual ID.
			if !isTokenCardType(other.cardType) {
				rarity = combatRarityCode(engine.catalog.Cards[other.cardID].Rarity)
			}
			matches = rarity >= combatRarityCode(value)
		case "SELF_OTHER_PLAY_SKILL_KIND":
			matches = strings.EqualFold(other.skill.Kind, value)
		}
		if matches {
			count++
		}
	}
	return count
}

func selectedActionsByMember(actions []battleAction, memberType int) []battleAction {
	selected := make([]battleAction, 0, len(actions))
	for _, action := range actions {
		if action.memberType == memberType && action.cardType != 0 && action.sphereSlot == 0 {
			selected = append(selected, action)
		}
	}
	return selected
}

func (engine *BattleEngine) unplayedHandCount(player *battlePlayer, actions []battleAction) int {
	played := make(map[int]struct{})
	for _, action := range actions {
		if action.memberType == player.MemberType {
			played[action.cardType] = struct{}{}
		}
	}
	count := 0
	for _, deckSlot := range player.Hand {
		if deckSlot == 0 {
			continue
		}
		cardType := player.handSlotCard(deckSlot).CardType
		if _, exists := played[cardType]; !exists {
			count++
		}
	}
	return count
}
