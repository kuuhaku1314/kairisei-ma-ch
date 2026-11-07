package battlecore

import (
	"errors"
	"fmt"
	"sort"
	"strings"
)

type enemyActionCandidate struct {
	action        CombatEnemyAction
	triggerTarget int
}

type enemyMemberActionPlan struct {
	enemyIndex int
	actions    []enemyActionCandidate
}

func enemyActionUseKey(enemy *battleEnemy, action CombatEnemyAction) int {
	return enemy.MemberType*32 + action.Slot
}

func (engine *BattleEngine) eligibleEnemyAction(enemy *battleEnemy, action CombatEnemyAction, consumed int) (enemyActionCandidate, bool) {
	if action.SkillID == 0 || action.MaxUses <= 0 || engine.enemyUses[enemyActionUseKey(enemy, action)] >= action.MaxUses ||
		consumed+action.ActionCost > enemy.Level.ActionsPerTurn {
		return enemyActionCandidate{}, false
	}
	engine.enemyTriggerTarget = 0
	if !engine.enemyActionHasDefinedRole(action.SkillID) || !engine.enemyActionConditionSatisfied(enemy, action) {
		return enemyActionCandidate{}, false
	}
	return enemyActionCandidate{action: action, triggerTarget: engine.enemyTriggerTarget}, true
}

func (engine *BattleEngine) tryScheduleEnemyAction(enemy *battleEnemy, action CombatEnemyAction, consumed *int) (enemyActionCandidate, bool) {
	candidate, eligible := engine.eligibleEnemyAction(enemy, action, *consumed)
	if !eligible {
		return enemyActionCandidate{}, false
	}
	succeeded := int(engine.rng.next()%10000) < action.Rate*100
	if succeeded || action.CountOnMiss {
		*consumed += action.ActionCost
	}
	if !succeeded {
		return enemyActionCandidate{}, false
	}
	if engine.enemyUses == nil {
		engine.enemyUses = make(map[int]int)
	}
	engine.enemyUses[enemyActionUseKey(enemy, action)]++
	return candidate, true
}

func (engine *BattleEngine) buildEnemyActionPlan(enemy *battleEnemy) []enemyActionCandidate {
	if enemy == nil || enemy.HP <= 0 {
		return nil
	}
	consumed := enemy.ActionConsumed
	plan := make([]enemyActionCandidate, 0, maxInt(0, minInt(20, enemy.Level.ActionsPerTurn)))
	// 5b0dc already rolled/charged the five special slots at TurnPhase.
	// 64670 adds ordinary actions to that SAME queue, then58412 sorts and
	// 58552 executes it. Do not roll or charge those entries a second time.
	plan = append(plan, enemy.ChargedActions[:enemy.ChargedActionCount]...)
	// Native FUN_0005b56a tests each of the fourteen ordinary skill slots once
	// in CSV order. Successful and rate-failed/count-on-miss rows both consume
	// their declared action cost before the normal attack fills what remains.
	for _, action := range enemy.Level.Actions {
		if action.Category != "skill" || len(plan) >= 20 {
			continue
		}
		if candidate, ok := engine.tryScheduleEnemyAction(enemy, action, &consumed); ok {
			plan = append(plan, candidate)
		}
	}
	var normal *CombatEnemyAction
	for index := range enemy.Level.Actions {
		if enemy.Level.Actions[index].Category == "normal" {
			normal = &enemy.Level.Actions[index]
			break
		}
	}
	if normal != nil {
		// FUN_0005b7a8 retries the one normal slot at most 20-currentCount
		// times. A failed rate with CountOnMiss=false therefore may retry; a
		// cost that no longer fits ends the fill immediately.
		attempts := 20 - len(plan)
		for attempt := 0; attempt < attempts; attempt++ {
			if consumed+normal.ActionCost > enemy.Level.ActionsPerTurn {
				break
			}
			if candidate, ok := engine.tryScheduleEnemyAction(enemy, *normal, &consumed); ok {
				plan = append(plan, candidate)
			}
		}
	}
	sortEnemyActionPlan(plan)
	enemy.ActionConsumed = consumed
	return plan
}

func (engine *BattleEngine) buildEnemyChargeStartPlan(enemy *battleEnemy) []enemyActionCandidate {
	if enemy == nil || enemy.HP <= 0 {
		return nil
	}
	consumed := enemy.ActionConsumed
	plan := make([]enemyActionCandidate, 0, 5)
	// enemy_lvup's five 澶ф妧 slots are selected before ordinary attacks.
	// Native FUN_0005b0dc tests them once at turn phase, consumes the same
	// per-turn action budget and emits CHARGE_START; roles execute later from
	// the retained queue, not during TurnPhase.
	for _, action := range enemy.Level.Actions {
		if action.Category != "special" || len(plan) >= len(enemy.ChargedActions) {
			continue
		}
		if candidate, ok := engine.tryScheduleEnemyAction(enemy, action, &consumed); ok {
			plan = append(plan, candidate)
		}
	}
	enemy.ActionConsumed = consumed
	enemy.ChargedActionCount = copy(enemy.ChargedActions[:], plan)
	return plan
}

