package battlecore

import "fmt"

// Both registered passive slots remain independent. The bonus slot compares
// SkillData.arthur_type (+20), not job_type (+16), before any branch lookup.
func (catalog *CombatCatalog) mainPassiveReference(card CombatCardDefinition, arthurType int) (int, int) {
	variants := catalog.Skills[card.ArthurPassiveSkillID]
	if card.ArthurPassiveSkillID != 0 && len(variants) > 0 && skillMatchesArthur(variants[0], arthurType) {
		return card.ArthurPassiveSkillID, 5
	}
	return card.PassiveSkillID, 4
}

func (catalog *CombatCatalog) validateMainPassiveProgram(cardID, id int) error {
	if id == 0 {
		return nil
	}
	variants, roleSets := catalog.Skills[id], catalog.SkillRoles
	if len(variants) == 0 {
		return fmt.Errorf("card %d main passive %d is unavailable", cardID, id)
	}
	for _, skill := range variants {
		if skill.Target != "SELF" && skill.Target != "USER_ALL" {
			return fmt.Errorf("card %d passive %d has unsupported target %q", cardID, id, skill.Target)
		}
		if skill.Type != "" && skill.Type != "NORMAL" && skill.Type != "PASSIVE" {
			return fmt.Errorf("card %d passive %d has unsupported type %q", cardID, id, skill.Type)
		}
		for _, condition := range []string{skill.BranchCondition, skill.BranchCondition2} {
			if !SupportsPlayerBranchCondition(condition) {
				return fmt.Errorf("card %d passive %d has unsupported branch %q", cardID, id, condition)
			}
		}
		roles := roleSets[skill.FunctionID]
		if len(roles) == 0 || len(roles) > 5 {
			return fmt.Errorf("card %d passive %d has unsupported roles", cardID, id)
		}
		// The original selected-first-NONE gate disables the complete role set.
		if roles[0].Function == "NONE" {
			continue
		}
		for _, role := range roles {
			if !SupportsMainCardPassiveFunction(role.Function) || role.Target != "SELECT" && role.Target != "SELF" && role.Target != "FRIEND_ALL" ||
				role.Function == "BEGINNING_DRAW" && (skill.Target != "SELF" || role.Target == "FRIEND_ALL") {
				return fmt.Errorf("card %d passive %d has unsupported role %q/%q", cardID, id, role.Function, role.Target)
			}
		}
	}
	return nil
}

// Admission audits both source references. Runtime only validates/executes the
// selected program; a mismatched bonus does not make its normal slot unusable.
func (catalog *CombatCatalog) ValidateMainCardPassives(card CombatCardDefinition) error {
	for _, id := range []int{card.PassiveSkillID, card.ArthurPassiveSkillID} {
		if err := catalog.validateMainPassiveProgram(card.ID, id); err != nil {
			return err
		}
	}
	return nil
}

func (catalog *CombatCatalog) CardMainPassive(card CombatCardDefinition, arthurType int) (CombatSkillDefinition, []CombatSkillRole, error) {
	id, _ := catalog.mainPassiveReference(card, arthurType)
	if id == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	if err := catalog.validateMainPassiveProgram(card.ID, id); err != nil {
		return CombatSkillDefinition{}, nil, err
	}
	variants, roleSets := catalog.Skills[id], catalog.SkillRoles
	skill := variants[0]
	return skill, roleSets[skill.FunctionID], nil
}

func SupportsMainCardPassiveFunction(function string) bool {
	switch function {
	case "NONE", "BEGINNING_DRAW", "NEED_COST_DOWN", "ATK_UP_FIXED", "DEF_UP_FIXED", "PARAM_LIMIT_BREAK_FIXED", "BURST_GAUGE_QUICK_UP", "ATK_BREAK_CUT", "GUARD_BREAK_CUT", "ATTR_DEF_DOWN_BOOST", "ENCHANT", "GUTS", "DAMAGE_BOOST_COUNT2":
		return true
	default:
		return SupportsSupportFunction(function)
	}
}

