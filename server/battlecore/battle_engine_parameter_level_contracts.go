package battlecore

import (
	"errors"
	"fmt"
)

// validateEnemySelfParameterLevelContracts owns the ordinary-enemy level-one
// proof for native roles 17/23/30. FUN_000a1618/FUN_000a12e9/FUN_000a0c8b
// all cap the acting member's p2 source by positive p6, multiply it by
// p3+p4*level per mille, and add the independent p5*level segment. Managed
// EnemyLevelupDataContainer supplies level 1 to the same skill object consumed
// by these producers.
func validateEnemySelfParameterLevelContracts(catalog *CombatCatalog) error {
	for _, roles := range catalog.EnemyRoleSets() {
		for index := range roles {
			role := &roles[index]
			switch role.Function {
			case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM":
			default:
				continue
			}
			if role.ExcludeSelf || role.ChainRate != 0 || role.Parameters[9] != "" {
				return fmt.Errorf("official enemy self-parameter shape changed: %+v", *role)
			}
			for attribute, enabled := range role.Attributes {
				if !enabled {
					return fmt.Errorf("official enemy self-parameter skill %d disabled attribute %d", role.SkillID, attribute)
				}
			}
			if role.Parameters[7] != "" || role.Parameters[8] != "" {
				if role.Function != "ATK_UP_BY_SELF_PARAM" || role.Parameters[5] != "INT" ||
					role.Parameters[7] != "ICE" || role.Parameters[8] != "MAGIC" {
					return fmt.Errorf("official enemy self-parameter legacy tail changed: %+v", *role)
				}

			}
		}
	}

	roles := catalog.SkillRoles[44169204]
	if len(roles) != 2 || roles[0].Function != "ATTACK_AA" || roles[1].Function != "ATK_UP_BY_SELF_PARAM" {
		return fmt.Errorf("official enemy self-parameter representative roles changed: %+v", roles)
	}
	representative := roles[1]
	if representative.RoleIndex != 1 || representative.Target != "SELF" || representative.Parameters[0] != "99" ||
		representative.Parameters[1] != "ATK" || representative.Parameters[2] != "MND" ||
		representative.Parameters[3] != "1000" || representative.Parameters[4] != "1" ||
		representative.Parameters[5] != "0" || representative.Parameters[6] != "0" ||
		EnemySkillLevel(representative.Function) != 1 {
		return fmt.Errorf("official enemy self-parameter representative changed: %+v", representative)
	}
	if value := selfScaledParameterValue(representative, 0, 1, 1500); value != 1500 {
		return fmt.Errorf("official enemy self-parameter level-zero control is %d, want 1500", value)
	}
	if value := selfScaledParameterValue(representative, 1, 1, 1500); value != 1501 {
		return fmt.Errorf("official enemy self-parameter level-one value is %d, want 1501", value)
	}

	definition, ok := catalog.Enemies[40017131]
	if !ok || definition.HP != 420000 || definition.Attack != 2000 || definition.Magic != 0 ||
		definition.Recovery != 1500 || definition.Defense != 0 || definition.MagicDefense != 0 || definition.Attribute != "LIGHT" {
		return fmt.Errorf("official enemy self-parameter definition changed: %+v", definition)
	}
	level, ok := catalog.EnemyLevels[40017131]
	if !ok {
		return errors.New("official enemy self-parameter level 40017131 is missing")
	}
	var action CombatEnemyAction
	for _, candidate := range level.Actions {
		if candidate.SkillID == 44169204 {
			action = candidate
			break
		}
	}
	if level.HPBars != 4 || level.ActionsPerTurn != 3 || action.Slot != 4 || action.Category != "skill" ||
		action.AIConditionID != 40167202 || action.Priority != 4 || action.Target != "LOW_HP_USER" ||
		action.ActionCost != 3 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy self-parameter action changed: level=%+v action=%+v", level, action)
	}

	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 20000, MaxHP: 20000}
	}
	engine.players[0].HP = 10000
	engine.players[0].MaxHP = 10000
	engine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: definition.ID, HP: definition.HP, MaxHP: definition.HP,
		Attack: definition.Attack, Magic: definition.Magic, Recovery: definition.Recovery,
		Defense: definition.Defense, MDefense: definition.MagicDefense, Attribute: definition.Attribute, Level: level,
		LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
	}
	results, err := engine.executeEnemyActionCandidate(&engine.enemies[0], enemyActionCandidate{action: action})
	if err != nil {
		return err
	}
	if len(results) != 5 || engine.players[0].HP != 8000 || engine.enemies[0].Attack != 3501 ||
		!equalBattleArgs(results[0].Args, []int64{5, 44169204, 1, 1, 1, 44169204, 0}) ||
		!equalBattleArgs(results[1].Args, []int64{1, 0, -2000, 10000, 0, 4, 100, 0, 0, 5}) ||
		!equalBattleArgs(results[2].Args, []int64{1, 10000, 8000, 1}) ||
		!equalBattleArgs(results[3].Args, []int64{5, 1, 0, 1, 1, 4, 1, 0, 0, 0, 0}) ||
		!equalBattleArgs(results[4].Args, []int64{5, 420000, 420000, 3501, 0, 1500, 0, 0, 99999, 99999, 99999}) {
		return fmt.Errorf("official enemy self-parameter level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	return nil
}
