package battlecore

type battleSkillTargets struct {
	source           int
	kind             string
	members          []int
	attackDamage     [4][10][3]int64
	attackCount      [5]uint32
	randomHandCounts map[int]int
}

// 7adb0 resolves the outer target list once. 8b660 reuses that list for
// SELECT and a role whose target kind equals the outer kind; other kinds
// still select from current state. Attribute/exclusion filters remain live.
func (engine *BattleEngine) executeSkillRoleSet(source, selected int, skill CombatSkillDefinition, roles []CombatSkillRole, passiveCard bool, consume func(CombatSkillRole) ([]BattleResult, error)) ([]BattleResult, error) {
	kind := skill.Target
	previous := engine.skillTargets
	context := &battleSkillTargets{source: source, kind: kind}
	switch kind {
	case "SELF", "HAND_SELECT", "HAND_ALL":
		// Hand targets belong to the casting member; card modifiers separately
		// consume the selected card slots. Member effects reuse this owner list.
		context.members = []int{source}
	case "USER_ALL":
		for _, p := range engine.playerParty(source) {
			if p.HP > 0 {
				context.members = append(context.members, p.MemberType)
			}
		}
	case "ENEMY_ALL", "DEAD_ENEMY_ALL":
		if engine.pvp {
			for _, p := range engine.opposingParty(source) {
				if p.HP > 0 || kind == "DEAD_ENEMY_ALL" {
					context.members = append(context.members, p.MemberType)
				}
			}
			break
		}
		for _, index := range engine.enemyTargetCandidates(kind == "DEAD_ENEMY_ALL") {
			context.members = append(context.members, index+5)
		}
	default:
		if selected > 0 {
			context.members = []int{selected}
		}
	}
	engine.skillTargets = context
	defer func() { engine.skillTargets = previous }()
	// The native skill first produces every role's random hand count, then
	// runs consumers. Later seal/trap/darkness roles affect earlier hit RNG.
	for _, role := range roles {
		var produced battleEffect
		if engine.prepareRandomHandRole(role, &produced) {
			if context.randomHandCounts == nil {
				context.randomHandCounts = make(map[int]int)
			}
			context.randomHandCounts[role.RoleIndex] = produced.Parameters[0]
		}
	}
	var results []BattleResult
	for _, role := range roles {
		if role.Function == "NONE" {
			continue // Later NONE roles have no consumer; entry guards handle the first.
		}
		rows, err := consume(role)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
	}
	engine.commitSkillAttackDamage(context)
	if stats := engine.memberSkillStatistics(source); stats != nil {
		for _, count := range context.attackCount {
			stats.AttackCount += count
		}
	}
	results = engine.projectSkillStatusResults(results, !passiveCard)
	results = append(results, engine.finishTranceReactions(skill.Cost)...)
	results = append(results, engine.resolvePlayerSkillGuts()...)
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		var guts []BattleResult
		enemy.HP, guts = resolveGuts(enemy.MemberType, enemy.MaxHP, enemy.HP, &enemy.Effects)
		if len(guts) > 0 {
			enemy.TurnSkillStats.Died = false
			enemy.SelfDestructed = false
			enemy.PendingBreak = false
			enemy.DeathActionTriggered = false
		}
		results = append(results, guts...)
	}
	// 4ccfc invokes death actions for all enemies before 4cd4a emits any
	// remaining break/drop rows. A nested death skill has its own target list.
	for i := 0; i < engine.enemyCount; i++ {
		var err error
		results, err = engine.triggerEnemyDeathAction(results, &engine.enemies[i])
		if err != nil {
			return nil, err
		}
	}
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		if enemy.PendingBreak {
			enemy.PendingBreak = false
			results = engine.commitEnemyBreak(results, enemy)
		}
	}
	return results, nil
}

func (engine *BattleEngine) skillRoleTargets(source int, role CombatSkillRole, players bool) ([]int, bool) {
	context := engine.skillTargets
	kind := role.Target
	if kind == "FRIEND_ALL" {
		// 8b660 maps role target 2 to skill target USER_ALL before comparing.
		kind = "USER_ALL"
	}
	if context == nil || context.source != source || kind != "SELECT" && kind != context.kind {
		return nil, false
	}
	var targets []int
	for _, member := range context.members {
		if players && engine.playerMember(member) {
			targets = append(targets, member)
		} else if !players && member >= 5 && member < 5+engine.enemyCount {
			targets = append(targets, member-5)
		}
	}
	return targets, true
}

func markEnemyPendingBreak(enemy *battleEnemy) {
	if enemy.HP <= 0 {
		enemy.SelfDestructed = false
		enemy.TurnSkillStats.Died = true
		enemy.PendingBreak = true
		enemy.DeathActionTriggered = false
	}
}
