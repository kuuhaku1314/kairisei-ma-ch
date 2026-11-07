package battlecore

import (
	"errors"
	"fmt"
)

// validateEnemyReviveLevelContracts separates the full official role-57
// producer level from the broader death lifecycle audit. MaxHP clamping can
// hide growth-column errors, so the producer segments are checked independently.
func validateEnemyReviveLevelContracts(catalog *CombatCatalog) error {

	roles := catalog.SkillRoles[44168001]
	if len(roles) != 1 || roles[0].Function != "REVIVE" {
		return fmt.Errorf("official enemy REVIVE representative roles changed: %+v", roles)
	}
	representative := roles[0]
	if representative.RoleIndex != 0 || representative.Target != "SELECT" || representative.Parameters[0] != "1000" ||
		representative.Parameters[1] != "1000" || representative.Parameters[2] != "1" ||
		EnemySkillLevel(representative.Function) != 1 {
		return fmt.Errorf("official enemy REVIVE representative changed: %+v", representative)
	}
	if value := reviveRoleValue(representative, 0, 9000); value != 9000 {
		return fmt.Errorf("official enemy REVIVE level-zero control is %d, want 9000", value)
	}
	if value := reviveRoleValue(representative, 1, 9000); value != 18001 {
		return fmt.Errorf("official enemy REVIVE level-one requested value is %d, want 18001", value)
	}
	segmentProbe := representative
	segmentProbe.Parameters[0] = "600"
	segmentProbe.Parameters[1] = "25"
	segmentProbe.Parameters[2] = "1"
	if zero, one := reviveRoleValue(segmentProbe, 0, 1000), reviveRoleValue(segmentProbe, 1, 1000); zero != 600 || one != 626 {
		return fmt.Errorf("native enemy REVIVE segment probe is level0=%d level1=%d, want 600/626", zero, one)
	}

	actorDefinition, ok := catalog.Enemies[40017011]
	if !ok || actorDefinition.HP != 80000 || actorDefinition.Attack != 0 || actorDefinition.Magic != 1000 ||
		actorDefinition.Recovery != 100 || actorDefinition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVIVE actor definition changed: %+v", actorDefinition)
	}
	targetDefinition, ok := catalog.Enemies[40017012]
	if !ok || targetDefinition.HP != 9000 || targetDefinition.Recovery != 500 || targetDefinition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVIVE target definition changed: %+v", targetDefinition)
	}
	level, ok := catalog.EnemyLevels[40017011]
	if !ok {
		return errors.New("official enemy REVIVE level 40017011 is missing")
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == 44168001 {
			action = candidate
			break
		}
	}
	if level.HPBars != 2 || level.ActionsPerTurn != 4 || action.Slot != 1 || action.Category != "skill" ||
		action.AIConditionID != 40166001 || action.Priority != 1 || action.Target != "ENEMY2" ||
		action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy REVIVE action changed: level=%+v action=%+v", level, action)
	}

	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 2}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: actorDefinition.ID, HP: actorDefinition.HP, MaxHP: actorDefinition.HP,
		Attack: actorDefinition.Attack, Magic: actorDefinition.Magic, Recovery: actorDefinition.Recovery,
		Defense: actorDefinition.Defense, MDefense: actorDefinition.MagicDefense, Attribute: actorDefinition.Attribute,
		Level: level,
	}
	engine.enemies[1] = battleEnemy{BaseAttribute: targetDefinition.Attribute, Attribute: targetDefinition.Attribute,
		MemberType: 6, EnemyID: targetDefinition.ID, HP: 0, MaxHP: targetDefinition.HP,
		Broken: true, DeathActionTriggered: true, DiedTurn: 2, DeathCount: 1,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	target := &engine.enemies[1]
	if len(results) != 2 || target.HP != 9000 || target.Broken || target.DeathActionTriggered || target.DiedTurn != 0 || target.DeathCount != 1 ||
		!equalBattleArgs(results[0].Args, []int64{5, 44168001, 6, 1, 5, 44168001, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{6, 0, 18001, 9000}) {
		return fmt.Errorf("official enemy REVIVE level-one action is results=%+v target=%+v", results, *target)
	}
	return nil
}
