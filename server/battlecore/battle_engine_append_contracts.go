package battlecore

import "fmt"

func validateEnemyAppendCardContracts(catalog *CombatCatalog) error {
	foundBless := false
	foundCurse := false
	for levelID, level := range catalog.EnemyLevels {
		for _, outerAction := range level.Actions {
			for _, outerSkill := range catalog.Skills[outerAction.SkillID] {
				for _, role := range catalog.SkillRoles[outerSkill.FunctionID] {
					if role.Function != "BLESS" && role.Function != "ENEMY_CURSE" {
						continue
					}
					foundBless = foundBless || role.Function == "BLESS"
					foundCurse = foundCurse || role.Function == "ENEMY_CURSE"
					slot := combatParameterInt(role.Parameters[1])
					if slot < 0 || slot >= len(level.CallSkillIDs) {
						return fmt.Errorf("official enemy level %d append role %d has invalid call-skill slot %d", levelID, role.SkillID, slot)
					}
					callSkillID := level.CallSkillIDs[slot]
					if callSkillID == 0 {
						return fmt.Errorf("official enemy level %d append role %d references empty call-skill slot %d", levelID, role.SkillID, slot)
					}
					variants := catalog.Skills[callSkillID]
					if len(variants) == 0 {
						return fmt.Errorf("official enemy level %d append role %d references missing call skill %d", levelID, role.SkillID, callSkillID)
					}
					for _, variant := range variants {
						roles := catalog.SkillRoles[variant.FunctionID]
						if len(roles) == 0 {
							return fmt.Errorf("official enemy call skill %d function %d has no roles", callSkillID, variant.FunctionID)
						}
						for _, callRole := range roles {
							if callRole.Function == "" || callRole.Function == "NONE" || callRole.Function == "OUTPUT_TEXT" {
								continue
							}
							if !SupportsPlayerFunction(callRole.Function) && !SupportsEnemyFunction(callRole.Function) {
								return fmt.Errorf("official enemy call skill %d has unsupported holder-context function %q", callSkillID, callRole.Function)
							}
						}
					}
				}
			}
		}
	}
	if !foundBless || !foundCurse {
		return fmt.Errorf("official enemy append-card graph is incomplete: bless=%t curse=%t", foundBless, foundCurse)
	}

	// 38111113 is an official enemy BLESS action. Its p1=0 selects level
	// 31160122 call slot zero (38111141): a one-shot APPEND_CARD_BLESS which
	// gives GUTS to the concrete player holder once SELF_OTHER_PLAY_NUM matches.
	level, exists := catalog.EnemyLevels[31160122]
	if !exists || level.CallSkillIDs[0] != 38111141 {
		return fmt.Errorf("official enemy BLESS call-skill graph changed: %+v", level)
	}
	roles := catalog.SkillRoles[38111113]
	var blessRole *CombatSkillRole
	for index := range roles {
		if roles[index].Function == "BLESS" {
			blessRole = &roles[index]
			break
		}
	}
	if blessRole == nil || blessRole.Parameters[0] != "99" || blessRole.Parameters[1] != "0" || blessRole.Parameters[2] != "0" {
		return fmt.Errorf("official enemy BLESS role changed: %+v", blessRole)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1, holdMax: 5, rng: newXorShift128(1)}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, ArthurType: index + 1, HP: 10000, MaxHP: 10000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Level: level}
	setResults, err := engine.executeEnemyBless(&engine.enemies[0], 1, *blessRole)
	if err != nil {
		return err
	}
	if len(setResults) != 1 || setResults[0].Command != CommandHoldSet ||
		!equalBattleArgs(setResults[0].Args, []int64{1, int64(blessRole.RoleIndex), 22, 0, 38111141, 0, 99, 1, 20}) ||
		len(engine.players[0].BlessHolds) != 1 || !engine.players[0].BlessHolds[0].EnemySkill ||
		engine.players[0].BlessHolds[0].SourceMember != 5 || engine.players[0].BlessHolds[0].Repeat {
		return fmt.Errorf("official enemy BLESS registration is results=%+v hold=%+v", setResults, engine.players[0].BlessHolds)
	}
	cursePass, err := engine.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		return err
	}
	if len(cursePass) != 1 || cursePass[0].Command != CommandHoldSkillEnd || len(engine.players[0].BlessHolds) != 1 {
		return fmt.Errorf("native append type-0 pass consumed BLESS: results=%+v holds=%+v", cursePass, engine.players[0].BlessHolds)
	}
	execResults, err := engine.executeBlessHolds("USER_ATTACK_END", 22)
	if err != nil {
		return err
	}
	if len(engine.players[0].BlessHolds) != 0 || len(engine.players[0].Effects) != 1 ||
		engine.players[0].Effects[0].Function != "GUTS" || engine.players[0].Effects[0].Source != 5 ||
		len(execResults) < 4 || execResults[0].Command != CommandHoldSkill ||
		execResults[len(execResults)-2].Command != CommandHoldLost || execResults[len(execResults)-1].Command != CommandHoldSkillEnd {
		return fmt.Errorf("official enemy BLESS holder execution is results=%+v player=%+v", execResults, engine.players[0])
	}
	if err := validateUserAttackTailOrderContract(catalog); err != nil {
		return err
	}
	if err := validateUserAttackTerminalTailContract(catalog); err != nil {
		return err
	}
	return nil
}

