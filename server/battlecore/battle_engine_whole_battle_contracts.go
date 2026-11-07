package battlecore

import (
	"errors"
	"fmt"
)

// validateWholeBattleContract keeps one official party and one official card
// deck in the same engine instance from setup through a complete enemy turn
// and a player victory. The focused producer contracts deliberately isolate
// arithmetic and ResultCmd families; this gate catches phase ownership,
// retained hand state and terminal ordering errors between those families.
func validateWholeBattleContract(catalog *CombatCatalog) error {
	const (
		partyID    = 10000101
		attackCard = 10165084
	)
	party, exists := catalog.EnemyParties[partyID]
	if !exists || party.Slots[0].EnemyID != 10000101 || party.Slots[1].EnemyID != 0 {
		return fmt.Errorf("official whole-battle party %d changed: %+v", partyID, party)
	}
	card, exists := catalog.Cards[attackCard]
	if !exists || card.MaxLevel != 80 {
		return fmt.Errorf("official whole-battle card %d changed: %+v", attackCard, card)
	}
	deck := make([]BattleCard, 10)
	for index := range deck {
		deck[index] = BattleCard{CardType: index + 1, CardID: attackCard, Level: card.MaxLevel}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         100000,
			Attack:     100000,
			Magic:      100000,
			Mind:       100000,
			DeckCards:  append([]BattleCard(nil), deck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  3,
		HoldMax:      5,
	}, members)
	if err != nil {
		return fmt.Errorf("create official whole-battle engine: %w", err)
	}
	start, err := engine.Start()
	if err != nil {
		return fmt.Errorf("start official whole-battle engine: %w", err)
	}
	if engine.phase != PhaseStarted || !battleResultsContainCommand(start, CommandBaseParam) ||
		!battleResultsContainCommand(start, CommandBattleParam) {
		return fmt.Errorf("official whole-battle start phase/results are phase=%d results=%+v", engine.phase, start)
	}
	turnOne, err := nextTurnForBattleContract(engine)
	if err != nil {
		return fmt.Errorf("start official whole-battle turn one: %w", err)
	}
	if engine.turn != 1 || engine.phase != PhaseTurn || !battleResultsContainCommand(turnOne, CommandTurn) {
		return fmt.Errorf("official whole-battle turn one is turn=%d phase=%d results=%+v", engine.turn, engine.phase, turnOne)
	}
	userOne, err := engine.UserPhase()
	if err != nil {
		return fmt.Errorf("start official whole-battle user phase one: %w", err)
	}
	if engine.phase != PhaseUser || !battleResultsContainCommand(userOne, CommandCardDeal) ||
		!battleResultsContainCommand(userOne, CommandEnableTarget) {
		return fmt.Errorf("official whole-battle user phase one is phase=%d results=%+v", engine.phase, userOne)
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		if _, err := engine.Submit(memberType, cardPlaySubmission{}); err != nil {
			return fmt.Errorf("submit official whole-battle turn-one pass for member %d: %w", memberType, err)
		}
	}
	passed, err := engine.UserAttack()
	if err != nil {
		return fmt.Errorf("execute official whole-battle turn-one pass: %w", err)
	}
	attackComplete := false
	for _, result := range passed {
		if result.Command == CommandTurn && equalBattleArgs(result.Args, []int64{1, 1}) {
			attackComplete = true
			break
		}
	}
	if engine.endType != 0 || engine.phase != PhaseUserAttack ||
		!battleResultsContainCommand(passed, CommandAttackPartition) || !attackComplete {
		return fmt.Errorf("official whole-battle pass is end=%d phase=%d results=%+v", engine.endType, engine.phase, passed)
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return fmt.Errorf("execute official whole-battle empty user chalice phase: %w", err)
	}
	enemyResults, err := engine.EnemyPhase()
	if err != nil {
		return fmt.Errorf("execute official whole-battle enemy phase: %w", err)
	}
	if engine.endType != 0 || engine.phase != PhaseEnemy ||
		!battleResultsContainCommand(enemyResults, CommandSkill) {
		return fmt.Errorf("official whole-battle enemy phase is end=%d phase=%d results=%+v", engine.endType, engine.phase, enemyResults)
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		return fmt.Errorf("execute official whole-battle empty enemy chalice phase: %w", err)
	}
	turnTwo, err := nextTurnForBattleContract(engine)
	if err != nil {
		return fmt.Errorf("start official whole-battle turn two: %w", err)
	}
	if engine.turn != 2 || engine.phase != PhaseTurn || !battleResultsContainCommand(turnTwo, CommandTurn) {
		return fmt.Errorf("official whole-battle turn two is turn=%d phase=%d results=%+v", engine.turn, engine.phase, turnTwo)
	}
	if _, err := engine.UserPhase(); err != nil {
		return fmt.Errorf("start official whole-battle user phase two: %w", err)
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		cardType := 0
		for _, deckSlot := range player.Hand {
			if deckSlot != 0 {
				cardType = player.Deck[deckSlot-1].CardType
				break
			}
		}
		if cardType == 0 {
			return fmt.Errorf("official whole-battle member %d has no turn-two card", memberType)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0] = cardType
		submission.Targets[0] = 5
		if _, err := engine.Submit(memberType, submission); err != nil {
			return fmt.Errorf("submit official whole-battle turn-two attack for member %d: %w", memberType, err)
		}
	}
	victory, err := engine.UserAttack()
	if err != nil {
		return fmt.Errorf("execute official whole-battle victory: %w", err)
	}
	if engine.endType != 1 || engine.phase != PhaseEnded || engine.enemies[0].HP != 0 ||
		!battleResultsContainCommand(victory, CommandCardSkill) || battleResultsContainCommand(victory, CommandAttackPartition) ||
		!battleResultsContainCommand(victory, CommandEnd) {
		return fmt.Errorf("official whole-battle victory is end=%d phase=%d enemy=%+v results=%+v", engine.endType, engine.phase, engine.enemies[0], victory)
	}
	cardIndex, endIndex := -1, -1
	for index, result := range victory {
		switch result.Command {
		case CommandCardSkill:
			cardIndex = index
		case CommandEnd:
			endIndex = index
		}
	}
	// Original user_attack skips ATTACK_PARTITION when the card action
	// already ended the wave; END follows that final action directly.
	if cardIndex < 0 || endIndex < 0 || cardIndex >= endIndex {
		return fmt.Errorf("official whole-battle terminal ordering is card=%d end=%d results=%+v", cardIndex, endIndex, victory)
	}
	for index := range engine.players {
		if engine.players[index].HP <= 0 {
			return fmt.Errorf("official whole-battle member %d died: %+v", index+1, engine.players[index])
		}
	}
	if err := validateMultiPartWholeBattleContract(catalog); err != nil {
		return err
	}
	if err := validatePostEnemyPhaseDOTTerminalContract(catalog); err != nil {
		return err
	}
	if err := validateRetainedDrawWholeBattleContract(catalog); err != nil {
		return err
	}
	if err := validateRetainedAttackWholeBattleContract(catalog); err != nil {
		return err
	}
	if err := validateRetainedDefenseWholeBattleContract(catalog); err != nil {
		return err
	}
	if err := validateResumeWholeBattleContract(catalog); err != nil {
		return err
	}
	return nil
}

