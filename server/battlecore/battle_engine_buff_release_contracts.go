package battlecore

import (
	"fmt"
)

// validateEnemyAllBuffReleaseContracts owns the complete ordinary-enemy
// BUFF_RELEASE object graph. Native FUN_00096755 produces action type 8 and
// p0+p1*level chance; the role, outer skill and enemy action remain separate
// target layers. In particular, the old revive skill 37801102 revives ENEMY3
// but its following SELF release still targets the acting enemy.
func validateEnemyAllBuffReleaseContracts(catalog *CombatCatalog) error {

	if err := validateEnemySelectedBuffReleaseAction(catalog); err != nil {
		return err
	}
	return validateEnemyReviveSelfBuffReleaseAction(catalog)
}

func validateEnemySelectedBuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 40018331
	const skillID = 44176005
	definition, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "USER_ONE" || len(roles) != 1 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "BUFF_RELEASE" || roles[0].Target != "SELECT" ||
		action.Slot != 4 || action.Target != "HIGH_ATK_USER" {
		return fmt.Errorf("official selected BUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := retainedGrowthContractEngine(catalog, definition, level, false)
	attacks := [4]int{1000, 1300, 1500, 1200}
	for index := range engine.players {
		engine.players[index].Attack = attacks[index]
		engine.players[index].BaseAttack = attacks[index]
	}
	engine.players[1].BaseAttack = 1100
	engine.players[1].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Delta: 200, Remaining: 2, RoleIndex: 4, Source: 1}}
	engine.players[2].BaseAttack = 1000
	engine.players[2].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "ATK", Kind: 1, Delta: 500, Remaining: 2, RoleIndex: 5, Source: 1}}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam}
	if len(results) != len(wantCommands) || len(engine.players[2].Effects) != 0 || engine.players[2].Attack != 1000 ||
		len(engine.players[1].Effects) != 1 || engine.players[1].Attack != 1300 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 3, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{3, 0, 84, 0, 0, 0, 0, 0}) {
		return fmt.Errorf("official selected BUFF_RELEASE action is results=%+v players=%+v", results, engine.players)
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official selected BUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}

func validateEnemyReviveSelfBuffReleaseAction(catalog *CombatCatalog) error {
	const levelID = 31100121
	const skillID = 37801102
	_, level, action, err := releaseGrowthActionBoundary(catalog, levelID, skillID)
	if err != nil {
		return err
	}
	skill, roles, ok := (&BattleEngine{catalog: catalog}).enemySkillBase(skillID)
	if !ok || skill.Target != "DEAD_ENEMY_ONE" || len(roles) != 2 ||
		roles[0].RoleIndex != 0 || roles[0].Function != "REVIVE" || roles[0].Target != "SELECT" ||
		roles[1].RoleIndex != 1 || roles[1].Function != "BUFF_RELEASE" || roles[1].Target != "SELF" ||
		action.Slot != 3 || action.Target != "ENEMY3" {
		return fmt.Errorf("official revive/SELF BUFF_RELEASE boundary changed: level=%+v action=%+v skill=%+v roles=%+v", level, action, skill, roles)
	}
	engine := &BattleEngine{catalog: catalog, turn: 1, rng: newXorShift128(1), enemyCount: 3}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 10000, MaxHP: 10000, Effects: []battleEffect{
		{Function: "CRITICAL_UP", Kind: 1, Remaining: 2, AppliedTurn: 1, RoleIndex: 4, Source: 5},
		{Function: "BURN", Kind: 2, Remaining: 2, AppliedTurn: 1, RoleIndex: 5, Source: 1},
	}}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 8000, MaxHP: 8000}
	engine.enemies[2] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 7, HP: 0, MaxHP: 9000, Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	wantCommands := []int{CommandSkill, CommandSkillRevive, CommandBuffRelease, 72, CommandBuffLostOne, CommandBattleParam}
	if len(results) != len(wantCommands) || engine.enemies[2].HP != 9000 || engine.enemies[2].Broken ||
		len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Function != "BURN" ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 7, 1, 5, skillID, 0}) ||
		!equalBattleArgs(results[2].Args, []int64{5, 1, 84, 0, 0, 0, 0, 0}) {
		return fmt.Errorf("official revive/SELF BUFF_RELEASE action is results=%+v enemies=%+v", results, engine.enemies[:3])
	}
	for index, command := range wantCommands {
		if results[index].Command != command {
			return fmt.Errorf("official revive/SELF BUFF_RELEASE command %d is %d, want %d", index, results[index].Command, command)
		}
	}
	return nil
}
