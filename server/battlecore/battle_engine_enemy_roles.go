package battlecore

import (
	"fmt"
	"strings"
)

func SupportsEnemyFunction(function string) bool {
	switch function {
	case "ATK_BREAK_BY_NOW_TURN_DAMAGE", "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "ATK_BREAK_FIXED",
		"ATK_UP_FIXED_ENEMY", "DEF_UP_FIXED_ENEMY", "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY",
		"ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT",
		"ATK_OP_DAMAGE_INCREASE", "ATK_OP_DRAIN", "ATK_OP_NOW_TURN_REVENGE", "ATK_OP_PIERCING", "ATK_OP_ATTR_DEF_PIERCING", "ATK_OP_REVENGE", "ATK_OP_REFLECTION_INVALID",
		"ATK_UP_BY_NOW_TURN_DAMAGE", "ATK_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_FIXED", "ATTACK_AA",
		"ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "ATTR_HIDE", "BLEED", "BLESS",
		"BLESS_TURN_DOWN", "BLESS_RELEASE", "CURSE_RELEASE", "CARD_TOKEN", "BUFF_RELEASE", "BUFF_RELEASE_OLD", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BURN",
		"BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN", "CARD_SEAL", "CARD_TRAP_DAMAGE", "NEED_COST_UP", "COST_BLOCK", "CRITICAL_DOWN", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "STAN",
		"DARKNESS_APPOINT", "DARKNESS_RANDOM", "DEAL_BONUS", "DEAL_PENALTY", "DEAL_PENALTY_TURN_APPOINT",
		"DEBUFF_REGIST", "DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEF_UP_BY_SELF_PARAM", "DEF_UP_FIXED", "DESTRUCT",
		"DOT_VALUE_UP", "ELECTRIC", "ENCHANT", "ENDURE", "ENEMY_AI_TRIGGER_FLAG_SET", "ENEMY_AWAKE_FLAG_SET", "ENEMY_AI_VAR_ADD", "ENEMY_AI_VAR_SET",
		"ENEMY_CURSE", "FORCE_BATTLE_END", "FREEZE", "GUARD_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_SELF_PARAM",
		"GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_FIXED", "GUTS", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP",
		"HEAL_FIXED", "HEAL_CUT", "HEAL_REVERSE", "HP_CUT", "NONE", "OUTPUT_TEXT", "PARAM_LIMIT_BREAK_FIXED", "POISON",
		"REFLECTION", "REGENERATE_BY_SELF_PARAM", "REGENERATE_FIXED", "REVIVE", "REWRITE", "WEAKNESS",
		"TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return true
	default:
		return false
	}
}

func FunctionNeedsBuffCode(function string) bool {
	switch function {
	case "DEF_UP_FIXED", "DEF_UP_BY_SELF_PARAM", "ATK_UP_FIXED", "ATK_UP_BY_SELF_PARAM", "ATK_UP_BY_TARGET_PARAM", "ATK_UP_BY_NOW_TURN_DAMAGE",
		"ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT",
		"ATK_UP_FIXED_ENEMY", "DEF_UP_FIXED_ENEMY", "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY",
		"ATK_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "ATK_BREAK_BY_NOW_TURN_DAMAGE",
		"GUARD_BREAK_FIXED", "GUARD_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_NOW_TURN_DAMAGE",
		"REGENERATE_FIXED", "REGENERATE_BY_SELF_PARAM", "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC", "ENCHANT",
		"ATTR_DEF_DOWN", "ATTR_DEF_UP", "ATTR_HIDE", "CRITICAL_UP", "CRITICAL_DOWN", "PARAM_LIMIT_BREAK_FIXED", "WEAKNESS",
		"REFLECTION", "COVERING", "BLESS", "CARD_SEAL", "CARD_SEAL_REGIST", "DARKNESS_APPOINT", "DARKNESS_RANDOM",
		"DARKNESS_REGIST", "GUTS", "ENDURE", "STAN", "COST_BLOCK", "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR",
		"ATTR_SEE", "CARD_TRAP_DAMAGE", "HEAL_CUT", "HEAL_REVERSE", "DEBUFF_REGIST", "REWRITE", "DEAL_BONUS", "DEAL_PENALTY",
		"DEAL_PENALTY_TURN_APPOINT":
		return true
	default:
		return false
	}
}

func (engine *BattleEngine) executeEnemyRole(actor *battleEnemy, selected int, role CombatSkillRole, roles []CombatSkillRole) (results []BattleResult, err error) {
	defer func() {
		if err == nil {
			results = engine.projectRoleBuffParameters(results)
		}
	}()
	switch role.Function {
	case "", "NONE", "OUTPUT_TEXT":
		return nil, nil
	case "ATTACK_AA":
		return engine.executeEnemyAttack(actor, selected, role, roles)
	case "ATK_OP_DAMAGE_INCREASE", "ATK_OP_DRAIN", "ATK_OP_NOW_TURN_REVENGE", "ATK_OP_PIERCING", "ATK_OP_ATTR_DEF_PIERCING", "ATK_OP_REVENGE", "ATK_OP_REFLECTION_INVALID":
		return nil, nil
	case "NEED_COST_UP":
		return engine.executeEnemyCardCostIncrease(actor, selected, role)
	case "PARAM_LIMIT_BREAK_FIXED":
		return engine.executeEnemyParameterLimit(actor, selected, role)
	case "ATK_UP_FIXED", "DEF_UP_FIXED", "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED",
		"ATK_UP_FIXED_ENEMY", "DEF_UP_FIXED_ENEMY", "ATK_BREAK_FIXED_ENEMY", "GUARD_BREAK_FIXED_ENEMY",
		"ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM",
		"ATK_UP_BY_TARGET_PARAM", "ATK_BREAK_BY_TARGET_PARAM", "GUARD_BREAK_BY_TARGET_PARAM",
		"ATK_UP_BY_NOW_TURN_DAMAGE", "ATK_BREAK_BY_NOW_TURN_DAMAGE", "GUARD_BREAK_BY_NOW_TURN_DAMAGE":
		return engine.executeEnemyParameterRole(actor, selected, role)
	case "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP":
		return engine.executeEnemyHeal(actor, selected, role)
	case "BUFF_RELEASE", "BUFF_RELEASE_OLD", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM",
		"DEBUFF_RELEASE", "DEBUFF_RELEASE_OLD", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM":
		return engine.executeEnemyRelease(actor, selected, role)
	case "BLESS":
		return engine.executeEnemyBless(actor, selected, role)
	case "ENEMY_CURSE":
		return engine.executeEnemyCurse(actor, selected, role)
	case "BLESS_TURN_DOWN":
		return engine.changeEnemyBlessTurns(actor, selected, role, -combatParameterInt(role.Parameters[0])), nil
	case "BURST_GAUGE_QUICK_UP", "BURST_GAUGE_QUICK_DOWN":
		results := make([]BattleResult, 0, 8)
		level := EnemySkillLevel(role.Function)
		amount := burstGaugeQuickValue(role, level)
		if role.Function == "BURST_GAUGE_QUICK_DOWN" {
			amount = -amount
		}
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			results = append(results, engine.addPlayerBurstGauge(memberType, role.RoleIndex, amount, role.Function == "BURST_GAUGE_QUICK_UP")...)
		}
		return results, nil
	case "DEAL_BONUS":
		return engine.executeEnemyDealChange(actor, selected, role, false)
	case "DEAL_PENALTY", "DEAL_PENALTY_TURN_APPOINT":
		return engine.executeEnemyDealChange(actor, selected, role, true)
	case "HP_CUT":
		return engine.executeEnemyHPCut(actor, selected, role)
	case "DESTRUCT":
		return engine.executeEnemyDestruct(actor, selected, role)
	case "REVIVE":
		return engine.executeEnemyRevive(actor, selected, role)
	case "FORCE_BATTLE_END":
		// Native FUN_00080a7b calls FUN_00062f03(1): it requests the normal
		// end-state evaluator instead of assigning DRAW or emitting a command.
		engine.forceEndCheck = true
		return nil, nil
	case "ENEMY_AI_VAR_ADD", "ENEMY_AI_VAR_SET":
		return engine.executeEnemyAIVariable(actor, selected, role), nil
	case "CARD_TOKEN":
		return engine.createEnemyTokenCards(actor, selected, role)
	case "BLESS_RELEASE", "CURSE_RELEASE":
		return engine.releaseEnemyAppendCards(actor, selected, role), nil
	case "TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return engine.executeEnemyTranceRole(actor, selected, role), nil
	case "ENEMY_AI_TRIGGER_FLAG_SET":
		// Native action type 14/subtype 0 calls FUN_0005711b: p0 selects one
		// of 32 trigger bits and any positive p1 sets it; zero clears it. It
		// does not emit ResultCmd94 (that command belongs to subtype 2's AI
		// variable accumulator).
		flag := combatParameterInt(role.Parameters[0])
		if flag < 32 {
			// 80ec0 passes its resolved target to 5711b, not the source
			// or a shared party flag. Death actions may still target SELF.
			includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
			for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
				mask := uint32(1) << (uint(flag) & 31)
				if combatParameterInt(role.Parameters[1]) > 0 {
					engine.enemies[index].AIFlags |= mask
				} else {
					engine.enemies[index].AIFlags &^= mask
				}
			}
		}
		return nil, nil
	case "ENEMY_AWAKE_FLAG_SET":
		// Subtype 1 calls FUN_0004cc73 on each resolved enemy target. Awake is
		// durable battle state but has no standalone ResultCmd projection.
		awake := combatParameterInt(role.Parameters[0])
		includeDead := role.Target != "ENEMY_ALL" && role.Target != "FRIEND_ALL"
		for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, includeDead) {
			engine.enemies[index].Awake = awake
		}
		return nil, nil
	case "REWRITE":
		return engine.executeEnemyRewrite(actor, selected, role), nil
	case "DOT_VALUE_UP":
		return engine.executeEnemyDOTValueUp(actor, selected, role), nil
	default:
		return engine.executeEnemyPersistentEffect(actor, selected, role)
	}
}