// This party is an official one-body/three-part object graph. Keeping the
// part break, enemy response and later body kill in one engine instance makes
// target member types and parent damage part of the phase contract instead of
// validating them only through isolated arithmetic helpers.
func validateMultiPartWholeBattleContract(catalog *CombatCatalog) error {
	const (
		partyID    = 40084001
		attackCard = 10154008
	)
	party, exists := catalog.EnemyParties[partyID]
	if !exists || party.Slots[0].EnemyID != 40008411 || party.Slots[0].ParentIndex != 0 ||
		party.Slots[1].EnemyID != 40008412 || party.Slots[1].ParentIndex != 1 ||
		party.Slots[2].EnemyID != 40008413 || party.Slots[2].ParentIndex != 1 ||
		party.Slots[3].EnemyID != 40008414 || party.Slots[3].ParentIndex != 1 {
		return fmt.Errorf("official multi-part party %d changed: %+v", partyID, party)
	}
	card, exists := catalog.Cards[attackCard]
	if !exists || card.MaxLevel != 80 {
		return fmt.Errorf("official multi-part card %d changed: %+v", attackCard, card)
	}
	deck := make([]BattleCard, 10)
	for index := range deck {
		deck[index] = BattleCard{CardType: index + 1, CardID: attackCard, Level: card.MaxLevel}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         1000000,
			Attack:     20000,
			Magic:      20000,
			Mind:       20000,
			DeckCards:  append([]BattleCard(nil), deck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  3,
		HoldMax:      5,
	}, members)
	if err != nil {
		return fmt.Errorf("create official multi-part engine: %w", err)
	}
	if _, err := engine.Start(); err != nil {
		return fmt.Errorf("start official multi-part engine: %w", err)
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return fmt.Errorf("start official multi-part turn one: %w", err)
	}
	if _, err := engine.UserPhase(); err != nil {
		return fmt.Errorf("start official multi-part user phase one: %w", err)
	}
	partCardType := firstBattleCardType(&engine.players[0])
	if partCardType == 0 {
		return errors.New("official multi-part member one has no turn-one card")
	}
	partPlay := cardPlaySubmission{}
	partPlay.CardTypes[0] = partCardType
	partPlay.Targets[0] = 6
	if _, err := engine.Submit(1, partPlay); err != nil {
		return fmt.Errorf("submit official multi-part part attack: %w", err)
	}
	for memberType := 2; memberType <= maxRoomMembers; memberType++ {
		if _, err := engine.Submit(memberType, cardPlaySubmission{}); err != nil {
			return fmt.Errorf("submit official multi-part turn-one pass for member %d: %w", memberType, err)
		}
	}
	partResults, err := engine.UserAttack()
	if err != nil {
		return fmt.Errorf("execute official multi-part part attack: %w", err)
	}
	if engine.endType != 0 || engine.enemies[0].HP <= 0 || engine.enemies[0].HP >= engine.enemies[0].MaxHP ||
		engine.enemies[1].HP != 0 || !engine.enemies[1].Broken || engine.enemies[2].HP <= 0 || engine.enemies[3].HP <= 0 ||
		!battleResultsContainCommand(partResults, CommandPartsBreak) || !battleResultsContainCommand(partResults, CommandAttackPartition) ||
		battleResultsContainCommand(partResults, CommandEnd) {
		return fmt.Errorf("official multi-part part break is end=%d enemies=%+v results=%+v", engine.endType, engine.enemies, partResults)
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return fmt.Errorf("execute official multi-part user chalice phase: %w", err)
	}
	if _, err := engine.EnemyPhase(); err != nil {
		return fmt.Errorf("execute official multi-part enemy phase: %w", err)
	}
	if engine.endType != 0 {
		return fmt.Errorf("official multi-part enemy phase ended battle: end=%d players=%+v", engine.endType, engine.players)
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		return fmt.Errorf("execute official multi-part enemy chalice phase: %w", err)
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return fmt.Errorf("start official multi-part turn two: %w", err)
	}
	if engine.endType != 0 {
		return fmt.Errorf("official multi-part turn boundary ended battle: end=%d players=%+v enemies=%+v", engine.endType, engine.players, engine.enemies)
	}
	if _, err := engine.UserPhase(); err != nil {
		return fmt.Errorf("start official multi-part user phase two: %w", err)
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		cardType := firstBattleCardType(&engine.players[memberType-1])
		if cardType == 0 {
			return fmt.Errorf("official multi-part member %d has no turn-two card", memberType)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0] = cardType
		submission.Targets[0] = 5
		if _, err := engine.Submit(memberType, submission); err != nil {
			return fmt.Errorf("submit official multi-part body attack for member %d: %w", memberType, err)
		}
	}
	victory, err := engine.UserAttack()
	if err != nil {
		return fmt.Errorf("execute official multi-part body victory: %w", err)
	}
	if engine.endType != 1 || engine.phase != PhaseEnded || engine.enemies[0].HP != 0 ||
		!battleResultsContainCommand(victory, CommandEnemyBreak) || battleResultsContainCommand(victory, CommandAttackPartition) ||
		!battleResultsContainCommand(victory, CommandEnd) {
		return fmt.Errorf("official multi-part body victory is end=%d phase=%d enemies=%+v results=%+v", engine.endType, engine.phase, engine.enemies, victory)
	}
	return nil
}