// The native entry traverses cards in deck-slot order, chooses one passive,
// then resolves its branch. A missing branch or first NONE emits no header and
// does not fall back to the other passive. First-draw flags come from the effects
// installed by these actual executions, not from merely referenced definitions.
func (engine *BattleEngine) executeMainCardPassives(owner *battlePlayer) ([]BattleResult, error) {
	var results []BattleResult
	for _, card := range owner.Deck {
		definition := engine.catalog.Cards[card.CardID]
		base, _, err := engine.catalog.CardMainPassive(definition, owner.ArthurType)
		if err != nil {
			return nil, err
		}
		if base.ID == 0 {
			continue
		}
		_, slot := engine.catalog.mainPassiveReference(definition, owner.ArthurType)
		level := card.Level
		if explicit := card.SkillLevels[slot]; explicit.ID == base.ID {
			level = explicit.Value
		}
		target := owner.MemberType
		if base.Target == "USER_ALL" {
			target = 0
		}
		action := battleAction{memberType: owner.MemberType, cardType: card.CardType, cardID: card.CardID,
			cardLevel: level, target: target, skill: base}
		variants, roleSets := engine.catalog.Skills[base.ID], engine.catalog.SkillRoles
		selected, marker := engine.selectCombatSkillVariantsMode(variants, action, engine.turnActions, nil, true)
		if selected.ID == 0 {
			continue
		}
		roles := roleSets[selected.FunctionID]
		if len(roles) == 0 || roles[0].Function == "NONE" {
			continue
		}
		selected.Target = base.Target
		action.skill, action.roles, action.branchIndex = selected, roles, marker
		rows, err := engine.executePlayerCardPassive(owner, action)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	return results, nil
}

// EX and selected main-card programs share native passive execution. Startup
// emits base parameters before PASSIVE-list statuses, without per-status 6.
func (engine *BattleEngine) executePlayerCardPassive(owner *battlePlayer, action battleAction) ([]BattleResult, error) {
	var results []BattleResult
	header, err := engine.playerCardSkillResult(action, 0)
	if err != nil {
		return nil, err
	}
	header.Args[6], header.Args[10] = 0, 1
	results = append(results, header)
	state := battleDisplayPower{Power: engine.cardDisplayPower(owner, action.cardLevel, action.skill, action.roles), Marker: action.branchIndex, Known: true}
	owner.CardDisplay[action.cardType] = state
	results = append(results, cardUpdate2Result(owner.MemberType, action.cardType, state, 0))
	listType := 0
	if action.skill.Type == "PASSIVE" {
		listType = 1
	}
	rows, err := engine.executePlayerPassiveRoleSet(owner, action, listType)
	if err != nil {
		return nil, err
	}
	results = append(results, rows...)
	engine.nativeSkillSerial++
	display, err := engine.refreshPassiveDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, display...)
	return results, nil
}

