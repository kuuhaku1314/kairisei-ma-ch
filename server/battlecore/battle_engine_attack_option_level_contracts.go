package battlecore

import "fmt"

// validateEnemyAttackOptionLevelContracts binds the two attack-option
// consumers with active enemy level growth to the managed level-one skill
// object. Native DRAIN and REVENGE both evaluate p0+p1*level, but their
// downstream consumers differ: DRAIN heals from actual committed damage while
// REVENGE adds a capped value derived from cumulative damage taken.
func validateEnemyAttackOptionLevelContracts(catalog *CombatCatalog) error {

	for _, roles := range catalog.EnemyRoleSets() {
		for _, role := range roles {
			switch role.Function {
			case "ATK_OP_PIERCING", "ATK_OP_DAMAGE_INCREASE", "ATK_OP_NOW_TURN_REVENGE":

				if combatParameterInt(role.Parameters[1]) != 0 {
					return fmt.Errorf("official enemy %s unexpectedly has level growth: %+v", role.Function, role)
				}
			case "ATK_OP_DRAIN", "ATK_OP_REVENGE":

				if role.Function == "ATK_OP_DRAIN" {

				} else {

				}

			}
		}
	}

	if err := validateEnemyDrainGrowthAction(catalog); err != nil {
		return err
	}
	return validateEnemyRevengeGrowthAction(catalog)
}

func enemyAttackOptionRole(catalog *CombatCatalog, skillID int, function string) (CombatSkillRole, error) {
	for _, role := range catalog.SkillRoles[skillID] {
		if role.Function == function {
			return role, nil
		}
	}
	return CombatSkillRole{}, fmt.Errorf("official enemy skill role %d/%s is missing", skillID, function)
}

func enemyAttackOptionAction(level CombatEnemyLevel, skillID int) (CombatEnemyAction, error) {
	for _, action := range level.Actions {
		if action.SkillID == skillID {
			return action, nil
		}
	}
	return CombatEnemyAction{}, fmt.Errorf("official enemy level %d action %d is missing", level.ID, skillID)
}

