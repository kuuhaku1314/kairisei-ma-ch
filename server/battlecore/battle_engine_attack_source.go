package battlecore

// Source-side power, card options and target selection stay with their producers.
// Both receivers use the same source context for critical rolls and drain.
type battleAttackSource struct {
	memberType            int
	power                 int
	boostFixed, boostRate int
	chainCount, chainRate int
	hateRatio             int
	modifiers             attackModifiers
	criticalSkill         *CombatSkillDefinition
	counted               []battleEffect
	player                *battlePlayer
	enemy                 *battleEnemy
}

func (source battleAttackSource) effects() []battleEffect {
	if source.player != nil {
		return source.player.Effects
	}
	return source.enemy.Effects
}

func (engine *BattleEngine) attackCriticalOutcome(source battleAttackSource, role CombatSkillRole) (int, int) {
	if source.criticalSkill != nil {
		return engine.nativeCriticalOutcome(source.effects(), combatParameterInt(role.Parameters[6]), *source.criticalSkill)
	}
	return engine.nativeCriticalOutcome(source.effects(), combatParameterInt(role.Parameters[6]))
}

func (engine *BattleEngine) attackDrainResults(source battleAttackSource, roleIndex, damage int, modifiers attackModifiers) []BattleResult {
	if source.player != nil {
		return engine.playerAttackDrainResults(source.player, roleIndex, damage, modifiers)
	}
	if damage <= 0 || modifiers.drainRate <= 0 || source.enemy.HP <= 0 {
		return nil
	}
	heal := damage * modifiers.drainRate / 100
	if modifiers.drainCap > 0 {
		heal = minInt(heal, modifiers.drainCap)
	}
	reported, hp := nativeHealCommit(source.enemy.HP, source.enemy.MaxHP, heal, source.effects(), false)
	source.enemy.HP = hp
	engine.recordRecoveryAction(source.memberType, reported)
	return []BattleResult{battleHealResult(source.memberType, roleIndex, reported, hp)}
}