// Card and sphere startup share role consumers and deferred passive status
// projection. Their entry points choose the original effect-list identity.
func (engine *BattleEngine) executePlayerPassiveRoleSet(owner *battlePlayer, action battleAction, listType int) ([]BattleResult, error) {
	return engine.executeSkillRoleSet(owner.MemberType, action.target, action.skill, action.roles, true, func(role CombatSkillRole) ([]BattleResult, error) {
		if role.Function == "NONE" {
			return nil, nil
		}
		role.SourceSkillID = action.skill.ID
		switch role.Function {
		case "ATK_UP_FIXED", "DEF_UP_FIXED", "PARAM_LIMIT_BREAK_FIXED", "BURST_GAUGE_QUICK_UP":
			action.buffListType = listType
			return engine.executePlayerRole(action, role, 1)
		case "ENCHANT", "GUTS", "DAMAGE_BOOST_COUNT2":
			action.buffListType = listType
			rows, err := engine.executePlayerRole(action, role, 1)
			if err != nil || listType != 1 {
				return rows, err
			}
			var output []BattleResult
			for _, row := range rows {
				if row.Command == CommandPassiveBuff && engine.playerMember(int(row.Args[0])) {
					output = append(output, playerBaseParameterResult(engine.player(int(row.Args[0]))))
				}
				output = append(output, row)
			}
			return output, nil
		}
		effect := battleEffect{Function: "BEGINNING_DRAW", ListType: listType,
			Source: owner.MemberType, SourceSkillID: action.skill.ID, CardType: action.cardType, RoleIndex: role.RoleIndex, AppliedTurn: engine.turn}
		parameterFlags, attributeFlags := 0, 1
		if role.Function == "NEED_COST_DOWN" {
			if combatParameterInt(role.Parameters[1]) <= 0 {
				return nil, nil
			}
			effect = passiveCostDiscountEffect(role, engine.turn, owner.MemberType)
			effect.ListType = listType
			if combatParameterInt(role.Parameters[8]) != 0 {
				effect.CostCardType = action.cardType
			}
			attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
		} else if role.Function == "ATTR_DEF_DOWN_BOOST" {
			effect.Function, effect.Kind = role.Function, 1
			effect.Value, effect.Rate = fixedRatePassiveValues(role, action.cardLevel)
			effect.Remaining = combatParameterInt(role.Parameters[0])
			effect.Attribute = role.Parameters[5]
			effect.ProfileTags = [2]int{combatParameterInt(role.Parameters[6]), combatParameterInt(role.Parameters[7])}
			attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
		} else if role.Function == "ATK_BREAK_CUT" || role.Function == "GUARD_BREAK_CUT" {
			fixed, rate := fixedRatePassiveValues(role, action.cardLevel)
			effect.Function, effect.Kind = role.Function, 1
			effect.Value, effect.Rate = fixed, rate
			effect.Remaining = combatParameterInt(role.Parameters[0])
			effect.Parameter = role.Parameters[5]
			parameterFlags = combatParameterFlag(effect.Parameter)
		} else if role.Function != "BEGINNING_DRAW" {
			var err error
			effect, err = sphereSupportEffect(role, action.cardLevel, engine.turn, owner.MemberType)
			if err != nil {
				return nil, err
			}
			effect.ListType = listType
			attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
			switch role.Function {
			case "ATK_UP_BOOST", "DEF_UP_BOOST", "ATK_BREAK_BOOST", "GUARD_BREAK_BOOST":
				parameterFlags = combatParameterFlag(effect.Parameter)
			}
		}
		var output []BattleResult
		for _, member := range engine.playerRoleTargets(action, role) {
			player := engine.player(member)
			if player.HP <= 0 || player.GameOver {
				continue
			}
			player.Effects = append(player.Effects, effect)
			engine.recordAIStatusApplied(member, effect)
			if listType == 1 {
				output = append(output, playerBaseParameterResult(player))
			}
			parameters := [4]int{}
			if role.Function == "DAMAGE_BOOST" {
				parameters = nativeStatusUIParameters(battleBuffCodes[role.Function], effect)
			}
			output = append(output, battleBuffResultWithListType(member, role.RoleIndex, listType,
				battleBuffCodes[role.Function], parameterFlags, attributeFlags, parameters[0], parameters[1], parameters[2], parameters[3]))
		}
		return output, nil
	})
}

// FUN_0004a66e starts with deck/card-type order, pulls the first five eligible
// cards into a fixed prefix, then area_shuffle only shuffles the suffix.
// Unlike ordinary shuffle, area_shuffle consumes the final modulo-one draw.
// This entry is ONLY used for the first battle: 5db7a sets the 0x44 latch;
// subsequent waves and depleted-pool reshuffles use ordinary shuffle.
func (engine *BattleEngine) shuffleOpeningDeck(player *battlePlayer, guaranteed [10]bool) {
	defer engine.notifyDeckOrder(player, true)
	// JP5a8b0 -> 5a75a skips the opening guarantee in PVP modes3/4.
	// Story's shuffle-off setting also bypasses guarantee partitioning.
	if engine.pvp || engine.deckShuffleOff {
		engine.shuffleDeck(player)
		return
	}
	count := 0
	for index := range player.DeckOrder {
		if guaranteed[player.DeckOrder[index]] {
			player.DeckOrder[index], player.DeckOrder[count] = player.DeckOrder[count], player.DeckOrder[index]
			count++
			if count == len(player.Hand) {
				break
			}
		}
	}
	if count == 0 {
		engine.shuffleDeck(player)
		return
	}
	for end := len(player.DeckOrder); count < end; end-- {
		swapIndex := count + int(engine.rng.next()%uint32(end-count))
		player.DeckOrder[end-1], player.DeckOrder[swapIndex] = player.DeckOrder[swapIndex], player.DeckOrder[end-1]
	}
}