// validateUserAttackTailOrderContract binds the four calls following native
// FUN_00063d60/ATTACK_PARTITION. BLESS holds run first, selected-card traps
// run only after every ordinary action, enemy-side retained DOT follows, and
// CURSE holds run last. Both append passes emit their own HOLD_SKILL_END.
func validateUserAttackTailOrderContract(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 1000, MaxHP: 1000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 1000, MaxHP: 1000,
		Effects: []battleEffect{{Function: "BURN", Value: 100, Remaining: 2, Source: 1}},
	}
	engine.players[0].Effects = []battleEffect{{
		Function: "CARD_TRAP_DAMAGE", CardType: 1, Value: 100, Remaining: 2,
		Kind: 2, RoleIndex: 7, Source: 5,
	}}
	// No-role cleanup holds use fixture IDs outside the supplied master.
	engine.players[0].BlessHolds = []battleBlessHold{
		{AppendIndex: 0, CardType: 22, SourceMember: 1, Skill: CombatSkillDefinition{ID: 919999901, Target: "SELF"}, CardLevel: 1, Target: 1, Remaining: 1},
		{AppendIndex: 1, CardType: 21, SourceMember: 1, Skill: CombatSkillDefinition{ID: 919999902, Target: "SELF"}, CardLevel: 1, Target: 1, Remaining: 1},
	}
	engine.turnActions = []battleAction{{memberType: 1, cardType: 1}}

	results, err := engine.executeBlessHolds("USER_ATTACK_END", 22)
	if err != nil {
		return err
	}
	results = append(results, engine.triggerPlayedCardTraps()...)
	dotResults, err := engine.tickEnemyDOTEffects()
	if err != nil {
		return err
	}
	results = append(results, dotResults...)
	curseResults, err := engine.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		return err
	}
	results = append(results, curseResults...)

	want := []int{CommandHoldSkill, 88, CommandHoldLost, CommandHoldSkillEnd, CommandBuffEffect, 60, 72, CommandBuffLostOne, CommandBattleParam, CommandBuffEffect, 60, CommandHoldSkill, 88, CommandHoldLost, CommandHoldSkillEnd}
	if len(results) != len(want) {
		return fmt.Errorf("native user-attack tail returned %d rows, want %d: %+v", len(results), len(want), results)
	}
	for index, command := range want {
		if results[index].Command != command {
			return fmt.Errorf("native user-attack tail command %d is %d, want %d: %+v", index, results[index].Command, command, results)
		}
	}
	if engine.players[0].HP != 900 || engine.enemies[0].HP != 900 || len(engine.players[0].BlessHolds) != 0 || len(engine.players[0].Effects) != 0 {
		return fmt.Errorf("native user-attack tail state is player=%+v enemy=%+v", engine.players[0], engine.enemies[0])
	}
	return nil
}