func firstBattleCardType(player *battlePlayer) int {
	if player == nil {
		return 0
	}
	for _, deckSlot := range player.Hand {
		if deckSlot != 0 {
			return player.Deck[deckSlot-1].CardType
		}
	}
	return 0
}

// FUN_000615b0 resolves player-side retained DOT after the complete enemy
// phase, not at the next TurnPhase. A lethal tick must settle that same phase
// and prevent the room layer from requesting another turn.
func validatePostEnemyPhaseDOTTerminalContract(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, turn: 1, phase: PhaseUserAttack, enemyCount: 1}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 1000, MaxHP: 1000}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1,
			HP:         100,
			MaxHP:      100,
			Effects: []battleEffect{{
				Function: "BURN", Value: 100, Kind: 2, Remaining: 1,
				AppliedTurn: 1, Source: 5,
			}},
		}
	}
	results, err := engine.EnemyPhase()
	if err != nil {
		return fmt.Errorf("execute post-enemy-phase DOT terminal contract: %w", err)
	}
	gameOvers := 0
	for _, result := range results {
		if result.Command == CommandGameOver {
			gameOvers++
		}
	}
	if engine.EndType() != 2 || engine.phase != PhaseEnded || gameOvers != maxRoomMembers ||
		!battleResultsContainCommand(results, CommandEnd) {
		return fmt.Errorf("post-enemy-phase DOT terminal state is end=%d phase=%d gameovers=%d results=%+v", engine.EndType(), engine.phase, gameOvers, results)
	}
	return nil
}

