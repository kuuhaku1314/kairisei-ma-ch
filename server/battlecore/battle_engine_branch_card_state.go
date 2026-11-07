package battlecore

import "strings"

// JP132 checks every selected card of the judging member, including the
// generating card for a CALL. Empty filters are unrestricted; p2 is exact base
// cost and p3 one profile tag, both narrowed to native int32. The fifth slot is
// unused. No selection is false; spheres use a separate selection vector.
func (engine *BattleEngine) branchOnlyPlayedCards(member int, actions []battleAction, parameters [5]string) bool {
	selected := false
	cost, tag := int(int32(combatParameterInt(parameters[2]))), int(int32(combatParameterInt(parameters[3])))
	for _, other := range actions {
		if other.memberType != member || other.cardType == 0 || other.sphereSlot != 0 {
			continue
		}
		selected = true
		if !combatNullValue(parameters[0]) && !strings.EqualFold(other.skill.Kind, parameters[0]) ||
			!combatNullValue(parameters[1]) && !combatAttributeMatches(other.skill.Attribute, parameters[1]) ||
			cost != 0 && other.skill.Cost != cost ||
			tag != 0 && !combatCardHasProfileTag(engine.catalog.Cards[other.cardID], tag) {
			return false
		}
	}
	return selected
}

func (engine *BattleEngine) branchDeckTagCount(player *battlePlayer, condition string, parameters [5]string) int {
	count := 0
	countDeck := func(cards [10]BattleCard) {
		for _, card := range cards {
			if card.CardID != 0 && combatCardHasProfileTag(engine.catalog.Cards[card.CardID], combatParameterInt(parameters[0]), combatParameterInt(parameters[1]), combatParameterInt(parameters[2])) {
				count++
			}
		}
	}
	if condition != "SELF_SUPPORT_DECK_TAG" {
		countDeck(player.Deck)
	}
	if condition != "SELF_MAIN_DECK_TAG" {
		countDeck(player.SupportDeck)
	}
	return count
}

func (engine *BattleEngine) branchOtherTagCount(action battleAction, actions []battleAction, parameters [5]string) int {
	count := 0
	for _, other := range actions {
		if other.memberType != action.memberType || other.cardType == 0 || other.sphereSlot != 0 || other.cardType == action.cardType {
			continue
		}
		if combatCardHasProfileTag(engine.catalog.Cards[other.cardID], combatParameterInt(parameters[0]), combatParameterInt(parameters[1]), combatParameterInt(parameters[2])) {
			count++
		}
	}
	return count
}

func (engine *BattleEngine) branchSelectedCost(condition string, action battleAction, actions []battleAction) int {
	value := 0
	arthurType := 0 // The enemy caller has no user Arthur class.
	if engine.playerMember(action.memberType) {
		arthurType = engine.player(action.memberType).ArthurType
	}
	for _, other := range actions {
		if other.cardType == 0 || other.sphereSlot != 0 || strings.HasPrefix(condition, "SELF_") && other.memberType != action.memberType {
			continue
		}
		// d25ac/d23ec query every selected card in the judging actor's job,
		// not the card owner's job. Both normal and class bonus can differ.
		cost := other.skill.Cost
		if !strings.HasPrefix(condition, "SELF_") {
			if skill, _, err := engine.catalog.CardSkill(other.cardID, arthurType); err == nil {
				cost = skill.Cost
			}
		}
		if condition == "FRIEND_PLAY_COST_TOTAL" {
			value = int(int32(value) + int32(cost))
		} else {
			value = maxInt(value, cost)
		}
	}
	return value
}

