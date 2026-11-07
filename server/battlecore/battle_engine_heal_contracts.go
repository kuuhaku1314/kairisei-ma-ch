package battlecore

import (
	"errors"
	"fmt"
)

// validateEnemyFixedHealLevelContracts keeps the ordinary-enemy level-one
// proof and the complete official HEAL_FIXED matrix out of the already broad
// cross-family contract audit. Managed EnemyLevelupDataContainer assigns
// ordinary enemy skills level 1, and native FUN_0007adb0 passes that same
// object to HEAL_FIXED producer FUN_000a24ff.
func validateEnemyFixedHealLevelContracts(catalog *CombatCatalog) error {

	var representative *CombatSkillRole
	for index := range catalog.SkillRoles[36901119] {
		role := &catalog.SkillRoles[36901119][index]
		if role.Function == "HEAL_FIXED" {
			representative = role
			break
		}
	}

	if representative == nil {
		return errors.New("official enemy HEAL_FIXED representative 36901119 is missing")
	}
	if representative.Target != "SELECT" || representative.Parameters[0] != "15000" || representative.Parameters[1] != "1000" ||
		representative.Parameters[2] != "1000" || representative.Parameters[3] != "0" || representative.Parameters[4] != "INT" ||
		EnemySkillLevel(representative.Function) != 1 {
		return fmt.Errorf("official enemy HEAL_FIXED representative changed: %+v", *representative)
	}
	if value := fixedHealRoleValue(*representative, 0, 1, 800); value != 15800 {
		return fmt.Errorf("official enemy HEAL_FIXED level-zero control is %d, want 15800", value)
	}
	if value := fixedHealRoleValue(*representative, 1, 1, 800); value != 15801 {
		return fmt.Errorf("official enemy HEAL_FIXED level-one value is %d, want 15801", value)
	}

	level, ok := catalog.EnemyLevels[30990123]
	if !ok {
		return errors.New("official enemy HEAL_FIXED level 30990123 is missing")
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == 36901119 {
			action = candidate
			break
		}
	}
	if level.HPBars != 2 || level.ActionsPerTurn != 20 || action.Slot != 4 || action.Category != "skill" ||
		action.AIConditionID != 36901119 || action.Priority != 6 || action.Target != "ENEMY1" ||
		action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy HEAL_FIXED action changed: level=%+v action=%+v", level, action)
	}

	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 3}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 100000, MaxHP: 450000}
	engine.enemies[1] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 6, HP: 125000, MaxHP: 125000}
	engine.enemies[2] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 7, HP: 100000, MaxHP: 100000, Magic: 800, Level: level}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[2], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 2 || engine.enemies[0].HP != 115801 || engine.enemies[1].HP != 125000 || engine.enemies[2].HP != 100000 ||
		!equalBattleArgs(results[0].Args, []int64{7, 36901119, 5, 1, 3, 36901119, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{5, int64(representative.RoleIndex), 15801, 115801}) {
		return fmt.Errorf("official enemy HEAL_FIXED level-one action is results=%+v enemies=%+v", results, engine.enemies[:3])
	}

	// Party 40084001 establishes 40008411 as the body and 40008412 as its
	// ParentIndex=1 part. Native FUN_000822a0 keeps the requested heal in
	// ResultCmd61 even when the selected part overheals, then commits that same
	// positive amount to the living body. The body does not evaluate its own
	// HEAL_REVERSE and is the only target that receives a ResultCmd3 row.
	engine = &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 3}
	engine.enemies[0] = battleEnemy{BaseAttribute: catalog.Enemies[40008411].Attribute, Attribute: catalog.Enemies[40008411].Attribute,
		MemberType: 5, EnemyID: 40008411, HP: 1000, MaxHP: 100000,
		Effects: []battleEffect{{Function: "HEAL_REVERSE", Rate: 100, Remaining: 2}},
	}
	engine.enemies[1] = battleEnemy{BaseAttribute: catalog.Enemies[40008412].Attribute, Attribute: catalog.Enemies[40008412].Attribute,
		MemberType: 6, EnemyID: 40008412, Parent: 1, HP: 99990, MaxHP: 100000,
	}
	engine.enemies[2] = battleEnemy{BaseAttribute: catalog.Enemies[30990123].Attribute, Attribute: catalog.Enemies[30990123].Attribute, MemberType: 7, EnemyID: 30990123, HP: 100000, MaxHP: 100000, Magic: 800}
	results, err = engine.executeEnemyHeal(&engine.enemies[2], 6, *representative)
	if err != nil {
		return err
	}
	if len(results) != 2 || engine.enemies[1].HP != 100000 || engine.enemies[0].HP != 16801 ||
		len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Remaining != 2 ||
		results[0].Command != 61 || !equalBattleArgs(results[0].Args, []int64{6, int64(representative.RoleIndex), 15801, 100000}) ||
		results[1].Command != CommandHP || !equalBattleArgs(results[1].Args, []int64{5, 100000, 16801, 2}) {
		return fmt.Errorf("official enemy part HEAL_FIXED parent propagation is results=%+v enemies=%+v", results, engine.enemies[:3])
	}
	return nil
}
