package battlecore

import (
	"fmt"
)

type retainedDefenseWholeBattleOutcome struct {
	defenseBeforeEnemy [maxRoomMembers]int
	defenseAtTurnTwo   [maxRoomMembers]int
	turnDamage         [maxRoomMembers]int
	totalDamage        int
	damagedMember      int
	endType            int
	finalDefense       [maxRoomMembers]int
	finalEffects       [maxRoomMembers]int
	results            []BattleResult
}

// Official 10131011/12502222 supplies USER_ALL physical defense and draw
// state. A same-seed pass path proves that the following official enemy
// 40000003 ATTACK_AA consumes the rebuilt DEF parameter; the defended path
// must retain that parameter into turn two and release it at terminal cleanup.
func validateRetainedDefenseWholeBattleContract(catalog *CombatCatalog) error {
	const (
		partyID     = 10000101
		defenseCard = 10131011
		enemySkill  = 40000003
	)
	card, exists := catalog.Cards[defenseCard]
	if !exists || card.MaxLevel != 60 || card.NormalSkillID != 12502221 || card.ArthurSkillID != 12502222 {
		return fmt.Errorf("official retained-defense card %d changed: %+v", defenseCard, card)
	}
	skill, roles, err := catalog.CardSkill(defenseCard, 2)
	if err != nil {
		return fmt.Errorf("load official retained-defense skill: %w", err)
	}
	var defenseRole, drawRole *CombatSkillRole
	for index := range roles {
		candidate := &roles[index]
		switch candidate.Function {
		case "DEF_UP_FIXED":
			defenseRole = candidate
		case "DEAL_BONUS":
			drawRole = candidate
		}
	}
	if skill.ID != 12502222 || skill.Target != "USER_ALL" || skill.Cost != 2 ||
		defenseRole == nil || defenseRole.Target != "SELECT" || defenseRole.Parameters[0] != "2" ||
		defenseRole.Parameters[1] != "DEF" || fixedBuffRoleValue(*defenseRole, card.MaxLevel, 1) != 1439 ||
		drawRole == nil || drawRole.Target != "SELECT" || drawRole.Parameters[0] != "1" {
		return fmt.Errorf("official retained-defense card contract changed: skill=%+v defense=%+v draw=%+v", skill, defenseRole, drawRole)
	}
	party, exists := catalog.EnemyParties[partyID]
	if !exists || party.Slots[0].EnemyID != 10000101 || party.Slots[1].EnemyID != 0 {
		return fmt.Errorf("official retained-defense party %d changed: %+v", partyID, party)
	}
	level := catalog.EnemyLevels[10000101]
	if len(level.Actions) != 1 || level.Actions[0].SkillID != enemySkill || level.Actions[0].Target != "RANDOM" {
		return fmt.Errorf("official retained-defense enemy action changed: %+v", level.Actions)
	}
	enemyVariants := catalog.Skills[enemySkill]
	enemyRoles := catalog.SkillRoles[enemySkill]
	if len(enemyVariants) != 1 || enemyVariants[0].Target != "USER_ONE" || enemyVariants[0].DamageKind != "PHYSICS" ||
		len(enemyRoles) != 1 || enemyRoles[0].Function != "ATTACK_AA" || enemyRoles[0].Target != "SELECT" ||
		enemyRoles[0].Parameters[2] != "1000" || enemyRoles[0].Parameters[5] != "ATK" ||
		enemyRoles[0].Parameters[6] != "0" || enemyRoles[0].Parameters[7] != "WIND" || enemyRoles[0].Parameters[8] != "PHYSICS" {
		return fmt.Errorf("official retained-defense enemy contract changed: skill=%+v roles=%+v", enemyVariants, enemyRoles)
	}

	baseline, err := runRetainedDefenseWholeBattleScenario(catalog, false)
	if err != nil {
		return fmt.Errorf("execute official retained-defense baseline: %w", err)
	}
	defended, err := runRetainedDefenseWholeBattleScenario(catalog, true)
	if err != nil {
		return fmt.Errorf("execute official retained-defense path: %w", err)
	}
	if baseline.totalDamage != 330 || defended.totalDamage != 1 || baseline.damagedMember == 0 ||
		baseline.damagedMember != defended.damagedMember {
		return fmt.Errorf("official retained-defense damage comparison is baseline=%+v defended=%+v", baseline, defended)
	}
	for index := 0; index < maxRoomMembers; index++ {
		if baseline.defenseBeforeEnemy[index] != 0 || baseline.defenseAtTurnTwo[index] != 0 ||
			defended.defenseBeforeEnemy[index] != 1439 || defended.defenseAtTurnTwo[index] != 1439 ||
			defended.finalDefense[index] != 0 || defended.finalEffects[index] != 0 {
			return fmt.Errorf("official retained-defense member %d state is baseline=%+v defended=%+v", index+1, baseline, defended)
		}
	}
	if defended.endType != 1 || !battleResultsContainCommand(defended.results, CommandEnd) ||
		!battleResultsContainCommand(defended.results, CommandBaseParam) {
		return fmt.Errorf("official retained-defense terminal state is %+v", defended)
	}
	endIndex, releaseIndex, baseIndex, parameterIndex := -1, -1, -1, -1
	for index, result := range defended.results {
		switch {
		case result.Command == CommandEnd:
			endIndex = index
		case result.Command == 72 && len(result.Args) >= 3 && result.Args[0] == 1 && result.Args[2] == int64(battleBuffCodes["DEF_UP_FIXED"]) && endIndex >= 0:
			releaseIndex = index
		case result.Command == CommandBaseParam && result.Args[0] == 1 && endIndex >= 0:
			baseIndex = index
		case result.Command == CommandBattleParam && len(result.Args) >= 1 && result.Args[0] == 1 && endIndex >= 0:
			parameterIndex = index
		}
	}
	if endIndex < 0 || releaseIndex <= endIndex || baseIndex <= releaseIndex || parameterIndex <= baseIndex {
		return fmt.Errorf("official default retained-defense cleanup order is end=%d release=%d base=%d param=%d results=%+v",
			endIndex, releaseIndex, baseIndex, parameterIndex, defended.results)
	}
	return nil
}

