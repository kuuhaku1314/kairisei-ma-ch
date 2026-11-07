package battlecore

import (
	"fmt"
)

// validateEnemyRewriteContracts owns the complete ordinary-enemy REWRITE
// object graph. Native FUN_000974bd only produces action type 4/subtype 3 with
// duration and attribute; the concrete member still comes from the separate
// role, outer-skill and enemy-action target layers.
func validateEnemyRewriteContracts(catalog *CombatCatalog) error {

	if err := validateEnemyRewriteNullSelfAction(catalog); err != nil {
		return err
	}
	if err := validateEnemyRewriteRandomDisplaySelfAction(catalog); err != nil {
		return err
	}
	return validateEnemyRewriteEnemyPartAction(catalog)
}

func validateEnemyRewriteNullSelfAction(catalog *CombatCatalog) error {
	const levelID = 40009632
	const skillID = 44099012
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "SELF" || len(roles) != 2 || roles[0].Function != "ATTR_HIDE" ||
		roles[1].Function != "REWRITE" || roles[1].Target != "SELECT" || roles[1].Parameters[1] != "LIGHT" ||
		action.Slot != 1 || action.Target != "" {
		return fmt.Errorf("official NULL/SELF REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.enemies[0].BaseAttribute = "DARK"
	engine.enemies[0].Attribute = "DARK"
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill, CommandRewrite, CommandBuff, CommandBattleParam, CommandBuff, CommandBattleParam}
	// D-377 party 40096003: NULL AI retains display target zero while
	// the outer SELF skill still applies both roles to the enemy caster.
	if len(results) != len(wantCommands) || engine.enemies[0].Attribute != "LIGHT" || len(engine.enemies[0].Effects) != 2 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 0, 1, 0, skillID, 0}) ||
		!equalBattleArgs(results[3].Args, battleParameterArgs(5, 125000, 125000, 3000, 3000, 1000, 0, 0, 99999, 99999, 99999)) ||
		!equalBattleArgs(results[1].Args, []int64{5, int64(combatAttributeCode("LIGHT"))}) {
		return fmt.Errorf("official NULL/SELF REWRITE action is results=%+v enemy=%+v", results, engine.enemies[0])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official NULL/SELF REWRITE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyRewriteRandomDisplaySelfAction(catalog *CombatCatalog) error {
	const levelID = 40009632
	const skillID = 44099004
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "SELF" || len(roles) != 4 || roles[2].Function != "REWRITE" ||
		roles[2].Target != "SELECT" || roles[2].Parameters[1] != "FIRE" || action.Slot != 4 || action.Target != "RANDOM" {
		return fmt.Errorf("official random-display SELF REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	for index := range engine.players {
		engine.players[index].BaseAttribute = "WIND"
		engine.players[index].Attribute = "WIND"
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	selected := int(results[0].Args[2])
	// Original D-329 API: 50 retains a random player, while SELF applies to
	// the enemy caster. Immediate 32 precedes deferred 67 and both 62/6 pairs.
	if selected < 1 || selected > 4 || engine.enemies[0].Attribute != "FIRE" || len(engine.enemies[0].Effects) != 2 ||
		len(results) != 7 || results[1].Command != CommandRewrite || !equalBattleArgs(results[1].Args, []int64{5, 1}) ||
		results[2].Command != CommandBuffReleaseFailed || results[3].Command != CommandBuff || results[4].Command != CommandBattleParam ||
		results[5].Command != CommandBuff || results[6].Command != CommandBattleParam ||
		!equalBattleArgs(results[5].Args, []int64{5, 2, 0, 202, 4, 0, 2, 0, 0, 0, 0}) ||
		!equalBattleArgs(results[4].Args, battleParameterArgs(5, 125000, 125000, 3000, 3000, 1000, 0, 0, 99999, 99999, 99999)) {
		return fmt.Errorf("official random-display SELF REWRITE action is results=%+v enemy=%+v", results, engine.enemies[0])
	}
	for index := range engine.players {
		if engine.players[index].Attribute != "WIND" || len(engine.players[index].Effects) != 0 {
			return fmt.Errorf("official SELF REWRITE changed player %d: %+v", index+1, engine.players[index])
		}
	}
	return nil
}

func validateEnemyRewriteEnemyPartAction(catalog *CombatCatalog) error {
	const levelID = 40009311
	const skillID = 44096005
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "ENEMY_ONE" || len(roles) != 2 || roles[0].Function != "ATTR_HIDE" ||
		roles[1].Function != "REWRITE" || roles[1].Target != "SELECT" || roles[1].Parameters[1] != "FIRE" ||
		action.Slot != 1 || action.Target != "ENEMY2" {
		return fmt.Errorf("official enemy-part REWRITE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	engine.enemyCount = 2
	engine.enemies[1] = battleEnemy{MemberType: 6, HP: 10000, MaxHP: 10000, BaseAttribute: "DARK", Attribute: "DARK"}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill, CommandRewrite, CommandBuff, CommandBattleParam, CommandBuff, CommandBattleParam}
	if len(results) != len(wantCommands) || engine.enemies[1].Attribute != "FIRE" || len(engine.enemies[1].Effects) != 2 ||
		len(engine.enemies[0].Effects) != 0 || !equalBattleArgs(results[0].Args, []int64{5, skillID, 6, 1, 3, skillID, 0}) ||
		!equalBattleArgs(results[3].Args, battleParameterArgs(6, 10000, 10000, 0, 0, 0, 0, 0, 0, 0, 0)) ||
		!equalBattleArgs(results[1].Args, []int64{6, int64(combatAttributeCode("FIRE"))}) {
		return fmt.Errorf("official enemy-part REWRITE action is results=%+v enemies=%+v", results, engine.enemies[:2])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official enemy-part REWRITE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