// 58412 uses c6e10's right-to-left bubble pass. Original x86 4ceb5 compares
// priority first; on a tie it compares the addresses of the adjacent queue
// entries, returning +1 for the earlier address. Therefore ties swap too,
// without RNG. A stable sort changes same-priority buff/attack ordering.
func sortEnemyActionPlan(plan []enemyActionCandidate) {
	for boundary := 0; boundary < len(plan)-1; boundary++ {
		for i := len(plan) - 2; i >= boundary; i-- {
			if plan[i].action.Priority >= plan[i+1].action.Priority {
				plan[i], plan[i+1] = plan[i+1], plan[i]
			}
		}
	}
}

func (engine *BattleEngine) enemyAttackSign(enemy *battleEnemy) int {
	if enemy == nil || enemy.HP <= 0 || combatEffectCount(enemy.Effects, "STAN", "") > 0 {
		return 0
	}
	// 5bb7a copies the already charged queue and budget, but NOT the use
	// counters. Ordinary skill slots require Rate>=100 before checking AI;
	// normal fill has no rate gate and stops at its first failed eligibility.
	// No rate rolls/use increments occur. AI-trigger RNG still commits.
	probe := *engine
	plan := append([]enemyActionCandidate(nil), enemy.ChargedActions[:enemy.ChargedActionCount]...)
	consumed := enemy.ActionConsumed
	for _, action := range enemy.Level.Actions {
		if action.Category != "skill" || action.Rate < 100 || len(plan) >= 20 {
			continue
		}
		if candidate, ok := probe.eligibleEnemyAction(enemy, action, consumed); ok {
			consumed += action.ActionCost
			plan = append(plan, candidate)
		}
	}
	for _, action := range enemy.Level.Actions {
		if action.Category != "normal" {
			continue
		}
		for len(plan) < 20 {
			candidate, ok := probe.eligibleEnemyAction(enemy, action, consumed)
			if !ok {
				break
			}
			consumed += action.ActionCost
			plan = append(plan, candidate)
		}
		break
	}
	engine.rng = probe.rng
	sign := 0
	for _, candidate := range plan {
		skill, _, ok := engine.enemySkillBase(candidate.action.SkillID)
		if !ok || skill.Kind != "ATTACK" && skill.Kind != "SORCERY" {
			continue
		}
		sign |= 1 << combatPhysicsIndex(skill.DamageKind)
	}
	return sign
}

func (engine *BattleEngine) executeEnemyActionCandidate(enemy *battleEnemy, candidate enemyActionCandidate) ([]BattleResult, error) {
	engine.enemyTriggerTarget = candidate.triggerTarget
	action := candidate.action
	// Native selectors may return zero without a recipient. The skill's outer
	// target gate below owns whether that cancels execution or keeps SELF/all-side.
	target, _ := engine.selectEnemyActionTarget(enemy, action)
	// The queued target selector still runs after an earlier action kills its
	// owner. Ordinary skill execution then stops; death actions use their own mode.
	if enemy.HP <= 0 && action.Category != "death" {
		return nil, nil
	}
	skill, roles, matched := engine.selectEnemySkillBranch(enemy, action.SkillID, target)
	if !matched {
		return nil, nil
	}
	if len(roles) == 0 {
		return nil, fmt.Errorf("enemy skill function %d is incomplete", skill.FunctionID)
	}
	// 7adb0 returns before 50 when the selected branch's first role is NONE.
	// Scheduling and AI target selection have already consumed their RNG/budget.
	if roles[0].Function == "NONE" {
		return nil, nil
	}
	if !combatRolesDefined(roles) {
		return nil, nil
	}
	// 7adb0 resolves the outer list before emitting 50 or executing roles.
	// An empty player target is skipped; enemy/self buffs can still execute after
	// the last player falls, so HP alone must not stop the enemy action queue.
	if skill.Target == "USER_ALL" && len(engine.playerTargetCandidates(false)) == 0 || skill.Target == "USER_ONE" && target == 0 {
		return nil, nil
	}
	if skill.Target == "ENEMY_ONE" && target == 0 {
		return nil, nil
	}
	if skill.Target == "DEAD_ENEMY_ALL" && len(engine.enemyTargetCandidates(true)) == 0 {
		return nil, nil
	}
	if skill.Target == "DEAD_ENEMY_ONE" && (target < 5 || target >= 5+engine.enemyCount || engine.enemies[target-5].HP > 0) {
		// The native outer dead-target filter runs before ResultCmd50. A
		// living/player selection cannot become an empty visible revive action.
		return nil, nil
	}
	level := 1
	if action.Category == "gauge" {
		level = 0
	}
	skillResult, err := enemySkillResult(enemy.MemberType, action.SkillID, target, skill, level, false)
	if err != nil {
		return nil, err
	}
	results := []BattleResult{skillResult}
	skillRows, err := engine.executeSkillRoleSet(enemy.MemberType, target, skill, roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
		resolvedRole := resolveEnemyActionRole(role, skill, target)
		return engine.executeEnemyRole(enemy, target, resolvedRole, roles)
	})
	if err != nil {
		return nil, err
	}
	results = append(results, skillRows...)
	engine.nativeSkillSerial++
	displayResults, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	if engine.endType == 0 && engine.allEnemyBodiesDead() {
		engine.endType = engine.enemyWaveEndType()
	}
	if engine.forceEndCheck {
		engine.resolveForcedBattleEnd()
	}
	return results, nil
}