func (engine *BattleEngine) executeEnemyRewrite(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	return engine.executeEnemyRewriteWithListType(actor, selected, role, 0)
}

func (engine *BattleEngine) executeEnemyRewriteWithListType(actor *battleEnemy, selected int, role CombatSkillRole, listType int) []BattleResult {
	return engine.executeRewrite(actor.MemberType, role, listType,
		engine.enemyRolePlayerTargets(actor, selected, role), engine.enemyRoleEnemyTargets(actor, selected, role, false))
}

func (engine *BattleEngine) executePlayerRewrite(action battleAction, role CombatSkillRole) []BattleResult {
	var enemies []int
	if !combatTargetIsPlayer(role.Target) {
		enemies = engine.playerRoleEnemyTargets(action, role)
	}
	return engine.executeRewrite(action.memberType, role, 0, engine.playerRoleTargets(action, role), enemies)
}

func (engine *BattleEngine) executeRewrite(source int, role CombatSkillRole, listType int, players, enemies []int) []BattleResult {
	// Native role 43 (FUN_000974bd) creates action type 4/subtype 3. Its
	// FUN_0008d7e0 consumer owns BATTLE_BUFF.ATTR(202), replaces an older
	// attribute rewrite, emits immediate 32, and queues ATTR's 62/6 status.
	// The full API projection is confirmed by D-329's official 44099004/12.
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	attribute := role.Parameters[1]
	effect := battleEffect{
		Function: "REWRITE", ListType: listType, Attribute: attribute, Kind: 1, Remaining: duration,
		AppliedTurn: engine.turn, Source: source, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
	}
	results := make([]BattleResult, 0, 4)
	for _, memberType := range players {
		player := engine.player(memberType)
		if player.BaseAttribute == "" {
			player.BaseAttribute = player.Attribute
			if player.BaseAttribute == "" {
				player.BaseAttribute = "NEUTRAL"
			}
		}
		var removed []battleEffect
		player.Effects, removed = replaceRewriteEffect(player.Effects, effect)
		results = appendRewriteReplacement(results, memberType, removed)
		player.Attribute = attribute
		results = append(results, rewriteResult(memberType, attribute),
			battleBuffResultWithListType(memberType, role.RoleIndex, listType, battleBuffCodes["REWRITE"], 0, 1<<combatAttributeCode(attribute), 0, 0, 0, 0))
	}
	for _, index := range enemies {
		enemy := &engine.enemies[index]
		if enemy.BaseAttribute == "" {
			enemy.BaseAttribute = enemy.Attribute
			if enemy.BaseAttribute == "" {
				enemy.BaseAttribute = "NULL"
			}
		}
		var removed []battleEffect
		enemy.Effects, removed = replaceRewriteEffect(enemy.Effects, effect)
		results = appendRewriteReplacement(results, enemy.MemberType, removed)
		enemy.Attribute = attribute
		results = append(results, rewriteResult(enemy.MemberType, attribute),
			battleBuffResultWithListType(enemy.MemberType, role.RoleIndex, listType, battleBuffCodes["REWRITE"], 0, 1<<combatAttributeCode(attribute), 0, 0, 0, 0))
	}
	return results
}

func (engine *BattleEngine) executeEnemyDestruct(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	// 8b660 invokes 8127b once per allowed role target, but 8127b consumes
	// its second argument (the caster), not the third (selected member).
	// Its 60 contains post-commit HP; it does not emit an additional 3.
	results := make([]BattleResult, 0, 8)
	count := len(engine.enemyRoleEnemyTargets(actor, selected, role, false))
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		count = len(engine.enemyRolePlayerTargets(actor, selected, role))
	}
	for i := 0; i < count; i++ {
		damage := nativeDestructDamage(actor.HP, role, EnemySkillLevel(role.Function))
		actor.HP = nativeHPCommit(actor.HP, actor.MaxHP, -damage, actor.Effects)
		engine.applyStoryEnemyFloor(actor)
		recordEnemyDamage(actor, damage, "")
		engine.recordSpecialEnemyDamage(actor.MemberType, actor.MemberType, damage)
		results = append(results, battleDamageResult(actor.MemberType, role.RoleIndex, -damage, actor.HP, 0, "NULL", 100, 0, 0, actor.MemberType))
		if actor.HP > 0 {
			continue
		}
		actor.SelfDestructed = true
		actor.DeathActionTriggered = false
		if combatParameterInt(role.Parameters[2]) != 0 {
			actor.PendingBreak = true
			continue
		}
		// Immediate notification leaves 59948/drop ownership unchanged and
		// may repeat; a later ordinary kill can still release the reward.
		actor.DeathCount++
		actor.DiedTurn = engine.turn
		actor.TurnSkillStats.Died = true
		command := CommandEnemyBreak
		if actor.Parent > 0 {
			command = CommandPartsBreak
		}
		results = append(results, BattleResult{Command: command, Args: []int64{int64(actor.MemberType), 0, 0, 0, boolInt64(actor.DropResolved)}})
	}
	return results, nil
}

func nativeDestructDamage(currentHP int, role CombatSkillRole, level int) int {
	base := int32(int64(currentHP) * int64(int32(combatParameterInt(role.Parameters[0]))) / 100)
	levelSegment := int32(combatParameterInt(role.Parameters[1]) * maxInt(0, level))
	return maxInt(1, int(base+levelSegment))
}

func replaceRewriteEffect(effects []battleEffect, replacement battleEffect) ([]battleEffect, []battleEffect) {
	kept := make([]battleEffect, 0, len(effects)+1)
	removed := make([]battleEffect, 0, 1)
	for _, effect := range effects {
		if effect.Function == "REWRITE" && effect.ListType == replacement.ListType {
			removed = append(removed, effect)
			continue
		}
		kept = append(kept, effect)
	}
	return append(kept, replacement), removed
}

func appendRewriteReplacement(results []BattleResult, memberType int, removed []battleEffect) []BattleResult {
	for _, effect := range removed {
		_, release := naturalEffectReleaseResult(memberType, effect, battleBuffCodes["REWRITE"])
		results = append(results, release)
	}
	return results
}

func rewriteResult(memberType int, attribute string) BattleResult {
	return BattleResult{Command: CommandRewrite, Args: []int64{int64(memberType), int64(combatAttributeCode(attribute))}}
}

