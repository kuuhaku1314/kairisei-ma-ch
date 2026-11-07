package battlecore

import (
	"errors"
	"fmt"
)

// executeEnemyPassiveSkill mirrors the one-shot passive traversal in
// battle5_api_start and battle5_api_pvp_start. The native member owns a full
// SkillData copy and executes it after every member's initial parameter rows,
// before the first turn phase. Passive durable effects use buff-list type 1;
// they are not ordinary enemy actions and consume no action budget.
func (engine *BattleEngine) executeEnemyPassiveSkill(actor *battleEnemy) ([]BattleResult, error) {
	if actor == nil || actor.Level.PassiveSkillID == 0 {
		return nil, nil
	}
	skillID := actor.Level.PassiveSkillID
	target := 0
	base, _, _ := engine.enemySkillBase(skillID)
	switch base.Target {
	case "SELF":
		target = actor.MemberType
	case "ENEMY_ONE":
		// JP64037 selects the passive header target before choosing its branch.
		// 322eb draws once from living enemies, including a single candidate.
		candidates := engine.enemyTargetCandidates(false)
		if len(candidates) > 0 {
			target = candidates[int(engine.rng.next()%uint32(len(candidates)))] + 5
		}
	}
	skill, roles, matched := engine.selectEnemySkillBranch(actor, skillID, target)
	// A conditional passive may have no matching branch. Missing definitions
	// still fail; an existing program that misses its condition is a no-op.
	if !matched && len(engine.catalog.Skills[skillID]) != 0 {
		return nil, nil
	}
	if !matched || len(roles) == 0 {
		return nil, fmt.Errorf("enemy passive skill %d is incomplete", skillID)
	}
	skillResult, err := enemySkillResult(actor.MemberType, skillID, target, skill, 1, true)
	if err != nil {
		return nil, err
	}
	rows, err := engine.executeSkillRoleSet(actor.MemberType, target, skill, roles, true, func(role CombatSkillRole) ([]BattleResult, error) {
		role = resolveEnemyActionRole(role, skill, target)
		var roleResults []BattleResult
		var err error
		switch role.Function {
		case "ATTR_HIDE", "ENDURE", "ATTR_CUT_ATTACK":
			roleResults, err = engine.executeEnemyPersistentEffectWithListType(actor, target, role, 1)
		case "REWRITE":
			roleResults = engine.executeEnemyRewriteWithListType(actor, target, role, 1)
		case "ENEMY_AI_TRIGGER_FLAG_SET", "ENEMY_AWAKE_FLAG_SET":
			roleResults, err = engine.executeEnemyRole(actor, target, role, roles)
		default:
			if !enemyPassiveParameterFunction(role.Function) {
				return nil, fmt.Errorf("enemy passive skill %d function %q is unsupported", skillID, role.Function)
			}
			roleResults, err = engine.executeEnemyParameterRoleWithListType(actor, target, role, 1)
		}
		if err != nil {
			return nil, err
		}
		var output []BattleResult
		for _, row := range roleResults {
			if row.Command == CommandPassiveBuff && len(row.Args) >= 3 && row.Args[2] == 1 {
				member := int(row.Args[0])
				if member >= 1 && member <= 4 {
					output = append(output, playerBaseParameterResult(&engine.players[member-1]))
				} else if member >= 5 && member < 5+engine.enemyCount {
					output = append(output, enemyBaseParameterResult(&engine.enemies[member-5], initialParameterLimit(engine.alchemist)))
				}
			}
			output = append(output, row)
		}
		return output, nil
	})
	if err != nil {
		return nil, err
	}
	engine.nativeSkillSerial++
	return append([]BattleResult{skillResult}, rows...), nil
}

func enemyPassiveParameterFunction(function string) bool {
	switch function {
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED",
		"ATK_UP_FIXED_ENEMY", "DEF_UP_FIXED_ENEMY", "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY":
		return true
	}
	return false
}

func supportsEnemyPassiveFunction(function string) bool {
	switch function {
	case "ATTR_HIDE", "ENDURE", "ATTR_CUT_ATTACK", "REWRITE", "ENEMY_AI_TRIGGER_FLAG_SET", "ENEMY_AWAKE_FLAG_SET":
		return true
	}
	return enemyPassiveParameterFunction(function)
}

