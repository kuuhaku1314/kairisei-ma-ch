package battlecore

// Each enemy owns five signed32 counters. JP ADD is ordinal173 and SET206;
// the older CN native ADD ordinal174 remains an adapter fact for that ABI.
func (engine *BattleEngine) executeEnemyAIVariable(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	// These producers operate on the absolute enemy roster. FRIEND_ALL is
	// the player roster here, unlike the source-relative status dispatcher.
	if role.Target == "FRIEND_ALL" {
		return nil
	}
	index := combatParameterInt(role.Parameters[0])
	if index < 0 || index >= 5 {
		return nil
	}
	var results []BattleResult
	includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
	for _, target := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
		enemy := &engine.enemies[target]
		if role.Function == "ENEMY_AI_VAR_SET" {
			enemy.AIVariables[index] = int32(combatParameterInt(role.Parameters[1]))
			continue // Original SET is silent; ADD publishes ResultCmd94.
		}
		enemy.AIVariables[index] += int32(combatParameterInt(role.Parameters[1]))
		args := []int64{int64(enemy.MemberType)}
		for _, value := range enemy.AIVariables {
			args = append(args, int64(value))
		}
		results = append(results, BattleResult{Command: CommandEnemyAIVariables, Args: args})
	}
	return results
}

func enemyAIVariableInRange(enemy *battleEnemy, indexText, lowText, highText string, branch bool) bool {
	index := combatParameterInt(indexText)
	value := int64(-1)
	if index >= 0 && index < len(enemy.AIVariables) {
		value = int64(enemy.AIVariables[index])
	} else if !branch {
		return false // AI trigger rejects invalid indices; branch getter returns -1.
	}
	low, high := int64(combatParameterInt(lowText)), int64(combatParameterInt(highText))
	// Original getExtendExecOrderParam IL_0564 and ParseTrigger IL_0942
	// default EMPTY bounds to int32 limits. Explicit zero is a real bound.
	if lowText == "" {
		low = -2147483648
	}
	if highText == "" {
		high = 2147483647
	}
	return low <= value && value <= high
}
