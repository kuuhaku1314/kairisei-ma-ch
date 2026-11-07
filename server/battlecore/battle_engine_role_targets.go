package battlecore

// 8b660 applies the role's attribute bitset and source exclusion AFTER target
// selection, before invoking a consumer. Rejection produces no failure row,
// mutation or extra target-selection/consumer RNG.
func combatRoleAllowsTarget(role CombatSkillRole, source, target int, attribute string) bool {
	if role.ExcludeSelf && source == target {
		return false
	}
	var mask uint32
	for i, allowed := range role.Attributes {
		if allowed {
			mask |= 1 << uint(i+1)
		}
	}
	if !role.HasTargetAttributes && mask == 0 {
		return true // A programmatic role did not specify an attribute rule.
	}
	code := combatAttributeCode(attribute)
	if parts := splitCombatAttributes(attribute); len(parts) == 2 {
		// The native consumer shifts by the raw ATTR enum (x86 low five bits),
		// not an OR of its components. Members normally have a single ATTR.
		code = 100*combatAttributeCode(parts[0]) + combatAttributeCode(parts[1])
	}
	return mask&(1<<uint(code&31)) != 0
}

func (engine *BattleEngine) filterRolePlayers(targets []int, source int, role CombatSkillRole) []int {
	kept := targets[:0]
	for _, member := range targets {
		if combatRoleAllowsTarget(role, source, member, engine.player(member).Attribute) {
			kept = append(kept, member)
		}
	}
	return kept
}

func (engine *BattleEngine) filterRoleEnemies(targets []int, source int, role CombatSkillRole) []int {
	kept := targets[:0]
	for _, index := range targets {
		enemy := &engine.enemies[index]
		if combatRoleAllowsTarget(role, source, enemy.MemberType, enemy.Attribute) {
			kept = append(kept, index)
		}
	}
	return kept
}

func (engine *BattleEngine) playerRoleTargets(action battleAction, role CombatSkillRole) []int {
	return engine.filterRolePlayers(engine.playerRoleTargetCandidates(action, role), action.memberType, role)
}

func (engine *BattleEngine) playerRoleTargetCandidates(action battleAction, role CombatSkillRole) []int {
	if targets, ok := engine.skillRoleTargets(action.memberType, role, true); ok {
		return targets
	}
	return engine.playerTargets(role.Target, action.memberType, action.target)
}

func (engine *BattleEngine) playerRoleEnemyTargets(action battleAction, role CombatSkillRole) []int {
	if engine.pvp {
		return nil
	}
	if targets, ok := engine.skillRoleTargets(action.memberType, role, false); ok {
		return engine.filterRoleEnemies(targets, action.memberType, role)
	}
	return engine.filterRoleEnemies(engine.enemyTargets(role.Target, action.target), action.memberType, role)
}

// Role targets22..25 name absolute monster slots, independent of the outer
// skill/AI target. Missing or KO slots have no fallback and consume no RNG.
func (engine *BattleEngine) fixedEnemyRoleTargets(kind string) ([]int, bool) {
	var index int
	switch kind {
	case "ENEMY1":
		index = 0
	case "ENEMY2":
		index = 1
	case "ENEMY3":
		index = 2
	case "ENEMY4":
		index = 3
	default:
		return nil, false
	}
	if index >= engine.enemyCount || engine.enemies[index].HP <= 0 {
		return nil, true
	}
	return []int{index}, true
}
