package battlecore

import "fmt"

// validateBattleInputTransportContracts keeps the native input-API result
// groups separate from the later attack evaluation. It runs once while the
// immutable combat catalog is attached, before any room can be created.
func validateBattleInputTransportContracts(catalog *CombatCatalog) error {
	cardEngine := &BattleEngine{
		catalog:       catalog,
		phase:         PhaseUser,
		enemyCount:    1,
		selectedPlays: make(map[int]cardPlaySubmission, maxRoomMembers),
	}
	cardEngine.players[0] = battlePlayer{
		MemberType: 1,
		ArthurType: 1,
		HP:         10000,
		MaxHP:      10000,
		Cost:       10,
	}
	cardEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 10000010, Level: 30}
	cardEngine.players[0].Hand[0] = 1
	cardEngine.enemies[0] = battleEnemy{MemberType: 5, HP: 10000, MaxHP: 10000}
	cardRows, err := cardEngine.Submit(1, cardPlaySubmission{
		CardTypes: [5]int{1},
		Targets:   [5]int{5},
	})
	if err != nil {
		return fmt.Errorf("simulate native card-play result group: %w", err)
	}
	if len(cardRows) != 2 || cardRows[0].Command != CommandCardPlay ||
		!equalBattleArgs(cardRows[0].Args, []int64{1, 1, 5, 0, 0, 0}) ||
		cardRows[1].Command != CommandCardUpdate || len(cardRows[1].Args) != 10 ||
		!equalBattleArgs(cardRows[1].Args[:7], []int64{1, 1, 1, 0, 0, 0, 0}) {
		return fmt.Errorf("native card-play result group is %+v", cardRows)
	}

	passEngine := &BattleEngine{
		catalog:       catalog,
		phase:         PhaseUser,
		selectedPlays: make(map[int]cardPlaySubmission, maxRoomMembers),
	}
	passEngine.players[0] = battlePlayer{MemberType: 1, ArthurType: 1, HP: 10000, MaxHP: 10000}
	passRows, err := passEngine.Submit(1, cardPlaySubmission{})
	if err != nil {
		return fmt.Errorf("simulate native card-pass result group: %w", err)
	}
	if len(passRows) != 1 || passRows[0].Command != CommandCardPass ||
		!equalBattleArgs(passRows[0].Args, []int64{1}) {
		return fmt.Errorf("native card-pass result group is %+v", passRows)
	}
	return nil
}
