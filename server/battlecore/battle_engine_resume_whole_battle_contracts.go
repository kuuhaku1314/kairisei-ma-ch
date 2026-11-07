package battlecore

import (
	"fmt"
	"reflect"
)

type resumeWholeBattleOutcome struct {
	resumeResults     []BattleResult
	continuation      []BattleResult
	terminalResults   []BattleResult
	rngAtSnapshot     xorShift128
	rngAfterSnapshot  xorShift128
	handAtSnapshot    [maxRoomMembers]int
	deckAtSnapshot    [maxRoomMembers]int
	defenseAtSnapshot [maxRoomMembers]int
	drawAtSnapshot    [maxRoomMembers]int
	handAtTurnTwo     [maxRoomMembers]int
	defenseAtTurnTwo  [maxRoomMembers]int
	finalDefense      [maxRoomMembers]int
	finalEffects      [maxRoomMembers]int
	endType           int
}

// The focused RESUME_* contract proves the native snapshot shape is read-only;
// this composition keeps official cards, retained effects, the enemy response,
// the eventual victory in one engine. Room ACK/barrier checks live in the
// transport contract suite because they do not consume engine state. A second
// same-seed path without ResumeResults must emit an identical continuation, so
// reconnect cannot fork RNG, turn ownership, hand/deck state or terminal state.
func validateResumeWholeBattleContract(catalog *CombatCatalog) error {
	const (
		defenseCard = 10131011
		attackCard  = 10165084
	)
	defense, exists := catalog.Cards[defenseCard]
	if !exists || defense.MaxLevel != 60 || defense.NormalSkillID != 12502221 || defense.ArthurSkillID != 12502222 {
		return fmt.Errorf("official resume defense card %d changed: %+v", defenseCard, defense)
	}
	attack, exists := catalog.Cards[attackCard]
	if !exists || attack.MaxLevel != 80 || attack.NormalSkillID != 11107761 || attack.ArthurSkillID != 11107792 {
		return fmt.Errorf("official resume attack card %d changed: %+v", attackCard, attack)
	}

	uninterrupted, err := runResumeWholeBattleScenario(catalog, false)
	if err != nil {
		return fmt.Errorf("execute uninterrupted whole-battle path: %w", err)
	}
	resumed, err := runResumeWholeBattleScenario(catalog, true)
	if err != nil {
		return fmt.Errorf("execute resumed whole-battle path: %w", err)
	}
	if uninterrupted.rngAtSnapshot != resumed.rngAtSnapshot || resumed.rngAtSnapshot != resumed.rngAfterSnapshot {
		return fmt.Errorf("resume snapshot changed RNG: uninterrupted=%+v before=%+v after=%+v",
			uninterrupted.rngAtSnapshot, resumed.rngAtSnapshot, resumed.rngAfterSnapshot)
	}
	if !reflect.DeepEqual(uninterrupted.continuation, resumed.continuation) {
		return fmt.Errorf("resume continuation forked from uninterrupted engine: uninterrupted=%+v resumed=%+v",
			uninterrupted.continuation, resumed.continuation)
	}
	if resumed.endType != 1 || !battleResultsContainCommand(resumed.terminalResults, CommandEnd) ||
		!battleResultsContainCommand(resumed.terminalResults, CommandBaseParam) {
		return fmt.Errorf("resumed whole-battle terminal state is %+v", resumed)
	}
	for index := 0; index < maxRoomMembers; index++ {
		if resumed.handAtSnapshot[index] != 3 || resumed.deckAtSnapshot[index] != 5 ||
			resumed.defenseAtSnapshot[index] <= 0 || resumed.drawAtSnapshot[index] != 1 ||
			resumed.handAtTurnTwo[index] != 5 || resumed.defenseAtTurnTwo[index] <= 0 ||
			resumed.finalDefense[index] != 0 || resumed.finalEffects[index] != 0 {
			return fmt.Errorf("resumed whole-battle member %d state is %+v", index+1, resumed)
		}
	}
	if err := validateResumeWholeBattleSnapshot(resumed.resumeResults); err != nil {
		return err
	}
	return nil
}