func (engine *BattleEngine) executeEnemyAttack(actor *battleEnemy, selected int, role CombatSkillRole, roles []CombatSkillRole) ([]BattleResult, error) {
	actorStats := playerFromEnemy(actor)
	level := EnemySkillLevel(role.Function)
	power := attackRolePower(role, level, maxInt(0, combatStatValue(actorStats, role.Parameters[5])))
	modifiers := collectEnemyAttackModifiers(modifierRolesForAttack(roles, role.RoleIndex, nil), actorStats, 1)
	power += modifiers.damageIncrease
	power += attackRevengeBonus(actor.DamageTaken, actorStats, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	power += attackRevengeBonus(actor.TurnDamage, actorStats, modifiers.nowTurnRevengeRate, modifiers.nowTurnRevengeParameter, modifiers.nowTurnRevengeCapRate)
	source := battleAttackSource{memberType: actor.MemberType, power: power, modifiers: modifiers, enemy: actor}
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		return engine.executeAttackOnPlayers(source, engine.enemyRolePlayerTargets(actor, selected, role), role, roles), nil
	}
	return engine.executeAttackOnEnemies(source, engine.enemyRoleEnemyTargets(actor, selected, role, false), role, roles), nil
}

func (engine *BattleEngine) playerHPAfterDamage(player *battlePlayer, damage int) int {
	if damage == 0 {
		return player.HP
	}
	hp := nativeHPCommit(player.HP, player.MaxHP, -damage, player.Effects)
	if player.HP > 0 && hp <= 0 {
		player.TurnSkillStats.Died = true
	}
	return hp
}

// 7adb0 -> 5feb6 sweeps actual players after all roles and deferred status
// registrations. Damage commits retain the death flag until GUTS revives them.
func (engine *BattleEngine) resolvePlayerSkillGuts() []BattleResult {
	var results []BattleResult
	for _, player := range engine.playerStates() {
		results = append(results, resolvePlayerGuts(player)...)
	}
	return results
}

func resolvePlayerGuts(player *battlePlayer) []BattleResult {
	// 5feb6 explicitly excludes formally retired users, independently of HP.
	if player.GameOver {
		return nil
	}
	var results []BattleResult
	player.HP, results = resolveGuts(player.MemberType, player.MaxHP, player.HP, &player.Effects)
	if len(results) > 0 {
		player.TurnSkillStats.Died = false
	}
	return results
}

func resolveGuts(memberType int, maxHP int, hp int, effects *[]battleEffect) (int, []BattleResult) {
	if hp > 0 || effects == nil {
		return hp, nil
	}
	selected := -1
	for index, effect := range *effects {
		if effect.Function == "GUTS" && effect.ListType >= 0 && effect.ListType <= 6 &&
			(selected < 0 || effect.ListType < (*effects)[selected].ListType) {
			selected = index
		}
	}
	if selected >= 0 {
		index := selected
		effect := &(*effects)[index]
		heal := maxInt(1, maxHP*maxInt(0, effect.Rate)/100)
		hp = minInt(maxHP, heal)
		effect.Uses = int(int32(effect.Uses) - 1)
		effect.Parameters[0] = effect.Uses
		results := []BattleResult{
			buffStatusEffectResult(memberType, battleBuffCodes["GUTS"]),
			battleHealResult(memberType, 0, heal, hp),
			{Command: 200, Args: []int64{int64(memberType), int64(battleBuffCodes["GUTS"]), int64(effect.Uses), 0}},
		}
		if effect.Uses <= 0 {
			listType := effect.ListType
			results = append(results, BattleResult{Command: 72, Args: []int64{int64(memberType), int64(listType), int64(battleBuffCodes["GUTS"]), 0, 1}})
			kept := (*effects)[:0]
			for _, candidate := range *effects {
				if candidate.Function != "GUTS" || candidate.ListType != listType {
					kept = append(kept, candidate)
				}
			}
			*effects = kept
		}
		return hp, results
	}
	return hp, nil
}

type incomingDamageResolution struct {
	Damage                int
	Reflected             int
	BarrierTriggered      bool
	BarrierRemainingUses  int
	BarrierAttributeIndex int
	Released              []battleEffect
}

// resolveIncomingDamageEffects follows the CN native commit order: CHALICE
// DAMAGE_CUT2 modifies the incoming value before an attack barrier gets the
// first chance to consume the hit, and reflection only sees damage which the
// barrier did not absorb. Barrier Uses is independent from Remaining.
func (engine *BattleEngine) resolveIncomingDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string, physics string) incomingDamageResolution {
	return engine.resolveDamageEffects(effects, damage, currentHP, attribute, physics, true)
}

func (engine *BattleEngine) resolveEnchantDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string) incomingDamageResolution {
	// 82c85 processes enchant barriers without the ordinary hit's support cut.
	return engine.resolveDamageEffects(effects, damage, currentHP, attribute, "", false)
}

func (engine *BattleEngine) resolveDamageEffects(effects *[]battleEffect, damage int, currentHP int, attribute string, physics string, supportCut bool) incomingDamageResolution {
	resolution := incomingDamageResolution{Damage: maxInt(0, damage)}
	if resolution.Damage == 0 || effects == nil {
		return resolution
	}
	if supportCut {
		resolution.Damage = sphereDamageCut(*effects, attribute, physics, resolution.Damage)
	}
	suffix := "ENCHANT"
	if supportCut {
		suffix = "ATTACK"
	}
	var cutExpired *battleEffect
	resolution.Damage, cutExpired = cutAttributeGuardDamage(effects, resolution.Damage, suffix, attribute, physics, "")

	for index := 0; index < len(*effects); index++ {
		effect := &(*effects)[index]
		if effect.Function != "ATTACK_BARRIER" && effect.Function != "ATTACK_BARRIER_APPOINT_ATTR" {
			continue
		}
		if effect.Remaining <= 0 || effect.Uses <= 0 || !damagePhysicsMatches(effect.DamageKind, physics) || !damageAttributeMatches(effect.Attribute, attribute) {
			continue
		}
		effect.Uses--
		resolution.BarrierTriggered = true
		resolution.BarrierRemainingUses = effect.Uses
		resolution.BarrierAttributeIndex = combatAttributeCode(effect.Attribute)
		if resolution.Damage <= effect.Value {
			resolution.Damage = 0
		}
		if effect.Uses == 0 {
			resolution.Released = append(resolution.Released, *effect)
			*effects = append((*effects)[:index], (*effects)[index+1:]...)
		}
		break
	}
	if resolution.Damage == 0 {
		if cutExpired != nil {
			resolution.Released = append(resolution.Released, *cutExpired)
		}
		return resolution
	}
	var drainExpired *battleEffect
	resolution.Damage, drainExpired = drainAttributeGuardDamage(effects, resolution.Damage, currentHP, suffix, attribute, physics, "")
	if drainExpired != nil {
		resolution.Released = append(resolution.Released, *drainExpired)
	}
	if cutExpired != nil {
		resolution.Released = append(resolution.Released, *cutExpired)
	}
	if resolution.Damage <= 0 {
		return resolution
	}

	for index := 0; index < len(*effects); index++ {
		effect := &(*effects)[index]
		if effect.Function != "REFLECTION" || effect.Remaining <= 0 || effect.Rate <= 0 || !damagePhysicsMatches(effect.DamageKind, physics) {
			continue
		}
		// 838d0 adds reflected damage; it does not absorb the incoming hit or
		// consume duration. The retained coefficient is in ten-thousandths.
		resolution.Reflected = int(int64(resolution.Damage) * int64(effect.Rate) / 10000)
		break
	}
	return resolution
}

// 838d0 caps reflection at the attacker's pre-commit HP minus one. The 60
// carries that requested damage and post-commit HP, even when ENDURE clips it.
func (engine *BattleEngine) reflectedDamageResults(member int, source int, requested int) []BattleResult {
	var hp *int
	var maxHP int
	var effects []battleEffect
	if engine.playerMember(member) {
		actor := engine.player(member)
		hp, maxHP, effects = &actor.HP, actor.MaxHP, actor.Effects
	} else if member >= 5 && member < 5+engine.enemyCount {
		actor := &engine.enemies[member-5]
		hp, maxHP, effects = &actor.HP, actor.MaxHP, actor.Effects
	} else {
		return nil
	}
	damage := minInt(requested, maxInt(0, *hp-1))
	if damage <= 0 {
		return nil
	}
	*hp = nativeHPCommit(*hp, maxHP, -damage, effects)
	rows := []BattleResult{battleDamageResult(member, 0, -damage, *hp, 0, "NULL", 100, 0, 2, source)}
	if engine.playerMember(member) {
		recordPlayerDamage(engine.player(member), damage)
		return rows
	}
	actor := &engine.enemies[member-5]
	recordEnemyDamage(actor, damage, "MAGIC")
	recordEnemyAIDamage(actor, damage, 2)
	engine.recordSpecialEnemyDamage(member, source, damage)
	if actor.Parent > 0 && actor.Parent <= engine.enemyCount {
		parent := &engine.enemies[actor.Parent-1]
		if parent.HP > 0 {
			parentDamage := minInt(damage, parent.HP-1)
			parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, -parentDamage, parent.Effects)
			recordEnemyDamageTotals(parent, parentDamage)
			engine.recordSpecialEnemyDamage(parent.MemberType, source, damage)
			rows = append(rows, BattleResult{Command: CommandHP, Args: []int64{int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 1}})
		}
	}
	return rows
}