func validateEnemyDrainGrowthAction(catalog *CombatCatalog) error {
	const levelID = 83005081
	const skillID = 36601603
	drain, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_DRAIN")
	if err != nil {
		return err
	}
	attack, err := enemyAttackOptionRole(catalog, skillID, "ATTACK_AA")
	if err != nil {
		return err
	}
	piercing, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_PIERCING")
	if err != nil {
		return err
	}
	if drain.RoleIndex != 1 || drain.Target != "SELECT" || drain.Parameters[0] != "1000" ||
		drain.Parameters[1] != "10000" || drain.Parameters[2] != "0" ||
		attack.RoleIndex != 2 || attack.Target != "SELECT" || attack.Parameters[0] != "40000" ||
		attack.Parameters[1] != "0" || attack.Parameters[2] != "500" || attack.Parameters[3] != "0" ||
		attack.Parameters[4] != "1" || attack.Parameters[5] != "ATK" || attack.Parameters[6] != "0" ||
		attack.Parameters[7] != "LIGHT" || attack.Parameters[8] != "PHYSICS" ||
		piercing.RoleIndex != 0 || piercing.Parameters[0] != "80" || piercing.Parameters[1] != "0" {
		return fmt.Errorf("official enemy DRAIN 36601603 roles changed: piercing=%+v drain=%+v attack=%+v", piercing, drain, attack)
	}
	if zero, one := attackOperatorLevelValue(drain, 0, 1), attackOperatorLevelValue(drain, 1, 1); zero != 1000 || one != 11000 || EnemySkillLevel(drain.Function) != 1 {
		return fmt.Errorf("native enemy DRAIN level values are %d/%d with calibrated level %d, want 1000/11000/1", zero, one, EnemySkillLevel(drain.Function))
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 55000000 || definition.Attack != 10000 || definition.Magic != 5000 ||
		definition.Recovery != 5000 || definition.Defense != 200000 || definition.MagicDefense != 200000 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy DRAIN definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy DRAIN level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 3 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 12 ||
		action.Target != "HIGH_HP_USER" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy DRAIN action changed: %+v", action)
	}
	actorProbe := playerFromEnemy(&battleEnemy{MemberType: 5, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999, Attack: definition.Attack, HP: 1, MaxHP: definition.HP})
	oldModifiers := collectAttackModifiers([]CombatSkillRole{drain}, actorProbe, 0, 1, false)
	newModifiers := collectEnemyAttackModifiers([]CombatSkillRole{drain}, actorProbe, 1)
	if oldModifiers.drainRate != 1000 || newModifiers.drainRate != 11000 || newModifiers.drainRoleIndex != 1 {
		return fmt.Errorf("official enemy DRAIN modifier boundary is old=%+v managed=%+v", oldModifiers, newModifiers)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	playerHP := [4]int{100001, 100002, 100004, 100003}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: playerHP[index], MaxHP: playerHP[index], Attribute: "NEUTRAL"}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: 1, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	// JP publishes the committed target HP before the accumulated drain row.
	if len(results) != 4 || engine.players[2].HP != 55004 || engine.players[2].DamageTaken != 45000 ||
		engine.enemies[0].HP != 4950001 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 3, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{3, 2, -45000, 100004, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{3, 100004, 55004, 1}) ||
		!equalBattleArgs(results[3].Args, []int64{5, 2, 4950000, 4950001}) {
		return fmt.Errorf("official enemy DRAIN level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}

func validateEnemyRevengeGrowthAction(catalog *CombatCatalog) error {
	const levelID = 30880123
	const skillID = 35601113
	revenge, err := enemyAttackOptionRole(catalog, skillID, "ATK_OP_REVENGE")
	if err != nil {
		return err
	}
	attack, err := enemyAttackOptionRole(catalog, skillID, "ATTACK_AA")
	if err != nil {
		return err
	}
	if revenge.RoleIndex != 0 || revenge.Target != "SELF" || revenge.Parameters[0] != "30" ||
		revenge.Parameters[1] != "1" || revenge.Parameters[2] != "ATK" || revenge.Parameters[3] != "30" ||
		attack.RoleIndex != 1 || attack.Target != "SELECT" || attack.Parameters[0] != "1000" ||
		attack.Parameters[1] != "0" || attack.Parameters[2] != "1000" || attack.Parameters[3] != "0" ||
		attack.Parameters[4] != "1" || attack.Parameters[5] != "ATK" || attack.Parameters[6] != "0" ||
		attack.Parameters[7] != "WIND" || attack.Parameters[8] != "PHYSICS" {
		return fmt.Errorf("official enemy REVENGE 35601113 roles changed: revenge=%+v attack=%+v", revenge, attack)
	}
	if zero, one := attackOperatorLevelValue(revenge, 0, 1), attackOperatorLevelValue(revenge, 1, 1); zero != 30 || one != 31 || EnemySkillLevel(revenge.Function) != 1 {
		return fmt.Errorf("native enemy REVENGE level values are %d/%d with calibrated level %d, want 30/31/1", zero, one, EnemySkillLevel(revenge.Function))
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 50000 || definition.Attack != 1000 || definition.Magic != 750 ||
		definition.Recovery != 0 || definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "WIND" {
		return fmt.Errorf("official enemy REVENGE definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy REVENGE level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 1 || action.Category != "skill" || action.AIConditionID != skillID || action.Priority != 13 ||
		action.Target != "RANDOM" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy REVENGE action changed: %+v", action)
	}
	actorProbe := playerFromEnemy(&battleEnemy{MemberType: 5, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999, Attack: definition.Attack, HP: definition.HP, MaxHP: definition.HP})
	oldModifiers := collectAttackModifiers([]CombatSkillRole{revenge}, actorProbe, 0, 1, false)
	newModifiers := collectEnemyAttackModifiers([]CombatSkillRole{revenge}, actorProbe, 1)
	if oldModifiers.revengeRate != 30 || newModifiers.revengeRate != 31 ||
		newModifiers.revengeParameter != "ATK" || newModifiers.revengeCapRate != 30 ||
		attackRevengeBonus(100, actorProbe, oldModifiers.revengeRate, oldModifiers.revengeParameter, oldModifiers.revengeCapRate) != 30 ||
		attackRevengeBonus(100, actorProbe, newModifiers.revengeRate, newModifiers.revengeParameter, newModifiers.revengeCapRate) != 31 {
		return fmt.Errorf("official enemy REVENGE modifier boundary is old=%+v managed=%+v", oldModifiers, newModifiers)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{MemberType: index + 1, HP: 100000, MaxHP: 100000, Attribute: "NEUTRAL"}
	}
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute,
		DamageTaken: 100, Level: level,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 3 || engine.players[3].HP != 97969 || engine.players[3].DamageTaken != 2031 ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 1, skillID, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{4, 1, -2031, 100000, 0, 3, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{4, 100000, 97969, 1}) {
		return fmt.Errorf("official enemy REVENGE level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}