func runResumeWholeBattleScenario(catalog *CombatCatalog, projectResume bool) (resumeWholeBattleOutcome, error) {
	const (
		partyID     = 10000101
		defenseCard = 10131011
		attackCard  = 10165084
	)
	defense := catalog.Cards[defenseCard]
	attack := catalog.Cards[attackCard]
	mixedDeck := make([]BattleCard, 10)
	for index := range mixedDeck {
		cardID, level := attackCard, attack.MaxLevel
		if index < 5 {
			cardID, level = defenseCard, defense.MaxLevel
		}
		mixedDeck[index] = BattleCard{CardType: index + 1, CardID: cardID, Level: level}
	}
	members := make([]Participant, maxRoomMembers)
	for index := range members {
		members[index] = Participant{
			MemberType: index + 1,
			ArthurType: index + 1,
			HP:         100000,
			Attack:     20000,
			Magic:      1000,
			Mind:       1000,
			DeckCards:  append([]BattleCard(nil), mixedDeck...),
		}
	}
	engine, err := NewBattleEngine(catalog, Config{
		EnemyPartyID: partyID,
		Seed:         602,
		CostInitial:  4,
		HoldMax:      5,
	}, members)
	if err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if _, err := engine.Start(); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if _, err := nextTurnForBattleContract(engine); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if _, err := engine.UserPhase(); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		cardTypes := make([]int, 0, 2)
		for _, deckSlot := range player.Hand {
			if deckSlot != 0 && player.Deck[deckSlot-1].CardID == defenseCard {
				cardTypes = append(cardTypes, player.Deck[deckSlot-1].CardType)
				if len(cardTypes) == 2 {
					break
				}
			}
		}
		if len(cardTypes) != 2 {
			return resumeWholeBattleOutcome{}, fmt.Errorf("seeded member %d opening hand lacks two defense cards: hand=%+v", memberType, player.Hand)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0], submission.CardTypes[1] = cardTypes[0], cardTypes[1]
		if memberType == 2 {
			submission.Targets[0], submission.Targets[1] = 0, 0
		} else {
			submission.Targets[0], submission.Targets[1] = memberType, memberType
		}
		if _, err := engine.Submit(memberType, submission); err != nil {
			return resumeWholeBattleOutcome{}, err
		}
	}
	if _, err := engine.UserAttack(); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if engine.endType != 0 {
		return resumeWholeBattleOutcome{}, fmt.Errorf("resume support turn ended battle: end=%d", engine.endType)
	}
	if _, err := engine.ExecuteChaliceUserPhase(); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if _, err := engine.EnemyPhase(); err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	if engine.endType != 0 || engine.phase != PhaseEnemy {
		return resumeWholeBattleOutcome{}, fmt.Errorf("resume snapshot point is end=%d phase=%d", engine.endType, engine.phase)
	}

	outcome := resumeWholeBattleOutcome{rngAtSnapshot: engine.rng, rngAfterSnapshot: engine.rng}
	for index := range engine.players {
		player := &engine.players[index]
		outcome.handAtSnapshot[index] = playerHandCount(player)
		outcome.deckAtSnapshot[index] = player.remainingDeckCount()
		outcome.defenseAtSnapshot[index] = player.Defense
		outcome.drawAtSnapshot[index] = playerDrawEffectValue(player, battleBuffCodes["DEAL_BONUS"])
	}
	if projectResume {
		outcome.resumeResults, err = engine.ResumeResults()
		if err != nil {
			return resumeWholeBattleOutcome{}, err
		}
		second, err := engine.ResumeResults()
		if err != nil {
			return resumeWholeBattleOutcome{}, err
		}
		if !reflect.DeepEqual(outcome.resumeResults, second) {
			return resumeWholeBattleOutcome{}, fmt.Errorf("repeated resume snapshot changed rows: first=%+v second=%+v", outcome.resumeResults, second)
		}
		outcome.rngAfterSnapshot = engine.rng

	}

	chaliceEnemy, err := engine.ExecuteChaliceEnemyPhase()
	if err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	outcome.continuation = append(outcome.continuation, chaliceEnemy...)
	turnTwo, err := nextTurnForBattleContract(engine)
	if err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	outcome.continuation = append(outcome.continuation, turnTwo...)
	userTwo, err := engine.UserPhase()
	if err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	outcome.continuation = append(outcome.continuation, userTwo...)
	for index := range engine.players {
		outcome.handAtTurnTwo[index] = playerHandCount(&engine.players[index])
		outcome.defenseAtTurnTwo[index] = engine.players[index].Defense
	}
	for memberType := 1; memberType <= maxRoomMembers; memberType++ {
		player := &engine.players[memberType-1]
		cardType := 0
		for _, deckSlot := range player.Hand {
			if deckSlot != 0 && player.Deck[deckSlot-1].CardID == attackCard {
				cardType = player.Deck[deckSlot-1].CardType
				break
			}
		}
		if cardType == 0 {
			return resumeWholeBattleOutcome{}, fmt.Errorf("resumed turn-two member %d has no attack card: hand=%+v", memberType, player.Hand)
		}
		submission := cardPlaySubmission{}
		submission.CardTypes[0] = cardType
		submission.Targets[0] = 0
		submitted, err := engine.Submit(memberType, submission)
		if err != nil {
			return resumeWholeBattleOutcome{}, err
		}
		outcome.continuation = append(outcome.continuation, submitted...)
	}
	terminal, err := engine.UserAttack()
	if err != nil {
		return resumeWholeBattleOutcome{}, err
	}
	outcome.continuation = append(outcome.continuation, terminal...)
	outcome.terminalResults = terminal
	outcome.endType = engine.endType
	for index := range engine.players {
		outcome.finalDefense[index] = engine.players[index].Defense
		outcome.finalEffects[index] = len(engine.players[index].Effects)
	}
	return outcome, nil
}