// validateUserAttackTerminalTailContract binds the terminal gates surrounding
// native FUN_00064120/FUN_00065010. FUN_0005d008 dispatches the GameMaster
// vtable +4 slot (TeamBattle::checkEnd), so a lethal enemy DOT locks win before
// FUN_00065010 is allowed to consider an all-player KO. The same end flag
// suppresses later CURSE execution without suppressing one-shot hold cleanup.
func validateUserAttackTerminalTailContract(catalog *CombatCatalog) error {
	terminalHold := &BattleEngine{catalog: catalog, turn: 1, endType: 1, enemyCount: 1}
	for index := range terminalHold.players {
		terminalHold.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	terminalHold.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 0, MaxHP: 100}
	terminalHold.players[0].BlessHolds = []battleBlessHold{{
		AppendIndex: 0, CardType: 21, SourceMember: 1, Skill: CombatSkillDefinition{ID: 9},
		CardLevel: 1, Target: 1, Remaining: 1,
	}}
	holdResults, err := terminalHold.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		return err
	}
	// 668b4/74d97's terminal output gate hides 316 and 59 while still
	// removing the one-shot from state (D-385 original API receipt).
	if len(holdResults) != 0 || len(terminalHold.players[0].BlessHolds) != 0 {
		return fmt.Errorf("terminal CURSE gate executed or retained a one-shot hold: results=%+v player=%+v", holdResults, terminalHold.players[0])
	}

	deadTrap := &BattleEngine{}
	deadTrap.players[0] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, HP: 0, MaxHP: 100, Effects: []battleEffect{{
		Function: "CARD_TRAP_DAMAGE", CardType: 1, Value: 100, Remaining: 1,
	}}}
	if trapResults := deadTrap.triggerCardTrap(1, 1); len(trapResults) != 0 || len(deadTrap.players[0].Effects) != 1 {
		return fmt.Errorf("dead holder consumed a selected-card trap: results=%+v player=%+v", trapResults, deadTrap.players[0])
	}

	engine := &BattleEngine{catalog: catalog, turn: 3, phase: PhaseUser, enemyCount: 1}
	for index := range engine.players {
		memberType := index + 1
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
			MemberType: memberType, HP: 100, MaxHP: 100,
			Effects: []battleEffect{{
				Function: "CARD_TRAP_DAMAGE", CardType: memberType, Value: 100, Remaining: 1,
				Kind: 2, RoleIndex: 7, Source: 5,
			}},
		}
		engine.turnActions = append(engine.turnActions, battleAction{memberType: memberType, cardType: memberType})
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 100, MaxHP: 100,
		Effects: []battleEffect{{Function: "BURN", Value: 100, Remaining: 1, Source: 1}},
	}
	results := appendAttackPartition(nil)
	bless, err := engine.executeBlessHolds("USER_ATTACK_END", 22)
	if err != nil {
		return err
	}
	results = append(results, bless...)
	results = append(results, engine.triggerPlayedCardTraps()...)
	dot, err := engine.tickEnemyDOTEffects()
	if err != nil {
		return err
	}
	results = append(results, dot...)
	curse, err := engine.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		return err
	}
	results = append(results, curse...)
	results, err = engine.settleActionPhase(results, PhaseUserAttack, true)
	if err != nil {
		return err
	}

	if engine.endType != 1 || engine.phase != PhaseEnded || engine.enemies[0].HP != 0 {
		return fmt.Errorf("nonlethal trap followed by DOT victory is end=%d phase=%d enemy=%+v", engine.endType, engine.phase, engine.enemies[0])
	}
	for _, player := range engine.players {
		if player.HP != 1 || player.GameOver {
			return fmt.Errorf("native nonlethal trap must leave 1 HP without retirement: %+v", player)
		}
	}
	turnIndex := -1
	endIndex := -1
	gameOverCount := 0
	invalidGameOver := false
	for index, result := range results {
		switch result.Command {
		case CommandTurn:
			if len(result.Args) == 2 && result.Args[0] == 3 && result.Args[1] == 1 {
				turnIndex = index
			}
		case CommandGameOver:
			gameOverCount++
			invalidGameOver = invalidGameOver || len(result.Args) != 1
		case CommandEnd:
			if len(result.Args) == 1 && result.Args[0] == 1 {
				endIndex = index
			}
		}
	}
	if turnIndex >= 0 || endIndex < 0 || gameOverCount != 0 || invalidGameOver {
		return fmt.Errorf("winning terminal must skip next TURN and retirement: results=%+v", results)
	}

	chaliceTerminal := &BattleEngine{endType: 1}
	for index := range chaliceTerminal.players {
		chaliceTerminal.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100}
	}
	chaliceResults, err := chaliceTerminal.settleActionPhase(
		[]BattleResult{{Command: CommandSphereCount, Args: []int64{1, 1, 0, 1}}},
		PhaseChaliceUser,
		false,
	)
	if err != nil {
		return err
	}
	if chaliceTerminal.phase != PhaseEnded || !battleResultsContainCommand(chaliceResults, CommandEnd) ||
		battleResultsContainCommand(chaliceResults, CommandTurn) {
		return fmt.Errorf("chalice terminal settlement inserted UserAttack TURN or lost END: results=%+v phase=%d", chaliceResults, chaliceTerminal.phase)
	}
	return nil
}