// Official card 10131011 owns DEAL_BONUS in both its SELF normal skill and
// USER_ALL Arthur skill. Keeping two played cards per member, the complete
// enemy phase and the following UserPhase in one engine instance proves that
// the displayed draw value is backed by retained state and an actual second
// CARD_DEAL, rather than a projection-only "+1" row.
func validateRetainedDrawWholeBattleContract(catalog *CombatCatalog) error {
	const (
		partyID     = 10000101
		supportCard = 10131011
	)
	card, exists := catalog.Cards[supportCard]
	if !exists || card.MaxLevel != 60 || card.NormalSkillID != 12502221 || card.ArthurSkillID != 12502222 {
		return fmt.Errorf("official retained-draw card %d changed: %+v", supportCard, card)
	}
	arthurSkill, arthurRoles, err := catalog.CardSkill(supportCard, 2)
	if err != nil {
		return fmt.Errorf("load official retained-draw Arthur skill: %w", err)
	}
	drawRoleFound := false
	for _, role := range arthurRoles {
		if role.Function == "DEAL_BONUS" && role.Target == "SELECT" && role.RoleIndex == 1 && role.Parameters[0] == "1" {
			drawRoleFound = true
			break
		}
	}
	if arthurSkill.Target != "USER_ALL" || arthurSkill.Cost != 2 || !drawRoleFound {
		return fmt.Errorf("official retained-draw Arthur contract changed: skill=%+v roles=%+v", arthurSkill, arthurRoles)
	}

	deck := make([]BattleCard, 10)
	for index := range deck {
		deck[index] = BattleCard{CardType: index + 1, CardID: supportCard, Level: card.MaxLevel}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         1000000,
			Attack:     1000,
			Magic:      1000,
			Mind:       1000,
			DeckCards:  append([]BattleCard(nil), deck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  4,
		HoldMax:      5,
	}, members)
	if err != nil {
		return fmt.Errorf("create official retained-draw engine: %w", err)
	}
	if _, err := engine.Start(); err != nil {
		return fmt.Errorf("start official retained-draw engine: %w", err)
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return fmt.Errorf("start official retained-draw turn one: %w", err)
	}
	if _, err := engine.UserPhase(); err != nil {
		return fmt.Errorf("start official retained-draw user phase one: %w", err)
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		cardTypes := make([]int, 0, 2)
		for _, deckSlot := range player.Hand {
			if deckSlot == 0 {
				continue
			}
			cardTypes = append(cardTypes, player.Deck[deckSlot-1].CardType)
			if len(cardTypes) == 2 {
				break
			}
		}
		if len(cardTypes) != 2 {
			return fmt.Errorf("official retained-draw member %d has only %d playable cards", memberType, len(cardTypes))
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0], submission.CardTypes[1] = cardTypes[0], cardTypes[1]
		if memberType == 2 {
			submission.Targets[0], submission.Targets[1] = 0, 0
		} else {
			submission.Targets[0], submission.Targets[1] = memberType, memberType
		}
		if _, err := engine.Submit(memberType, submission); err != nil {
			return fmt.Errorf("submit official retained-draw cards for member %d: %w", memberType, err)
		}
	}
	played, err := engine.UserAttack()
	if err != nil {
		return fmt.Errorf("execute official retained-draw cards: %w", err)
	}
	drawBuffRows := 0
	for _, result := range played {
		if result.Command == CommandBuff && len(result.Args) >= 4 && result.Args[3] == int64(battleBuffCodes["DEAL_BONUS"]) {
			drawBuffRows++
		}
	}
	if drawBuffRows != maxRoomMembers {
		return fmt.Errorf("official retained-draw application projected %d draw rows, want %d: %+v", drawBuffRows, maxRoomMembers, played)
	}
	for index := range engine.players {
		player := &engine.players[index]
		if playerHandCount(player) != 3 || playerDrawEffectValue(player, battleBuffCodes["DEAL_BONUS"]) != 1 {
			return fmt.Errorf("official retained-draw post-play member %d state is hand=%d effects=%+v", index+1, playerHandCount(player), player.Effects)
		}
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return fmt.Errorf("execute official retained-draw user chalice phase: %w", err)
	}
	if _, err := engine.EnemyPhase(); err != nil {
		return fmt.Errorf("execute official retained-draw enemy phase: %w", err)
	}
	if engine.endType != 0 {
		return fmt.Errorf("official retained-draw enemy phase ended battle: end=%d players=%+v", engine.endType, engine.players)
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		return fmt.Errorf("execute official retained-draw enemy chalice phase: %w", err)
	}
	turnTwo, err := nextTurnForBattleContract(engine)
	if err != nil {
		return fmt.Errorf("start official retained-draw turn two: %w", err)
	}
	for _, result := range turnTwo {
		if result.Command == CommandBuffRelease && len(result.Args) >= 3 && result.Args[2] == int64(battleBuffCodes["DEAL_BONUS"]) {
			return fmt.Errorf("official retained-draw state released before its consumer: %+v", turnTwo)
		}
	}
	userTwo, err := engine.UserPhase()
	if err != nil {
		return fmt.Errorf("start official retained-draw user phase two: %w", err)
	}
	dealtByMember := [maxRoomMembers]int{}
	for _, result := range userTwo {
		if result.Command == CommandCardDeal && len(result.Args) >= 1 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers {
			dealtByMember[result.Args[0]-1]++
		}
	}
	for index := range engine.players {
		player := &engine.players[index]
		if dealtByMember[index] != 2 || playerHandCount(player) != 5 ||
			playerDrawEffectValue(player, battleBuffCodes["DEAL_BONUS"]) != 1 {
			return fmt.Errorf("official retained-draw consumer member %d is dealt=%d hand=%d effects=%+v", index+1, dealtByMember[index], playerHandCount(player), player.Effects)
		}
	}
	return nil
}