func validateResumeWholeBattleSnapshot(results []BattleResult) error {
	if len(results) == 0 || results[0].Command != 23 {
		return fmt.Errorf("whole-battle resume identity prefix is %+v", results)
	}
	hands := [maxRoomMembers]bool{}
	decks := [maxRoomMembers]bool{}
	defenseBuffs := [maxRoomMembers]bool{}
	drawBuffs := [maxRoomMembers]bool{}
	enemyFound := false
	for _, result := range results {
		switch result.Command {
		case CommandResumeTurn:
			if !equalBattleArgs(result.Args, []int64{1, 0}) {
				return fmt.Errorf("whole-battle resume after EnemyPhase is %+v", result)
			}
		case CommandResumeCardHand:
			if len(result.Args) != 11 || result.Args[0] < 1 || result.Args[0] > maxRoomMembers {
				return fmt.Errorf("whole-battle resume hand row is %+v", result)
			}
			nonempty := 0
			for index := 1; index < len(result.Args); index += 2 {
				if result.Args[index] != 0 {
					nonempty++
				}
			}
			if nonempty == 3 {
				hands[result.Args[0]-1] = true
			}
		case CommandResumeCardDeck:
			if len(result.Args) == 2 && result.Args[0] >= 1 && result.Args[0] <= maxRoomMembers && result.Args[1] == 5 {
				decks[result.Args[0]-1] = true
			}
		case CommandResumeBuff:
			if len(result.Args) != 11 || result.Args[0] < 1 || result.Args[0] > maxRoomMembers {
				continue
			}
			memberIndex := result.Args[0] - 1
			switch result.Args[2] {
			case int64(battleBuffCodes["DEF_UP_FIXED"]):
				defenseBuffs[memberIndex] = true
			case int64(battleBuffCodes["DEAL_BONUS"]):
				drawBuffs[memberIndex] = true
			}
		case CommandResumeEnemy:
			if len(result.Args) == 7 && result.Args[0] == 5 && result.Args[2] == 0 {
				enemyFound = true
			}
		case 50, 51, 53, 60, 61, CommandBuff, CommandHPCut:
			return fmt.Errorf("whole-battle resume replayed committed event %+v", result)
		}
	}
	for index := 0; index < maxRoomMembers; index++ {
		if !hands[index] || !decks[index] || !defenseBuffs[index] || !drawBuffs[index] {
			return fmt.Errorf("whole-battle resume member %d projection is hand=%t deck=%t defense=%t draw=%t results=%+v",
				index+1, hands[index], decks[index], defenseBuffs[index], drawBuffs[index], results)
		}
	}
	if !enemyFound {
		return fmt.Errorf("whole-battle resume snapshot lacks living enemy: %+v", results)
	}
	return nil
}
