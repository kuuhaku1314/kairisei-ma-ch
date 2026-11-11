package battlecore

import "fmt"

func (engine *BattleEngine) playerCardCostResult(player *battlePlayer, cardType, baseCost int) BattleResult {
	args := []int64{int64(player.MemberType), int64(cardType),
		int64(engine.effectiveCardCost(player, cardType, baseCost)), int64(engine.cardCostReduction(player, cardType, baseCost))}
	args = append(args, int64(player.cardCostIncrease(cardType)))
	return BattleResult{Command: CommandCardCost, Args: args}
}

func playerCardStateResult(player *battlePlayer, cardType int) BattleResult {
	state, remaining := playerCardState(player, cardType)
	return BattleResult{Command: CommandCardState, Args: []int64{int64(player.MemberType), int64(cardType), int64(state), int64(remaining)}}
}

// 4a48c: seal, trap and registered Buddy card modifier are independent bits.
func playerCardState(player *battlePlayer, cardType int) (state, remaining int) {
	if effect, sealed := playerCardSealEffect(player, cardType); sealed {
		state |= 1
		remaining = effect.Remaining
	}
	if effect, trapped := playerCardTrapEffect(player, cardType); trapped {
		state |= 2
		// 4a266/4a3b8 expose effect+8 (remaining duration). Trap damage
		// remains internal until the card is played; it is not row29 data.
		remaining = effect.Remaining
	}
	if cardType > 0 && cardType < len(player.CardBurstSkills) && len(player.CardBurstSkills[cardType]) > 0 {
		state |= 4
	}
	return state, remaining
}

// cardDisplayState resolves the same base/extend family used for execution,
// with the original preview RNG boundary: 3fa8a/41a42 pass preview=1 to 7982a,
// which saves xor128 via a8fc2 and restores it via a9015, including on failure.
// d3f50 also rejects RANDOM in the independent mode0 branch lookup. Other
// deterministic branches still resolve, but no random branch is previewed.
func (engine *BattleEngine) cardDisplayState(player *battlePlayer, card BattleCard) (battleDisplayPower, error) {
	return engine.cardDisplayStateMode(player, card, false, false)
}

func (engine *BattleEngine) cardDisplayStateMode(player *battlePlayer, card BattleCard, selection, baseOnly bool) (battleDisplayPower, error) {
	if engine == nil || engine.catalog == nil || player == nil {
		return battleDisplayPower{}, fmt.Errorf("combat card display resolver is unavailable")
	}
	baseSkill, baseRoles, err := engine.handCardSkill(player, card)
	if err != nil {
		return battleDisplayPower{}, err
	}
	action := battleAction{
		memberType: player.MemberType, cardID: card.CardID, cardType: card.CardType,
		cardLevel: card.ExecutionLevel(baseSkill.ID), skill: baseSkill, roles: baseRoles,
	}
	action.enemySkill = player.tokenUsesEnemySkill(card.CardType)
	action.callSkill, action.callRoles, err = engine.handCardCallSkill(player, card)
	if err != nil {
		return battleDisplayPower{}, err
	}
	action.callLevel, action.hasCallLevel = card.ExecutionLevel(action.callSkill.ID), true
	return engine.cardActionDisplayStateMode(player, action, selection, baseOnly)
}

// Held token slots may already have been released. Resolve the captured CALL
// program directly, just as execution does, instead of rereading the hand.
func (engine *BattleEngine) cardActionDisplayStateMode(player *battlePlayer, action battleAction, selection, baseOnly bool) (battleDisplayPower, error) {
	previewRNG := engine.rng
	defer func() { engine.rng = previewRNG }()
	baseSkill := action.skill
	for _, selected := range engine.turnActions {
		if baseOnly {
			break
		}
		if selected.memberType == player.MemberType && selected.cardType == action.cardType {
			action.target = selected.target
			break
		}
	}
	counts := battleActionChainCounts(engine.partyActions(engine.turnActions, player.MemberType))
	_, registered := engine.selectedPlays[player.MemberType]
	if selection || registered {
		// 796a8(...,1) includes a registered source even for a zero-card
		// PASS or a selection whose attribute differs from this hand card.
		counts = engine.appendChainCounts(player.MemberType, baseSkill)
	}
	variants := engine.catalog.Skills[action.skill.ID]
	if baseOnly && len(variants) > 0 {
		variants = variants[:1]
		counts = nil
	}
	branch, marker := engine.selectCombatSkillVariantsMode(variants, action, engine.turnActions, counts, false)
	if branch.ID != 0 {
		roles := engine.catalog.SkillRoles[branch.FunctionID]
		if len(roles) == 0 {
			return battleDisplayPower{}, fmt.Errorf("combat card %d display role %d is unavailable", action.cardID, branch.FunctionID)
		}
		action.skill = branch
		action.roles = append([]CombatSkillRole(nil), roles...)
	} else {
		return battleDisplayPower{Known: true}, nil
	}
	if err := engine.attachBurstCardModifiers(&action); err != nil {
		return battleDisplayPower{}, err
	}
	power, err := engine.actionDisplayPower(player, &action)
	if err != nil {
		return battleDisplayPower{}, err
	}
	state := battleDisplayPower{Power: power, Marker: marker, Known: true}
	// 7982a preserves the skill preview for sealed cards. Sealing is exposed
	// through the separate hand-state fields and does not zero 314 power.
	return state, nil
}