type retainedAttackWholeBattleOutcome struct {
	baseAttack   int
	attackAtPlay int
	damage       int
	endType      int
	finalAttack  int
	finalEffects int
	results      []BattleResult
}

// Official card 10122051 supplies a retained physical ATK_UP_FIXED before the
// complete enemy phase. Comparing it with a same-seed pass path proves that
// the next-turn 10165084 attack reads the rebuilt battle parameter, then the
// terminal cleanup restores the base tuple and removes the retained owner.
func validateRetainedAttackWholeBattleContract(catalog *CombatCatalog) error {
	const (
		supportCard = 10122051
		attackCard  = 10165084
	)
	support, exists := catalog.Cards[supportCard]
	if !exists || support.MaxLevel != 60 || support.NormalSkillID != 11400211 || support.ArthurSkillID != 11400212 {
		return fmt.Errorf("official retained-attack support card %d changed: %+v", supportCard, support)
	}
	supportSkill, supportRoles, err := catalog.CardSkill(supportCard, 1)
	if err != nil {
		return fmt.Errorf("load official retained-attack support skill: %w", err)
	}
	var supportRole *CombatSkillRole
	for index := range supportRoles {
		candidate := &supportRoles[index]
		if candidate.Function == "ATK_UP_FIXED" {
			supportRole = candidate
			break
		}
	}
	if supportSkill.ID != 11400212 || supportSkill.Target != "SELF" || supportSkill.Cost != 3 ||
		supportRole == nil || supportRole.Target != "SELECT" || supportRole.Parameters[0] != "4" ||
		supportRole.Parameters[1] != "ATK" {
		return fmt.Errorf("official retained-attack support contract changed: skill=%+v role=%+v", supportSkill, supportRole)
	}
	attack, exists := catalog.Cards[attackCard]
	if !exists || attack.MaxLevel != 80 || attack.NormalSkillID != 11107761 || attack.ArthurSkillID != 11107792 {
		return fmt.Errorf("official retained-attack damage card %d changed: %+v", attackCard, attack)
	}
	attackSkill, attackRoles, err := catalog.CardSkill(attackCard, 1)
	if err != nil {
		return fmt.Errorf("load official retained-attack damage skill: %w", err)
	}
	attackRoleFound := false
	for _, role := range attackRoles {
		if role.Function == "ATTACK_AA" && role.Target == "SELECT" && role.Parameters[5] == "ATK" && role.Parameters[8] == "PHYSICS" {
			attackRoleFound = true
			break
		}
	}
	if attackSkill.ID != 11107792 || attackSkill.Target != "ENEMY_ALL" || attackSkill.Cost != 1 || !attackRoleFound {
		return fmt.Errorf("official retained-attack damage contract changed: skill=%+v roles=%+v", attackSkill, attackRoles)
	}

	baseline, err := runRetainedAttackWholeBattleScenario(catalog, false)
	if err != nil {
		return fmt.Errorf("execute official retained-attack baseline: %w", err)
	}
	boosted, err := runRetainedAttackWholeBattleScenario(catalog, true)
	if err != nil {
		return fmt.Errorf("execute official retained-attack boosted path: %w", err)
	}
	if baseline.baseAttack != 1000 || baseline.attackAtPlay != 1000 || boosted.baseAttack != 1000 ||
		boosted.attackAtPlay != 12951 ||
		boosted.damage <= baseline.damage {
		return fmt.Errorf("official retained-attack comparison is baseline=%+v boosted=%+v", baseline, boosted)
	}
	if boosted.endType != 1 || boosted.finalAttack != boosted.baseAttack || boosted.finalEffects != 0 ||
		!battleResultsContainCommand(boosted.results, CommandEnd) || !battleResultsContainCommand(boosted.results, CommandBaseParam) {
		return fmt.Errorf("official retained-attack terminal state is %+v", boosted)
	}
	endIndex, releaseIndex, baseIndex, parameterIndex := -1, -1, -1, -1
	for index, result := range boosted.results {
		switch {
		case result.Command == CommandEnd:
			endIndex = index
		case result.Command == 72 && len(result.Args) >= 3 && result.Args[0] == 1 && result.Args[2] == int64(battleBuffCodes["ATK_UP_FIXED"]) && endIndex >= 0:
			releaseIndex = index
		case result.Command == CommandBaseParam && result.Args[0] == 1 && endIndex >= 0:
			baseIndex = index
		case result.Command == CommandBattleParam && len(result.Args) >= 1 && result.Args[0] == 1 && endIndex >= 0:
			parameterIndex = index
		}
	}
	if endIndex < 0 || releaseIndex <= endIndex || baseIndex <= releaseIndex || parameterIndex <= baseIndex {
		return fmt.Errorf("official default retained-attack terminal cleanup order is end=%d release=%d base=%d param=%d results=%+v",
			endIndex, releaseIndex, baseIndex, parameterIndex, boosted.results)
	}
	return nil
}