// The same native predicates serve user and enemy skill branches. Cost total
// (e2eb0) selects headers in the judging actor's Arthur class; cost count
// (e2c30), attribute and kind instead read each selected card in its owner's
// class. All consume retained selections, not the history of executed roles.
func (engine *BattleEngine) branchFriendSelectedCondition(condition string, member int, parameters [5]string, actions []battleAction) bool {
	switch condition {
	case "FRIEND_PLAY_ATTR_NUM", "FRIEND_PLAY_SKILL_KIND_NUM":
		return engine.branchFriendSelectedCount(condition, member, parameters, actions)
	case "FRIEND_PLAY_COST_TOTAL":
		return combatValueInRange(engine.branchSelectedCost(condition, battleAction{memberType: member}, actions), parameters[0], parameters[1])
	case "FRIEND_PLAY_COST_NUM":
		count := 0
		for _, other := range actions {
			if other.cardType == 0 || other.sphereSlot != 0 {
				continue
			}
			cost := other.skill.Cost
			if skill, _, err := engine.catalog.CardSkill(other.cardID, engine.player(other.memberType).ArthurType); err == nil {
				cost = skill.Cost
			}
			if combatValueInRange(cost, parameters[0], parameters[1]) {
				count++
			}
		}
		return combatValueInRange(count, parameters[2], parameters[3])
	case "FRIEND_PLAY_TAG":
		count := 0
		for _, other := range actions {
			// Profile selectors are signed32; a card contributes once even
			// with several matches. Retained KO selections are included.
			if other.cardType != 0 && other.sphereSlot == 0 && combatCardHasProfileTag(engine.catalog.Cards[other.cardID],
				int(int32(combatParameterInt(parameters[0]))), int(int32(combatParameterInt(parameters[1]))), int(int32(combatParameterInt(parameters[2])))) {
				count++
			}
		}
		return count >= combatParameterInt(parameters[3])
	}
	return false
}

func (engine *BattleEngine) branchCriticalMatches(condition string, actor int, target int, parameters [5]string) bool {
	check := func(effects []battleEffect) bool {
		value := nativeCriticalRate(effects, 0, CombatSkillDefinition{DamageKind: parameters[0]})
		return nativeOptionalRange(value, parameters[1], parameters[2])
	}
	switch condition {
	case "SELF_CRITICAL":
		return check(engine.battleMemberEffects(actor))
	case "TARGET_CRITICAL":
		if _, _, ok := engine.battleMemberHP(target); !ok {
			return false
		}
		return check(engine.battleMemberEffects(target))
	default:
		for _, p := range engine.playerParty(actor) {
			if p.MemberType != 0 && p.HP > 0 && check(p.Effects) {
				return true
			}
		}
		return false
	}
}

// JP123 compares the owning physical card slot. JP124 reads a specified
// registered slot of the judging member, then its selected base skill and tags.
// Card-type casts require the complete Int64 to be in0..32, not its low word.
func (engine *BattleEngine) branchDeckIndex(condition string, parameters [5]string, action battleAction) bool {
	slot := combatParameterInt(parameters[0])
	if condition == "OWNER_CARD_DECK_INDEX" && slot != action.cardType {
		return false
	}
	actor := engine.player(action.memberType)
	card, exists := conditionCardByType(actor, slot)
	if !exists {
		return false
	}
	if condition == "OWNER_CARD_DECK_INDEX" {
		return true
	}
	skill, _, err := engine.handCardSkill(actor, card)
	if err != nil || !supportAttributeMatches(parameters[1], skill.Attribute) {
		return false
	}
	definition := engine.catalog.Cards[card.CardID]
	for _, value := range parameters[2:4] {
		// Each positive Int64 selector is required independently. The native
		// profile lookup receives its low signed32 bits; the fifth slot is unused.
		if tag := combatParameterInt(value); tag > 0 && !combatCardHasProfileTag(definition, int(int32(tag))) {
			return false
		}
	}
	return true
}

func conditionCardByType(player *battlePlayer, cardType int) (BattleCard, bool) {
	switch {
	case cardType >= 1 && cardType <= len(player.Deck):
		card := player.Deck[cardType-1]
		return card, card.CardID != 0
	case cardType >= 11 && cardType <= 20:
		card := player.SupportDeck[cardType-11]
		return card, card.CardID != 0
	case isTokenCardType(cardType):
		return player.handSlotCard(cardType), player.Tokens[cardType-tokenCardFirst].SkillID != 0
	default:
		return BattleCard{}, false
	}
}