func damagePhysicsMatches(filter string, physics string) bool {
	filter = strings.ToUpper(filter)
	physics = strings.ToUpper(physics)
	return filter == "" || filter == "ALL" || filter == "NULL" || physics == "" || physics == "ALL" || physics == "NULL" || filter == physics
}

func damageAttributeMatches(filter string, attribute string) bool {
	filter = strings.ToUpper(filter)
	if filter == "" || filter == "ALL" || filter == "NULL" {
		return true
	}
	if strings.EqualFold(filter, attribute) {
		return true
	}
	// Native FUN_000cec12 splits both decimal pair attributes and succeeds
	// when either component intersects. This covers LIGHT_DARK filters against
	// LIGHT, DARK and dual-attribute attacks without broadening to other attrs.
	for _, component := range splitCombatAttributes(filter) {
		if combatAttributeMatches(attribute, component) {
			return true
		}
	}
	return false
}

func playerCoveringDamage(player *battlePlayer, damage int, attribute string, physics string) int {
	if player == nil || damage <= 0 {
		return maxInt(0, damage)
	}
	remainingRate := 1000
	for _, effect := range player.Effects {
		if effect.Function != "COVERING" || effect.Remaining <= 0 ||
			!damageAttributeMatches(effect.Attribute, attribute) || !damagePhysicsMatches(effect.DamageKind, physics) {
			continue
		}
		remainingRate = remainingRate * maxInt(0, 1000-effect.Rate) / 1000
	}
	return maxInt(1, damage*remainingRate/1000)
}

func damageEffectResults(memberType int, resolution incomingDamageResolution) []BattleResult {
	results := make([]BattleResult, 0, len(resolution.Released)+1)
	if resolution.BarrierTriggered {
		// Managed ResultCmd 200 calls MemberData.setBuffCount with
		// [member,buff,count,managed-attribute]. Native FUN_00082c85 emits it for
		// every matching barrier hit, even when the damage exceeds the threshold.
		results = append(results, BattleResult{Command: 200, Args: []int64{
			int64(memberType), int64(battleBuffCodes["ATTACK_BARRIER"]),
			int64(resolution.BarrierRemainingUses), int64(resolution.BarrierAttributeIndex),
		}})
	}
	for _, effect := range resolution.Released {
		if code, exists := battleBuffCodes[effect.Function]; exists {
			_, result := naturalEffectReleaseResult(memberType, effect, code)
			results = append(results, result)
		}
	}
	return results
}

func (engine *BattleEngine) executeEnemyParameterLimit(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := EnemySkillLevel(role.Function)
	value := fixedBuffRoleValue(role, level, 1)
	if value <= 0 {
		return nil, nil
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	code := battleBuffCodes[role.Function]
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			ensurePlayerBaseParameters(player)
			if !adjustPlayerParameterLimit(player, role.Parameters[1], value) {
				return nil, fmt.Errorf("unsupported player parameter limit %q", role.Parameters[1])
			}
			player.Effects = append(player.Effects, battleEffect{
				Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value,
				Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
			})
			engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
			refreshAppliedPlayerParameter(player, role.Parameters[1], 0)
			results = append(results,
				battleParameterBuffResult(memberType, role, code, 0),
				BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		ensureEnemyBaseParameters(target)
		if !adjustEnemyParameterLimit(target, role.Parameters[1], value) {
			return nil, fmt.Errorf("unsupported enemy parameter limit %q", role.Parameters[1])
		}
		target.Effects = append(target.Effects, battleEffect{
			Function: role.Function, Parameter: role.Parameters[1], Value: value, Delta: value,
			Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
		})
		engine.recordAIStatusApplied(target.MemberType, target.Effects[len(target.Effects)-1])
		refreshAppliedEnemyParameter(target, role.Parameters[1], 0)
		results = append(results,
			battleParameterBuffResult(target.MemberType, role, code, 0),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(target.MemberType, target.HP, target.MaxHP, target.Attack, target.Magic, target.Recovery, target.Defense, target.MDefense, target.LimitAttack, target.LimitMagic, target.LimitRecovery)},
		)
	}
	return results, nil
}

func adjustEnemyParameterLimit(enemy *battleEnemy, name string, amount int) bool {
	switch strings.ToUpper(name) {
	case "ATK":
		enemy.LimitAttack = maxInt(0, activeParameterLimit(enemy.LimitAttack)+amount)
	case "INT":
		enemy.LimitMagic = maxInt(0, activeParameterLimit(enemy.LimitMagic)+amount)
	case "MND":
		enemy.LimitRecovery = maxInt(0, activeParameterLimit(enemy.LimitRecovery)+amount)
	default:
		return false
	}
	return true
}

func (engine *BattleEngine) executeEnemyParameterRole(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	return engine.executeEnemyParameterRoleWithListType(actor, selected, role, 0)
}

func (engine *BattleEngine) executeEnemyParameterRoleWithListType(actor *battleEnemy, selected int, role CombatSkillRole, listType int) ([]BattleResult, error) {
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	level := EnemySkillLevel(role.Function)
	value := fixedBuffRoleValue(role, level, 1)
	currentValue := func() int { return value }
	if strings.Contains(role.Function, "BY_SELF_PARAM") {
		currentValue = func() int {
			return selfScaledParameterValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[2]))
		}
	}
	debuff := strings.HasPrefix(role.Function, "ATK_BREAK") || strings.HasPrefix(role.Function, "GUARD_BREAK")
	code := battleBuffCodes[role.Function]
	if !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported battle parameter %q", role.Parameters[1])
	}
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			amount := currentValue()
			if strings.Contains(role.Function, "BY_TARGET_PARAM") {
				amount = targetScaledParameterValue(role, level, combatStatValue(player, role.Parameters[2]))
			}
			if strings.Contains(role.Function, "BY_NOW_TURN_DAMAGE") {
				amount = nowTurnDamageParameterValue(role, actor.TurnDamage, level, 1)
			}
			if debuff {
				if !strings.HasSuffix(role.Function, "_FIXED_ENEMY") {
					amount = parameterDebuffValue(player.Effects, role.Function, role.Parameters[1], amount, 0, 0)
				}
				amount = -amount
			} else if !strings.HasSuffix(role.Function, "_FIXED_ENEMY") {
				// Direct enemy-only roles already supply their final magnitude.
				amount = parameterEffectMagnitude(amount, 0, 0)
			}
			ensurePlayerBaseParameters(player)
			kind := 1
			if debuff {
				kind = 2
			}
			player.Effects = append(player.Effects, battleEffect{Function: role.Function, ListType: listType, Parameter: role.Parameters[1], Value: absInt(amount), Delta: amount, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
			engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
			refreshAppliedPlayerParameter(player, role.Parameters[1], amount)
			results = append(results,
				battleParameterBuffResult(memberType, role, code, listType),
				BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
			)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		amount := currentValue()
		if strings.Contains(role.Function, "BY_TARGET_PARAM") {
			amount = targetScaledParameterValue(role, level, combatEnemyStatValue(target, role.Parameters[2]))
		}
		if strings.Contains(role.Function, "BY_NOW_TURN_DAMAGE") {
			amount = nowTurnDamageParameterValue(role, actor.TurnDamage, level, 1)
		}
		if debuff {
			if !strings.HasSuffix(role.Function, "_FIXED_ENEMY") {
				amount = parameterDebuffValue(target.Effects, role.Function, role.Parameters[1], amount, 0, 0)
			}
			amount = -amount
		} else if !strings.HasSuffix(role.Function, "_FIXED_ENEMY") {
			amount = parameterEffectMagnitude(amount, 0, 0)
		}
		ensureEnemyBaseParameters(target)
		kind := 1
		if debuff {
			kind = 2
		}
		target.Effects = append(target.Effects, battleEffect{Function: role.Function, ListType: listType, Parameter: role.Parameters[1], Value: absInt(amount), Delta: amount, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(target.MemberType, target.Effects[len(target.Effects)-1])
		refreshAppliedEnemyParameter(target, role.Parameters[1], amount)
		results = append(results,
			battleParameterBuffResult(target.MemberType, role, code, listType),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(target.MemberType, target.HP, target.MaxHP, target.Attack, target.Magic, target.Recovery, target.Defense, target.MDefense, target.LimitAttack, target.LimitMagic, target.LimitRecovery)},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyHeal(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := EnemySkillLevel(role.Function)
	value := fixedHealRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[4]))
	if role.Function == "HEAL_BY_SELF_PARAM" {
		value = selfScaledHealRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[0]))
	}
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			target := &engine.players[memberType-1]
			amount := value
			if role.Function == "HEAL_BY_TARGET_HP" || role.Function == "HEAL_BY_TARGET_MAXHP" {
				source := target.HP
				if role.Function == "HEAL_BY_TARGET_MAXHP" {
					source = target.MaxHP
				}
				amount = targetHPHealRoleValue(role, level, 1, source)
			}
			reported, hp := nativeHealCommit(target.HP, target.MaxHP, amount, target.Effects, true)
			target.HP = hp
			engine.recordRecoveryAction(actor.MemberType, reported)
			results = append(results, battleHealResult(target.MemberType, role.RoleIndex, reported, target.HP))
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		amount := value
		if role.Function == "HEAL_BY_TARGET_HP" || role.Function == "HEAL_BY_TARGET_MAXHP" {
			source := target.HP
			if role.Function == "HEAL_BY_TARGET_MAXHP" {
				source = target.MaxHP
			}
			amount = targetHPHealRoleValue(role, level, 1, source)
		}
		rows := engine.commitDirectEnemyHeal(target, role.RoleIndex, amount)
		engine.recordRecoveryAction(actor.MemberType, int(rows[0].Args[2]))
		results = append(results, rows...)
	}
	return results, nil
}