func runRetainedAttackWholeBattleScenario(catalog *CombatCatalog, applyBuff bool) (retainedAttackWholeBattleOutcome, error) {
	const (
		partyID     = 40084001
		supportCard = 10122051
		attackCard  = 10165084
	)
	support := catalog.Cards[supportCard]
	attack := catalog.Cards[attackCard]
	memberOneDeck := make([]BattleCard, 10)
	for index := range memberOneDeck {
		cardID, level := attackCard, attack.MaxLevel
		if index < 5 {
			cardID, level = supportCard, support.MaxLevel
		}
		memberOneDeck[index] = BattleCard{CardType: index + 1, CardID: cardID, Level: level}
	}
	attackDeck := make([]BattleCard, 10)
	for index := range attackDeck {
		attackDeck[index] = BattleCard{CardType: index + 1, CardID: attackCard, Level: attack.MaxLevel}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		deck := attackDeck
		attackValue := 20000
		if index == 0 {
			deck = memberOneDeck
			attackValue = 1000
		}
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         1000000,
			Attack:     attackValue,
			Magic:      1000,
			Mind:       1000,
			DeckCards:  append([]BattleCard(nil), deck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  3,
		HoldMax:      5,
	}, members)
	if err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := engine.Start(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := engine.UserPhase(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	supportType, attackType := 0, 0
	for _, deckSlot := range engine.players[0].Hand {
		if deckSlot == 0 {
			continue
		}
		card := engine.players[0].Deck[deckSlot-1]
		switch card.CardID {
		case supportCard:
			if supportType == 0 {
				supportType = card.CardType
			}
		case attackCard:
			if attackType == 0 {
				attackType = card.CardType
			}
		}
	}
	if supportType == 0 || attackType == 0 {
		return retainedAttackWholeBattleOutcome{}, fmt.Errorf("seeded opening hand lacks support/attack: hand=%+v deck=%+v", engine.players[0].Hand, engine.players[0].Deck)
	}
	firstSubmission := cardPlaySubmission{}
	if applyBuff {
		firstSubmission.CardTypes[0] = supportType
		firstSubmission.Targets[0] = 1
	}
	if _, err := engine.Submit(1, firstSubmission); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	for memberType := 2; memberType <= maxRoomMembers; memberType++ {
		if _, err := engine.Submit(memberType, cardPlaySubmission{}); err != nil {
			return retainedAttackWholeBattleOutcome{}, err
		}
	}
	if _, err := engine.UserAttack(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if engine.endType != 0 {
		return retainedAttackWholeBattleOutcome{}, fmt.Errorf("support turn ended battle: end=%d", engine.endType)
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := engine.EnemyPhase(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if engine.endType != 0 {
		return retainedAttackWholeBattleOutcome{}, fmt.Errorf("enemy phase ended battle: end=%d", engine.endType)
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	if _, err := engine.UserPhase(); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	attackType = 0
	for _, deckSlot := range engine.players[0].Hand {
		if deckSlot != 0 && engine.players[0].Deck[deckSlot-1].CardID == attackCard {
			attackType = engine.players[0].Deck[deckSlot-1].CardType
			break
		}
	}
	if attackType == 0 {
		return retainedAttackWholeBattleOutcome{}, fmt.Errorf("turn-two hand lacks attack card: hand=%+v", engine.players[0].Hand)
	}
	baseAttack := engine.players[0].BaseAttack
	if baseAttack == 0 {
		baseAttack = engine.players[0].Attack
	}
	attackAtPlay := engine.players[0].Attack
	attackSubmission := cardPlaySubmission{}
	attackSubmission.CardTypes[0] = attackType
	attackSubmission.Targets[0] = 0
	if _, err := engine.Submit(1, attackSubmission); err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	for memberType := 2; memberType <= maxRoomMembers; memberType++ {
		cardType := firstBattleCardType(&engine.players[memberType-1])
		if cardType == 0 {
			return retainedAttackWholeBattleOutcome{}, fmt.Errorf("turn-two member %d has no attack card", memberType)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0] = cardType
		submission.Targets[0] = 0
		if _, err := engine.Submit(memberType, submission); err != nil {
			return retainedAttackWholeBattleOutcome{}, err
		}
	}
	results, err := engine.UserAttack()
	if err != nil {
		return retainedAttackWholeBattleOutcome{}, err
	}
	return retainedAttackWholeBattleOutcome{
		baseAttack:   baseAttack,
		attackAtPlay: attackAtPlay,
		damage:       engine.turnStats.DamageByUser[0],
		endType:      engine.endType,
		finalAttack:  engine.players[0].Attack,
		finalEffects: len(engine.players[0].Effects),
		results:      results,
	}, nil
}