func (engine *BattleEngine) sphereDisplayState(player *battlePlayer, sphere *battleSphere) (battleDisplayPower, error) {
	if engine == nil || engine.catalog == nil || player == nil || sphere == nil {
		return battleDisplayPower{}, fmt.Errorf("combat sphere display resolver is unavailable")
	}
	baseSkill, baseRoles, err := engine.catalog.SphereSkill(sphere.SphereID)
	if err != nil {
		return battleDisplayPower{}, err
	}
	action := battleAction{
		memberType: player.MemberType, sphereSlot: sphere.Slot, cardLevel: sphere.executionLevel(baseSkill.ID),
		skill: baseSkill, roles: baseRoles,
	}
	for _, selected := range engine.turnActions {
		if selected.memberType == player.MemberType && selected.sphereSlot == sphere.Slot {
			action.target = selected.target
			break
		}
	}
	return engine.sphereActionDisplayState(player, action)
}

func (engine *BattleEngine) sphereActionDisplayState(player *battlePlayer, action battleAction) (battleDisplayPower, error) {
	previewRNG := engine.rng
	defer func() { engine.rng = previewRNG }()
	branch, marker := engine.selectCombatSkillBranchMode(action, engine.turnActions, battleActionChainCounts(engine.partyActions(engine.turnActions, player.MemberType)), false)
	if branch.ID != 0 {
		roles := engine.catalog.SkillRoles[branch.FunctionID]
		if len(roles) == 0 {
			return battleDisplayPower{}, fmt.Errorf("combat sphere slot %d display role %d is unavailable", action.sphereSlot, branch.FunctionID)
		}
		action.skill = branch
		action.roles = roles
	} else {
		return battleDisplayPower{Known: true}, nil
	}
	// Sphere queries override the card index with0, but preserve the retained
	// sphere/Buddy indices. Card and append previews resolve their own source
	// explicitly above and leave the retained state untouched.
	source := player.ExecutionSource
	source.CardType = 0
	var err error
	action.callSkill, action.callRoles, action.callLevel, err = engine.playerSourceCall(player, source)
	if err != nil {
		return battleDisplayPower{}, err
	}
	action.hasCallLevel = true
	power, err := engine.actionDisplayPower(player, &action)
	if err != nil {
		return battleDisplayPower{}, err
	}
	return battleDisplayPower{
		Power: power, Marker: marker, Known: true,
	}, nil
}

func (engine *BattleEngine) holdDisplayState(owner *battlePlayer, hold battleBlessHold) (battleDisplayPower, error) {
	if owner == nil {
		return battleDisplayPower{}, fmt.Errorf("combat append-card display owner is unavailable")
	}
	return engine.resolveHoldDisplayState(owner, hold, engine.appendChainCounts(owner.MemberType, hold.Skill), true)
}

// 80510 initializes holds with Chain=0 and preview=1; 3fa8a later refreshes
// with the current Chain. a275b's separate producer lookup uses preview=0,
// but ALL three call7982a -> d7578 with allowRandom=0.
func (engine *BattleEngine) resolveHoldDisplayState(owner *battlePlayer, hold battleBlessHold, chainCounts map[string]int, preview bool) (battleDisplayPower, error) {
	if owner == nil {
		return battleDisplayPower{}, fmt.Errorf("combat append-card display owner is unavailable")
	}
	if preview {
		previewRNG := engine.rng
		defer func() { engine.rng = previewRNG }()
	}
	roles := hold.Roles
	displaySkill := hold.Skill
	marker := 0
	source := owner
	if hold.EnemySkill {
		if hold.SourceMember < 5 || hold.SourceMember >= 5+engine.enemyCount {
			return battleDisplayPower{}, fmt.Errorf("append-card display source enemy %d is unavailable", hold.SourceMember)
		}
		actor := &engine.enemies[hold.SourceMember-5]
		view := enemyDisplaySource(actor)
		source = &view
		if skill, selectedRoles, selectedMarker, matched := engine.selectEnemySkillBranchMode(actor, hold.Skill.ID, hold.Target, false); matched {
			if len(selectedRoles) == 0 {
				return battleDisplayPower{}, fmt.Errorf("combat enemy append-card skill %d display role %d is unavailable", hold.Skill.ID, skill.FunctionID)
			}
			displaySkill = skill
			roles = selectedRoles
			marker = selectedMarker
		} else if len(engine.catalog.Skills[hold.Skill.ID]) > 0 {
			return battleDisplayPower{Known: true}, nil
		}
	} else {
		if engine.playerMember(hold.SourceMember) {
			source = engine.player(hold.SourceMember)
		}
		action := battleAction{
			memberType: source.MemberType, cardType: hold.CardType, cardLevel: hold.CardLevel,
			target: hold.Target, skill: hold.Skill, roles: roles,
		}
		if branch, selectedMarker := engine.selectCombatSkillBranchMode(action, engine.turnActions, chainCounts, false); branch.ID != 0 {
			selectedRoles := engine.catalog.SkillRoles[branch.FunctionID]
			if len(selectedRoles) == 0 {
				return battleDisplayPower{}, fmt.Errorf("combat append-card skill %d display role %d is unavailable", hold.Skill.ID, branch.FunctionID)
			}
			roles = selectedRoles
			marker = selectedMarker
			displaySkill = branch
		} else if len(engine.catalog.Skills[hold.Skill.ID]) > 0 {
			return battleDisplayPower{Known: true}, nil
		}
	}
	// The original CALL info query runs every role producer. These temporary
	// counts affect RNG even when a different role owns the displayed number.
	for _, role := range roles {
		var temporary battleEffect
		engine.prepareRandomHandRole(role, &temporary)
	}
	return battleDisplayPower{
		Power: engine.cardDisplayPower(source, hold.CardLevel, displaySkill, roles), Marker: marker, Known: true,
	}, nil
}