// commitDirectEnemyHeal mirrors native FUN_000822a0. ResultCmd61 already
// carries the selected target's authoritative HP. When that target is a living
// enemy part, the same positive reported heal is also committed to its living
// parent, without evaluating the parent's HEAL_REVERSE effects a second time.
// Native exposes only that parent change as a ResultCmd3 row, with update flag 2.
func (engine *BattleEngine) commitDirectEnemyHeal(target *battleEnemy, roleIndex int, amount int) []BattleResult {
	reported, hp := nativeHealCommit(target.HP, target.MaxHP, amount, target.Effects, true)
	target.HP = hp
	results := []BattleResult{battleHealResult(target.MemberType, roleIndex, reported, target.HP)}
	if reported <= 0 || target.Parent <= 0 || target.Parent > engine.enemyCount {
		return results
	}
	parent := &engine.enemies[target.Parent-1]
	if parent.HP <= 0 {
		return results
	}
	parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, reported, parent.Effects)
	return append(results, BattleResult{Command: CommandHP, Args: []int64{
		int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 2,
	}})
}

func (engine *BattleEngine) executeEnemyHPCut(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			delta, _ := nativeHPCut(player.HP, combatParameterInt(role.Parameters[0]))
			player.HP = nativeHPCommit(player.HP, player.MaxHP, delta, player.Effects)
			recordPlayerDamage(player, -delta)
			results = append(results, battleHPCutResult(memberType, role.RoleIndex, delta, player.HP))
			results = append(results, engine.burstDamageGaugeResults(player, -delta)...)
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		delta, _ := nativeHPCut(target.HP, combatParameterInt(role.Parameters[0]))
		target.HP = nativeHPCommit(target.HP, target.MaxHP, delta, target.Effects)
		recordEnemyDamageTotals(target, -delta)
		engine.recordSpecialEnemyDamage(target.MemberType, actor.MemberType, -delta)
		results = append(results, battleHPCutResult(target.MemberType, role.RoleIndex, delta, target.HP))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyRevive(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	level := EnemySkillLevel(role.Function)
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, true) {
		target := &engine.enemies[index]
		if target.HP > 0 {
			continue
		}
		requested := reviveRoleValue(role, level, target.MaxHP)
		target.HP = minInt(target.MaxHP, requested)
		target.Broken = false
		target.SelfDestructed = false
		target.DeathActionTriggered = false
		target.DiedTurn = 0
		// Ordinary REVIVE does not clear 7e6c0; unlike GUTS it preserves
		// ENEMY_DEAD_NOW_TURN until the next TurnPhase.
		results = append(results,
			BattleResult{Command: CommandSkillRevive, Args: []int64{int64(target.MemberType), int64(role.RoleIndex), int64(requested), int64(target.HP)}},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyDOTValueUp(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers)
	level := EnemySkillLevel(role.Function)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			var projected []BattleResult
			engine.players[memberType-1].Effects, projected = applyDOTValueUp(engine.players[memberType-1].Effects, memberType, role, level, 1)
			results = append(results, projected...)
		}
		return results
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		var projected []BattleResult
		engine.enemies[index].Effects, projected = applyDOTValueUp(engine.enemies[index].Effects, engine.enemies[index].MemberType, role, level, 1)
		results = append(results, projected...)
	}
	return results
}

func (engine *BattleEngine) executeEnemyPersistentEffect(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	return engine.executeEnemyPersistentEffectWithListType(actor, selected, role, 0)
}

