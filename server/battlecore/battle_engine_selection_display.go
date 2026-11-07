package battlecore

// fullCardUpdateResult is CARD_UPDATE, not the power-only CARD_UPDATE2. Its
// third value means matching Arthur skill; the next four are other members'
// Chain participation. Managed HandsData uses these for separate UI states.
func fullCardUpdateResult(member, cardType int, arousal bool, chains [4]bool, display battleDisplayPower, appendIndex int) BattleResult {
	return BattleResult{Command: CommandCardUpdate, Args: []int64{
		int64(member), int64(cardType), boolInt64(arousal),
		boolInt64(chains[0]), boolInt64(chains[1]), boolInt64(chains[2]), boolInt64(chains[3]),
		int64(display.Power), int64(display.Marker), int64(appendIndex),
	}}
}

// 796a8's contributor output excludes the source, selects at most one
// matching card per other member, and never counts spheres. It may contain
// one contributor even though the numeric Chain result would be zero.
func (engine *BattleEngine) selectionChainFlags(source int, skill CombatSkillDefinition) [4]bool {
	var flags [4]bool
	if source < 1 || source > engine.playerCount()+engine.enemyCount || combatNullValue(skill.Attribute) {
		return flags
	}
	for _, action := range engine.turnActions {
		// The four wire bits retain global user slots1..4. Numeric opponent
		// Chain counts are party-local, but do not become new display-bit slots.
		if action.memberType != source && action.memberType >= 1 && action.memberType <= maxRoomMembers && engine.samePlayerParty(source, action.memberType) &&
			action.cardType > 0 && action.sphereSlot == 0 && damageAttributeMatches(skill.Attribute, action.skill.Attribute) {
			flags[action.memberType-1] = true
		}
	}
	return flags
}

// 40908 calls 3d680 then 40392 after registering a selection (including
// PASS). Unsubmitted members are projected first, with each hand card
// temporarily selected on its own; submitted members keep their real set.
// None of these hypothetical selections may change the action snapshot/RNG.
func (engine *BattleEngine) selectionDisplayResults(partyMember int) ([]BattleResult, error) {
	results, err := engine.selectionCardDisplayResults(0, partyMember)
	if err != nil {
		return nil, err
	}
	for _, player := range engine.playerStates() {
		if partyMember != 0 && !engine.samePlayerParty(partyMember, player.MemberType) {
			continue
		}
		if player.HP <= 0 {
			continue
		}
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			display, err := engine.sphereDisplayState(player, sphere)
			if err != nil {
				return nil, err
			}
			sphere.Display = display
			results = append(results, sphereUpdateResult(player.MemberType, sphere.Slot, display.Power, display.Marker))
		}
		for _, hold := range player.CardHolds {
			if hold.Action.sphereSlot == 0 {
				continue
			}
			display, err := engine.sphereActionDisplayState(player, hold.Action)
			if err != nil {
				return nil, err
			}
			results = append(results, sphereUpdateResult(player.MemberType, hold.Action.sphereSlot, display.Power, display.Marker))
		}
	}
	return results, nil
}

// 3d680 publishes cards and holds; 40392's sphere sweep is separate.
func (engine *BattleEngine) selectionCardDisplayResults(onlyMember, partyMember int) ([]BattleResult, error) {
	results := make([]BattleResult, 0, len(engine.players)*5)
	for _, submittedGroup := range []bool{false, true} {
		for _, player := range engine.playerStates() {
			if partyMember != 0 && !engine.samePlayerParty(partyMember, player.MemberType) {
				continue
			}
			if onlyMember != 0 && player.MemberType != onlyMember {
				continue
			}
			_, submitted := engine.selectedPlays[player.MemberType]
			if player.HP <= 0 || submitted != submittedGroup {
				continue
			}
			for _, deckSlot := range player.displayCardSlots() {
				if deckSlot == 0 {
					continue
				}
				card := player.handSlotCard(deckSlot)
				skill, roles, err := engine.handCardSkill(player, card)
				if err != nil {
					return nil, err
				}
				view := *engine
				if !submitted {
					view.turnActions = append(append([]battleAction(nil), engine.turnActions...), battleAction{
						memberType: player.MemberType, cardID: card.CardID, cardType: card.CardType,
						cardLevel: card.ExecutionLevel(skill.ID), skill: skill, roles: roles,
					})
				}
				display, err := view.cardDisplayStateMode(player, card, true, false)
				if err != nil {
					return nil, err
				}
				results = append(results, fullCardUpdateResult(player.MemberType, card.CardType,
					engine.catalog.cardUsesArthurSkill(card.CardID, player.ArthurType),
					view.selectionChainFlags(player.MemberType, skill), display, 0))
				if submitted {
					player.CardDisplay[card.CardType] = display
				}
			}
			for holdIndex := range player.BlessHolds {
				hold := &player.BlessHolds[holdIndex]
				if !submitted && hold.Target == 0 {
					continue
				}
				// Unlike 3fa8a's ongoing holder refresh, 3d680 queries Chain
				// using the original append caster. Enemy-source CALL cards
				// can also show matching player contributors (D-358).
				flags := engine.selectionChainFlags(hold.SourceMember, hold.Skill)
				count := 0
				for _, active := range flags {
					if active {
						count++
					}
				}
				if _, registered := engine.selectedPlays[hold.SourceMember]; registered {
					count++
				}
				if count < 2 {
					count = 0
				}
				display, err := engine.resolveHoldDisplayState(player, *hold, map[string]int{hold.Skill.Attribute: count}, true)
				if err != nil {
					return nil, err
				}
				results = append(results, fullCardUpdateResult(player.MemberType, hold.CardType, false, flags, display, hold.AppendIndex))
			}
		}
	}
	return results, nil
}