// refreshBattleDisplayPowers mirrors the comparison work of native
// FUN_0003fa8a/FUN_00041a42. Within each party, finish the hand/hold
// cards before its sphere slots; then visit the next party. Unknown entries are initialized
// silently because their full CARD_UPDATE/SPHR_UPDATE producer owns first
// publication; only a known-value change becomes ResultCmd314/315.
func (engine *BattleEngine) refreshBattleDisplayPowers() ([]BattleResult, error) {
	results := make([]BattleResult, 0, 12)
	for first := 1; first <= engine.playerCount(); first += maxRoomMembers {
		party := engine.playerParty(first)
		for i := range party {
			player := &party[i]
			// 3fa8a gates HP (6f45d) and formal retirement before comparisons.
			if player.HP <= 0 || player.GameOver {
				continue
			}
			// 3fa8a requires 3b32c's registered selection, including a zero-card PASS.
			if _, registered := engine.selectedPlays[player.MemberType]; !registered {
				continue
			}
			present := [33]bool{}
			for _, deckSlot := range player.displayCardSlots() {
				if deckSlot == 0 {
					continue
				}
				card := player.handSlotCard(deckSlot)
				if card.CardType < 1 || card.CardType >= len(player.CardDisplay) {
					return nil, fmt.Errorf("combat member %d card type %d is outside display cache", player.MemberType, card.CardType)
				}
				present[card.CardType] = true
				state, err := engine.cardDisplayState(player, card)
				if err != nil {
					return nil, err
				}
				previous := player.CardDisplay[card.CardType]
				if previous.Power != state.Power || previous.Marker != state.Marker {
					results = append(results, cardUpdate2Result(player.MemberType, card.CardType, state, 0))
				}
				player.CardDisplay[card.CardType] = state
			}
			for cardType := 1; cardType < len(player.CardDisplay); cardType++ {
				if !present[cardType] {
					player.CardDisplay[cardType] = battleDisplayPower{}
				}
			}
			for holdIndex := range player.BlessHolds {
				hold := &player.BlessHolds[holdIndex]
				state, err := engine.holdDisplayState(player, *hold)
				if err != nil {
					return nil, err
				}
				if hold.PowerKnown && (hold.Power != state.Power || hold.Marker != state.Marker) {
					results = append(results, cardUpdate2Result(player.MemberType, hold.CardType, state, hold.AppendIndex))
				}
				hold.Power = state.Power
				hold.Marker = state.Marker
				hold.PowerKnown = true
			}
		}
		for i := range party {
			player := &party[i]
			// 41a42 also leaves a KO member's sphere display cache unchanged.
			if player.HP <= 0 {
				continue
			}
			for sphereIndex := range player.Spheres {
				sphere := &player.Spheres[sphereIndex]
				if sphere.SphereID == 0 {
					sphere.Display = battleDisplayPower{}
					continue
				}
				state, err := engine.sphereDisplayState(player, sphere)
				if err != nil {
					return nil, err
				}
				previous := sphere.Display
				if previous.Known && (previous.Power != state.Power || previous.Marker != state.Marker) {
					results = append(results, sphereUpdate2Result(player.MemberType, sphere.Slot, state.Power, state.Marker))
				}
				sphere.Display = state
			}
		}
	}
	return append(results, engine.publishStatusProjections()...), nil
}

// UserPhase and active Burst both refresh all living members' five darkness
// slots, including empty hand positions.
func (engine *BattleEngine) handDarknessResults() []BattleResult {
	var results []BattleResult
	for _, player := range engine.playerStates() {
		if player.HP <= 0 {
			continue
		}
		mask := playerDarknessMask(player)
		// a6e6c publishes all five hand slots, including empty ones. This
		// prevents a retained slot icon from surviving a multi-card play.
		for slot := 0; slot < len(player.Hand); slot++ {
			results = append(results, BattleResult{Command: 306, Args: []int64{
				int64(player.MemberType), int64(slot + 1), int64(mask >> (4 - slot) & 1)}})
		}
	}
	return results
}