// The selected header member and the outer target list have separate owners.
// Normal and passive enemy skills resolve SELECT through the same outer kind.
func resolveEnemyActionRole(role CombatSkillRole, skill CombatSkillDefinition, target int) CombatSkillRole {
	role.SourceSkillID = skill.ID
	if role.Target == "SELECT" && (target == 0 || skill.Target == "SELF" || skill.Target == "USER_ALL" || skill.Target == "ENEMY_ALL" || skill.Target == "DEAD_ENEMY_ALL") {
		role.Target = skill.Target
	}
	return role
}

func (engine *BattleEngine) EnemyPhase() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseUserAttack && engine.phase != PhaseChaliceUser {
		return nil, errors.New("combat enemy phase is unavailable")
	}
	results := make([]BattleResult, 0, engine.enemyCount*12)
	plans := make([]enemyMemberActionPlan, 0, engine.enemyCount)
	for enemyIndex := 0; enemyIndex < engine.enemyCount; enemyIndex++ {
		enemy := &engine.enemies[enemyIndex]
		if enemy.HP <= 0 {
			continue
		}
		if combatEffectCount(enemy.Effects, "STAN", "") > 0 {
			// Native FUN_00064670 emits ResultCmd31(member) and omits the
			// stunned enemy from this enemy-action phase.
			results = append(results, BattleResult{Command: CommandStun, Args: []int64{int64(enemy.MemberType)}})
			continue
		}
		if plan := engine.buildEnemyActionPlan(enemy); len(plan) > 0 {
			plans = append(plans, enemyMemberActionPlan{enemyIndex: enemyIndex, actions: plan})
		}
	}
	actionRows, err := engine.executeEnemyPlans(plans)
	if err != nil {
		return nil, err
	}
	results = append(results, actionRows...)
	if engine.story && engine.endType == 3 {
		return engine.finishStoryPhase(results, 0)
	}
	if engine.endType == 0 && !battleResultsContainCommand(results, CommandSkill) &&
		!battleResultsContainCommand(results, CommandStun) {
		// 64670 emits WAIT_AND_SEE only without skill or stun rows.
		results = append(results, BattleResult{Command: CommandWaitAndSee})
	}
	holdRows, err := engine.executeCardHolds("ENEMY_ACTION_END")
	if err != nil {
		return nil, err
	}
	results = append(results, holdRows...)
	// JP 8.0.7 enemy_phase: ordinary card holds (740b4), BLESS and CURSE
	// (74490 with phase 10, types 1 then 0), then player DOT/death/trance.
	// Snapshot each pass separately, retaining its own terminal/cleanup gate.
	for _, cardType := range []int{22, 21} {
		appendRows, err := engine.executeBlessHolds("ENEMY_ACTION_END", cardType)
		if err != nil {
			return nil, err
		}
		results = append(results, appendRows...)
	}
	if engine.endType == 0 {
		results = append(results, engine.tickPlayerDOTEffects()...)
	}
	results = engine.settlePlayerDeaths(results)
	results = append(results, engine.advanceTranceStates()...)
	if engine.beginStoryEnd(true) {
		return engine.finishStoryPhase(results, 0)
	}
	engine.resumeSide = 0
	engine.flushTokenStock()
	if engine.endType != 0 {
		engine.phase = PhaseEnded
		results = append(results, battleEndResult(engine.endType))
		return engine.appendTerminalBuffCleanup(results)
	} else {
		engine.phase = PhaseEnemy
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyPlans(plans []enemyMemberActionPlan) ([]BattleResult, error) {
	var results []BattleResult
	// FUN_0006f431/FUN_00061c90 orders enemy owners by their first planned
	// action priority, then by member ID. Every action of one owner remains a
	// contiguous group after its own plan has been sorted.
	sort.SliceStable(plans, func(left int, right int) bool {
		leftAction, rightAction := plans[left].actions[0].action, plans[right].actions[0].action
		if leftAction.Priority == rightAction.Priority {
			return engine.enemies[plans[left].enemyIndex].MemberType < engine.enemies[plans[right].enemyIndex].MemberType
		}
		return leftAction.Priority < rightAction.Priority
	})
	for _, plan := range plans {
		enemy := &engine.enemies[plan.enemyIndex]
		for _, candidate := range plan.actions {
			if engine.endType != 0 {
				break
			}
			actionResults, err := engine.executeEnemyActionCandidate(enemy, candidate)
			if err != nil {
				return nil, err
			}
			results = append(results, actionResults...)
			if engine.beginStoryEnd(true) {
				break
			}
		}
		if engine.endType != 0 {
			break
		}
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyFirstAttack() ([]BattleResult, error) {
	// 1d8f0 -> 65680 selects only ordinary skill slots at wave turn zero.
	// JP keeps the first-attack cost for this turn's charge/sign/action budget;
	// the next TurnPhase resets it together with the ordinary queue.
	turn := engine.turn
	engine.turn = engine.enemyStartTurn
	defer func() { engine.turn = turn }()
	var plans []enemyMemberActionPlan
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		if enemy.HP <= 0 {
			continue
		}
		consumed := 0
		var plan []enemyActionCandidate
		for _, action := range enemy.Level.Actions {
			// An implicit condition has no explicit turn-zero enable bit.
			if action.Category != "skill" || (action.AIConditionID == 0 && action.Order == nil) || len(plan) >= 20 {
				continue
			}
			if candidate, ok := engine.tryScheduleEnemyAction(enemy, action, &consumed); ok {
				plan = append(plan, candidate)
			}
		}
		enemy.ActionConsumed = consumed
		if len(plan) > 0 {
			sortEnemyActionPlan(plan)
			plans = append(plans, enemyMemberActionPlan{enemyIndex: index, actions: plan})
		}
	}
	if len(plans) == 0 {
		return nil, nil
	}
	rows, err := engine.executeEnemyPlans(plans)
	if err != nil {
		return nil, err
	}
	results := append([]BattleResult{{Command: CommandEnemyFirstAttack}}, rows...)
	if engine.endType == 0 {
		results = append(results, BattleResult{Command: CommandAttackPartition})
	}
	return results, nil
}

func battleResultsContainCommand(results []BattleResult, command int) bool {
	for _, result := range results {
		if result.Command == command {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) resolveForcedBattleEnd() {
	engine.forceEndCheck = false
	engine.endType = engine.enemyWaveEndType()
}

// JP checkEnd (5f6c0) uses the same awake scan after ordinary body defeat
// and a forced end. The scan at5f60f includes KO members; the flag itself
// does not end a living encounter or declare a successor party.
func (engine *BattleEngine) enemyWaveEndType() int {
	for index := 0; index < engine.enemyCount; index++ {
		if engine.enemies[index].Awake != 0 {
			return 4
		}
	}
	return 1
}

func (engine *BattleEngine) selectEnemyAction(enemy *battleEnemy) (CombatEnemyAction, int, bool) {
	for _, candidate := range engine.buildEnemyActionPlan(enemy) {
		engine.enemyTriggerTarget = candidate.triggerTarget
		if target, ok := engine.selectEnemyActionTarget(enemy, candidate.action); ok {
			return candidate.action, target, true
		}
	}
	engine.enemyTriggerTarget = 0
	return CombatEnemyAction{}, 0, false
}

func enemyFixedUserOrder(kind string) [4]int {
	switch kind {
	case "USER1":
		return [4]int{1, 2, 3, 4}
	case "USER2":
		return [4]int{2, 3, 4, 1}
	case "USER3":
		return [4]int{3, 4, 2, 1}
	case "USER4":
		return [4]int{4, 3, 2, 1}
	}
	return [4]int{}
}

func (engine *BattleEngine) selectEnemyActionTarget(enemy *battleEnemy, action CombatEnemyAction) (int, bool) {
	kind := strings.ToUpper(strings.TrimSpace(action.Target))
	if isEnemyFilteredTarget(kind) {
		return engine.selectEnemyFilteredTarget(action, kind)
	}
	if kind == "" || kind == "NULL" {
		// 57530 -> 4ce6c selects zero. The outer skill resolves its own
		// target list afterwards; it does not turn NULL into a random draw.
		return 0, true
	}
	if kind == "USER_ALL" || kind == "ENEMY_ALL" || kind == "FRIEND_ALL" || kind == "ALL" {
		return 0, true
	}
	if kind == "SELF" {
		return enemy.MemberType, true
	}
	if order := enemyFixedUserOrder(kind); order[0] != 0 {
		// Indexed member selectors use fixed fallback orders, independently
		// of profession and without drawing a random replacement target.
		for _, member := range order {
			if player := &engine.players[member-1]; player.MemberType != 0 && player.HP > 0 {
				return engine.finalizeEnemyPlayerTarget(member, false)
			}
		}
		return 0, false
	}
	if kind == "SUMMONS" {
		candidates := engine.playerTargetCandidates(false)
		if len(candidates) == 1 {
			return engine.finalizeEnemyPlayerTarget(candidates[0], false)
		}
		var summoned []int
		for _, member := range candidates {
			if engine.players[member-1].SummonSource != 0 {
				summoned = append(summoned, member)
			}
		}
		if len(summoned) > 0 {
			candidates = summoned
		}
		return engine.randomFinalizedPlayerTarget(candidates, false)
	}
	if kind == "TRIGGER_TARGET" {
		target := engine.enemyTriggerTarget
		if target >= 1 && target <= maxRoomMembers && engine.player(target).HP > 0 {
			return engine.finalizeEnemyPlayerTarget(target, false)
		}
		if target >= 5 && target < 5+engine.enemyCount && engine.enemies[target-5].HP > 0 {
			return target, true
		}
		// FUN_0004e48a falls back through FUN_0004e448 when the retained
		// target no longer exists/alive. It does not cancel the action.
		return engine.enemyDefaultRandomTarget(action)
	}
	if strings.HasPrefix(kind, "ENEMY") && len(kind) == len("ENEMY1") {
		index := combatParameterInt(strings.TrimPrefix(kind, "ENEMY")) - 1
		skill, _, _ := engine.enemySkillBase(action.SkillID)
		deadTarget := skill.Target == "DEAD_ENEMY_ONE"
		if index >= 0 && index < engine.enemyCount && deadTarget == (engine.enemies[index].HP <= 0) {
			return 5 + index, true
		}
		// FUN_0004ddab chooses the first eligible enemy when the explicit
		// part is unavailable, with no random draw (including revive skills).
		if candidates := engine.enemyTargetCandidates(deadTarget); len(candidates) > 0 {
			return 5 + candidates[0], true
		}
		return 0, false
	}
	if arthurType := combatArthurType(strings.TrimSuffix(kind, "_INVOLVE_DEAD")); arthurType != 0 {
		return engine.selectEnemyArthurTarget(arthurType, strings.HasSuffix(kind, "_INVOLVE_DEAD"))
	}
	if kind == "ENEMY_ONE" || kind == "DEAD_ENEMY_RANDOM" || strings.HasSuffix(kind, "_ENEMY") {
		candidates := engine.enemyTargetCandidates(kind == "DEAD_ENEMY_RANDOM")
		if len(candidates) == 0 {
			return 0, false
		}
		if kind == "ENEMY_ONE" || kind == "DEAD_ENEMY_RANDOM" {
			return candidates[int(engine.rng.next()%uint32(len(candidates)))] + 5, true
		}
		best, ok := engine.selectEnemyMetricTarget(kind, candidates)
		if !ok {
			return 0, false
		}
		return best + 5, true
	}
	if kind == "WEAKNESS_USER" || kind == "WEAKNESS_USER_NOT_FOR_RANDOM" || kind == "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM" {
		includeDead := kind == "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM"
		for _, memberType := range engine.playerTargetCandidates(includeDead) {
			if engine.playerHasDebuffKind(memberType, "WEAKNESS") {
				return engine.finalizeEnemyPlayerTarget(memberType, includeDead)
			}
		}
		if kind == "WEAKNESS_USER" {
			return 0, false
		}
		return engine.enemyDefaultRandomTarget(action)
	}
	if kind == "USER_DEBUFF" {
		candidates := make([]int, 0, maxRoomMembers)
		for _, memberType := range engine.playerTargetCandidates(false) {
			if engine.playerMatchesEnemyDebuffSelector(memberType, action.TargetParams) {
				candidates = append(candidates, memberType)
			}
		}
		if len(candidates) == 0 {
			candidates = engine.playerTargetCandidates(false)
		}
		return engine.randomFinalizedPlayerTarget(candidates, false)
	}
	if kind == "RANDOM_EXCEPT_HATE1" {
		candidates := engine.playerTargetCandidates(false)
		if len(candidates) == 1 {
			// 57970 returns the sole living member before the random draw.
			return engine.finalizeEnemyPlayerTarget(candidates[0], false)
		}
		if len(candidates) > 1 {
			hate1, _ := engine.playerHateRankTarget(1)
			kept := candidates[:0]
			for _, candidate := range candidates {
				if candidate != hate1 {
					kept = append(kept, candidate)
				}
			}
			candidates = kept
		}
		return engine.randomFinalizedPlayerTarget(candidates, false)
	}
	if kind == "HATE1_OR_RANDOM" {
		if engine.rng.next()%10000 < 5000 {
			target, ok := engine.playerHateRankTarget(1)
			if !ok {
				return 0, false
			}
			return engine.finalizeEnemyPlayerTarget(target, false)
		}
		return engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: "RANDOM_EXCEPT_HATE1"})
	}
	if strings.HasPrefix(kind, "HATE") && len(kind) == len("HATE1") {
		target, ok := engine.playerHateRankTarget(combatParameterInt(strings.TrimPrefix(kind, "HATE")))
		if !ok {
			return 0, false
		}
		return engine.finalizeEnemyPlayerTarget(target, false)
	}
	if kind == "SELECT" || kind == "USER_ONE" || kind == "FRIEND_ONE" || kind == "RANDOM" || kind == "RANDOM_INVOLVE_DEAD" || strings.HasPrefix(kind, "RANDOM_EXCEPT_") {
		if kind == "RANDOM" {
			if skill, _, ok := engine.enemySkillBase(action.SkillID); ok && skill.Target == "ENEMY_ONE" {
				// Native RANDOM uses 4e448, whose pool follows the outer skill.
				return engine.enemyDefaultRandomTarget(action)
			}
		}
		includeDead := kind == "RANDOM_INVOLVE_DEAD"
		candidates := engine.playerTargetCandidates(includeDead)
		if kind == "RANDOM" && len(candidates) == 0 {
			// An empty display target does not cancel queued SELF/all-enemy
			// roles. 58552 still emits 50(target=0), without a target RNG draw.
			return 0, true
		}
		excluded := combatArthurType(strings.TrimPrefix(kind, "RANDOM_EXCEPT_"))
		if excluded != 0 {
			// FUN_0004d630 keeps the sole surviving member even when it is
			// the excluded profession, without consuming another random value.
			if len(candidates) == 1 {
				return engine.finalizeEnemyPlayerTarget(candidates[0], includeDead)
			}
			kept := candidates[:0]
			for _, candidate := range candidates {
				if engine.players[candidate-1].ArthurType != excluded {
					kept = append(kept, candidate)
				}
			}
			candidates = kept
		}
		return engine.randomFinalizedPlayerTarget(candidates, includeDead)
	}
	if kind == "BULLY" {
		// BULLY chooses the lowest current HP,then the first member on a tie.
		// LOW_HP_USER uses a different selector whose tied sort consumes RNG.
		best := 0
		for _, member := range engine.playerTargetCandidates(false) {
			if best == 0 || engine.players[member-1].HP < engine.players[best-1].HP {
				best = member
			}
		}
		return engine.finalizeEnemyPlayerTarget(best, false)
	}
	if strings.HasSuffix(kind, "_USER") {
		best, ok := engine.selectPlayerMetricTarget(kind, engine.playerTargetCandidates(false))
		if !ok {
			return 0, false
		}
		return engine.finalizeEnemyPlayerTarget(best, false)
	}
	return 0, false
}

// FUN_0004e448 resolves the fallback side from the outer skill, not from the
// failed selector. The native ENEMY_ONE branch draws from living enemies;
// other active PvE branches draw from living players.
func (engine *BattleEngine) enemyDefaultRandomTarget(action CombatEnemyAction) (int, bool) {
	if skill, _, ok := engine.enemySkillBase(action.SkillID); ok && skill.Target == "ENEMY_ONE" {
		candidates := engine.enemyTargetCandidates(false)
		if len(candidates) == 0 {
			return 0, false
		}
		return candidates[int(engine.rng.next()%uint32(len(candidates)))] + 5, true
	}
	return engine.randomFinalizedPlayerTarget(engine.playerTargetCandidates(false), false)
}

// Native ENEMY_AI_TARGET table 0x00122040 entries 47..50 and 53..56
// call FUN_0004f330: filter by user Arthur type, falling back to the eligible
// party when that class is unavailable. With more than one eligible user,
// native consumes RNG even if the filtered class has only one member.
func (engine *BattleEngine) selectEnemyArthurTarget(arthurType int, includeDead bool) (int, bool) {
	candidates := engine.playerTargetCandidates(includeDead)
	if len(candidates) == 1 {
		return engine.finalizeEnemyPlayerTarget(candidates[0], includeDead)
	}
	matching := make([]int, 0, len(candidates))
	for _, memberType := range candidates {
		if engine.players[memberType-1].ArthurType == arthurType {
			matching = append(matching, memberType)
		}
	}
	if len(matching) > 0 {
		candidates = matching
	}
	return engine.randomFinalizedPlayerTarget(candidates, includeDead)
}

func (engine *BattleEngine) playerTargetCandidates(includeDead bool) []int {
	targets := make([]int, 0, maxRoomMembers)
	for index := range engine.players {
		player := &engine.players[index]
		// INVOLVE_DEAD means HP-zero members awaiting gameover, not members
		// already retired. Native 6f390/ce708/4e4e0 use the separate 7e958 flag.
		if player.MemberType == 0 || !includeDead && player.HP <= 0 || includeDead && player.GameOver {
			continue
		}
		targets = append(targets, player.MemberType)
	}
	return targets
}

func (engine *BattleEngine) enemyTargetCandidates(dead bool) []int {
	targets := make([]int, 0, engine.enemyCount)
	for index := 0; index < engine.enemyCount; index++ {
		if dead == (engine.enemies[index].HP <= 0) {
			targets = append(targets, index)
		}
	}
	return targets
}

func (engine *BattleEngine) randomFinalizedPlayerTarget(candidates []int, includeDead bool) (int, bool) {
	if len(candidates) == 0 {
		return 0, false
	}
	target := candidates[int(engine.rng.next()%uint32(len(candidates)))]
	return engine.finalizeEnemyPlayerTarget(target, includeDead)
}

func (engine *BattleEngine) finalizeEnemyPlayerTarget(target int, includeDead bool) (int, bool) {
	if !engine.playerMember(target) || engine.player(target).MemberType == 0 {
		return 0, false
	}
	if engine.player(target).HP <= 0 {
		return target, includeDead && !engine.player(target).GameOver
	}
	// 3d224 keeps an already selected cover owner; otherwise it redirects with
	// one RNG draw, even when 7452e found only one living cover owner.
	if covering := engine.coveringPlayerTargets(target); len(covering) > 0 {
		if containsInt(covering, target) {
			return target, true
		}
		return covering[int(engine.rng.next()%uint32(len(covering)))], true
	}
	return target, true
}

func (engine *BattleEngine) selectEnemyMetricTarget(kind string, candidates []int) (int, bool) {
	if len(candidates) == 0 {
		return 0, false
	}
	metric := func(index int) int {
		target := &engine.enemies[index]
		name := strings.TrimSuffix(kind, "_ENEMY")
		name = strings.TrimPrefix(strings.TrimPrefix(name, "HIGH_"), "LOW_")
		switch name {
		case "MAX_HP":
			return target.MaxHP
		case "HP":
			return target.HP
		case "ATK":
			return target.Attack
		case "INT":
			return target.Magic
		case "MND":
			return target.Recovery
		case "DEF":
			return target.Defense
		case "MDEF":
			return target.MDefense
		default:
			return 0
		}
	}
	high := strings.HasPrefix(kind, "HIGH_")
	bestValue := metric(candidates[0])
	best := candidates[0]
	for _, candidate := range candidates[1:] {
		value := metric(candidate)
		better := high && value > bestValue || !high && value < bestValue
		if better || value == bestValue && (high && candidate > best || !high && candidate < best) {
			bestValue, best = value, candidate
		}
	}
	// FUN_000cda7f/cdb53 sort by stat, then member ID (cdef3 et al).
	// HIGH takes the last entry, LOW the first. Neither consumes RNG.
	return best, true
}

func (engine *BattleEngine) selectPlayerMetricTarget(kind string, candidates []int) (int, bool) {
	if len(candidates) == 0 {
		return 0, false
	}
	metric := func(memberType int) int {
		player := &engine.players[memberType-1]
		switch kind {
		case "HIGH_MAX_HP_USER", "LOW_MAX_HP_USER":
			return player.MaxHP
		case "HIGH_HP_USER", "LOW_HP_USER":
			return player.HP
		case "HIGH_ATK_USER", "LOW_ATK_USER":
			return player.Attack
		case "HIGH_INT_USER", "LOW_INT_USER":
			return player.Magic
		case "HIGH_MND_USER", "LOW_MND_USER":
			return player.Recovery
		case "HIGH_DEF_USER", "LOW_DEF_USER":
			return player.Defense
		case "HIGH_MDEF_USER", "LOW_MDEF_USER":
			return player.MDefense
		case "HIGH_BLESS_NUM_USER", "LOW_BLESS_NUM_USER":
			return playerAppendCardCount(player, 22, "")
		case "HIGH_CURSE_NUM_USER", "LOW_CURSE_NUM_USER":
			return playerAppendCardCount(player, 21, "")
		case "HIGH_CRITICAL_USER", "LOW_CRITICAL_USER":
			return nativeCriticalRate(player.Effects, 0)
		default:
			return 0
		}
	}
	high := strings.HasPrefix(kind, "HIGH_")
	randomExtremum := strings.Contains(kind, "_CURSE_NUM_") || strings.Contains(kind, "_BLESS_NUM_") || strings.Contains(kind, "_CRITICAL_")
	if !randomExtremum {
		engine.sortPlayerMetricTargets(candidates, metric)
		selected := candidates[0]
		if high {
			selected = candidates[len(candidates)-1]
		}
		return selected, true
	}
	bestValue := metric(candidates[0])
	best := []int{candidates[0]}
	for _, candidate := range candidates[1:] {
		value := metric(candidate)
		better := high && value > bestValue || !high && value < bestValue
		if better {
			bestValue, best = value, []int{candidate}
		} else if value == bestValue {
			best = append(best, candidate)
		}
	}
	// Append-count and critical selectors choose among equal extrema once;
	// ordinary parameter selectors use the comparator stream above.
	return best[int(engine.rng.next()%uint32(len(best)))], true
}

// FUN_00043034 counts registered append cards by CURSE/BLESS identity. These
// are not ordinary BAD_STATUS entries, and the two append kinds stay separate.
func playerAppendCardCount(player *battlePlayer, cardType int, attribute string) int {
	count := 0
	for _, hold := range player.BlessHolds {
		if hold.CardType == cardType && (attribute == "" || strings.EqualFold(attribute, "NULL") || combatAttributeMatches(hold.Skill.Attribute, attribute)) {
			count++
		}
	}
	return count
}

func (engine *BattleEngine) playerDebuffCount(memberType int) int {
	count := 0
	for _, effect := range engine.players[memberType-1].Effects {
		if effect.Kind == 2 && effect.Remaining > 0 {
			count++
		}
	}
	return count
}

func (engine *BattleEngine) playerHasDebuffKind(memberType int, wanted string) bool {
	wantedKind := combatTriggerDebuffKind(wanted)
	for _, effect := range engine.players[memberType-1].Effects {
		if effect.Remaining > 0 && effect.Kind == 2 && (wantedKind == 0 && strings.EqualFold(effect.Function, wanted) || wantedKind != 0 && skillRoleKindDebuff(effect) == wantedKind) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) playerMatchesEnemyDebuffSelector(memberType int, parameters [5]string) bool {
	if strings.TrimSpace(parameters[0]) == "" {
		return engine.playerDebuffCount(memberType) > 0
	}
	for _, parameter := range parameters {
		if parameter != "" && engine.playerHasDebuffKind(memberType, parameter) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) addPlayerHate(memberType int, value int64) {
	if !engine.playerMember(memberType) || value <= 0 {
		return
	}
	engine.player(memberType).HateHistory[0] += value
}

func (engine *BattleEngine) addPlayerRoleHate(memberType int, value int, hateRate int, hateLimit int) {
	if value <= 0 || hateRate <= 0 || hateLimit <= 0 {
		return
	}
	// FUN_00057b69/FUN_00057c40 perform two signed integer divisions rather
	// than one combined fraction, then cap at SkillRoleData.hate_limit.
	generated := int64(value) * int64(hateRate) / 100
	generated = generated * 10 / 100
	if generated > int64(hateLimit) {
		generated = int64(hateLimit)
	}
	engine.addPlayerHate(memberType, generated)
}

func (engine *BattleEngine) playerHate(memberType int) int64 {
	if !engine.playerMember(memberType) {
		return -1
	}
	total := int64(0)
	for index, value := range engine.player(memberType).HateHistory {
		total += value * int64(100-index*10) / 100
	}
	return total
}

func (engine *BattleEngine) playerHateRankTarget(rank int) (int, bool) {
	candidates := engine.playerTargetCandidates(false)
	if len(candidates) == 0 {
		return 0, false
	}
	sort.SliceStable(candidates, func(left int, right int) bool {
		leftHate, rightHate := engine.playerHate(candidates[left]), engine.playerHate(candidates[right])
		if leftHate == rightHate {
			return candidates[left] < candidates[right]
		}
		return leftHate > rightHate
	})
	rank = minInt(maxInt(1, rank), len(candidates))
	return candidates[rank-1], true
}

func (engine *BattleEngine) coveringPlayerTargets(target int) []int {
	targets := make([]int, 0, maxRoomMembers)
	for _, player := range engine.playerParty(target) {
		if player.HP <= 0 {
			continue
		}
		for _, effect := range player.Effects {
			if effect.Function == "COVERING" && effect.Remaining > 0 {
				targets = append(targets, player.MemberType)
				break
			}
		}
	}
	return targets
}
