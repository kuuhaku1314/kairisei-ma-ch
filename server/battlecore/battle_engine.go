package battlecore

import (
	"errors"
	"fmt"
	"strings"
)

func (engine *BattleEngine) Start() ([]BattleResult, error) {
	if engine.phase != PhaseCreated {
		return nil, errors.New("combat start phase is unavailable")
	}
	results := make([]BattleResult, 0, 70)
	if engine.endTurn > 0 {
		results = append(results, BattleResult{Command: CommandLimitTurn, Args: []int64{int64(engine.endTurn)}})
	}

	if engine.pvp {
		// 66bce chooses each user's first registered card's active skill attribute.
		for _, p := range engine.playerStates() {
			skill, _, err := engine.catalog.CardSkill(p.Deck[0].CardID, p.ArthurType)
			if err != nil {
				return nil, err
			}
			attribute := splitCombatAttributes(skill.Attribute)[0]
			p.Attribute, p.BaseAttribute = attribute, attribute
			results = append(results, rewriteResult(p.MemberType, p.Attribute))
		}
	}
	results = append(results, engine.waveRecovery...)
	// Start's 62af0 -> a50c4 also repairs a non-retired zero-HP initial user.
	// Recovery rows precede CARD identities in the transport packet.
	for _, player := range engine.playerStates() {
		if player.MemberType != 0 && !player.GameOver && player.HP <= 0 {
			player.HP = 1
			results = append(results, BattleResult{Command: CommandHP,
				Args: []int64{int64(player.MemberType), int64(player.MaxHP), 1, 0}})
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		if engine.enemies[index].Trance.Reactive != nil {
			results = append(results, resumeEnemyTrance(&engine.enemies[index])...)
		}
	}
	results = append(results, engine.setupIdentityResults()...)
	if engine.openingShufflePending || !engine.awakeTakeover.KeepBuff {
		spherePassiveResults, err := engine.executePlayerSupportPassives()
		if err != nil {
			return nil, err
		}
		results = append(results, spherePassiveResults...)
	}
	for index := 0; index < engine.enemyCount; index++ {
		passiveResults, err := engine.executeEnemyPassiveSkill(&engine.enemies[index])
		if err != nil {
			return nil, err
		}
		results = append(results, passiveResults...)
		if len(passiveResults) != 0 {
			displayResults, err := engine.refreshPassiveDisplayPowers()
			if err != nil {
				return nil, err
			}
			results = append(results, displayResults...)
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 {
			continue
		}
		hp := player.HP
		refreshPlayerBattleParameters(player)
		// Start/getters expose raw current HP even if the supplied tuple
		// exceeds the projected maximum; only HP-mutating consumers clamp it.
		player.HP = hp
		results = append(results,
			playerBaseParameterResult(player),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			BattleResult{Command: CommandBurstGaugeState, Args: []int64{int64(player.MemberType), int64(player.Burst)}},
		)
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		results = append(results,
			BattleResult{Command: CommandBaseParam, Args: baseParameterArgsWithLimit(enemy.MemberType, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, initialParameterLimit(engine.alchemist))},
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)},
		)
	}
	if engine.storyHelp != nil {
		p := &engine.storyHelp.Actor
		results = append(results, playerBaseParameterResult(p), BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(p.MemberType, p.HP, p.MaxHP, p.Attack, p.Magic, p.Recovery, p.Defense, p.MDefense, p.LimitAttack, p.LimitMagic, p.LimitRecovery)})
	}
	// Two internal gimmick members are present in the official solo oracle.
	for memberType := 10; memberType <= 11; memberType++ {
		limit := initialParameterLimit(engine.alchemist)
		results = append(results,
			BattleResult{Command: CommandBaseParam, Args: baseParameterArgsWithLimit(memberType, 1, 0, 0, 0, 0, 0, limit)},
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(memberType, 0, 1, 0, 0, 0, 0, 0, limit, limit, limit)},
		)
	}
	// Start calls 663a4 (all member passives) before 62d8a -> 5db7a
	// shuffles opening decks. Their shared RNG must be consumed in that order.
	if engine.openingShufflePending {
		for _, player := range engine.playerStates() {
			if player.MemberType == 0 {
				continue
			}
			var guaranteed [10]bool
			for slot, card := range player.Deck {
				for _, effect := range player.Effects {
					if effect.Function == "BEGINNING_DRAW" && effect.CardType == card.CardType {
						guaranteed[slot] = true
					}
				}
			}
			engine.shuffleOpeningDeck(player, guaranteed)
		}
		engine.openingShufflePending = false
	}
	engine.phase = PhaseStarted
	engine.waveRecovery = nil
	return results, nil
}

