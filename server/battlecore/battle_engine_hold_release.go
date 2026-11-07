package battlecore

func (engine *BattleEngine) releasePlayerAppendCards(action battleAction, role CombatSkillRole) []BattleResult {
	return engine.releaseAppendCards(engine.playerRoleTargets(action, role), role, action.cardLevel)
}

func (engine *BattleEngine) releaseEnemyAppendCards(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	return engine.releaseAppendCards(engine.enemyRolePlayerTargets(actor, selected, role), role, EnemySkillLevel(role.Function))
}

// The original consumer rolls once per selected user, even without matching
// holds. It releases this family in vector order, preserving ordinary held
// cards and the other append family.
func (engine *BattleEngine) releaseAppendCards(targets []int, role CombatSkillRole, level int) []BattleResult {
	cardType := 21
	if role.Function == "BLESS_RELEASE" {
		cardType = 22
	}
	rate := int32(combatParameterInt(role.Parameters[0])) + int32(level)*int32(combatParameterInt(role.Parameters[1]))
	attribute := role.Parameters[2]
	codes, _ := sphereSupportAttributeCodes(attribute)
	flags := 0
	for _, code := range codes {
		flags |= 1 << uint(code&31)
	}
	var results []BattleResult
	for _, member := range targets {
		if int32(engine.rng.next()%10000) >= rate*100 {
			continue
		}
		player := engine.player(member)
		kept := player.BlessHolds[:0]
		announced := false
		for _, hold := range player.BlessHolds {
			if hold.CardType != cardType || !damageAttributeMatches(attribute, hold.Skill.Attribute) {
				kept = append(kept, hold)
				continue
			}
			if !announced {
				results = append(results, BattleResult{Command: CommandAppendRelease, Args: []int64{int64(member), int64(role.RoleIndex), int64(cardType), int64(flags)}})
				announced = true
			}
			results = append(results, blessHoldLostResult(member, hold))
		}
		player.BlessHolds = kept
	}
	return results
}
