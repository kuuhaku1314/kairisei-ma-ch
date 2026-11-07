package battlecore

import "fmt"

// validateEnemyAllDebuffReleaseContracts closes the ordinary-enemy action
// boundary for native role 38 (DEBUFF_RELEASE). The selector is deliberately
// checked at three levels: the role target, the outer skill target, and the
// enemy-level action override. Conflating those levels breaks SELF cleanses,
// ENEMY_ALL cleanses whose action selects a player, and the revive-then-cleanse
// sequence used by old multipart bosses.
func validateEnemyAllDebuffReleaseContracts(catalog *CombatCatalog) error {

	if err := validateEnemySelfDebuffReleaseAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyAllDebuffReleaseAction(catalog); err != nil {
		return err
	}
	return validateEnemyReviveDebuffReleaseAction(catalog)
}

func validateEnemySelfDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40032421
	const skillID = 44324101
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "USER_ALL" || len(roles) != 3 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "BUFF_RELEASE_ONE" || roles[0].Target != "SELECT" ||
		roles[1].RoleIndex != 1 || roles[1].Function != "BUFF_RELEASE_ONE" || roles[1].Target != "SELECT" ||
		roles[2].RoleIndex != 2 || roles[2].Function != "DEBUFF_RELEASE" || roles[2].Target != "SELF" ||
		action.Slot != 1 || action.Target != "RANDOM" || action.AIConditionID != 40324112 {
		return fmt.Errorf("official enemy SELF DEBUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "POISON", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 6, Source: 1},
	}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill,
		CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam,
		CommandBuffReleaseFailed, CommandBuffReleaseFailed, CommandBuffReleaseFailed, CommandBuffReleaseFailed,
		CommandBuffReleaseFailed, CommandBuffReleaseFailed, CommandBuffReleaseFailed, CommandBuffReleaseFailed}
	if len(results) != len(wantCommands) || len(engine.enemies[0].Effects) != 1 ||
		engine.enemies[0].Effects[0].Function != "ATK_UP_FIXED" || len(engine.enemies[1].Effects) != 1 ||
		engine.enemies[1].Effects[0].Function != "POISON" ||
		!equalBattleArgs(results[1].Args, []int64{5, 2, 0, 0, 0, 33, 0, 0}) {
		return fmt.Errorf("official enemy SELF DEBUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy SELF DEBUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	for role := 0; role < 2; role++ {
		for member := 1; member <= 4; member++ {
			// 7adb0 drains failed roles after immediate successful consumers;
			// D-329's mixed REWRITE/release API confirms this deferred ordering.
			if !equalBattleArgs(results[5+role*4+member-1].Args, []int64{int64(member), int64(role)}) {
				return fmt.Errorf("official USER_ALL release failure lost target/role: %+v", results)
			}
		}
	}
	return nil
}

func validateEnemyAllDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40030521
	const skillID = 44305001
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "ENEMY_ALL" || len(roles) != 1 || roles[0].RoleIndex != 0 ||
		roles[0].Function != "DEBUFF_RELEASE" || roles[0].Target != "ENEMY_ALL" ||
		action.Slot != 1 || action.Target != "RANDOM" || action.AIConditionID != 40305001 {
		return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 10000, MaxHP: 10000}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "WEAKNESS", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 6, Source: 1},
	}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam}
	if len(results) != len(wantCommands) || len(engine.enemies[0].Effects) != 1 ||
		engine.enemies[0].Effects[0].Function != "ATK_UP_FIXED" || len(engine.enemies[1].Effects) != 0 ||
		!equalBattleArgs(results[1].Args, []int64{5, 0, 0, 0, 0, 33, 0, 0}) ||
		!equalBattleArgs(results[5].Args, []int64{6, 0, 0, 0, 0, 33, 0, 0}) {
		return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy ENEMY_ALL DEBUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyReviveDebuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 31210121
	const skillID = 38401112
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "DEAD_ENEMY_ONE" || len(roles) != 4 ||
		roles[0].Function != "REVIVE" || roles[1].Function != "DEBUFF_RELEASE" ||
		roles[2].Function != "DEF_UP_FIXED" || roles[3].Function != "DEF_UP_FIXED" ||
		action.Slot != 10 || action.Target != "ENEMY2" || action.AIConditionID != skillID {
		return fmt.Errorf("official revive/cleanse boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 2}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 6, HP: 0, MaxHP: 9000, Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1,
		Effects: []battleEffect{
			{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 1},
			{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 5},
		},
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	target := &engine.enemies[1]
	wantCommands := []int{CommandSkill, CommandSkillRevive, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam,
		CommandBuff, CommandBattleParam, CommandBuff, CommandBattleParam}
	if len(results) != len(wantCommands) || target.HP != 9000 || target.Broken || target.DeathActionTriggered || target.DiedTurn != 0 ||
		len(target.Effects) != 3 || target.Effects[0].Function != "ATK_UP_FIXED" ||
		target.Effects[1].Function != "DEF_UP_FIXED" || target.Effects[1].Parameter != "DEF" ||
		target.Effects[2].Function != "DEF_UP_FIXED" || target.Effects[2].Parameter != "MDEF" ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 6, 1, 5, skillID, 0}) ||
		!equalBattleArgs(results[2].Args, []int64{6, 1, 0, 0, 0, 33, 0, 0}) {
		return fmt.Errorf("official revive/cleanse action is results=%+v target=%+v", results, *target)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official revive/cleanse command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