// setupIdentityResults is 1b3c0's common Start/Resume identity tail. CARD
// identities are supplied by the start transport or the resume snapshot.
func (engine *BattleEngine) setupIdentityResults() []BattleResult {
	results := engine.sphereItemResults()
	for _, player := range engine.playerStates() {
		results = append(results, engine.buddyResults(player)...)
	}
	for _, player := range engine.playerStates() {
		if player.MemberType != 0 {
			results = append(results, sphereHandResult(player.MemberType))
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType != 0 && !player.GameOver {
			results = append(results, BattleResult{Command: CommandHoldMax, Args: []int64{int64(player.MemberType), int64(engine.holdMax)}})
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType != 0 {
			results = append(results, engine.buddySlotResult(player))
		}
	}
	return results
}

func (engine *BattleEngine) TurnPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseStarted && engine.phase != PhaseChaliceEnemy && !(engine.pvp && engine.phase == PhaseUserAttack) {
		return nil, errors.New("combat turn phase is unavailable")
	}
	firstTurn := engine.phase == PhaseStarted
	engine.turn++
	engine.resumeSide = 0
	copy(engine.damageHistory[1:], engine.damageHistory[:28])
	engine.damageHistory[0] = engine.turnStats.DamageEvents
	// Charge headers still query the preceding selection's card count. The
	// native UserPhase clears it before accepting the next selection.
	engine.turnStats = battleTurnStats{PlayedByUser: engine.turnStats.PlayedByUser}
	for _, player := range engine.playerStates() {
		player.TurnDamage = 0
		// The dedicated PVP boundary retains these histories between turns.
		if !engine.pvp {
			player.ExecutedBuffKinds = [skillRoleBuffKindCount]uint32{}
			player.TurnSkillStats = battleSkillStatistics{}
		}
		player.AppendExecutions = [2][13]battleAppendExecution{}
		player.AppendExecutionCounts = [2]int{}
		copy(player.HateHistory[1:], player.HateHistory[:10])
		player.HateHistory[0] = 0
	}
	for index := range engine.enemies {
		engine.enemies[index].TurnDamage = 0
		engine.enemies[index].TurnPhysical = 0
		engine.enemies[index].TurnMagic = 0
		engine.enemies[index].AITurn = battleEnemyAITurnStats{}
		engine.enemies[index].Trance.StateChanges = 0 // 53984, before charge queries.
		engine.enemies[index].ExecutedBuffKinds = [skillRoleBuffKindCount]uint32{}
		engine.enemies[index].TurnSkillStats = battleSkillStatistics{}
		engine.enemies[index].ActionConsumed = 0
		engine.enemies[index].ChargedActions = [5]enemyActionCandidate{}
		engine.enemies[index].ChargedActionCount = 0
	}
	results := make([]BattleResult, 0, 25)
	// Standard JP TurnPhase publishes available gauges before transitions;
	// the dedicated PVP boundary publishes actual changes and transitions only.
	for _, player := range engine.playerStates() {
		if !engine.pvp && player.MemberType != 0 && player.HP > 0 && player.BurstState != burstGaugeUnavailable {
			results = append(results, BattleResult{Command: CommandBurstGaugeState,
				Args: []int64{int64(player.MemberType), int64(player.Burst)}})
		}
	}
	burstResults, err := engine.transitionBurstStates()
	if err != nil {
		return nil, err
	}
	results = append(results, burstResults...)
	// 1d8f0 calls a86c0 before the first-attack dispatch at 65680.
	results = append(results, engine.burstPlayConditionResults()...)
	// Original TurnPhase calls 5dd72 before 65680. First-attack skills can
	// inspect hands or KO a member; drawing afterwards would lose that hand.
	engine.prepareTurnDraw()
	if firstTurn {
		firstAttack, err := engine.executeEnemyFirstAttack()
		if err != nil {
			return nil, err
		}
		results = append(results, firstAttack...)
	}
	// 1d8f0 has no unconditional 3fa8a/41a42 sweep. Actual Burst passive
	// executions already refresh after each 7adb0-equivalent action.
	if engine.endType != 0 {
		engine.phase = PhaseEnded
		results = append(results, battleEndResult(engine.endType))
		return engine.appendTerminalBuffCleanup(results)
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 || player.GameOver {
			continue
		}
		results = append(results, BattleResult{Command: CommandHoldStore, Args: []int64{int64(player.MemberType)}})
		for _, hold := range player.CardHolds {
			results = append(results, BattleResult{Command: CommandHoldRemainder, Args: []int64{
				int64(player.MemberType), int64(hold.Action.cardType), int64(hold.Action.sphereSlot), int64(hold.Remaining), 0,
			}})
		}
		// a72e2 projects BLESS before CURSE, preserving order within each kind.
		for _, cardType := range [...]int{22, 21} {
			for _, hold := range player.BlessHolds {
				if hold.CardType == cardType {
					results = append(results, BattleResult{Command: CommandHoldRemainder, Args: []int64{
						int64(player.MemberType), int64(hold.CardType), 0, int64(hold.Remaining), int64(hold.AppendIndex),
					}})
				}
			}
		}
	}
	results = append(results, BattleResult{Command: CommandTurn, Args: []int64{int64(engine.turn), engine.turnDisplayLimit()}})
	if engine.storyHelpTurnEnabled() {
		helperRows, err := engine.executeStoryHelp()
		if err != nil {
			return nil, err
		}
		results = append(results, helperRows...)
		if engine.beginStoryEnd(false) {
			return engine.finishStoryPhase(results, 0)
		}
		if engine.endType != 0 {
			engine.phase = PhaseEnded
			results = append(results, battleEndResult(engine.endType))
			return engine.appendTerminalBuffCleanup(results)
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 {
			continue
		}
		for _, candidate := range engine.buildEnemyChargeStartPlan(enemy) {
			skill, _, matched := engine.selectEnemySkillBranch(enemy, candidate.action.SkillID, 0)
			if !matched {
				// JP 5895f stops this owner's remaining previews on a missing
				// branch; the already scheduled actions still execute later.
				break
			}
			results = append(results, BattleResult{Command: CommandChargeStart, Args: []int64{
				int64(enemy.MemberType), int64(candidate.action.SkillID), int64(skill.FunctionID),
			}})
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 {
			continue
		}
		sign := engine.enemyAttackSign(enemy)
		results = append(results, BattleResult{Command: CommandEnemyAttackSign, Args: []int64{int64(enemy.MemberType), int64(sign)}})
	}
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 || player.GameOver {
			continue
		}
		blocked := engine.playerEffectValue(player, "COST_BLOCK")
		player.Cost = maxInt(0, engine.memberTurnCost(player)-blocked)
		results = append(results,
			BattleResult{Command: CommandCost, Args: []int64{int64(player.MemberType), int64(engine.memberTurnCost(player))}},
			BattleResult{Command: CommandCostBlock, Args: []int64{int64(player.MemberType), int64(blocked)}},
		)
	}
	sphereResults, err := engine.sphereConditionResults()
	if err != nil {
		return nil, err
	}
	results = append(results, sphereResults...)
	for _, player := range engine.playerStates() {
		if !engine.pvp && !player.GameOver && player.HP > 0 {
			results = append(results, BattleResult{Command: CommandPlayLogState, Args: []int64{int64(player.MemberType), int64(engine.elapsedWaveTurns + engine.turn), 0}})
		}
	}
	if engine.pvp && !firstTurn {
		results = append(results, engine.advancePVPAttributes()...)
	}
	engine.phase = PhaseTurn
	// 1d8f0 calls 65010 after hold/cost/selection setup. First-attack KOs
	// must retain those setup rows before their retirement notification.
	// Dedicated PVP leaves KO members registered until an explicit retirement.
	if !engine.pvp {
		results = engine.settlePlayerDeaths(results)
	}
	if engine.endType != 0 {
		engine.phase = PhaseEnded
		results = append(results, battleEndResult(engine.endType))
		return engine.appendTerminalBuffCleanup(results)
	}
	return results, nil
}

// Native FUN_0006edef caps the base at 10; FUN_0006ee0d subtracts
// COST_BLOCK afterwards. FUN_000a6078 sends the base, not the remainder:
// managed HandsData.COST.getEnableCost subtracts block_cost itself.
func (engine *BattleEngine) turnCost() int {
	return maxInt(0, minInt(10, engine.costInitial+maxInt(0, engine.costTurnOffset+engine.turn-1)))
}

func (engine *BattleEngine) UserPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseTurn {
		return nil, errors.New("combat user phase is unavailable")
	}
	results := make([]BattleResult, 0, 180)
	type handCard struct {
		member  int
		slot    int
		card    BattleCard
		skill   CombatSkillDefinition
		display battleDisplayPower
	}
	handCards := make([]handCard, 0, maxRoomMembers*5)
	for _, player := range engine.playerStates() {
		if player.HP <= 0 {
			continue
		}
		player.CardDisplay = [33]battleDisplayPower{}
		for slotIndex, deckSlot := range player.playableCardSlots() {
			if deckSlot == 0 {
				continue
			}
			card := player.handSlotCard(deckSlot)
			skill, _, err := engine.handCardSkill(player, card)
			if err != nil {
				return nil, err
			}
			display, err := engine.cardDisplayStateMode(player, card, false, true)
			if err != nil {
				return nil, err
			}
			handCards = append(handCards, handCard{member: player.MemberType, slot: slotIndex + 1, card: card, skill: skill, display: display})
		}
	}
	for _, item := range handCards {
		results = append(results, fullCardUpdateResult(item.member, item.card.CardType,
			engine.catalog.cardUsesArthurSkill(item.card.CardID, engine.player(item.member).ArthurType), [4]bool{}, item.display, 0))
	}
	// 63cc0 runs 3c596's base display before the two 5fe48 regeneration
	// sweeps, then publishes darkness/draw/selection state.
	results = append(results, engine.regenerateMembers()...)
	if !engine.pvp {
		results = append(results, engine.handDarknessResults()...)
	}
	for playerIndex, player := range engine.playerStates() {
		for _, item := range handCards {
			if item.member == player.MemberType && engine.turnDrawn[playerIndex][item.slot-1] {
				results = append(results, BattleResult{Command: CommandCardDeal, Args: []int64{int64(item.member), int64(item.card.CardType), int64(item.card.Level)}})
			}
		}
		// 5de62 always publishes the alive member's deck remainder, even
		// when a retained full hand needed no new card after Start.
		if player.HP > 0 {
			results = append(results, BattleResult{Command: CommandCardDeck, Args: []int64{int64(player.MemberType), int64(player.remainingDeckCount()), 0}})
		}
		for _, item := range handCards {
			// 5de62 checks SkillData.kind (+8), independently of its roles.
			if item.member != player.MemberType || player.SummonSource != 0 || (item.skill.Kind != "ATTACK" && item.skill.Kind != "SORCERY") {
				continue
			}
			weakness := []int64{int64(item.member), int64(item.card.CardType)}
			// FUN_0005de62 reads SkillData.attribute (+0xc) for the hand
			// weakness preview. The ATTACK_AA role attribute controls damage
			// resolution, but can legitimately differ from this UI identity.
			attribute := item.skill.Attribute
			for enemyIndex := 0; enemyIndex < maxRoomMembers; enemyIndex++ {
				rate := 0
				if enemyIndex < engine.enemyCount {
					rate = enemyAttributeRate(&engine.enemies[enemyIndex], attribute)
				}
				weakness = append(weakness, int64(rate))
			}
			results = append(results, BattleResult{Command: CommandCardWeak, Args: weakness})
		}
	}
	for _, item := range handCards {
		results = append(results, engine.playerCardCostResult(engine.player(item.member), item.card.CardType, item.skill.Cost))
	}
	for _, item := range handCards {
		if engine.player(item.member).SummonSource == 0 {
			results = append(results, playerCardStateResult(engine.player(item.member), item.card.CardType))
		}
	}
	// Retained CARD notifications precede the sphere count/cost projection.
	results = append(results, engine.cardModifierResults()...)
	sphereResults, err := engine.sphereCountCostResults()
	if err != nil {
		return nil, err
	}
	results = append(results, sphereResults...)
	engine.turnStats.PlayedByUser = [8]int{}
	engine.selectedPlays = make(map[int]cardPlaySubmission, maxRoomMembers)
	engine.selectionOrder = [8]int{}
	// 63cc0 clears the selection table at UserPhase, not TurnPhase. Charge
	// conditions evaluated before that point can still read last turn's cards.
	engine.turnActions = nil
	results = append(results, engine.summonConditionResults()...)
	displayResults, err := engine.selectionDisplayResults(0)
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	covering := make(map[int]bool)
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP > 0 {
			for _, effect := range enemy.Effects {
				if effect.Function == "COVERING" && effect.ListType >= 0 && effect.ListType <= 6 {
					covering[enemy.MemberType] = true
				}
			}
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		enabled := int64(0)
		if enemy.HP > 0 && (len(covering) == 0 || covering[enemy.MemberType]) {
			enabled = 1
		}
		results = append(results, BattleResult{Command: CommandEnableTarget, Args: []int64{int64(enemy.MemberType), enabled}})
	}
	engine.phase = PhaseUser
	for i, player := range engine.playerStates() {
		engine.npcSimulatedHP[i], engine.npcSimulatedMaxHP[i] = player.HP, player.MaxHP
	}
	return results, nil
}

func (engine *BattleEngine) Submit(memberType int, submission cardPlaySubmission) ([]BattleResult, error) {
	if err := CheckSummonSelectionInput(submission.SummonsCardMask, engine.alchemist); err != nil {
		return nil, err
	}
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseUser {
		return nil, errors.New("combat card submission phase is unavailable")
	}
	if !engine.playerMember(memberType) {
		return nil, errors.New("combat card submission member is invalid")
	}
	if _, duplicate := engine.selectedPlays[memberType]; duplicate {
		return nil, errors.New("combat card submission is duplicated")
	}
	player := engine.player(memberType)
	if player.SummonSource != 0 && !submission.Automatic && submission.ActionCount() != 0 {
		return nil, errors.New("summoned action cards require CPU selection")
	}
	if err := engine.validateSummonSelection(memberType, submission); err != nil {
		return nil, err
	}
	if player.HP <= 0 {
		if submission.ActionCount() != 0 {
			return nil, errors.New("combat KO member cannot select cards")
		}
		return nil, nil
	}
	remainingCost := player.Cost
	results := make([]BattleResult, 0, 6)
	selectedCards := make(map[int]struct{}, len(submission.CardTypes))
	sphereAction, err := engine.validateSphereSubmission(memberType, submission)
	if err != nil {
		return nil, err
	}
	for index, cardType := range submission.CardTypes {
		if cardType == 0 {
			continue
		}
		if _, duplicate := selectedCards[cardType]; duplicate {
			return nil, errors.New("combat card submission repeats a hand card")
		}
		selectedCards[cardType] = struct{}{}
		card, found := player.cardInHand(cardType)
		if !found {
			return nil, fmt.Errorf("member %d card type %d is not in hand", memberType, cardType)
		}
		// The original PvE UserCardPlay accepts ordinary cards on a player
		// carrying STAN; enemy action interruption is a separate consumer.
		if _, sealed := playerCardSealEffect(player, cardType); sealed {
			return nil, fmt.Errorf("member %d cannot play sealed cards", memberType)
		}
		skill, _, err := engine.handCardSkill(player, card)
		if err != nil {
			return nil, err
		}
		cost := engine.effectiveCardCost(player, cardType, skill.Cost)
		if cost > remainingCost && player.SummonSource == 0 {
			return nil, fmt.Errorf("member %d card cost exceeds remaining cost", memberType)
		}
		// Original CPU summon choices retain target0: the source skill is
		// not executed. UserCardPlay resolves single-target zero aliases.
		automaticSummon := submission.Automatic && submission.SummonsCardMask&(1<<index) != 0
		if !automaticSummon {
			if submission.Targets[index] == 0 {
				switch skill.Target {
				case "SELF":
					submission.Targets[index] = memberType
				case "USER_ONE", "ENEMY_ONE":
					submission.Targets[index], _, _ = engine.randomLivingTarget(memberType, skill.Target == "ENEMY_ONE")
				}
			}
			if err := engine.validateTarget(skill.Target, memberType, submission.Targets[index]); err != nil {
				return nil, err
			}
		}
		if player.SummonSource == 0 {
			remainingCost -= cost
		}
		state := playerCardStateResult(player, cardType).Args[2]
		args := []int64{int64(memberType), int64(cardType), int64(submission.Targets[index]), int64(engine.playerCardDarkness(memberType, cardType)), state}
		args = append(args, boolInt64(submission.SummonsCardMask&(1<<index) != 0))
		results = append(results, BattleResult{Command: CommandCardPlay, Args: args})
	}
	if sphereAction.sphereSlot != 0 {
		position := len(results)
		if submission.SpherePosition != 0 {
			position = submission.SpherePosition - 1
			if position < 0 || position > len(results) {
				return nil, errors.New("combat sphere result position is invalid")
			}
		}
		row := BattleResult{Command: CommandSpherePlay, Args: []int64{
			int64(memberType), int64(sphereAction.sphereSlot), int64(sphereAction.target),
		}}
		results = append(results, BattleResult{})
		copy(results[position+1:], results[position:])
		results[position] = row
	}
	if len(results) == 0 {
		// Native FUN_00040908 emits CARD_PASS when one member commits no card
		// and no sphere. The result belongs to ApiCardPlayR, not ApiUserAttack.
		results = append(results, BattleResult{Command: CommandCardPass, Args: []int64{int64(memberType)}})
	}
	// Full preview can fail on an unavailable referenced skill. Keep the
	// accepted selection, cost and display cache atomic in that case too.
	preview := engine.selectionView()
	preview.player(memberType).Cost = remainingCost
	preview.selectedPlays[memberType] = submission
	preview.rememberSelection(memberType)
	preview.turnActions, err = preview.selectedBattleActions()
	if err != nil {
		return nil, err
	}
	displayResults, err := preview.selectionDisplayResults(memberType)
	if err != nil {
		return nil, err
	}
	engine.commitSelections(&preview)
	results = append(results, displayResults...)
	return results, nil
}

func (engine *BattleEngine) battleMemberHP(memberType int) (int, int, bool) {
	if member := engine.parameterState(memberType); member != nil {
		return member.HP, member.MaxHP, true
	}
	if memberType >= 5 && memberType < 5+engine.enemyCount {
		member := &engine.enemies[memberType-5]
		return member.HP, member.MaxHP, true
	}
	return 0, 0, false
}

func (engine *BattleEngine) battleMemberEffects(memberType int) []battleEffect {
	if member := engine.parameterState(memberType); member != nil {
		return member.Effects
	}
	if memberType >= 5 && memberType < 5+engine.enemyCount {
		return engine.enemies[memberType-5].Effects
	}
	return nil
}

func combatEnemyMemberType(target string) int {
	target = strings.ToUpper(strings.TrimSpace(target))
	if !strings.HasPrefix(target, "ENEMY") {
		return 0
	}
	index := combatParameterInt(strings.TrimPrefix(target, "ENEMY"))
	if index < 1 || index > 4 {
		return 0
	}
	return index + 4
}

// combatRolesDefined distinguishes a valid no-effect role from an incomplete
// skill. CN native roles 45 (NONE) and 51 (OUTPUT_TEXT) return success with
// zero effect output; the outer enemy skill still consumes its action and
// projects ResultCmd50. An empty function, in contrast, is not a usable role
// definition.
func combatRolesDefined(roles []CombatSkillRole) bool {
	for _, role := range roles {
		if strings.TrimSpace(role.Function) != "" {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) EndType() int { return engine.endType }

func (engine *BattleEngine) appendNewPlayerGameOver(results []BattleResult) []BattleResult {
	// 65010/61d60 finish a winning/awake wave before any continue/gameover
	// exchange. A KO in that same action remains eligible for a50c4's 1 HP
	// recovery. The local no-continue policy must not retire it prematurely.
	if engine.endType == 1 || engine.endType == 4 {
		return results
	}
	for index := range engine.players {
		player := &engine.players[index]
		if player.MemberType == 0 || player.HP > 0 || player.GameOver {
			continue
		}
		player.GameOver = true
		// 228c0 -> 425cc clears the pending holds/selection. It does not
		// restore HP or clear the retirement flag at the next wave.
		player.BlessHolds, player.ReservedChalice = nil, 0
		player.CardHolds = nil
		delete(engine.selectedPlays, player.MemberType)
		results = append(results, battleGameOverResult(player.MemberType))
	}
	return results
}

// Parts do not keep a defeated body alive. Independent bodies each have to
// fall before ordinary victory, regardless of which member occupies slot5.
func (engine *BattleEngine) allEnemyBodiesDead() bool {
	for i := 0; i < engine.enemyCount; i++ {
		if engine.enemies[i].Parent == 0 && engine.enemies[i].HP > 0 {
			return false
		}
	}
	return engine.enemyCount > 0
}

func (engine *BattleEngine) allPlayersDead() bool {
	for _, player := range engine.playerStates() {
		if player.HP > 0 {
			return false
		}
	}
	return true
}

func (engine *BattleEngine) removeCardFromHand(memberType int, cardType int) {
	player := engine.player(memberType)
	for index, deckSlot := range player.Hand {
		if deckSlot != 0 && player.handSlotCard(deckSlot).CardType == cardType {
			player.Hand[index] = 0
			for _, hold := range player.CardHolds {
				if hold.Action.cardType == cardType {
					return
				}
			}
			if cardType > 0 && cardType < len(player.CardDisplay) {
				player.CardDisplay[cardType] = battleDisplayPower{}
			}
			player.clearCardModifiers(cardType)
			if isTokenCardType(cardType) {
				player.Tokens[cardType-tokenCardFirst] = battleToken{}
			} else {
				player.discardCard(deckSlot - 1)
			}
			return
		}
	}
}

// 45726 removes CARD-list modifiers when a card leaves the hand or hold.
// A later draw of the same physical card must not inherit them.
func (player *battlePlayer) clearCardModifiers(cardType int) {
	if cardType > 0 && cardType < len(player.CardCostDown) {
		player.CardCostDown[cardType] = 0
		player.CardCostIncreases[cardType] = nil
		player.CardBurstSkills[cardType] = nil
	}
}

func playerCardTrapEffect(player *battlePlayer, cardType int) (battleEffect, bool) {
	for _, effect := range player.Effects {
		if effect.Function == "CARD_TRAP_DAMAGE" && effect.CardType == cardType && effect.Remaining > 0 {
			return effect, true
		}
	}
	return battleEffect{}, false
}

func playerCardSealEffect(player *battlePlayer, cardType int) (battleEffect, bool) {
	for _, effect := range player.Effects {
		if effect.Function == "CARD_SEAL" && effect.CardType == cardType && effect.Remaining > 0 {
			return effect, true
		}
	}
	return battleEffect{}, false
}

func removePlayerCardEffect(player *battlePlayer, function string, cardType int) (removed []battleEffect) {
	kept := player.Effects[:0]
	for _, effect := range player.Effects {
		if effect.Function == function && effect.CardType == cardType && effect.Remaining > 0 {
			removed = append(removed, effect)
			continue
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
	return removed
}

func removePlayerCardBoundEffects(player *battlePlayer, cardType int) (buffs []battleEffect, debuffs []battleEffect) {
	kept := player.Effects[:0]
	for _, effect := range player.Effects {
		if effect.CardType == cardType && effect.Remaining > 0 {
			switch effect.Kind {
			case 1:
				buffs = append(buffs, effect)
				continue
			case 2:
				debuffs = append(debuffs, effect)
				continue
			}
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
	return buffs, debuffs
}

func (engine *BattleEngine) triggerCardTrap(memberType int, cardType int) []BattleResult {
	player := engine.player(memberType)
	// FUN_0005f044 filters dead members before it looks up/removes the selected
	// card's trap. A dead holder therefore retains an unconsumed trap entry.
	if player.HP <= 0 {
		return nil
	}
	kept := player.Effects[:0]
	var trap *battleEffect
	for index := range player.Effects {
		effect := player.Effects[index]
		if trap == nil && effect.Function == "CARD_TRAP_DAMAGE" && effect.CardType == cardType && effect.Remaining > 0 {
			copy := effect
			trap = &copy
			continue
		}
		kept = append(kept, effect)
	}
	player.Effects = kept
	if trap == nil {
		return nil
	}
	// 5f044 caps the request at current HP minus one before 73e9f. A
	// card trap is nonlethal, independently of ENDURE or GUTS effects.
	damage := maxInt(0, minInt(trap.Value, player.HP-1))
	player.HP = engine.playerHPAfterDamage(player, damage)
	recordPlayerDamage(player, damage)
	results := []BattleResult{
		buffStatusEffectResult(memberType, battleBuffCodes["CARD_TRAP_DAMAGE"]),
		// 5f044 keeps the capped request even when ENDURE raises committed HP.
		{Command: 60, Args: []int64{int64(memberType), 0, -int64(damage), int64(player.HP), 0, 0, 100, 0, 0, 0}},
	}
	if trap.Source >= 5 && trap.Source < 5+engine.enemyCount {
		results = append(engine.burstDamageGaugeResults(player, damage), results...)
	}
	// 5f044 -> 449a0 removes this card's trap. The shared 72a18 -> 47190
	// release path keeps the grouped status while other traps remain, but
	// still emits this entry's 71 before the parameter snapshot.
	ensurePlayerBaseParameters(player)
	refreshPlayerBattleParameters(player)
	refreshPlayerAttribute(player)
	results = appendNaturalExpiryRows(results, memberType, []battleEffect{*trap}, player.Effects, player.Attribute)
	results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
		player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
		player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
	)})
	var guts []BattleResult
	guts = resolvePlayerGuts(player)
	return append(results, guts...)
}

// triggerPlayedCardTraps mirrors FUN_0005f044 after the complete ordered card
// action list. Native walks members and their selected hand slots, so use the
// unsorted turnActions snapshot rather than skill-priority execution order.
func (engine *BattleEngine) triggerPlayedCardTraps() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers*5)
	for _, action := range engine.turnActions {
		if action.cardType == 0 {
			continue
		}
		results = append(results, engine.triggerCardTrap(action.memberType, action.cardType)...)
	}
	return results
}

func (engine *BattleEngine) playerTargets(kind string, actor int, selected int) []int {
	if engine.pvp {
		switch kind {
		case "ENEMY_ONE", "ENEMY_ONE_INVOLVE_DEAD":
			if engine.playerMember(selected) && !engine.samePlayerParty(actor, selected) {
				return []int{selected}
			}
			return nil
		case "ENEMY_ALL", "DEAD_ENEMY_ALL":
			var out []int
			for _, p := range engine.opposingParty(actor) {
				if p.HP > 0 || kind == "DEAD_ENEMY_ALL" {
					out = append(out, p.MemberType)
				}
			}
			return out
		}
	}

	if targets, ok := engine.userSideRoleTargets(kind); ok {
		return targets
	}
	switch kind {
	case "SELF":
		return []int{actor}
	case "USER_ONE", "SELECT":
		if selected >= 1 && selected <= 4 {
			return []int{selected}
		}
	case "USER_ALL", "FRIEND_ALL":
		result := make([]int, 0, maxRoomMembers)
		for _, p := range engine.playerParty(actor) {
			if p.HP > 0 {
				result = append(result, p.MemberType)
			}
		}
		return result
	case "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		if memberType := engine.memberForJob(kind); memberType != 0 && engine.player(memberType).HP > 0 {
			return []int{memberType}
		}
	}
	return nil
}

func (engine *BattleEngine) enemyTargets(kind string, selected int) []int {
	if targets, ok := engine.fixedEnemyRoleTargets(kind); ok {
		return targets
	}
	if kind == "ENEMY_ALL" {
		result := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP > 0 {
				result = append(result, index)
			}
		}
		return result
	}
	if selected == 0 {
		selected = 5
	}
	if selected >= 5 && selected < 5+engine.enemyCount && engine.enemies[selected-5].HP > 0 {
		return []int{selected - 5}
	}
	return nil
}
