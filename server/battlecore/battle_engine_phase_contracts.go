package battlecore

import "fmt"

func validateBattlePhaseControlContracts(encodeBattleResult func(BattleResult) (string, error)) error {
	for _, row := range []BattleResult{
		{Command: CommandWaitAndSee},
		{Command: CommandBuffPartition},
		{Command: CommandBuffLostOne, Args: []int64{1, 17, 0}},
	} {
		if _, err := encodeBattleResult(row); err != nil {
			return fmt.Errorf("phase-control ResultCmd%d boundary: %w", row.Command, err)
		}
	}

	waitEngine := &BattleEngine{phase: PhaseUserAttack, enemyCount: 1}
	for index := range waitEngine.players {
		waitEngine.players[index] = battlePlayer{MemberType: index + 1, HP: 1, MaxHP: 1}
	}
	waitEngine.enemies[0] = battleEnemy{MemberType: 5, HP: 1, MaxHP: 1}
	waitResults, err := waitEngine.EnemyPhase()
	if err != nil || len(waitResults) != 3 || waitResults[0].Command != CommandWaitAndSee ||
		waitResults[1].Command != CommandHoldSkillEnd || waitResults[2].Command != CommandHoldSkillEnd {
		return fmt.Errorf("JP empty enemy phase is results=%+v err=%v", waitResults, err)
	}

	stunEngine := &BattleEngine{phase: PhaseUserAttack, enemyCount: 1}
	for index := range stunEngine.players {
		stunEngine.players[index] = battlePlayer{MemberType: index + 1, HP: 1, MaxHP: 1}
	}
	stunEngine.enemies[0] = battleEnemy{
		MemberType: 5, HP: 1, MaxHP: 1,
		Effects: []battleEffect{{Function: "STAN", Remaining: 1}},
	}
	stunResults, err := stunEngine.EnemyPhase()
	if err != nil || len(stunResults) != 3 || stunResults[0].Command != CommandStun ||
		stunResults[1].Command != CommandHoldSkillEnd || stunResults[2].Command != CommandHoldSkillEnd {
		return fmt.Errorf("JP interrupted enemy phase is results=%+v err=%v", stunResults, err)
	}

	chaliceEngine := &BattleEngine{phase: PhaseEnemy}
	chaliceResults, err := chaliceEngine.ExecuteChaliceEnemyPhase()
	if err != nil || len(chaliceResults) != 5 || chaliceResults[0].Command != CommandBuffPartition ||
		chaliceEngine.phase != PhaseChaliceEnemy {
		return fmt.Errorf("native chalice enemy partition is results=%+v phase=%d err=%v", chaliceResults, chaliceEngine.phase, err)
	}
	// Original API transcripts retain both NORMAL/EVENT snapshots for the
	// two internal field members, even with no real actors in this fixture.
	for index, row := range chaliceResults[1:] {
		if row.Command != CommandBattleParam || !equalBattleArgs(row.Args,
			battleParameterArgs(10+index/2, 0, 1, 0, 0, 0, 0, 0, 99999, 99999, 99999)) {
			return fmt.Errorf("native field-member expiry snapshot is %+v", row)
		}
	}

	terminalEngine := &BattleEngine{phase: PhaseUserAttack, endType: 1}
	terminalEngine.players[0] = battlePlayer{
		MemberType: 1, HP: 100, MaxHP: 100,
		Effects: []battleEffect{{Function: "BURN", Kind: 2, Remaining: 2, RoleIndex: 4}},
	}
	terminalResults, err := terminalEngine.appendTerminalBuffCleanup([]BattleResult{battleEndResult(1)})
	if err != nil {
		return err
	}
	wantTerminal := []int{CommandEnd, 72, CommandBaseParam, CommandBattleParam}
	if len(terminalResults) != len(wantTerminal) {
		return fmt.Errorf("native terminal buff cleanup is %+v", terminalResults)
	}
	for index, command := range wantTerminal {
		if terminalResults[index].Command != command {
			return fmt.Errorf("native terminal buff cleanup order is %+v", terminalResults)
		}
	}
	if len(terminalResults[0].Args) != 1 || terminalResults[0].Args[0] != 1 {
		return fmt.Errorf("native END projection is %+v", terminalResults[0])
	}
	if terminalResults[1].Args[0] != 1 || terminalResults[1].Args[2] != int64(battleBuffCodes["BURN"]) || len(terminalEngine.players[0].Effects) != 0 {
		return fmt.Errorf("native default terminal release is result=%+v player=%+v", terminalResults[1], terminalEngine.players[0])
	}
	return nil
}