// validateAppendConditionContracts exercises every condition family reachable
// from the official CN enemy call-skill and player BLESS object graphs. These
// are deterministic native-contract simulations, not feature unit tests.
func validateAppendConditionContracts(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, turn: 1}
	engine.selectedPlays = map[int]cardPlaySubmission{1: {CardTypes: [5]int{1, 2}}, 2: {CardTypes: [5]int{1}}}
	actor := &engine.players[0]
	// Native parameter projection has a 500 minimum MaxHP. Keep the
	// intended 50% condition valid after the real buff producer refreshes it.
	*actor = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 1, ArthurType: 1, HP: 250, MaxHP: 500, Attack: 100}
	for index := range actor.Hand {
		actor.Deck[index] = BattleCard{CardType: index + 1, CardID: 10131011, Level: 60}
		actor.Hand[index] = index + 1
	}
	engine.players[1] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 2, HP: 500, MaxHP: 500}

	// BUFF_EXEC is an execution counter in native FUN_000d0889; SELF_BUFF is
	// independently evaluated from durable status state in FUN_000d5a4a.
	buffRole := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF"}
	buffRole.Parameters[0] = "2"
	buffRole.Parameters[1] = "ATK"
	buffRole.Parameters[2] = "1000"
	buffRole.Parameters[3] = "100"
	buffResults, err := engine.executePlayerRole(battleAction{memberType: 1, cardLevel: 1}, buffRole, 1)
	if err != nil || len(buffResults) == 0 || engine.players[0].ExecutedBuffKinds[1] != 1 {
		return fmt.Errorf("append BUFF_EXEC producer is results=%+v counter=%d err=%v", buffResults, engine.players[0].ExecutedBuffKinds[1], err)
	}

	actions := []battleAction{
		{memberType: 1, cardID: 10131011, cardType: 1, skill: CombatSkillDefinition{Cost: 1, Kind: "DEFENSE", Attribute: "FIRE"}},
		{memberType: 1, cardID: 10131011, cardType: 2, skill: CombatSkillDefinition{Cost: 2, Kind: "ATTACK", Attribute: "ICE"}},
		{memberType: 2, cardID: 10131011, cardType: 1, skill: CombatSkillDefinition{Cost: 5, Kind: "RECOVERY", Attribute: "FIRE"}},
	}
	current := battleAction{memberType: 1, cardType: 21, skill: CombatSkillDefinition{Attribute: "FIRE"}}
	chains := map[string]int{"FIRE": 2, "ICE": 1}
	type conditionCase struct {
		name       string
		parameters [5]string
	}
	cases := []conditionCase{
		{name: "DECK_COMBO_COUNT", parameters: [5]string{"2", "2"}},
		{name: "SELF_OTHER_PLAY_ATTR", parameters: [5]string{"FIRE", "1"}},
		{name: "SELF_OTHER_PLAY_SKILL_KIND", parameters: [5]string{"DEFENSE", "1"}},
		{name: "SELF_OTHER_PLAY_RARITY", parameters: [5]string{"NORMAL", "2"}},
		{name: "SELF_HP_PER", parameters: [5]string{"50", "50"}},
		{name: "SELF_OTHER_PLAY_NUM", parameters: [5]string{"2", "2"}},
		{name: "SELF_PLAY_MOST_LOW_COST", parameters: [5]string{"1", "1"}},
		{name: "SELF_PLAY_COST_TOTAL", parameters: [5]string{"3", "3"}},
		{name: "SELF_BUFF", parameters: [5]string{"ATK_UP_BY_ATK"}},
		{name: "SELF_PLAY_COST_NUM", parameters: [5]string{"1", "1", "1", "1"}},
		{name: "SELF_NOT_PLAY_HAND_NUM", parameters: [5]string{"3", "3"}},
		{name: "BUFF_EXEC", parameters: [5]string{"ATK_UP_BY_ATK"}},
	}
	for _, test := range cases {
		if !engine.branchConditionSatisfied(test.name, test.parameters, current, actions, chains) {
			return fmt.Errorf("official append condition %s rejected its native-contract simulation", test.name)
		}
	}
	if engine.branchConditionSatisfied("SELF_PLAY_MOST_LOW_COST", [5]string{"3", "3"}, current, actions, chains) {
		return fmt.Errorf("official append minimum-cost condition accepted an out-of-range hand")
	}
	engine.players[0].ExecutedBuffKinds[1] = 0
	if engine.branchConditionSatisfied("BUFF_EXEC", [5]string{"ATK_UP_BY_ATK"}, current, actions, chains) {
		return fmt.Errorf("official append BUFF_EXEC condition ignored its execution counter")
	}
	return nil
}
