package battlecore

import (
	"errors"
	"fmt"
)

// validateEnemyFixedRegenerateLevelContracts binds both ordinary-enemy
// REGENERATE_FIXED growth columns to managed level 1 and native role-16
// producer FUN_000a1e00. The complete official matrix has only two growing
// rows, but they exercise different segments: fixed p2 and source-rate p4.
func validateEnemyFixedRegenerateLevelContracts(catalog *CombatCatalog) error {

	fixedRoles := catalog.SkillRoles[35401163]
	if len(fixedRoles) != 1 || fixedRoles[0].Function != "REGENERATE_FIXED" {
		return fmt.Errorf("official enemy fixed-growth regenerate roles changed: %+v", fixedRoles)
	}
	fixedRole := fixedRoles[0]
	if fixedRole.RoleIndex != 0 || fixedRole.Target != "SELECT" ||
		fixedRole.Parameters[0] != "3" || fixedRole.Parameters[1] != "700" || fixedRole.Parameters[2] != "1000" ||
		fixedRole.Parameters[3] != "0" || fixedRole.Parameters[4] != "0" || fixedRole.Parameters[5] != "MND" ||
		EnemySkillLevel(fixedRole.Function) != 1 {
		return fmt.Errorf("official enemy fixed-growth regenerate representative changed: %+v", fixedRole)
	}
	if zero, one := fixedRegenerateRoleValue(fixedRole, 0, 1, 1000), fixedRegenerateRoleValue(fixedRole, 1, 1, 1000); zero != 700 || one != 701 {
		return fmt.Errorf("native enemy fixed-growth regenerate is level0=%d level1=%d, want 700/701", zero, one)
	}

	sourceRoles := catalog.SkillRoles[45004103]
	var sourceRole *CombatSkillRole
	for index := range sourceRoles {
		if sourceRoles[index].Function == "REGENERATE_FIXED" {
			sourceRole = &sourceRoles[index]
			break
		}
	}
	if sourceRole == nil || sourceRole.RoleIndex != 1 || sourceRole.Target != "SELF" ||
		sourceRole.Parameters[0] != "3" || sourceRole.Parameters[1] != "100000" || sourceRole.Parameters[2] != "0" ||
		sourceRole.Parameters[3] != "0" || sourceRole.Parameters[4] != "30000" || sourceRole.Parameters[5] != "MND" {
		return fmt.Errorf("official enemy source-growth regenerate representative changed: %+v", sourceRole)
	}
	if zero, one := fixedRegenerateRoleValue(*sourceRole, 0, 1, 1000), fixedRegenerateRoleValue(*sourceRole, 1, 1, 1000); zero != 100000 || one != 130000 {
		return fmt.Errorf("native enemy source-growth regenerate is level0=%d level1=%d, want 100000/130000", zero, one)
	}

	fixedDefinition, ok := catalog.Enemies[30860124]
	if !ok || fixedDefinition.HP != 15000 || fixedDefinition.Attack != 0 || fixedDefinition.Magic != 0 ||
		fixedDefinition.Recovery != 1000 || fixedDefinition.Attribute != "FIRE" {
		return fmt.Errorf("official enemy fixed-growth regenerate definition changed: %+v", fixedDefinition)
	}
	fixedLevel, ok := catalog.EnemyLevels[30860124]
	if !ok {
		return errors.New("official enemy fixed-growth regenerate level 30860124 is missing")
	}
	var fixedAction CombatEnemyAction
	for _, candidate := range fixedLevel.Actions {
		if candidate.SkillID == 35401160 {
			fixedAction = candidate
			break
		}
	}
	if fixedLevel.HPBars != 2 || fixedLevel.ActionsPerTurn != 1 || fixedAction.Slot != 4 || fixedAction.Category != "skill" ||
		fixedAction.AIConditionID != 35401160 || fixedAction.Priority != 6 || fixedAction.Target != "RANDOM" ||
		fixedAction.ActionCost != 0 || fixedAction.MaxUses != 1000 || fixedAction.Rate != 100 || fixedAction.CountOnMiss {
		return fmt.Errorf("official enemy fixed-growth regenerate action changed: level=%+v action=%+v", fixedLevel, fixedAction)
	}
	fixedEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(4), turn: 1, enemyCount: 1}
	for index := range fixedEngine.players {
		fixedEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 5000, MaxHP: 5000}
	}
	fixedEngine.players[2].HP = 500
	fixedEngine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: fixedDefinition.ID, HP: fixedDefinition.HP, MaxHP: fixedDefinition.HP,
		Attack: fixedDefinition.Attack, Magic: fixedDefinition.Magic, Recovery: fixedDefinition.Recovery,
		Defense: fixedDefinition.Defense, MDefense: fixedDefinition.MagicDefense, Attribute: fixedDefinition.Attribute,
		Level: fixedLevel,
	}
	fixedResults, err := fixedEngine.executeEnemyActionCandidate(&fixedEngine.enemies[0], enemyActionCandidate{action: fixedAction})
	if err != nil {
		return err
	}
	if len(fixedResults) != 9 || len(fixedEngine.players[2].Effects) != 1 ||
		fixedEngine.players[2].Effects[0].Function != "REGENERATE_FIXED" || fixedEngine.players[2].Effects[0].Value != 701 ||
		fixedEngine.players[2].Effects[0].Remaining != 3 || fixedEngine.players[2].Effects[0].Source != 5 ||
		!equalBattleArgs(fixedResults[0].Args, []int64{5, 35401160, 3, 1, 2, 35401163, 0}) ||
		!equalBattleArgs(fixedResults[5].Args, []int64{3, 0, 0, 200, 4, 0, 1, 0, 0, 0, 0}) ||
		fixedResults[6].Command != CommandBattleParam ||
		!equalBattleArgs(fixedResults[6].Args, battleParameterArgs(3, 500, 5000, 0, 0, 0, 0, 0, 0, 0, 0)) {
		return fmt.Errorf("official enemy fixed-growth regenerate application is results=%+v player=%+v", fixedResults, fixedEngine.players[2])
	}
	for index, player := range fixedEngine.players {
		if len(player.Effects) != 1 || player.Effects[0].Value != 701 ||
			!equalBattleArgs(fixedResults[1+2*index].Args, []int64{int64(index + 1), 0, 0, 200, 4, 0, 1, 0, 0, 0, 0}) ||
			fixedResults[2+2*index].Command != CommandBattleParam {
			return fmt.Errorf("official all-target regenerate lost member %d: %+v", index+1, fixedResults)
		}
	}
	fixedEngine.enemies[0].Recovery = 0
	fixedEngine.turn = 2
	fixedTick, err := expireForEffectContract(fixedEngine)
	if err != nil {
		return err
	}
	fixedTick = append(fixedTick, fixedEngine.regenerateMembers()...)
	if len(fixedTick) != 8 || fixedTick[4].Command != 53 || fixedEngine.players[2].HP != 1201 || fixedEngine.players[2].Effects[0].Remaining != 2 ||
		!equalBattleArgs(fixedTick[5].Args, []int64{3, 0, 701, 1201}) {
		return fmt.Errorf("official enemy fixed-growth regenerate tick is results=%+v player=%+v", fixedTick, fixedEngine.players[2])
	}

	sourceDefinition, ok := catalog.Enemies[41000401]
	if !ok || sourceDefinition.HP != 2300000 || sourceDefinition.Attack != 5000 || sourceDefinition.Magic != 5000 ||
		sourceDefinition.Recovery != 0 || sourceDefinition.Attribute != "DARK" {
		return fmt.Errorf("official enemy source-growth regenerate definition changed: %+v", sourceDefinition)
	}
	sourceLevel, ok := catalog.EnemyLevels[41000401]
	if !ok {
		return errors.New("official enemy source-growth regenerate level 41000401 is missing")
	}
	var sourceAction CombatEnemyAction
	for _, candidate := range sourceLevel.Actions {
		if candidate.SkillID == 45004103 {
			sourceAction = candidate
			break
		}
	}
	if sourceLevel.HPBars != 5 || sourceLevel.ActionsPerTurn != 1 || sourceAction.Slot != 2 || sourceAction.Category != "skill" ||
		sourceAction.AIConditionID != 45004103 || sourceAction.Priority != 7 || sourceAction.Target != "RANDOM" ||
		sourceAction.ActionCost != 0 || sourceAction.MaxUses != 1000 || sourceAction.Rate != 100 || sourceAction.CountOnMiss {
		return fmt.Errorf("official enemy source-growth regenerate action changed: level=%+v action=%+v", sourceLevel, sourceAction)
	}
	sourceEngine := &BattleEngine{catalog: catalog, rng: newXorShift128(1), turn: 1, enemyCount: 1}
	for index := range sourceEngine.players {
		sourceEngine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 50000, MaxHP: 50000}
	}
	sourceEngine.enemies[0] = battleEnemy{
		MemberType: 5, EnemyID: sourceDefinition.ID, HP: 1000000, MaxHP: sourceDefinition.HP,
		Attack: sourceDefinition.Attack, Magic: sourceDefinition.Magic, Recovery: 1000,
		Defense: sourceDefinition.Defense, MDefense: sourceDefinition.MagicDefense, Attribute: sourceDefinition.Attribute,
		Level: sourceLevel,
	}
	sourceResults, err := sourceEngine.executeEnemyActionCandidate(&sourceEngine.enemies[0], enemyActionCandidate{action: sourceAction})
	if err != nil {
		return err
	}
	if len(sourceResults) != 5 || sourceEngine.players[3].HP != 41500 || len(sourceEngine.enemies[0].Effects) != 1 ||
		sourceEngine.enemies[0].Effects[0].Function != "REGENERATE_FIXED" || sourceEngine.enemies[0].Effects[0].Value != 130000 ||
		sourceEngine.enemies[0].Effects[0].Remaining != 3 || sourceEngine.enemies[0].Effects[0].Source != 5 ||
		!equalBattleArgs(sourceResults[0].Args, []int64{5, 45004103, 4, 1, 1, 45004103, 0}) ||
		sourceResults[1].Command != 60 || sourceResults[2].Command != CommandHP ||
		!equalBattleArgs(sourceResults[3].Args, []int64{5, 1, 0, 200, 4, 0, 1, 0, 0, 0, 0}) ||
		sourceResults[4].Command != CommandBattleParam ||
		!equalBattleArgs(sourceResults[4].Args, battleParameterArgs(5, 1000000, 2300000, 5000, 5000, 1000,
			sourceDefinition.Defense, sourceDefinition.MagicDefense, 0, 0, 0)) {
		return fmt.Errorf("official enemy source-growth regenerate action is results=%+v enemy=%+v player=%+v", sourceResults, sourceEngine.enemies[0], sourceEngine.players[3])
	}
	sourceEngine.enemies[0].Recovery = 0
	sourceEngine.turn = 2
	sourceTick, err := expireForEffectContract(sourceEngine)
	if err != nil {
		return err
	}
	sourceTick = append(sourceTick, sourceEngine.regenerateMembers()...)
	if len(sourceTick) != 2 || sourceTick[0].Command != 53 || sourceEngine.enemies[0].HP != 1130000 || sourceEngine.enemies[0].Effects[0].Remaining != 2 ||
		!equalBattleArgs(sourceTick[1].Args, []int64{5, 0, 130000, 1130000}) {
		return fmt.Errorf("official enemy source-growth regenerate tick is results=%+v enemy=%+v", sourceTick, sourceEngine.enemies[0])
	}
	return nil
}