func ValidateEnemyPassiveDefinitions(catalog *CombatCatalog) error {
	if catalog == nil {
		return errors.New("enemy passive catalog is unavailable")
	}
	references := 0
	for levelID, level := range catalog.EnemyLevels {
		if level.PassiveSkillID == 0 {
			continue
		}
		references++
		variants := catalog.Skills[level.PassiveSkillID]
		if len(variants) == 0 {
			return fmt.Errorf("official enemy level %d references missing passive skill %d", levelID, level.PassiveSkillID)
		}
		for _, variant := range variants {
			if _, ok := combatSkillTargetCode(variant.Target); !ok {
				return fmt.Errorf("official enemy passive skill %d has unsupported SKILL_TARGET %q", level.PassiveSkillID, variant.Target)
			}
			if !SupportsEnemyBranchCondition(variant.BranchCondition) || !SupportsEnemyBranchCondition(variant.BranchCondition2) {
				return fmt.Errorf("official enemy passive skill %d has unsupported branch condition %q/%q", level.PassiveSkillID, variant.BranchCondition, variant.BranchCondition2)
			}
			roles := catalog.SkillRoles[variant.FunctionID]
			if len(roles) == 0 {
				return fmt.Errorf("official enemy passive skill %d function %d has no roles", level.PassiveSkillID, variant.FunctionID)
			}
			for _, role := range roles {
				if !supportsEnemyPassiveFunction(role.Function) {
					return fmt.Errorf("official enemy passive skill %d function %d uses unsupported role %q", level.PassiveSkillID, variant.FunctionID, role.Function)
				}
			}
		}
	}
	if references == 0 {
		return errors.New("official enemy levels contain no passive-skill references")
	}
	return nil
}

func validateEnemyPassiveSimulation(catalog *CombatCatalog) error {
	engine := &BattleEngine{catalog: catalog, phase: PhaseCreated, enemyCount: 1, rng: newXorShift128(1)}
	for index := range engine.players {
		engine.players[index] = battlePlayer{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: index + 1, HP: 100, MaxHP: 100, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	engine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL",
		MemberType: 5, HP: 1000, MaxHP: 1000,
		LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999,
		Level: CombatEnemyLevel{PassiveSkillID: 36001401},
	}
	results, err := engine.Start()
	if err != nil {
		return err
	}
	if len(results) < 4 {
		return errors.New("enemy passive start emitted no result rows")
	}
	passiveAt := -1
	for index, row := range results {
		if row.Command == CommandSkill && len(row.Args) > 1 && row.Args[1] == 36001401 {
			passiveAt = index
			break
		}
	}
	if passiveAt < 0 || passiveAt+3 >= len(results) || results[passiveAt+3].Command != CommandBaseParam || results[passiveAt+3].Args[0] != 1 {
		return fmt.Errorf("JP enemy passive must precede initial member parameters: %+v", results)
	}
	skillResult := results[passiveAt]
	baseResult := results[passiveAt+1]
	buffResult := results[passiveAt+2]
	var paramResult BattleResult
	for _, row := range results[passiveAt+3:] {
		if row.Command == CommandBattleParam && len(row.Args) > 0 && row.Args[0] == 5 {
			paramResult = row
			break
		}
	}
	if skillResult.Command != CommandSkill || len(skillResult.Args) != 7 || skillResult.Args[0] != 5 || skillResult.Args[1] != 36001401 ||
		skillResult.Args[2] != 5 || skillResult.Args[3] != 1 || skillResult.Args[4] != 0 || skillResult.Args[5] != 36001401 || skillResult.Args[6] != 1 {
		return fmt.Errorf("enemy passive skill projection is %+v", skillResult)
	}
	if baseResult.Command != 5 || !equalBattleArgs(baseResult.Args, []int64{5, 1000, 0, 0, 0, 0, 0, 99999, 99999, 99999}) {
		return fmt.Errorf("enemy passive base parameters are %+v", baseResult)
	}
	if buffResult.Command != CommandPassiveBuff || len(buffResult.Args) != 11 || buffResult.Args[0] != 5 || buffResult.Args[2] != 1 || buffResult.Args[3] != int64(battleBuffCodes["ENDURE"]) {
		return fmt.Errorf("enemy passive buff projection is %+v", buffResult)
	}
	if paramResult.Command != CommandBattleParam || !equalBattleArgs(paramResult.Args, battleParameterArgs(5, 1000, 1000, 0, 0, 0, 0, 0, 99999, 99999, 99999)) {
		return fmt.Errorf("enemy passive parameter projection is %+v", paramResult)
	}
	if len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].Function != "ENDURE" || engine.enemies[0].Effects[0].ListType != 1 || engine.enemies[0].Effects[0].Value != 1 || engine.enemies[0].Effects[0].Remaining != 99 || engine.enemies[0].Effects[0].SourceSkillID != 36001401 {
		return fmt.Errorf("enemy passive durable state is %+v", engine.enemies[0].Effects)
	}
	flagEngine := &BattleEngine{catalog: catalog, enemyCount: 1, rng: newXorShift128(1)}
	flagEngine.enemies[0] = battleEnemy{BaseAttribute: "NEUTRAL", Attribute: "NEUTRAL", MemberType: 5, HP: 1000, MaxHP: 1000, Level: CombatEnemyLevel{PassiveSkillID: 35001327}}
	flagResults, err := flagEngine.executeEnemyPassiveSkill(&flagEngine.enemies[0])
	if err != nil {
		return err
	}
	if len(flagResults) != 1 || flagResults[0].Command != CommandSkill || len(flagResults[0].Args) != 7 || flagResults[0].Args[4] != 3 ||
		!flagEngine.enemies[0].hasAIFlag(1) || !flagEngine.enemies[0].hasAIFlag(2) {
		return fmt.Errorf("enemy passive AI flags are results=%+v flags=%b", flagResults, flagEngine.enemies[0].AIFlags)
	}
	return nil
}