func runRetainedDefenseWholeBattleScenario(catalog *CombatCatalog, applyDefense bool) (retainedDefenseWholeBattleOutcome, error) {
	const (
		partyID     = 10000101
		defenseCard = 10131011
		attackCard  = 10165084
	)
	defense := catalog.Cards[defenseCard]
	attack := catalog.Cards[attackCard]
	defenseDeck := make([]BattleCard, 10)
	for index := range defenseDeck {
		cardID, level := attackCard, attack.MaxLevel
		if index < 5 {
			cardID, level = defenseCard, defense.MaxLevel
		}
		defenseDeck[index] = BattleCard{CardType: index + 1, CardID: cardID, Level: level}
	}
	attackDeck := make([]BattleCard, 10)
	for index := range attackDeck {
		attackDeck[index] = BattleCard{CardType: index + 1, CardID: attackCard, Level: attack.MaxLevel}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		deck := attackDeck
		if index == 1 {
			deck = defenseDeck
		}
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         100000,
			Attack:     20000,
			Magic:      1000,
			Mind:       1000,
			DeckCards:  append([]BattleCard(nil), deck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  2,
		HoldMax:      5,
	}, members)
	if err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if _, err := engine.Start(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if _, err := engine.UserPhase(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	defenseType, attackType := 0, 0
	for _, deckSlot := range engine.players[1].Hand {
		if deckSlot == 0 {
			continue
		}
		card := engine.players[1].Deck[deckSlot-1]
		switch card.CardID {
		case defenseCard:
			if defenseType == 0 {
				defenseType = card.CardType
			}
		case attackCard:
			if attackType == 0 {
				attackType = card.CardType
			}
		}
	}
	if defenseType == 0 || attackType == 0 {
		return retainedDefenseWholeBattleOutcome{}, fmt.Errorf("seeded member-two hand lacks defense/attack: hand=%+v deck=%+v", engine.players[1].Hand, engine.players[1].Deck)
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		submission := cardPlaySubmission{}
		if applyDefense && memberType == 2 {
			submission.CardTypes[0] = defenseType
			submission.Targets[0] = 0
		}
		if _, err := engine.Submit(memberType, submission); err != nil {
			return retainedDefenseWholeBattleOutcome{}, err
		}
	}
	if _, err := engine.UserAttack(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	var outcome retainedDefenseWholeBattleOutcome
	for index := range engine.players {
		outcome.defenseBeforeEnemy[index] = engine.players[index].Defense
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if _, err := engine.EnemyPhase(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if engine.endType != 0 {
		return retainedDefenseWholeBattleOutcome{}, fmt.Errorf("enemy phase ended battle: end=%d", engine.endType)
	}
	for index := range engine.players {
		outcome.turnDamage[index] = engine.players[index].TurnDamage
		outcome.totalDamage += engine.players[index].TurnDamage
		if engine.players[index].TurnDamage > 0 {
			outcome.damagedMember = index + 1
		}
	}
	if _, err := engine.ExecuteChaliceEnemyPhase(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	for index := range engine.players {
		outcome.defenseAtTurnTwo[index] = engine.players[index].Defense
	}
	if _, err := engine.UserPhase(); err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		cardType := 0
		for _, deckSlot := range engine.players[memberType-1].Hand {
			if deckSlot != 0 && engine.players[memberType-1].Deck[deckSlot-1].CardID == attackCard {
				cardType = engine.players[memberType-1].Deck[deckSlot-1].CardType
				break
			}
		}
		if cardType == 0 {
			return retainedDefenseWholeBattleOutcome{}, fmt.Errorf("turn-two member %d has no attack card", memberType)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0] = cardType
		submission.Targets[0] = 0
		if _, err := engine.Submit(memberType, submission); err != nil {
			return retainedDefenseWholeBattleOutcome{}, err
		}
	}
	results, err := engine.UserAttack()
	if err != nil {
		return retainedDefenseWholeBattleOutcome{}, err
	}
	outcome.endType = engine.endType
	outcome.results = results
	for index := range engine.players {
		outcome.finalDefense[index] = engine.players[index].Defense
		outcome.finalEffects[index] = len(engine.players[index].Effects)
	}
	return outcome, nil
}
