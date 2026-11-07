package battlecore

import "fmt"

// validateHandProjectionContracts is a deterministic native-boundary oracle,
// not a feature unit test. It binds FUN_0003fa8a/FUN_00041a42's durable cache,
// branch-marker comparison and exact managed ResultCmd314/315 argument shapes.
func validateHandProjectionContracts(encodeBattleResult func(BattleResult) (string, error)) error {
	attackRole := CombatSkillRole{SkillID: 100, RoleIndex: 0, Function: "ATTACK_AA"}
	attackRole.Parameters[0] = "100"
	attackRole.Parameters[2] = "1000"
	attackRole.Parameters[5] = "ATK"
	base := CombatSkillDefinition{ID: 100, DisplayRole: 1, FunctionID: 1000, Attribute: "FIRE", Target: "ENEMY_ONE"}
	extend := base
	extend.FunctionID = 1001
	extend.BranchCondition = "TURN"
	extend.BranchParameters[0] = "2"
	extend.BranchPriority = 1
	catalog := &CombatCatalog{
		Cards: map[int]CombatCardDefinition{
			1: {ID: 1, NormalSkillID: 100},
		},
		Spheres: map[int]CombatSphereDefinition{
			1: {ID: 1, SkillID: 100, MaxLevel: 1},
		},
		Skills: map[int][]CombatSkillDefinition{
			100: {base, extend},
		},
		SkillRoles: map[int][]CombatSkillRole{
			1000: {attackRole},
			1001: {attackRole},
		},
	}

	cardEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(314)}
	cardEngine.selectedPlays = map[int]cardPlaySubmission{1: {}}
	cardEngine.players[0] = battlePlayer{MemberType: 1, ArthurType: 1, Attack: 1000, HP: 1000, MaxHP: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	cardEngine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
	cardEngine.players[0].Hand[0] = 1
	cardEngine.players[0].CardDisplay[1] = battleDisplayPower{Power: 1100, Marker: 0, Known: true}
	cardEngine.players[0].Attack = 1200
	cardRows, err := cardEngine.refreshBattleDisplayPowers()
	if err != nil {
		return err
	}
	if len(cardRows) != 1 || cardRows[0].Command != CommandCardUpdate2 ||
		!equalBattleArgs(cardRows[0].Args, []int64{1, 1, 1300, 0, 0}) {
		return fmt.Errorf("native CARD_UPDATE2 power projection is %+v", cardRows)
	}
	unchanged, err := cardEngine.refreshBattleDisplayPowers()
	if err != nil || len(unchanged) != 0 {
		return fmt.Errorf("native CARD_UPDATE2 unchanged suppression is rows=%+v err=%v", unchanged, err)
	}
	cardEngine.turn = 2
	markerRows, err := cardEngine.refreshBattleDisplayPowers()
	if err != nil {
		return err
	}
	if len(markerRows) != 1 || markerRows[0].Command != CommandCardUpdate2 ||
		!equalBattleArgs(markerRows[0].Args, []int64{1, 1, 1300, 1, 0}) {
		return fmt.Errorf("native CARD_UPDATE2 marker-only projection is %+v", markerRows)
	}

	holdEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(315)}
	holdEngine.selectedPlays = map[int]cardPlaySubmission{1: {}}
	holdEngine.players[0] = battlePlayer{MemberType: 1, ArthurType: 1, Attack: 1200, HP: 1000, MaxHP: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	holdEngine.players[0].BlessHolds = []battleBlessHold{{
		AppendIndex: 3, CardType: 22, SourceMember: 1, Skill: base, Roles: []CombatSkillRole{attackRole},
		CardLevel: 1, Power: 1100, PowerKnown: true,
	}}
	holdRows, err := holdEngine.refreshBattleDisplayPowers()
	if err != nil {
		return err
	}
	if len(holdRows) != 1 || holdRows[0].Command != CommandCardUpdate2 ||
		!equalBattleArgs(holdRows[0].Args, []int64{1, 22, 1300, 0, 3}) ||
		holdEngine.players[0].BlessHolds[0].Power != 1300 {
		return fmt.Errorf("native append CARD_UPDATE2 projection/state is rows=%+v hold=%+v", holdRows, holdEngine.players[0].BlessHolds[0])
	}

	sphereEngine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(316)}
	sphereEngine.players[0] = battlePlayer{MemberType: 1, ArthurType: 1, Attack: 1200, HP: 1000, MaxHP: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	sphereEngine.players[0].Spheres[0] = battleSphere{
		Slot: 1, SphereID: 1, Level: 1,
		Display: battleDisplayPower{Power: 1100, Marker: 0, Known: true},
	}
	sphereRows, err := sphereEngine.refreshBattleDisplayPowers()
	if err != nil {
		return err
	}
	if len(sphereRows) != 1 || sphereRows[0].Command != CommandSphereUpdate2 ||
		!equalBattleArgs(sphereRows[0].Args, []int64{1, 1, 1300, 0}) {
		return fmt.Errorf("native SPHR_UPDATE2 power projection is %+v", sphereRows)
	}
	sphereEngine.turn = 2
	sphereMarkerRows, err := sphereEngine.refreshBattleDisplayPowers()
	if err != nil {
		return err
	}
	if len(sphereMarkerRows) != 1 || sphereMarkerRows[0].Command != CommandSphereUpdate2 ||
		!equalBattleArgs(sphereMarkerRows[0].Args, []int64{1, 1, 1300, 1}) {
		return fmt.Errorf("native SPHR_UPDATE2 marker-only projection is %+v", sphereMarkerRows)
	}

	if _, err := encodeBattleResult(cardUpdate2Result(1, 1, battleDisplayPower{Power: 1, Known: true}, 0)); err != nil {
		return fmt.Errorf("CARD_UPDATE2 managed boundary rejected exact row: %w", err)
	}
	if _, err := encodeBattleResult(sphereUpdate2Result(1, 1, 1, 0)); err != nil {
		return fmt.Errorf("SPHR_UPDATE2 managed boundary rejected exact row: %w", err)
	}
	if _, err := encodeBattleResult((BattleResult{Command: CommandCardUpdate2, Args: []int64{1, 1, 1, 0}})); err == nil {
		return fmt.Errorf("CARD_UPDATE2 managed boundary accepted four arguments")
	}
	if _, err := encodeBattleResult((BattleResult{Command: CommandSphereUpdate2, Args: []int64{1, 1}})); err == nil {
		return fmt.Errorf("SPHR_UPDATE2 managed boundary accepted two arguments")
	}
	return nil
}