func (engine *BattleEngine) executeEnemyPersistentEffectWithListType(actor *battleEnemy, selected int, role CombatSkillRole, listType int) ([]BattleResult, error) {
	code, exists := battleBuffCodes[role.Function]
	if !exists {
		return nil, fmt.Errorf("persistent enemy combat function %q has no official buff code", role.Function)
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	kind := persistentEffectKind(role.Function)
	level := EnemySkillLevel(role.Function)
	value := enemyPersistentRoleValue(role, level, actor)
	effect := persistentBattleEffect(role, value, duration, kind, engine.turn, actor.MemberType, level)
	effect.ListType = listType
	engine.consumeRandomHandRole(role, &effect)
	results := make([]BattleResult, 0, 8)
	if engine.enemyRoleTargetsPlayers(role.Target, selected) {
		for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
			player := &engine.players[memberType-1]
			targetEffect := effect
			if role.Function == "COST_BLOCK" {
				results = append(results, engine.registerPlayerCostBlock(player, role, targetEffect)...)
				continue
			}
			if applied, handled := engine.registerRetainedStatus(memberType, &player.Effects, role, targetEffect); handled {
				results = append(results, applied...)
				continue
			}
			if role.Function == "WEAKNESS" {
				results = append(results, engine.registerWeakness(memberType, role, targetEffect)...)
				continue
			}
			if role.Function == "STAN" && !engine.stunHits(player.Effects, 0, 0, role, level) {
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
				continue
			}
			if isCombatDOTFunction(role.Function) {
				if hasActiveCombatEffect(player.Effects, role.Function) {
					results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
					continue
				}
				// Player bad-status base resistance is currently zero, but the
				// native target consumer still consumes this independent draw.
				if engine.rollPercent10000(0) ||
					!engine.persistentEffectHits(role, level) ||
					engine.effectsResistDOT(player.Effects, role.Function) {
					results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
					continue
				}
				targetEffect.Value = registeredDOTValue(targetEffect.Value, 100, 0)
			}
			immediate, applied := engine.applyPlayerPersistentEffect(player, role, &targetEffect)
			results = append(results, immediate...)
			if applied {
				results = append(results, battlePersistentResult(memberType, role, code, targetEffect))
			} else {
				// Retain the consumer failure; the complete skill projects it
				// only if this role has no successful status targets.
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
			}
		}
		return results, nil
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		enemy := &engine.enemies[index]
		targetEffect := effect
		if applied, handled := engine.registerRetainedStatus(enemy.MemberType, &enemy.Effects, role, targetEffect); handled {
			results = append(results, applied...)
			continue
		}
		if role.Function == "WEAKNESS" {
			results = append(results, engine.registerWeakness(enemy.MemberType, role, targetEffect)...)
			continue
		}
		if role.Function == "STAN" && !engine.stunHits(enemy.Effects, enemy.StatusCooldown[1], enemy.Level.StatusResistances[1], role, level) {
			results = append(results, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
			continue
		}
		if isCombatDOTFunction(role.Function) {
			if hasActiveCombatEffect(enemy.Effects, role.Function) ||
				engine.enemyResistsDOTStatus(enemy, role.Function) ||
				!engine.persistentEffectHits(role, level) ||
				engine.effectsResistDOT(enemy.Effects, role.Function) {
				results = append(results, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
				continue
			}
			targetEffect = engine.dotEffectForEnemy(targetEffect, enemy)
		}
		enemy.Effects = append(enemy.Effects, targetEffect)
		engine.recordAIStatusApplied(enemy.MemberType, targetEffect)
		results = append(results, battlePersistentResult(enemy.MemberType, role, code, targetEffect))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyDealChange(actor *battleEnemy, selected int, role CombatSkillRole, penalty bool) ([]BattleResult, error) {
	code := battleBuffCodes[role.Function]
	value := absInt(combatParameterInt(role.Parameters[0]))
	duration := 2
	if role.Function == "DEAL_PENALTY_TURN_APPOINT" {
		duration = maxInt(1, value)
		value = absInt(combatParameterInt(role.Parameters[1]))
	}
	if value == 0 {
		return nil, nil
	}
	kind := 1
	if penalty {
		kind = 2
	}
	results := make([]BattleResult, 0, maxRoomMembers)
	for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
		player := &engine.players[memberType-1]
		effect := battleEffect{Function: role.Function, Value: value, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: actor.MemberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
		if !applyPlayerDrawEffect(player, effect, code) {
			continue
		}
		results = append(results, battleBuffResultWithParameters(memberType, role.RoleIndex, code, 0, 1, value, 0, 0, 0))
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyRelease(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	level := EnemySkillLevel(role.Function)
	results := make([]BattleResult, 0, 8)
	failed := make([]BattleResult, 0, 8)
	for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
		player := &engine.players[memberType-1]
		_, childRows := engine.releasePlayerEffectsWithRows(player, role, level)
		if len(childRows) > 0 {
			results = append(results, childRows...)
		} else {
			failed = append(failed, battleBuffReleaseFailedResult(memberType, role.RoleIndex))
		}
	}
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		target := &engine.enemies[index]
		_, childRows := engine.releaseEnemyEffectsWithRows(target, role, level)
		if len(childRows) > 0 {
			results = append(results, childRows...)
		} else {
			failed = append(failed, battleBuffReleaseFailedResult(target.MemberType, role.RoleIndex))
		}
	}
	if len(results) == 0 {
		return failed, nil
	}
	return results, nil
}

func (engine *BattleEngine) enemyRolePlayerTargets(actor *battleEnemy, selected int, role CombatSkillRole) (targets []int) {
	defer func() { targets = engine.filterRolePlayers(targets, actor.MemberType, role) }()
	if targets, ok := engine.skillRoleTargets(actor.MemberType, role, true); ok {
		return targets
	}
	kind := role.Target
	if targets, ok := engine.userSideRoleTargets(kind); ok {
		return targets
	}
	if !engine.enemyRoleTargetsPlayers(kind, selected) {
		return nil
	}
	if kind == "USER_ALL" || kind == "FRIEND_ALL" {
		result := make([]int, 0, maxRoomMembers)
		for index := range engine.players {
			if engine.players[index].HP > 0 {
				result = append(result, engine.players[index].MemberType)
			}
		}
		return result
	}
	if combatArthurType(kind) != 0 {
		selected = engine.memberForJob(kind)
	}
	if selected >= 1 && selected <= maxRoomMembers {
		player := &engine.players[selected-1]
		// 79ee0 preserves the action's explicit target after a lethal earlier
		// role. Both subsequent attacks and parameter debuffs still consume it.
		if player.HP > 0 || player.MemberType != 0 && (kind == "SELECT" || kind == "USER_ONE" || kind == "FRIEND_ONE") {
			return []int{selected}
		}
	}
	return nil
}

func (engine *BattleEngine) enemyRoleTargetsPlayers(kind string, selected int) bool {
	// Enemy skill-role SELF is the acting enemy. It cannot reuse the player
	// skill target classifier, where SELF means the acting Arthur.
	if kind == "SELF" {
		return false
	}
	// SELECT inherits the concrete target chosen for the enemy action. Resolve
	// it before combatTargetIsPlayer: that shared helper treats SELECT as a
	// player-side selectable target, which previously discarded every enemy
	// member/part selection (member types 5+).
	if kind == "SELECT" {
		return selected >= 1 && selected <= maxRoomMembers
	}
	if combatTargetIsPlayer(kind) || kind == "USER_ALL" || kind == "USER_ONE" {
		return true
	}
	return false
}

func (engine *BattleEngine) enemyRoleEnemyTargets(actor *battleEnemy, selected int, role CombatSkillRole, includeDead bool) (targets []int) {
	// 8b660 filters source identity AFTER target resolution. Do not reroll or
	// substitute another target when the chosen member is excluded.
	defer func() { targets = engine.filterRoleEnemies(targets, actor.MemberType, role) }()
	if targets, ok := engine.fixedEnemyRoleTargets(role.Target); ok {
		return targets
	}
	if targets, ok := engine.skillRoleTargets(actor.MemberType, role, false); ok {
		return targets
	}
	kind := role.Target
	valid := func(index int) bool {
		return index >= 0 && index < engine.enemyCount && (includeDead || engine.enemies[index].HP > 0)
	}
	if kind == "ENEMY_ALL" || kind == "DEAD_ENEMY_ALL" {
		result := make([]int, 0, engine.enemyCount)
		for index := 0; index < engine.enemyCount; index++ {
			if valid(index) && (kind != "DEAD_ENEMY_ALL" || engine.enemies[index].HP <= 0) {
				result = append(result, index)
			}
		}
		return result
	}
	if kind == "SELF" {
		// Explicit SELF retains the caster even during its death action.
		// Native 8b660 can apply its status roles before the break notification.
		if actor.MemberType >= 5 && actor.MemberType < 5+engine.enemyCount {
			return []int{actor.MemberType - 5}
		}
		return nil
	}
	if selected >= 5 && valid(selected-5) {
		return []int{selected - 5}
	}
	if kind == "DEAD_ENEMY_ONE" {
		for index := 0; index < engine.enemyCount; index++ {
			if engine.enemies[index].HP <= 0 {
				return []int{index}
			}
		}
	}
	return nil
}

func enemyPersistentRoleValue(role CombatSkillRole, level int, actor *battleEnemy) int {
	switch role.Function {
	case "REGENERATE_FIXED":
		return fixedRegenerateRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[5]))
	case "REGENERATE_BY_SELF_PARAM":
		return combatEnemyStatValue(actor, role.Parameters[1])*combatParameterInt(role.Parameters[2])/1000 + combatParameterInt(role.Parameters[3])
	case "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC":
		_, _, _, value := dotRoleValues(role, level, 1, combatEnemyStatValue(actor, role.Parameters[7]))
		return value
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		_, fixed := attributeDefenseRoleValues(role, 0, 1)
		return fixed
	case "CARD_TRAP_DAMAGE":
		return cardTrapDamageRoleValue(role, level, 1, combatEnemyStatValue(actor, role.Parameters[7]))
	case "CRITICAL_UP", "CRITICAL_DOWN", "WEAKNESS":
		return retainedRateRoleValue(role, level)
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		return attackBarrierRoleValue(role, level)
	case "ENCHANT":
		return persistentRoleValue(role, level, playerFromEnemy(actor), 1)
	default:
		return combatParameterInt(role.Parameters[1])
	}
}

func combatEnemyStatValue(enemy *battleEnemy, name string) int {
	switch strings.ToUpper(name) {
	case "HP":
		return enemy.HP
	case "MAX_HP":
		return enemy.MaxHP
	case "ATK":
		return enemy.Attack
	case "INT":
		return enemy.Magic
	case "MND":
		return enemy.Recovery
	case "DEF":
		return enemy.Defense
	case "MDEF":
		return enemy.MDefense
	default:
		return 0
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}
	return value
}

func (engine *BattleEngine) playerEffectValue(player *battlePlayer, function string) int {
	value := 0
	for _, effect := range player.Effects {
		if effect.Function == function {
			value += maxInt(0, effect.Value)
		}
	}
	return value
}

func (engine *BattleEngine) enemyActionHasDefinedRole(skillID int) bool {
	if skillID <= 0 {
		return false
	}
	for _, skill := range engine.catalog.Skills[skillID] {
		if combatRolesDefined(engine.catalog.SkillRoles[skill.FunctionID]) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) enemySkillBase(skillID int) (CombatSkillDefinition, []CombatSkillRole, bool) {
	variants := engine.catalog.Skills[skillID]
	if len(variants) == 0 {
		return CombatSkillDefinition{}, nil, false
	}
	// The first row is the same fallback used by the managed SkillDataContainer
	// before an extension condition selects a higher-priority variant. Role rows
	// are owned by FunctionID, not necessarily by the outer skill ID.
	skill := variants[0]
	roles := engine.catalog.SkillRoles[skill.FunctionID]
	return skill, roles, true
}

func (engine *BattleEngine) selectEnemySkillBranch(actor *battleEnemy, skillID int, selectedTarget int) (CombatSkillDefinition, []CombatSkillRole, bool) {
	skill, roles, _, matched := engine.selectEnemySkillBranchWithIndex(actor, skillID, selectedTarget)
	return skill, roles, matched
}

func (engine *BattleEngine) selectEnemySkillBranchWithIndex(actor *battleEnemy, skillID int, selectedTarget int) (CombatSkillDefinition, []CombatSkillRole, int, bool) {
	return engine.selectEnemySkillBranchMode(actor, skillID, selectedTarget, true)
}

func (engine *BattleEngine) selectEnemySkillBranchMode(actor *battleEnemy, skillID int, selectedTarget int, allowRandom bool) (CombatSkillDefinition, []CombatSkillRole, int, bool) {
	variants := engine.catalog.Skills[skillID]
	var selected CombatSkillDefinition
	selectedIndex := 0
	matched := false
	for index, variant := range variants {
		if !allowRandom && (variant.BranchCondition == "RANDOM" || variant.BranchCondition2 == "RANDOM") {
			continue
		}
		if !engine.enemyBranchConditionSatisfied(actor, selectedTarget, variant.Target, variant.BranchCondition, variant.BranchParameters) ||
			!engine.enemyBranchConditionSatisfied(actor, selectedTarget, variant.Target, variant.BranchCondition2, variant.BranchParameters2) {
			continue
		}
		// Native FUN_000d7578 replaces the selected slot only on a strictly
		// greater priority. Equal-priority rows therefore keep CSV order.
		if !matched || variant.BranchPriority > selected.BranchPriority {
			selected = variant
			selectedIndex = index
			matched = true
		}
	}
	if !matched {
		return CombatSkillDefinition{}, nil, 0, false
	}
	return selected, engine.catalog.SkillRoles[selected.FunctionID], selectedIndex, true
}

func (engine *BattleEngine) enemyBranchConditionSatisfied(actor *battleEnemy, selectedTarget int, _ string, condition string, parameters [5]string) bool {
	condition = strings.ToUpper(strings.TrimSpace(condition))
	if condition == "" || condition == "NONE" {
		return true
	}
	if commonMemberConditionSupported(condition) {
		return engine.commonMemberCondition(condition, actor.MemberType, selectedTarget, parameters)
	}
	parameter := func(index int) string { return strings.ToUpper(strings.TrimSpace(parameters[index])) }
	switch condition {
	case "RANDOM":
		// FUN_000d3f50 uses xor128()%10000 against percent*100.
		return int(engine.rng.next()%10000) < combatParameterInt(parameter(0))*100
	case "DEBUFF_EXEC", "SELF_PARAM_UP_VALUE", "TARGET_PARAM_UP_VALUE", "USER_SIDE_PARAM_UP_VALUE":
		return engine.branchSkillStatistic(condition, actor.MemberType, selectedTarget, parameters)
	case "USER_PARAM", "USER_ENCHANT", "USER_COVERING", "USER_REFLECTION", "USER_REGENERATE":
		return engine.branchMemberCondition(condition, actor.MemberType, parameters)
	case "USER_ATTACK", "USER_ATTACK_NUM":
		return engine.branchAttackCondition(condition, actor.MemberType, parameters)
	case "TARGET_ENEMY_CHARGE_START":
		return engine.branchTargetChargeStarted(selectedTarget)
	case "SELF_BUFF_RELEASE", "TARGET_BUFF_RELEASE", "USER_SIDE_BUFF_RELEASE", "SELF_DEBUFF_RELEASE", "TARGET_DEBUFF_RELEASE", "USER_SIDE_DEBUFF_RELEASE":
		return engine.branchReleaseStatistic(condition, actor.MemberType, selectedTarget, parameters)
	case "TURN":
		return combatValueInRange(engine.turn, parameter(0), parameter(1))
	case "SELF_HP_PER":
		return combatValueInRange(actor.HP*100/maxInt(1, actor.MaxHP), parameter(0), parameter(1))
	case "SELF_HP_FIXED":
		return combatValueInRange(actor.HP, parameter(0), parameter(1))
	case "TARGET_HP_PER":
		hp, maxHP, ok := engine.battleMemberHP(selectedTarget)
		return ok && combatValueInRange(maxInt(0, hp)*100/maxInt(1, maxHP), parameter(0), parameter(1))
	case "TARGET_ATTR":
		return engine.branchTargetAttributeMatches(selectedTarget, parameters)
	case "SELF_TRANCE_GAUGE_STATE", "ENEMY_TRANCE_GAUGE_STATE", "TARGET_TRANCE_GAUGE_STATE":
		return engine.branchEnemyTranceState(condition, actor.MemberType, selectedTarget, parameters[0])
	case "SELF_TRANCE_GAUGE_VALUE":
		return enemyTranceValueMatches(actor, parameters[0], parameters[1], parameters[2])
	case "FRIEND_PLAY_ATTR_NUM", "FRIEND_PLAY_SKILL_KIND_NUM", "FRIEND_PLAY_TAG", "FRIEND_PLAY_COST_NUM", "FRIEND_PLAY_COST_TOTAL":
		return engine.branchFriendSelectedCondition(condition, actor.MemberType, parameters, engine.turnActions)
	case "SELF_ENEMY_AI_FLAG":
		first := combatParameterInt(parameter(0))
		second := combatParameterInt(parameter(1))
		return actor.hasAIFlag(first) || actor.hasAIFlag(second)
	case "SELF_ENEMY_AI_FLAG_AND":
		first := combatParameterInt(parameter(0))
		second := combatParameterInt(parameter(1))
		return actor.hasAIFlag(first) && actor.hasAIFlag(second)
	case "SELF_ENEMY_AI_VAR":
		return enemyAIVariableInRange(actor, parameters[0], parameters[1], parameters[2], true)
	case "FRIEND_PLAY_NUM":
		count := 0
		for _, played := range engine.turnStats.PlayedByUser {
			count += played
		}
		return combatValueInRange(count, parameter(0), parameter(1))
	case "SELF_BUFF", "SELF_DEBUFF":
		return branchHasStatus(actor.Effects, condition == "SELF_BUFF", parameters)
	case "TARGET_BUFF", "TARGET_DEBUFF":
		return branchHasStatus(engine.battleMemberEffects(selectedTarget), condition == "TARGET_BUFF", parameters)
	case "USER_SIDE_BUFF", "USER_SIDE_DEBUFF":
		return engine.branchSideHasStatus(actor.MemberType, true, condition == "USER_SIDE_BUFF", parameters)
	case "ENEMY_SIDE_DEBUFF":
		return engine.branchSideHasStatus(actor.MemberType, false, false, parameters)
	case "SELF_BUFF_FROM_FRIEND", "SELF_DEBUFF_FROM_ENEMY":
		return branchHasSourcedStatus(actor.Effects, actor.MemberType, condition == "SELF_BUFF_FROM_FRIEND", parameters)
	case "TARGET_HOLD_NUM":
		return engine.branchMemberHoldState("HOLD_NUM", selectedTarget, parameters)
	case "TARGET_BUFF_KIND_NUM", "TARGET_DEBUFF_KIND_NUM":
		_, _, ok := engine.battleMemberHP(selectedTarget)
		return ok && combatValueInRange(branchStatusKindCount(engine.battleMemberEffects(selectedTarget), condition == "TARGET_BUFF_KIND_NUM"), parameter(0), parameter(1))
	case "USER_SIDE_BLESS":
		// d2a0a tests d2976 for each living user. It is ANY user's filtered
		// BLESS count in range, not a party-wide sum including CURSE/KO.
		for index := range engine.players {
			player := &engine.players[index]
			if player.HP > 0 && combatValueInRange(playerAppendCardCount(player, 22, parameter(0)), parameter(1), parameter(2)) {
				return true
			}
		}
		return false
	case "SELF_DAMAGE", "FRIEND_DAMAGE", "SELF_ENCHANT_DAMAGE", "RECOVERY_ACTION":
		return engine.branchDamageStatistic(condition, actor.MemberType, parameters)
	case "ENEMY_DEAD_NOW_TURN":
		return engine.branchEnemyDiedThisTurn(parameter(0))
	default:
		return false
	}
}

func SupportsEnemyBranchCondition(condition string) bool {
	if commonMemberConditionSupported(strings.ToUpper(strings.TrimSpace(condition))) {
		return true
	}
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "FRIEND_PLAY_ATTR_NUM", "FRIEND_PLAY_SKILL_KIND_NUM", "FRIEND_PLAY_TAG", "FRIEND_PLAY_COST_NUM", "FRIEND_PLAY_COST_TOTAL", "SELF_TRANCE_GAUGE_VALUE":
		return true
	case "USER_ATTACK", "USER_ATTACK_NUM", "TARGET_ENEMY_CHARGE_START":
		return true
	case "USER_PARAM", "USER_ENCHANT", "USER_COVERING", "USER_REFLECTION", "USER_REGENERATE":
		return true
	case "SELF_BUFF_RELEASE", "TARGET_BUFF_RELEASE", "USER_SIDE_BUFF_RELEASE", "SELF_DEBUFF_RELEASE", "TARGET_DEBUFF_RELEASE", "USER_SIDE_DEBUFF_RELEASE":
		return true
	case "FRIEND_DAMAGE", "SELF_ENCHANT_DAMAGE", "RECOVERY_ACTION":
		return true
	case "DEBUFF_EXEC", "SELF_PARAM_UP_VALUE", "TARGET_PARAM_UP_VALUE", "USER_SIDE_PARAM_UP_VALUE":
		return true
	case "", "NONE", "RANDOM", "TURN", "SELF_HP_PER", "SELF_HP_FIXED", "TARGET_HP_PER", "TARGET_ATTR",
		"SELF_ENEMY_AI_FLAG", "SELF_ENEMY_AI_FLAG_AND", "SELF_ENEMY_AI_VAR", "FRIEND_PLAY_NUM", "SELF_BUFF", "SELF_DEBUFF",
		"TARGET_BUFF", "TARGET_DEBUFF", "USER_SIDE_BUFF", "USER_SIDE_DEBUFF", "ENEMY_SIDE_DEBUFF",
		"TARGET_DEBUFF_KIND_NUM", "USER_SIDE_BLESS", "SELF_DAMAGE", "ENEMY_DEAD_NOW_TURN":
		return true
	case "SELF_TRANCE_GAUGE_STATE", "ENEMY_TRANCE_GAUGE_STATE", "TARGET_TRANCE_GAUGE_STATE":
		return true
	case "SELF_BUFF_FROM_FRIEND", "SELF_DEBUFF_FROM_ENEMY", "TARGET_BUFF_KIND_NUM", "TARGET_HOLD_NUM":
		return true
	default:
		return false
	}
}

func SupportsPlayerBranchCondition(condition string) bool {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "FRIEND_PLAY_COST_NUM":
		return true
	case "USER_ATTACK", "USER_ATTACK_NUM", "TARGET_ENEMY_CHARGE_START", "ENEMY_DEAD_NOW_TURN":
		return true
	case "USER_PARAM", "USER_ENCHANT", "USER_COVERING", "USER_REFLECTION", "USER_REGENERATE":
		return true
	case "SELF_BUFF_RELEASE", "TARGET_BUFF_RELEASE", "USER_SIDE_BUFF_RELEASE", "SELF_DEBUFF_RELEASE", "TARGET_DEBUFF_RELEASE", "USER_SIDE_DEBUFF_RELEASE":
		return true
	case "SELF_DAMAGE", "FRIEND_DAMAGE", "SELF_ENCHANT_DAMAGE", "RECOVERY_ACTION":
		return true
	case "DEBUFF_EXEC", "SELF_PARAM_UP_VALUE", "TARGET_PARAM_UP_VALUE", "USER_SIDE_PARAM_UP_VALUE":
		return true
	case "SELF_HOLD_NUM", "TARGET_HOLD_NUM", "SELF_BUFF_FROM_FRIEND", "SELF_DEBUFF_FROM_ENEMY", "TARGET_BUFF_KIND_NUM", "TARGET_DEBUFF_KIND_NUM":
		return true
	case "TARGET_CARD_ONLY_SELF_PLAY", "OWNER_CARD_DECK_INDEX", "CARD_DECK_INDEX", "OWNER_CARD_FAME", "SUMMONS_EVERY_TURN":
		return true
	case "", "BUFF_EXEC", "DECK_COMBO_COUNT", "ENEMY_SIDE_BUFF",
		"ENEMY_SIDE_DEBUFF", "ENEMY_TRANCE_GAUGE_STATE", "ENEMY_TRIBAL", "FRIEND_CRITICAL",
		"FRIEND_HP_FIXED", "FRIEND_HP_PER", "FRIEND_PLAY_ATTR_NUM", "FRIEND_PLAY_COST_TOTAL",
		"FRIEND_PLAY_MOST_HIGH_COST", "FRIEND_PLAY_MOST_LOW_COST", "FRIEND_PLAY_NUM", "FRIEND_PLAY_SKILL_KIND_NUM",
		"FRIEND_PLAY_TAG", "FRIEND_PLAYED_SKILL_KIND", "FRIEND_PLAYED_TAG", "NONE", "OWNER_CARD_CARD_BUFF", "RANDOM",
		"SELF_ALL_DECK_TAG", "SELF_ARTHUR_TYPE", "SELF_BLESS", "SELF_BUFF",
		"SELF_BURST_BUFF", "SELF_BURST_GAUGE_STATE", "SELF_BURST_GAUGE_VALUE", "SELF_CARD_BUFF",
		"SELF_CRITICAL", "SELF_CURSE", "SELF_DEBUFF", "SELF_HP_FIXED",
		"SELF_HP_PER", "SELF_MAIN_DECK_ATTR", "SELF_MAIN_DECK_SKILL_KIND", "SELF_MAIN_DECK_TAG",
		"SELF_NOT_PLAY_HAND_NUM", "SELF_OTHER_PLAY_ATTR", "SELF_OTHER_PLAY_NUM", "SELF_OTHER_PLAY_RARITY",
		"SELF_OTHER_PLAY_SKILL_KIND", "SELF_OTHER_PLAY_TAG", "SELF_PLAY_COST_NUM", "SELF_PLAY_COST_TOTAL",
		"SELF_PLAY_MOST_HIGH_COST", "SELF_PLAY_MOST_LOW_COST", "SELF_PLAYED_SKILL_KIND", "SELF_PLAYED_TAG", "SELF_SUPPORT_DECK_TAG", "TARGET_ATTR",
		"TARGET_BLESS", "TARGET_BUFF", "TARGET_BURST_BUFF", "TARGET_BURST_GAUGE_STATE",
		"TARGET_BURST_GAUGE_VALUE", "TARGET_CARD_BUFF", "TARGET_CRITICAL", "TARGET_CURSE",
		"TARGET_DEBUFF", "TARGET_EXEC_BLESS", "TARGET_EXEC_CURSE", "TARGET_HP_FIXED",
		"TARGET_HP_PER", "TARGET_JOB_TYPE", "TARGET_TRANCE_GAUGE_STATE", "TARGET_TRIBAL",
		"TURN", "USER_SIDE_BLESS", "USER_SIDE_BUFF", "USER_SIDE_BURST_BUFF",
		"USER_SIDE_BURST_GAUGE_STATE", "USER_SIDE_BURST_GAUGE_VALUE", "USER_SIDE_CARD_BUFF", "USER_SIDE_CURSE",
		"USER_SIDE_DEBUFF", "USER_SIDE_EXEC_BLESS", "USER_SIDE_EXEC_CURSE":
		return true
	default:
		return false
	}
}
