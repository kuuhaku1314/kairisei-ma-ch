package battlecore

import (
	"errors"
	"fmt"
)

// validateEnemyAttackLevelContracts binds ordinary-enemy ATTACK_AA to the
// managed level-one skill object and native producer FUN_000a45d2. Only two
// active enemy rows have a nonzero fixed-level coefficient, and integer
// division makes their level-zero and level-one output equal. The exact
// object graph and a larger structural probe therefore guard the level path
// without inventing a corrected live damage difference.
func validateEnemyAttackLevelContracts(catalog *CombatCatalog) error {

	for _, roles := range catalog.EnemyRoleSets() {
		for index := range roles {
			role := &roles[index]
			if role.Function != "ATTACK_AA" {
				continue
			}

			if role.ExcludeSelf || role.Parameters[9] != "" || combatParameterInt(role.Parameters[0]) < 0 ||
				combatParameterInt(role.Parameters[2]) < 0 || combatParameterInt(role.Parameters[4]) < 1 {
				return fmt.Errorf("official enemy ATTACK_AA shape changed: %+v", *role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy ATTACK_AA skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
		}
	}

	physicalRole, err := enemyAttackGrowthRole(catalog, 44202203, "ATK", "ICE", "PHYSICS")
	if err != nil {
		return err
	}
	magicRole, err := enemyAttackGrowthRole(catalog, 44325402, "INT", "LIGHT", "MAGIC")
	if err != nil {
		return err
	}
	if EnemySkillLevel(physicalRole.Function) != 1 || EnemySkillLevel(magicRole.Function) != 1 {
		return errors.New("official enemy ATTACK_AA is not bound to managed level one")
	}
	if zero, one, structural := attackRolePower(physicalRole, 0, 10000), attackRolePower(physicalRole, 1, 10000), attackRolePower(physicalRole, 200, 10000); zero != 33000 || one != 33000 || structural != 33001 {
		return fmt.Errorf("native physical ATTACK_AA level values are %d/%d/%d, want 33000/33000/33001", zero, one, structural)
	}
	sourceProbe := physicalRole
	sourceProbe.Parameters[3] = "1"
	if zero, one := attackRolePower(sourceProbe, 0, 10000), attackRolePower(sourceProbe, 1, 10000); zero != 33000 || one != 33010 {
		return fmt.Errorf("native ATTACK_AA source-growth structural probe is %d/%d, want 33000/33010", zero, one)
	}

	if err := validateEnemyAttackGrowthAction(catalog, 40021451, 44202203, 40200203, 7, physicalRole, 10000, 0, 1000, 2000, 32000, 4); err != nil {
		return err
	}
	if err := validateEnemyAttackGrowthAction(catalog, 40032551, 44325402, 40325402, 5, magicRole, 0, 10000, 1000, 2000, 31000, 4); err != nil {
		return err
	}
	return nil
}

func enemyAttackGrowthRole(catalog *CombatCatalog, skillID int, source string, attribute string, physics string) (CombatSkillRole, error) {
	roles := catalog.SkillRoles[skillID]
	if len(roles) != 1 || roles[0].Function != "ATTACK_AA" {
		return CombatSkillRole{}, fmt.Errorf("official enemy ATTACK_AA growth role %d changed: %+v", skillID, roles)
	}
	role := roles[0]
	if role.RoleIndex != 0 || role.Target != "SELECT" || role.Parameters[0] != "23000" || role.Parameters[1] != "5" ||
		role.Parameters[2] != "1000" || role.Parameters[3] != "0" || role.Parameters[4] != "1" ||
		role.Parameters[5] != source || role.Parameters[6] != "0" || role.Parameters[7] != attribute || role.Parameters[8] != physics {
		return CombatSkillRole{}, fmt.Errorf("official enemy ATTACK_AA growth role %d parameters changed: %+v", skillID, role)
	}
	return role, nil
}

func validateEnemyAttackGrowthAction(catalog *CombatCatalog, levelID int, skillID int, conditionID int, actionCost int, role CombatSkillRole, attack int, magic int, defense int, magicDefense int, expectedDamage int, expectedTarget int) error {
	definition, ok := catalog.Enemies[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_AA definition %d is missing", levelID)
	}
	if definition.Attack != attack || definition.Magic != magic {
		return fmt.Errorf("official enemy ATTACK_AA definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_AA level %d is missing", levelID)
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == skillID {
			action = candidate
			break
		}
	}
	if action.Slot != 2 || action.Category != "skill" || action.AIConditionID != conditionID || action.Priority != 2 ||
		action.Target != "RANDOM" || action.ActionCost != actionCost || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy ATTACK_AA action %d changed: level=%+v action=%+v", skillID, level, action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, HP: 100000, MaxHP: 100000, Attribute: "NEUTRAL",
			Defense: defense, MDefense: magicDefense,
		}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 9 || !equalBattleArgs(results[0].Args, []int64{5, int64(skillID), int64(expectedTarget), 1, 2, int64(skillID), 0}) {
		return fmt.Errorf("official enemy ATTACK_AA all-target header %d is %+v", skillID, results)
	}
	for index := range engine.players {
		target := &engine.players[index]
		damage, hp := results[1+index*2], results[2+index*2]
		if target.HP != 100000-expectedDamage || target.DamageTaken != expectedDamage ||
			damage.Command != 60 || damage.Args[0] != int64(index+1) || damage.Args[1] != int64(role.RoleIndex) ||
			damage.Args[2] != int64(-expectedDamage) || damage.Args[3] != 100000 || damage.Args[7] != 0 || damage.Args[9] != 5 ||
			!equalBattleArgs(hp.Args, []int64{int64(index + 1), 100000, int64(100000 - expectedDamage), 1}) {
			return fmt.Errorf("official enemy ATTACK_AA action %d is results=%+v target=%+v", skillID, results, *target)
		}
	}
	return nil
}
