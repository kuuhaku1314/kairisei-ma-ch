package battlecore

import (
	"fmt"
)

// validateEnemyAttackBarrierLevelContracts binds ordinary ATTACK_BARRIER to
// managed enemy skill level one. The barrier hit/release lifecycle is already
// shared and typed; this contract guards the producer matrix and proves that
// p1+p2*level reaches that existing consumer through a complete official
// outer action.
func validateEnemyAttackBarrierLevelContracts(catalog *CombatCatalog) error {

	return validateEnemyAttackBarrierGrowthAction(catalog)
}

func validateEnemyAttackBarrierGrowthAction(catalog *CombatCatalog) error {
	const levelID = 40029621
	const skillID = 44279009
	roles := catalog.SkillRoles[skillID]
	if len(roles) != 2 || roles[0].Function != "ATK_UP_FIXED" || roles[1].Function != "ATTACK_BARRIER" {
		return fmt.Errorf("official enemy ATTACK_BARRIER role set %d changed: %+v", skillID, roles)
	}
	barrier := roles[1]
	if barrier.RoleIndex != 1 || barrier.Target != "SELF" || barrier.Parameters[0] != "3" ||
		barrier.Parameters[1] != "1" || barrier.Parameters[2] != "1" || barrier.Parameters[3] != "4" ||
		barrier.Parameters[4] != "ALL" || attackBarrierRoleValue(barrier, 0) != 1 ||
		attackBarrierRoleValue(barrier, 1) != 2 || EnemySkillLevel(barrier.Function) != 1 {
		return fmt.Errorf("official enemy ATTACK_BARRIER 44279009 changed: %+v", barrier)
	}
	definition, ok := catalog.Enemies[levelID]
	if !ok || definition.HP != 250000 || definition.Attack != 1200 || definition.Attribute != "WIND" {
		return fmt.Errorf("official enemy ATTACK_BARRIER definition %d changed: %+v", levelID, definition)
	}
	level, ok := catalog.EnemyLevels[levelID]
	if !ok {
		return fmt.Errorf("official enemy ATTACK_BARRIER level %d is missing", levelID)
	}
	action, err := enemyAttackOptionAction(level, skillID)
	if err != nil {
		return err
	}
	if action.Slot != 10 || action.Category != "skill" || action.AIConditionID != 40277008 || action.Priority != 13 ||
		action.Target != "RANDOM" || action.ActionCost != 1 || action.MaxUses != 1000 || action.Rate != 100 || action.CountOnMiss {
		return fmt.Errorf("official enemy ATTACK_BARRIER action changed: %+v", action)
	}
	engine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range engine.players {
		engine.players[index] = battlePlayer{
			MemberType: index + 1, HP: 10000, MaxHP: 10000, Attack: 1000, Magic: 900,
			Recovery: 800, Defense: 700, MDefense: 600, Attribute: "NEUTRAL",
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
	effects := engine.enemies[0].Effects
	if len(results) != 5 || results[0].Command != CommandSkill || results[1].Command != CommandBuff ||
		results[2].Command != CommandBattleParam || results[3].Command != CommandBuff ||
		!equalBattleArgs(results[0].Args, []int64{5, skillID, 4, 1, 4, skillID, 0}) ||
		results[1].Args[0] != 5 || engine.players[3].Attack != 1000 || len(engine.players[3].Effects) != 0 ||
		len(effects) != 2 || effects[0].Function != "ATK_UP_FIXED" || effects[0].Delta != 300 ||
		effects[1].Function != "ATTACK_BARRIER" || effects[1].Value != 2 ||
		effects[1].Uses != 4 || effects[1].Remaining != 3 || effects[1].DamageKind != "ALL" ||
		effects[1].Source != 5 || effects[1].RoleIndex != 1 ||
		!equalBattleArgs(results[3].Args, []int64{5, 1, 0, 205, 4, 0, 1, 2, 4, 0, 0}) ||
		results[4].Command != CommandBattleParam ||
		!equalBattleArgs(results[4].Args, battleParameterArgs(5, 250000, 250000, 1500, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy ATTACK_BARRIER level-one action is results=%+v players=%+v enemy=%+v", results, engine.players, engine.enemies[0])
	}
	absorbed := engine.resolveIncomingDamageEffects(&engine.enemies[0].Effects, 2, engine.enemies[0].HP, "DARK", "MAGIC")
	over := engine.resolveIncomingDamageEffects(&engine.enemies[0].Effects, 3, engine.enemies[0].HP, "LIGHT", "PHYSICS")
	rowsAfterAbsorb := damageEffectResults(5, absorbed)
	rowsAfterOver := damageEffectResults(5, over)
	if absorbed.Damage != 0 || absorbed.BarrierRemainingUses != 3 || len(rowsAfterAbsorb) != 1 ||
		rowsAfterAbsorb[0].Command != 200 || rowsAfterAbsorb[0].Args[2] != 3 ||
		over.Damage != 3 || over.BarrierRemainingUses != 2 || len(rowsAfterOver) != 1 ||
		rowsAfterOver[0].Command != 200 || rowsAfterOver[0].Args[2] != 2 {
		return fmt.Errorf("official enemy ATTACK_BARRIER corrected consumer is absorbed=%+v rows=%+v over=%+v rows=%+v", absorbed, rowsAfterAbsorb, over, rowsAfterOver)
	}
	return nil
}
