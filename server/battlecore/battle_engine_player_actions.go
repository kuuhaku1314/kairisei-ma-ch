package battlecore

import (
	"errors"
	"fmt"
	"strings"
)

type battleAction struct {
	helper       bool // Neutral STORY2 card entry; no hand/party ownership.
	summon       bool
	enemySkill   bool // An enemy CALL program can become a player-owned token.
	automatic    bool
	held         bool
	burst        bool
	memberType   int
	cardID       int
	cardType     int
	sphereSlot   int
	cardLevel    int
	target       int
	branchIndex  int
	buffListType int
	// Scratch state shared only while executing one Burst/passive role set.
	// Native 7ed76 installs it and 7ea92 consumes the first matching segment.
	skillBonus *battleSkillBonus
	skill      CombatSkillDefinition
	roles      []CombatSkillRole
	// CARD-list piercing is installed before this skill's producers. Keep it
	// outside the indexed original roles used by display and result projection.
	cardAttackOperators []CombatSkillRole
	// Card/SphrData call skills are the independent APPEND_CARD_BLESS payload.
	// Never store and replay the BLESS registration skill itself.
	callSkill           CombatSkillDefinition
	callRoles           []CombatSkillRole
	callLevel           int
	hasCallLevel        bool
	callProducerQueried [5]bool // One prototype lookup per BLESS role in this action.
}

func (engine *BattleEngine) UserAttack() ([]BattleResult, error) {
	if engine.continuePending {
		return nil, errors.New("combat continuation is pending")
	}
	if engine.phase != PhaseUser {
		return nil, errors.New("combat user attack phase is unavailable")
	}
	for _, player := range engine.playerStates() {
		if player.HP > 0 {
			if _, submitted := engine.selectedPlays[player.MemberType]; !submitted {
				return nil, errors.New("combat user attack selection is incomplete")
			}
		}
	}
	results, err := engine.createSelectedSummons()
	if err != nil {
		return nil, err
	}
	actions, err := engine.selectedBattleActions()
	if err != nil {
		return nil, err
	}
	executed, err := engine.executeUserActions(actions)
	return append(results, executed...), err
}

// The selection snapshot is also consumed by native 3d680's CARD_UPDATE
// preview immediately after each commit, before UserAttack starts.
func (engine *BattleEngine) selectedBattleActions() ([]battleAction, error) {
	actions := make([]battleAction, 0, maxRoomMembers*2)
	for _, memberType := range engine.selectionMembers() {
		player := engine.player(memberType)
		if player.HP <= 0 {
			continue
		}
		submission := engine.selectedPlays[memberType]
		for selectionIndex, cardType := range submission.CardTypes {
			if cardType == 0 {
				continue
			}
			card, found := player.cardInHand(cardType)
			if !found {
				return nil, fmt.Errorf("member %d selected card %d left its hand", memberType, cardType)
			}
			skill, roles, err := engine.handCardSkill(player, card)
			if err != nil {
				return nil, err
			}
			callSkill, callRoles, err := engine.handCardCallSkill(player, card)
			if err != nil {
				return nil, err
			}
			actions = append(actions, battleAction{summon: submission.SummonsCardMask&(1<<selectionIndex) != 0, automatic: submission.Automatic, enemySkill: player.tokenUsesEnemySkill(cardType), memberType: memberType, cardID: card.CardID, cardType: cardType, cardLevel: card.ExecutionLevel(skill.ID), target: submission.Targets[selectionIndex], skill: skill, roles: roles, callSkill: callSkill, callRoles: callRoles, callLevel: card.ExecutionLevel(callSkill.ID), hasCallLevel: true})
		}
		if submission.SphereSlot != 0 {
			action, err := engine.validateSphereSubmission(memberType, submission)
			if err != nil {
				return nil, err
			}
			action.automatic = submission.Automatic
			actions = append(actions, action)
		}
	}
	return actions, nil
}

func (engine *BattleEngine) selectionMembers() []int {
	var result []int
	var seen [9]bool
	for _, member := range engine.selectionOrder {
		if engine.playerMember(member) && !seen[member] {
			if _, exists := engine.selectedPlays[member]; exists {
				result = append(result, member)
				seen[member] = true
			}
		}
	}
	// Direct internal fixtures may supply the selection map without an API
	// registration sequence. Ordinary Submit always records the real order.
	for member := 1; member <= engine.playerCount(); member++ {
		if _, exists := engine.selectedPlays[member]; exists && !seen[member] {
			result = append(result, member)
		}
	}
	return result
}

func (engine *BattleEngine) rememberSelection(member int) {
	var order [8]int
	n := 0
	for _, previous := range engine.selectionMembers() {
		if previous != member {
			order[n] = previous
			n++
		}
	}
	order[n] = member
	engine.selectionOrder = order
}

func (engine *BattleEngine) prepareUserActions(actions []battleAction) ([]battleAction, []battleAction, map[string]int) {
	chainCounts := battleActionChainCounts(actions)
	engine.turnActions = append([]battleAction(nil), actions...)
	if engine.alchemist {
		playable := make([]battleAction, 0, len(actions))
		for _, action := range actions {
			if !action.summon {
				playable = append(playable, action)
			}
		}
		actions = playable
	}
	for _, action := range actions {
		if action.cardType == 0 {
			continue
		}
		engine.turnStats.PlayedByUser[action.memberType-1]++
	}
	actions, held := engine.partitionPlayerActions(actions, chainCounts)
	engine.sortPlayerActions(actions)
	return actions, held, chainCounts
}

func (engine *BattleEngine) executeUserActions(actions []battleAction) ([]BattleResult, error) {
	actions, held, chainCounts := engine.prepareUserActions(actions)
	results, err := engine.chalicePlayableResults()
	if err != nil {
		return nil, err
	}
	results = append(results, engine.burstSelectionGaugeResults(engine.turnActions)...)
	for _, action := range actions {
		rows, err := engine.executePlayerAction(action, chainCounts)
		if err != nil {
			return nil, err
		}
		results = append(results, rows...)
		if engine.beginStoryEnd(false) {
			return engine.finishStoryUserAttack(results)
		}
		if engine.endType != 0 {
			break
		}
	}
	// Native battle5_api_user_attack emits ATTACK_PARTITION after the ordered
	// card/sphere action list and before appended CURSE/BLESS execution. It is a
	// zero-argument managed single-row delimiter, not a burst-only effect.
	if engine.endType == 0 {
		results = appendAttackPartition(results)
	}
	results = append(results, engine.storeCardHolds(held)...)
	// Native battle5_api_user_attack calls the complete tail even when an
	// ordinary card already set the GameMaster end flag. Each callee owns its
	// own terminal gate: append-card execution is suppressed but cleaned up,
	// selected-card traps still run, and retained enemy DOT returns immediately.
	// The fixed order is BLESS -> card trap -> enemy DOT -> CURSE.
	holdResults, err := engine.executeBlessHolds("USER_ATTACK_END", 22)
	if err != nil {
		return nil, err
	}
	results = append(results, holdResults...)
	if engine.beginStoryEnd(false) {
		return engine.finishStoryUserAttack(results)
	}
	trapResults := engine.triggerPlayedCardTraps()
	results = append(results, trapResults...)
	if engine.beginStoryEnd(false) {
		return engine.finishStoryUserAttack(results)
	}
	emitTail := engine.endType == 0
	var dotResults []BattleResult
	if engine.pvp {
		dotResults = engine.tickPlayerDOTEffects()
	} else {
		dotResults, err = engine.tickEnemyDOTEffects()
	}
	if err != nil {
		return nil, err
	}
	if emitTail {
		results = append(results, dotResults...)
	}
	if engine.beginStoryEnd(false) {
		return engine.finishStoryUserAttack(results)
	}
	curseResults, err := engine.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		return nil, err
	}
	results = append(results, curseResults...)
	if engine.beginStoryEnd(false) {
		return engine.finishStoryUserAttack(results)
	}
	if engine.pvp {
		return engine.settlePVPUserAttack(results)
	}
	// 64120 -> a7cd0 discards the registered selection only after BLESS,
	// card traps, enemy DOT and CURSE. Until then the cards remain visible to
	// display refreshes and hand/selection conditions, even after execution.
	engine.discardSelectedCards()
	if engine.endType == 0 && engine.allEnemyBodiesDead() {
		engine.endType = engine.enemyWaveEndType()
	}
	return engine.settleActionPhase(results, PhaseUserAttack, true)
}

// 3d342 executes one ordinary card or sphere, also reused by 664ee holds.
func (engine *BattleEngine) executePlayerAction(action battleAction, chainCounts map[string]int) ([]BattleResult, error) {
	if engine.pvp {
		chainCounts = battleActionChainCounts(engine.partyActions(engine.turnActions, action.memberType))
	}
	var results []BattleResult
	// FUN_0003d342 gates both ordinary cards and spheres on the actor's
	// current HP. A sphere selected before an earlier action kills its
	// owner must not execute or consume a use afterwards.
	if !action.helper && engine.player(action.memberType).HP <= 0 {
		return nil, nil
	}
	engine.player(action.memberType).ExecutionSource.setPhysical(0, 0)
	if engine.pvp {
		// 32602 increments the base skill's components after7a210 returns,
		// including a valid action whose branch produces no effect.
		defer engine.recordPVPActionAttribute(action.memberType, action.skill.Attribute)
	}
	if !action.helper {
		engine.retargetPlayerAction(&action)
	}
	// Native sorts the base skills (FUN_0003ae10), then selects a branch
	// inside each execution (FUN_0007adb0 -> FUN_000d7578). In particular,
	// BUFF_EXEC/SELF_BUFF/HP conditions must observe earlier actions.
	branch, branchIndex := engine.selectCombatSkillBranchWithIndex(action, engine.turnActions, chainCounts)
	if branch.ID == 0 {
		return nil, nil // Original7adb0 skips a valid card with no matching extend.
	}
	roles := engine.catalog.SkillRoles[branch.FunctionID]
	if len(roles) == 0 {
		return nil, fmt.Errorf("branched player skill role %d is unavailable", branch.FunctionID)
	}
	if roles[0].Function == "NONE" {
		return nil, nil // The original skill entry returns before its header.
	}
	// SkillData owns the outer target; extend records contain roles and
	// conditions only. 7adb0 uses the base target for both 79ee0 and 51.
	branch.Target = action.skill.Target
	action.skill, action.branchIndex = branch, branchIndex
	// Burst modifiers are per play, never writes to the shared catalog.
	action.roles = append([]CombatSkillRole(nil), roles...)
	if err := engine.attachBurstCardModifiers(&action); err != nil {
		return nil, err
	}
	if action.sphereSlot != 0 {
		sphereResults, err := engine.executeSphereAction(action, 0)
		if err != nil {
			return nil, err
		}
		return sphereResults, nil
	}
	engine.player(action.memberType).ExecutionSource.setPhysical(action.cardType, 0)
	chainCount := maxCombatChain(action.skill.Attribute, chainCounts)
	cardSkillResult, err := engine.playerCardSkillResult(action, chainCount)
	if err != nil {
		return nil, err
	}
	results = append(results, cardSkillResult)
	origin := "CARD"
	if isTokenCardType(action.cardType) {
		origin = "TOKEN"
	}
	if action.held {
		origin = "HOLD_CARD"
	}
	gaugeRows := engine.reactiveGaugeSkillStart(action, origin)
	results = append(results, gaugeRows...)
	if action.helper || action.cardType > 0 && action.cardType < 33 {
		player := engine.player(action.memberType)
		power, err := engine.actionDisplayPower(player, &action)
		if err != nil {
			return nil, err
		}
		display := battleDisplayPower{Power: power, Marker: action.branchIndex, Known: true}
		results = append(results, cardUpdate2Result(action.memberType, action.cardType, display, 0))
	}
	// 7adb0 mode 1 marks the card after branch selection and before roles.
	// A selection, pending hold, sphere or CALL does not set this flag.
	if action.cardType > 0 && action.cardType < 11 {
		engine.player(action.memberType).PlayedCardTypes[action.cardType] = true
	}
	if !action.helper {
		engine.recordExecutedPlayerSkill(action, chainCount)
	}
	// Native a4ac0 evaluates deck-bonus producers in one skill-local context,
	// including ordinary cards; the following matching fixed effect consumes it.
	action.skillBonus = &battleSkillBonus{}
	skillResults, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, action.roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executePlayerRole(action, role, chainCount)
	})
	if err != nil {
		return nil, err
	}
	results = append(results, skillResults...)
	engine.nativeSkillSerial++
	displayResults, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	results = append(results, displayResults...)
	// Native FUN_0003d4xx calls FUN_00057cf0 once after the complete card
	// role set. Value-derived attack/heal hate is added by the role paths.
	engine.addPlayerHate(action.memberType, int64(maxInt(0, action.skill.HateRatio)))
	if engine.allEnemyBodiesDead() {
		engine.endType = engine.enemyWaveEndType()
	}
	return results, nil
}

// 7adb0 modes 1 (card) and 5 (sphere) call 792cc only after a skill starts.
// 570ef keeps the highest actual Chain, while 57428 records the executed kind.
// Mode 7 (append/CALL) deliberately does not contribute to either AI trigger.
func (engine *BattleEngine) recordExecutedPlayerSkill(action battleAction, chain int) {
	for _, component := range splitCombatAttributes(action.skill.Attribute) {
		if attribute := combatAttributeCode(component); attribute > 0 {
			engine.turnStats.ExecutedAttributes |= uint32(1) << (uint(attribute) & 31)
		}
	}
	engine.turnStats.MaxChain = maxInt(engine.turnStats.MaxChain, chain)
	kinds := engine.turnStats.KindsByUser[action.memberType-1]
	if kinds == nil {
		kinds = make(map[string]int)
		engine.turnStats.KindsByUser[action.memberType-1] = kinds
	}
	kinds[strings.ToUpper(action.skill.Kind)]++
}

// FUN_0003c8ea uses the right-to-left bubble pass in FUN_000c6e10, not a
// standard stable sort. Equal-priority actions put manual before automatic
// players (FUN_00040908 flag), then FUN_0003ae10 consumes a random low bit
// for each comparison of different actors. Changing the algorithm changes
// both the order and the RNG state used by subsequent damage/target rolls.
func (engine *BattleEngine) sortPlayerActions(actions []battleAction) {
	for boundary := 0; boundary < len(actions)-1; boundary++ {
		for index := len(actions) - 2; index >= boundary; index-- {
			left, right := actions[index], actions[index+1]
			leftPriority, rightPriority := engine.actionPriority(left.skill), engine.actionPriority(right.skill)
			swap := leftPriority > rightPriority
			if leftPriority == rightPriority {
				if engine.pvp && engine.pvpCardWeight(left) != engine.pvpCardWeight(right) {
					swap = engine.pvpCardWeight(left) < engine.pvpCardWeight(right)
				} else {
					switch {
					case engine.actionOrderGroup(left) != engine.actionOrderGroup(right):
						swap = engine.actionOrderGroup(left)
					case left.memberType != right.memberType:
						swap = engine.rng.next()&1 != 0
					}
				}
			}
			if swap {
				actions[index], actions[index+1] = right, left
			}
		}
	}
}

// FUN_0003d342 -> FUN_0003d038 reselects a dead target before branch selection
// and the direction header. Earlier cards can destroy the selected part;
// rejecting that later action would leave the room in a partially applied turn.
func (engine *BattleEngine) retargetPlayerAction(action *battleAction) {
	// Native CPU plans can become stale after an earlier skill moves the
	// weakness mark. Manual selections retain their explicit living target.
	if action.automatic && action.skill.Target == "ENEMY_ONE" {
		for _, member := range engine.livingSideMembers(action.memberType, true, false) {
			if hasActiveCombatEffect(engine.battleMemberEffects(member), "WEAKNESS") {
				action.target = member
				break
			}
		}
	}
	if action.target == 0 {
		return
	}
	hp, _, exists := engine.battleMemberHP(action.target)
	if !exists || hp <= 0 {
		var candidates []int
		switch action.skill.Target {
		case "USER_ONE":
			candidates = engine.livingSideMembers(action.memberType, false, false)
		case "ENEMY_ONE":
			candidates = engine.livingSideMembers(action.memberType, true, false)
		case "USER_ALL", "ENEMY_ALL":
			action.target = 0
			return
		default:
			return
		}
		action.target = 0
		if len(candidates) > 0 {
			// Native consumes a roll even when only one candidate remains.
			action.target = candidates[int(engine.rng.next()%uint32(len(candidates)))]
		}
	}
	// Original7a210 calls324e0 before branch selection, independently of the
	// CPU-only weakness override. CURSE type21 deliberately skips covering.
	if engine.pvp && action.target != 0 && action.cardType != 21 && !engine.samePlayerParty(action.memberType, action.target) {
		action.target, _ = engine.finalizeEnemyPlayerTarget(action.target, false)
	}
}

func appendAttackPartition(results []BattleResult) []BattleResult {
	return append(results, BattleResult{Command: CommandAttackPartition})
}

// settleActionPhase mirrors the shared terminal work performed after the
// native action API has completed its own ordered subcalls. UserAttack first
// projects FUN_00064120's ResultCmd1(turn, user-attack-complete=1); chalice
// user execution reaches FUN_00065010 directly and therefore omits that row.
// FUN_00065010 only writes a player-loss end type when no earlier win/timeout
// state exists, so an enemy DOT victory wins a simultaneous all-player KO.
func (engine *BattleEngine) settleActionPhase(results []BattleResult, nextPhase Phase, emitUserAttackTurn bool) ([]BattleResult, error) {
	engine.flushTokenStock()
	if nextPhase == PhaseUserAttack {
		engine.resumeSide = 1
	} else if nextPhase == PhaseEnemy {
		engine.resumeSide = 0
	}
	if emitUserAttackTurn && engine.endType == 0 {
		results = append(results, BattleResult{Command: CommandTurn, Args: []int64{int64(engine.turn), 1}})
	}
	results = engine.settlePlayerDeaths(results)
	engine.phase = nextPhase
	if emitUserAttackTurn {
		results = append(results, engine.advanceTranceStates()...)
	}
	if engine.endType != 0 {
		results = append(results, battleEndResult(engine.endType))
		engine.phase = PhaseEnded
		return engine.appendTerminalBuffCleanup(results)
	}
	return results, nil
}

func (engine *BattleEngine) executePlayerRole(action battleAction, role CombatSkillRole, chainCount int) (results []BattleResult, err error) {
	role.SourceSkillID = action.skill.ID
	defer func() {
		if err == nil {
			results = engine.projectRoleBuffParameters(results)
		}
	}()
	if ordinary, costSlot := burstParameterRole(role); costSlot >= 0 {
		original := role
		defer func() {
			if err == nil {
				results = append(results, engine.burstParameterPayment(action, original, costSlot)...)
			}
		}()
		role = ordinary
	}
	// Successful status consumers record BUFF_EXEC on the actual caster.
	// ResultCmd presence is not evidence that a good-status registration won.
	role.Target = engine.resolvedPlayerRoleTarget(action, role.Target)
	switch role.Function {
	case "ATTACK_AA":
		return engine.executePlayerAttack(action, role, chainCount)
	case "TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return engine.executePlayerTranceRole(action, role), nil
	case "DEF_UP_FIXED":
		return engine.executeFixedDefense(action, role, chainCount)
	case "ATK_UP_FIXED":
		return engine.executeFixedParameter(action, role, chainCount)
	case "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM":
		return engine.executeSelfScaledParameter(action, role, chainCount)
	case "ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM":
		return engine.executeEnemyParameterDebuff(action, role, chainCount)
	case "DAMAGE_BOOST_COUNT2":
		return engine.executePersistentEffect(action, countedDamageStatusRole(role), action.cardLevel, chainCount)
	case "CARD_SEARCH":
		return engine.executeCardSearch(action, role)
	case "HEAL_FIXED":
		return engine.executeFixedHeal(action, role, chainCount)
	case "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP":
		return engine.executeTargetScaledHeal(action, role, chainCount)
	case "HEAL_BY_SELF_PARAM":
		return engine.executeSelfScaledHeal(action, role, chainCount)
	case "DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEBUFF_RELEASE", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_OLD",
		"BUFF_RELEASE", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_OLD":
		return engine.executeRelease(action, role)
	case "ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL", "ATK_OP_REVENGE", "ATK_OP_PIERCING", "ATK_OP_ATTR_DEF_PIERCING", "ATK_OP_DAMAGE_INCREASE", "ATK_OP_ATTR_RATE_DOWN_INVALID", "ATK_OP_REFLECTION_INVALID":
		// Attack operators are collected once by executePlayerAttack so order
		// inside a skill-role row set cannot change the arithmetic.
		return nil, nil
	case "DAMAGE_BOOST_ORDER_TRIBAL", "ATK_UP_BOOST_ORDER_TRIBAL", "DEF_UP_BOOST_ORDER_TRIBAL":
		// Already attached to this selected card by BurstSkillExec. The
		// attack/parameter consumers read them; they are not new card actions.
		return nil, nil
	case "DEAL_BONUS":
		return engine.executeDealChange(action, role, false)
	case "PARAM_LIMIT_BREAK_FIXED":
		return engine.executePlayerParameterLimit(action, role, chainCount)
	case "HEAL_CUT", "HEAL_REVERSE", "DEBUFF_REGIST", "ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT",
		"REGENERATE_FIXED", "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC", "ENCHANT", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "DAMAGE_BOOST", "WEAKNESS", "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN", "REFLECTION", "ENDURE", "COVERING", "CARD_SEAL", "CARD_SEAL_REGIST", "DARKNESS_REGIST", "GUTS", "STAN", "COST_BLOCK", "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_SEE", "CARD_TRAP_DAMAGE", "DARKNESS_RANDOM":
		return engine.executePersistentEffect(action, role, action.cardLevel, chainCount)
	case "PARAM_UP_SKILL_BONUS", "LIMIT_BREAK_BONUS":
		if action.skillBonus != nil {
			*action.skillBonus = battleSkillBonus{active: true, parameter: role.Parameters[0],
				limit: role.Function == "LIMIT_BREAK_BONUS", attribute: role.Parameters[4],
				tag:     combatParameterInt(role.Parameters[5]),
				percent: int32(combatParameterInt(role.Parameters[1])),
				fixed:   int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(action.cardLevel)}
		}
		return nil, nil
	case "BLESS":
		if action.cardType == 22 {
			// Native FUN_000a275b suppresses recursive hold creation when the
			// currently executed card type is APPEND_CARD_BLESS.
			return nil, nil
		}
		return engine.executePlayerBless(action, role, chainCount)
	case "BLESS_TURN_UP":
		return engine.changePlayerBlessTurns(action, role, combatParameterInt(role.Parameters[0])), nil
	case "CARD_TOKEN":
		return engine.createTokenCards(engine.playerRoleTargets(action, role), role, action.callSkill.ID, action.callExecutionLevel(), false)
	case "CURSE_RELEASE", "BLESS_RELEASE":
		return engine.releasePlayerAppendCards(action, role), nil
	case "HP_CUT":
		return engine.executePlayerHPCut(action, role)
	case "REWRITE":
		return engine.executePlayerRewrite(action, role), nil
	case "BURST_GAUGE_QUICK_UP":
		var results []BattleResult
		for _, member := range engine.playerRoleTargets(action, role) {
			results = append(results, engine.addPlayerBurstGauge(member, role.RoleIndex, burstGaugeQuickValue(role, action.cardLevel), true)...)
		}
		return results, nil
	case "DEAL_PENALTY":
		return engine.executeDealChange(action, role, true)
	case "DOT_VALUE_UP":
		return engine.executePlayerDOTValueUp(action, role, chainCount), nil
	default:
		return nil, fmt.Errorf("unsupported player combat function %q", role.Function)
	}
}

func (engine *BattleEngine) resolvedPlayerRoleTarget(action battleAction, roleTarget string) string {
	if roleTarget != "SELECT" {
		return roleTarget
	}
	switch action.skill.Target {
	case "USER_ALL", "ENEMY_ALL", "SELF":
		return action.skill.Target
	}
	if engine.playerMember(action.target) {
		if !engine.samePlayerParty(action.memberType, action.target) {
			return "ENEMY_ONE"
		}
		return "USER_ONE"
	}
	if action.target >= 5 {
		return "ENEMY_ONE"
	}
	return action.skill.Target
}

func SupportsPlayerFunction(function string) bool {
	switch function {
	case "ATTACK_AA", "DEF_UP_FIXED", "ATK_UP_FIXED", "ATK_UP_BY_SELF_PARAM", "DEF_UP_BY_SELF_PARAM",
		"DAMAGE_BOOST_COUNT2", "CARD_SEARCH",
		"BURST_ATK_UP_FIXED", "BURST_DEF_UP_FIXED", "BURST_DEF_UP_BY_SELF_PARAM", "BURST_ATK_BREAK_FIXED",
		"ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT",
		"ATK_BREAK_FIXED", "GUARD_BREAK_FIXED", "ATK_BREAK_BY_SELF_PARAM", "GUARD_BREAK_BY_SELF_PARAM", "HEAL_FIXED", "HEAL_BY_SELF_PARAM", "HEAL_BY_TARGET_HP", "HEAL_BY_TARGET_MAXHP", "HEAL_CUT", "HEAL_REVERSE", "DEBUFF_REGIST",
		"DEBUFF_RELEASE_ONE", "DEBUFF_RELEASE_ONE_NUM", "DEBUFF_RELEASE", "DEBUFF_RELEASE_RANDOM", "DEBUFF_RELEASE_OLD",
		"BUFF_RELEASE", "BUFF_RELEASE_ONE", "BUFF_RELEASE_ONE_NUM", "BUFF_RELEASE_RANDOM", "BUFF_RELEASE_OLD",
		"ATK_OP_DRAIN", "ATK_OP_DRAIN_ALL", "ATK_OP_REVENGE", "ATK_OP_PIERCING", "ATK_OP_ATTR_DEF_PIERCING", "ATK_OP_DAMAGE_INCREASE",
		"ATK_OP_ATTR_RATE_DOWN_INVALID", "ATK_OP_REFLECTION_INVALID", "DEAL_BONUS", "REGENERATE_FIXED", "BURN", "POISON", "FREEZE",
		"BLEED", "ELECTRIC", "ENCHANT", "ATTR_DEF_DOWN", "ATTR_DEF_UP", "CRITICAL_UP", "CRITICAL_DAMAGE_BOOST", "DAMAGE_BOOST", "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN",
		"PARAM_LIMIT_BREAK_FIXED", "PARAM_UP_SKILL_BONUS", "LIMIT_BREAK_BONUS", "WEAKNESS", "REFLECTION", "ENDURE", "COVERING", "BLESS", "CARD_SEAL", "CARD_SEAL_REGIST",
		"DARKNESS_REGIST", "GUTS", "STAN", "DEAL_PENALTY", "COST_BLOCK", "HP_CUT", "BURST_GAUGE_QUICK_UP", "DOT_VALUE_UP",
		"ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR", "ATTR_SEE", "CARD_TRAP_DAMAGE", "DARKNESS_RANDOM", "CARD_TOKEN", "BLESS_TURN_UP", "CURSE_RELEASE", "BLESS_RELEASE",
		"TRANCE_GAUGE_STATE_CHANGE", "TRANCE_GAUGE_VALUE_UP", "TRANCE_GAUGE_VALUE_DOWN", "TRANCE_GAUGE_OVER_HEAT_TURN_ADD":
		return true
	default:
		return false
	}
}

// nativeCriticalOutcome mirrors the common critical branch in CN libbattle5
// FUN_000838d0/FUN_000927b0. Rates use tenths of one percent: 150 is 15%,
// positive rolls use the normal 150% multiplier, and a negative critical roll
// (from CRITICAL_DOWN exceeding the base/up rate) halves the pre-defense value.
func (engine *BattleEngine) nativeCriticalOutcome(effects []battleEffect, baseRate int, skills ...CombatSkillDefinition) (flag int, multiplier int) {
	rate := nativeCriticalRate(effects, baseRate, skills...)
	criticalDamageRate := 150
	for _, effect := range effects {
		if effect.Remaining <= 0 {
			continue
		}
		if effect.Function == "CRITICAL_DAMAGE_BOOST" {
			criticalDamageRate = maxInt(criticalDamageRate, 150+effect.Rate)
		}
	}
	rate = maxInt(-1000, minInt(1000, rate))
	roll := int(engine.rng.next() % 10000)
	if rate > 0 && roll < rate*10 {
		return 1, criticalDamageRate
	}
	if rate < 0 && roll < -rate*10 {
		return -1, 50
	}
	return 0, 100
}

func nativeCriticalRate(effects []battleEffect, baseRate int, skills ...CombatSkillDefinition) int {
	rate := baseRate
	if baseRate >= 1000 {
		return rate
	}
	// 504b0/505f0 evaluate HIGH/LOW_CRITICAL_USER using an empty skill and
	// physics ALL, so their metric includes unrestricted EX critical boosts.
	skill := CombatSkillDefinition{DamageKind: "ALL"}
	if len(skills) > 0 {
		skill = skills[0]
	}
	for _, effect := range effects {
		if effect.Remaining <= 0 {
			continue
		}
		switch effect.Function {
		case "CRITICAL_UP":
			rate += effect.Rate
		case "CRITICAL_DOWN":
			rate -= effect.Rate
		case "CRITICAL_BOOST":
			if effect.ListType == 1 && supportAttributeMatches(effect.Attribute, skill.Attribute) &&
				damagePhysicsMatches(effect.DamageKind, skill.DamageKind) && supportCostMatches(effect, skill.Cost) {
				rate += effect.Rate
			}
		}
	}
	return rate
}

func activeEnchantEffect(effects []battleEffect) (value int, attribute string, active bool) {
	// JP consumes retained lists in enum order. PASSIVE has no expiry check;
	// zero-duration NORMAL remains active until its first removal boundary.
	for list := 0; list <= 6; list++ {
		for _, effect := range effects {
			if effect.Function != "ENCHANT" || effect.ListType != list {
				continue
			}
			active = true
			value += effect.Value
			if effect.Attribute != "" {
				attribute = effect.Attribute
			}
		}
	}
	return value, attribute, active
}

func (engine *BattleEngine) executePlayerAttack(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := engine.player(action.memberType)
	attackPower := attackRolePowerScaled(role, action.cardLevel, maxInt(0, combatStatValue(actor, role.Parameters[5])), engine.roleScale(role))
	modifiers := collectPlayerAttackModifiers(action, role, actor, chainCount, engine.pvp)
	attackPower += modifiers.damageIncrease
	attackPower += attackRevengeBonus(actor.DamageTaken, actor, modifiers.revengeRate, modifiers.revengeParameter, modifiers.revengeCapRate)
	attackPower += attackRevengeBonus(actor.TurnDamage, actor, modifiers.nowTurnRevengeRate, modifiers.nowTurnRevengeParameter, modifiers.nowTurnRevengeCapRate)
	// 927b0 first combines fixed additions and the attacker's boost rates.
	// Conditional tribal boosts (466e2) add AFTER the ordinary 71d48 cap,
	// not as another multiplier. Chain belongs after target WEAKNESS/ATTR_DEF.
	boostFixed, boostRate := damageBoostTerms(actor.Effects, action.skill.Attribute, role.Parameters[8], action.skill.Cost)
	for _, tribal := range modifiers.tribalDamage {
		if engine.enemyPartyHasRace(tribal.raceID) && damagePhysicsMatches(tribal.damageKind, role.Parameters[8]) &&
			supportAttributeMatches(tribal.attribute, action.skill.Attribute) &&
			action.skill.Cost >= tribal.costMin && (tribal.costMax == 0 || action.skill.Cost <= tribal.costMax) {
			boostFixed += tribal.fixed
			boostRate += tribal.rate
		}
	}
	criticalSkill := action.skill
	criticalSkill.DamageKind = role.Parameters[8]
	counted := countedDamageSnapshot(actor.Effects, role.Parameters[7], role.Parameters[8])
	source := battleAttackSource{memberType: action.memberType, power: attackPower,
		boostFixed: boostFixed, boostRate: boostRate, chainCount: chainCount, chainRate: role.ChainRate,
		modifiers: modifiers, criticalSkill: &criticalSkill, counted: counted, player: actor, hateRatio: action.skill.HateRatio}
	var results []BattleResult
	if targets := engine.playerRoleTargetCandidates(action, role); len(targets) > 0 {
		targets = engine.filterRolePlayers(targets, action.memberType, role)
		if len(targets) == 0 {
			return nil, nil
		}
		results = engine.executeAttackOnPlayers(source, targets, role, action.roles)
	} else {
		targets := engine.playerRoleEnemyTargets(action, role)
		if len(targets) == 0 {
			return nil, nil
		}
		results = engine.executeAttackOnEnemies(source, targets, role, action.roles)
	}
	return append(results, commitCountedDamage(actor, counted, maxInt(1, combatParameterInt(role.Parameters[4])))...), nil
}

// Each hit commits its drain immediately, but JP publishes one accumulated
// healing row per recipient after the attack target's HP has been committed.
func (engine *BattleEngine) appendDrainResults(results, drains []BattleResult, roleIndex int) []BattleResult {
	var accumulated [12]int
	var seen [12]bool
	var order []int
	for _, row := range drains {
		member := int(row.Args[0])
		if !seen[member-1] {
			seen[member-1] = true
			order = append(order, member)
		}
		accumulated[member-1] += int(row.Args[2])
	}
	for _, member := range order {
		hp, _, _ := engine.battleMemberHP(member)
		results = append(results, battleHealResult(member, roleIndex, accumulated[member-1], hp))
	}
	return results
}

// Self drain precedes party drain after each ordinary hit. Both use the
// attack role index and shared cap. JP ordinary/PVP receivers apply their
// own heal cut/reversal; a converted caster delta is not broadcast to allies.
func (engine *BattleEngine) playerAttackDrainResults(actor *battlePlayer, roleIndex, damage int, modifiers attackModifiers) []BattleResult {
	var results []BattleResult
	for i, rate := range [...]int{modifiers.drainRate, modifiers.drainAllRate} {
		heal := damage * rate / 100
		if modifiers.drainCap > 0 {
			heal = minInt(heal, modifiers.drainCap)
		}
		if heal <= 0 {
			continue
		}
		delta, hp := nativeHealCommit(actor.HP, actor.MaxHP, heal, actor.Effects, false)
		// 838d0 broadcasts once per positive self/party drain to the source
		// member's slot, not once for each ally and not the clamped HP gain.
		if delta > 0 {
			if engine.playerMember(actor.MemberType) {
				engine.turnStats.DrainByUser[actor.MemberType-1] += int64(delta)
			}
			engine.recordRecoveryAction(actor.MemberType, delta)
		}
		if i == 0 {
			actor.HP = hp
			if delta < 0 {
				recordPlayerDamage(actor, -delta)
			}
			results = append(results, battleHealResult(actor.MemberType, roleIndex, delta, hp))
			continue
		}
		party := engine.playerParty(actor.MemberType)
		for index := range party {
			player := &party[index]
			if player.HP <= 0 || player.GameOver {
				continue
			}
			received, restoredHP := nativeHealCommit(player.HP, player.MaxHP, heal, player.Effects, false)
			player.HP = restoredHP
			if received < 0 {
				recordPlayerDamage(player, -received)
			}
			if received == 0 {
				continue // JP omits a reversed party-drain heal row at the1HP floor.
			}
			results = append(results, battleHealResult(player.MemberType, roleIndex, received, player.HP))
		}
	}
	return results
}

type attackModifiers struct {
	piercingRate            int
	attributePiercingRate   int
	drainRate               int
	drainCap                int
	drainRoleIndex          int
	drainAllRate            int
	drainAllCap             int
	drainAllRoleIndex       int
	damageIncrease          int
	revengeRate             int
	revengeParameter        string
	revengeCapRate          int
	nowTurnRevengeRate      int
	nowTurnRevengeParameter string
	nowTurnRevengeCapRate   int
	attrRateDownInvalid     bool
	tribalDamage            []tribalDamageModifier
}

type tribalDamageModifier struct {
	rate       int
	fixed      int
	damageKind string
	attribute  string
	costMin    int
	costMax    int
	raceID     int
}

func collectAttackModifiers(roles []CombatSkillRole, actor *battlePlayer, level int, chainCount int, pvp bool) attackModifiers {
	return collectAttackModifiersByRoleLevel(roles, actor, chainCount, func(CombatSkillRole) int { return level }, false, pvp)
}

func modifierRolesForAttack(roles []CombatSkillRole, attackIndex int, cardOperators []CombatSkillRole) []CombatSkillRole {
	// Piercing and drain producers update the scratch state consumed by subsequent
	// attacks. CARD-list operators are installed before the indexed skill,
	// so they remain eligible independently of their stored list position.
	ordered := make([]CombatSkillRole, 0, len(roles)+len(cardOperators))
	attackReached := false
	for _, role := range roles {
		if role.Function == "ATTACK_AA" && role.RoleIndex == attackIndex {
			attackReached = true
		}
		if attackReached && (role.Function == "ATK_OP_PIERCING" || role.Function == "ATK_OP_ATTR_DEF_PIERCING" || role.Function == "ATK_OP_DRAIN" || role.Function == "ATK_OP_DRAIN_ALL") {
			continue
		}
		ordered = append(ordered, role)
	}
	return append(ordered, cardOperators...)
}

func collectPlayerAttackModifiers(action battleAction, role CombatSkillRole, actor *battlePlayer, chainCount int, pvp bool) attackModifiers {
	return collectAttackModifiersByRoleLevel(modifierRolesForAttack(action.roles, role.RoleIndex, action.cardAttackOperators), actor, chainCount, func(CombatSkillRole) int { return action.cardLevel }, true, pvp)
}

func collectEnemyAttackModifiers(roles []CombatSkillRole, actor *battlePlayer, chainCount int) attackModifiers {
	return collectAttackModifiersByRoleLevel(roles, actor, chainCount, func(role CombatSkillRole) int {
		return EnemySkillLevel(role.Function)
	}, true, false)
}

func collectAttackModifiersByRoleLevel(roles []CombatSkillRole, actor *battlePlayer, chainCount int, roleLevel func(CombatSkillRole) int, execution bool, pvp bool) attackModifiers {
	var result attackModifiers
	var seenDrain, seenDrainAll, lastDrainAll bool
	for _, role := range roles {
		level := roleLevel(role)
		switch role.Function {
		case "ATK_OP_PIERCING":
			result.piercingRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_ATTR_DEF_PIERCING":
			result.attributePiercingRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_DRAIN":
			if !seenDrain {
				seenDrain, lastDrainAll = true, false
			}
			if result.drainRate == 0 {
				result.drainRoleIndex = role.RoleIndex
			}
			result.drainRate += attackOperatorLevelValue(role, level, chainCount)
			result.drainCap = mergePositiveAttackCap(result.drainCap, combatParameterInt(role.Parameters[2]))
		case "ATK_OP_DRAIN_ALL":
			if !seenDrainAll {
				seenDrainAll, lastDrainAll = true, true
			}
			if result.drainAllRate == 0 {
				result.drainAllRoleIndex = role.RoleIndex
			}
			result.drainAllRate += attackOperatorLevelValue(role, level, chainCount)
			result.drainAllCap = mergePositiveAttackCap(result.drainAllCap, combatParameterInt(role.Parameters[2]))
		case "ATK_OP_DAMAGE_INCREASE":
			scale := battleRoleScale{pvp: pvp, percent: 100}
			if pvp {
				scale.percent = int32(role.PVPRate)
			}
			stat := maxInt(0, combatStatValue(actor, role.Parameters[4]))
			if cap := combatParameterInt(role.Parameters[5]); cap > 0 {
				stat = minInt(stat, cap)
			}
			// Extra-power execution rounds the source segment
			// through float32; the display producer retains integer division.
			if execution {
				result.damageIncrease += damageIncreaseExecutionPower(role, level, stat, scale)
			} else {
				result.damageIncrease += attackRolePowerScaled(role, level, stat, scale)
			}
		case "ATK_OP_REVENGE":
			if result.revengeRate == 0 {
				result.revengeParameter = role.Parameters[2]
				result.revengeCapRate = combatParameterInt(role.Parameters[3])
			}
			result.revengeRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_NOW_TURN_REVENGE":
			if result.nowTurnRevengeRate == 0 {
				result.nowTurnRevengeParameter = role.Parameters[2]
				result.nowTurnRevengeCapRate = combatParameterInt(role.Parameters[3])
			}
			result.nowTurnRevengeRate += attackOperatorLevelValue(role, level, chainCount)
		case "ATK_OP_ATTR_RATE_DOWN_INVALID":
			result.attrRateDownInvalid = true
		case "DAMAGE_BOOST_ORDER_TRIBAL":
			result.tribalDamage = append(result.tribalDamage, tribalDamageModifier{
				rate:       combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level,
				fixed:      combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level/1000,
				damageKind: role.Parameters[6], attribute: role.Parameters[5],
				costMin: combatParameterInt(role.Parameters[7]), costMax: combatParameterInt(role.Parameters[8]),
				raceID: combatParameterInt(role.Parameters[9]),
			})
		}
	}
	result.piercingRate = minInt(100, maxInt(0, result.piercingRate))
	result.attributePiercingRate = minInt(100, maxInt(0, result.attributePiercingRate))
	// 93790/939a0 merge repeated roles into their first producer entry.
	// 81db0 then overwrites the same cap field in producer-entry order.
	if lastDrainAll {
		result.drainCap = result.drainAllCap
	} else {
		result.drainAllCap = result.drainCap
	}
	return result
}

func attackOperatorLevelValue(role CombatSkillRole, level int, chainCount int) int {
	value := linearRoleValue(role, 0, 1, level)
	if chainCount > 1 && role.ChainRate != 0 {
		value = value * (100 + role.ChainRate*(chainCount-1)) / 100
	}
	return value
}

func mergePositiveAttackCap(current int, incoming int) int {
	if incoming <= 0 {
		return current
	}
	if current <= 0 {
		return incoming
	}
	return minInt(current, incoming)
}

func attackRevengeBonus(damageTaken int, actor *battlePlayer, rate int, parameter string, capRate int) int {
	if damageTaken <= 0 || rate <= 0 {
		return 0
	}
	value := damageTaken * rate / 100
	cap := combatStatValue(actor, parameter) * capRate / 100
	return maxInt(0, minInt(value, cap))
}

func recordPlayerDamage(player *battlePlayer, damage int) {
	if damage <= 0 {
		return
	}
	player.DamageTaken += damage
	player.TurnDamage += damage
}

func recordEnemyDamage(enemy *battleEnemy, damage int, damageKind string) {
	if damage <= 0 {
		return
	}
	recordEnemyDamageTotals(enemy, damage)
	recordEnemyDamageKind(enemy, damage, damageKind)
}

func recordEnemyDamageTotals(enemy *battleEnemy, damage int) {
	if damage <= 0 {
		return
	}
	enemy.DamageTaken += damage
	enemy.TurnDamage += damage
}

func recordEnemyDamageKind(enemy *battleEnemy, damage int, damageKind string) {
	if damage <= 0 {
		return
	}
	switch strings.ToUpper(damageKind) {
	case "PHYSICS":
		enemy.TurnPhysical += damage
	case "MAGIC":
		enemy.TurnMagic += damage
	}
}

func (engine *BattleEngine) recordDamageEvent(target int, source int, damage int, attribute string, physics string, dot string, enchant bool) {
	if damage <= 0 {
		return
	}
	if dot == "" {
		engine.recordSkillDamage(target, damage, attribute, physics, enchant)
	}
	engine.turnStats.DamageEvents = append(engine.turnStats.DamageEvents, battleDamageEvent{
		Target: target, Source: source, Value: damage, Attribute: strings.ToUpper(attribute),
		Physics: strings.ToUpper(physics), DOT: strings.ToUpper(dot), Enchant: enchant,
	})
}

func combatPhysicsIndex(value string) int {
	switch strings.ToUpper(value) {
	case "MAGIC":
		return 1
	case "ALL":
		return 2
	default:
		return 0
	}
}

func (engine *BattleEngine) resolveEnemyDeath(results []BattleResult, enemy *battleEnemy) ([]BattleResult, error) {
	// An immediate DESTRUCT already notified its death. A later DOT sweep
	// must not manufacture another break/drop while that member stays dead.
	if enemy.HP > 0 || enemy.Broken || enemy.SelfDestructed && !enemy.PendingBreak {
		return results, nil
	}
	enemy.SelfDestructed = false
	var err error
	results, err = engine.triggerEnemyDeathAction(results, enemy)
	if err != nil || enemy.HP > 0 {
		return results, err
	}
	return engine.commitEnemyBreak(results, enemy), nil
}

func (engine *BattleEngine) triggerEnemyDeathAction(results []BattleResult, enemy *battleEnemy) ([]BattleResult, error) {
	if enemy.HP > 0 || enemy.DeathActionTriggered {
		return results, nil
	}
	if !enemy.DeathActionTriggered {
		// Native FUN_0005b9d8 clears the per-enemy death trigger before it
		// evaluates slot 20. A failed condition/rate/budget therefore does not
		// retry on another HP consumer while the member remains dead.
		enemy.DeathActionTriggered = true
		for _, action := range enemy.Level.Actions {
			if action.Category != "death" {
				continue
			}
			consumed := enemy.ActionConsumed
			candidate, scheduled := engine.tryScheduleEnemyAction(enemy, action, &consumed)
			enemy.ActionConsumed = consumed
			if scheduled {
				projected, err := engine.executeEnemyActionCandidate(enemy, candidate)
				if err != nil {
					return nil, fmt.Errorf("enemy %d death action %d: %w", enemy.MemberType, action.SkillID, err)
				}
				results = append(results, projected...)
			}
			break
		}
	}
	return results, nil
}

func (engine *BattleEngine) commitEnemyBreak(results []BattleResult, enemy *battleEnemy) []BattleResult {
	enemy.Broken = true
	enemy.TurnSkillStats.Died = true
	enemy.DiedTurn = engine.turn
	enemy.DeathCount++
	command := CommandEnemyBreak
	if enemy.Parent > 0 {
		command = CommandPartsBreak
	}
	results = append(results, BattleResult{Command: command, Args: []int64{int64(enemy.MemberType), 0, 0, 0, 1}})
	return append(results, enemyBreakDropResults(enemy)...)
}

func (engine *BattleEngine) executeFixedDefense(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	return engine.applyPlayerParameterValue(action, role, func() int {
		value := engine.fixedParameterActionValue(action, role, chainCount)
		return engine.parameterBoostedValue(action, role, "DEF_UP_BOOST", value)
	})
}

func (engine *BattleEngine) executeFixedParameter(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	return engine.applyPlayerParameterValue(action, role, func() int {
		value := engine.fixedParameterActionValue(action, role, chainCount)
		return engine.parameterBoostedValue(action, role, "ATK_UP_BOOST", value)
	})
}

func (engine *BattleEngine) executePlayerParameterLimit(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	// Native role 142 creates action type 2/subtype 32. Its value is accumulated
	// into the selected ATK/INT/MND ceiling; it never mutates that battle
	// parameter directly. ResultCmd6 exposes the three current ceilings.
	duration := combatParameterInt(role.Parameters[0])
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerBuffTargets(action, role) {
		// 7ea92 consumes the pending bonus on the first matching recipient;
		// an all-target effect must not copy that bonus to every recipient.
		value := engine.fixedParameterActionValue(action, role, chainCount)
		player := engine.player(memberType)
		ensurePlayerBaseParameters(player)
		if !adjustPlayerParameterLimit(player, role.Parameters[1], value) {
			return nil, fmt.Errorf("unsupported player parameter limit %q", role.Parameters[1])
		}
		player.Effects = append(player.Effects, battleEffect{
			Function: role.Function, ListType: action.buffListType, Parameter: role.Parameters[1], Value: value, Delta: value,
			Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
		})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], 0)
		results = append(results, playerParameterStatusResults(player, role, battleBuffCodes[role.Function], action.buffListType)...)
	}
	return results, nil
}

func adjustPlayerParameterLimit(player *battlePlayer, name string, amount int) bool {
	switch strings.ToUpper(name) {
	case "ATK":
		player.LimitAttack += amount
	case "INT":
		player.LimitMagic += amount
	case "MND":
		player.LimitRecovery += amount
	default:
		return false
	}
	return true
}

func (engine *BattleEngine) executeFixedHeal(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := engine.player(action.memberType)
	value := fixedHealRoleValueScaled(role, action.cardLevel, 1, combatStatValue(actor, role.Parameters[4]), engine.roleScale(role))
	value = supportHealValue(actor.Effects, action.skill, value)
	value = scaleHealChain(value, role.ChainRate, chainCount)
	return engine.healPlayerTargets(action, role, value)
}

func (engine *BattleEngine) executeSelfScaledParameter(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := engine.player(action.memberType)
	boostFunction := "ATK_UP_BOOST"
	if strings.HasPrefix(role.Function, "DEF_UP") {
		boostFunction = "DEF_UP_BOOST"
	}
	// 88a20 reads the source again for each recipient. A source that receives
	// its own ATK-based ATK buff changes the value for subsequent recipients.
	return engine.applyPlayerParameterValue(action, role, func() int {
		value := selfScaledParameterValueScaled(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[2]), engine.roleScale(role))
		return engine.parameterBoostedValue(action, role, boostFunction, value)
	})
}

func (engine *BattleEngine) applyPlayerParameterValue(action battleAction, role CombatSkillRole, currentValue func() int) ([]BattleResult, error) {
	buffCode, exists := battleBuffCodes[role.Function]
	if !exists {
		return nil, fmt.Errorf("player parameter function %q has no official buff code", role.Function)
	}
	// HP is a declared enum but is outside the retained six-parameter tuple.
	// It still registers the original duration/status, with no parameter delta.
	inert := strings.EqualFold(role.Parameters[1], "HP")
	if !inert && !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported player parameter %q", role.Parameters[1])
	}
	duration := combatParameterInt(role.Parameters[0])
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerBuffTargets(action, role) {
		player := engine.player(memberType)
		value := currentValue()
		delta := value
		if inert {
			delta = 0
		}
		ensurePlayerBaseParameters(player)
		player.Effects = append(player.Effects, battleEffect{Function: role.Function, ListType: action.buffListType, Parameter: role.Parameters[1], Value: value, Delta: delta, Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], delta)
		results = append(results, playerParameterStatusResults(player, role, buffCode, action.buffListType)...)
	}
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		enemy := &engine.enemies[index]
		value := currentValue()
		delta := value
		if inert {
			delta = 0
		}
		ensureEnemyBaseParameters(enemy)
		effect := battleEffect{Function: role.Function, ListType: action.buffListType, Parameter: role.Parameters[1], Value: value, Delta: delta, Kind: 1, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
		enemy.Effects = append(enemy.Effects, effect)
		engine.recordAIStatusApplied(enemy.MemberType, effect)
		refreshAppliedEnemyParameter(enemy, role.Parameters[1], delta)
		results = append(results, battleParameterBuffResult(enemy.MemberType, role, buffCode, action.buffListType),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)})
	}
	return results, nil
}

func (engine *BattleEngine) executeEnemyParameterDebuff(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	role.Target = engine.resolvedPlayerRoleTarget(action, role.Target)
	value := fixedBuffRoleValueScaled(role, action.cardLevel, chainCount, engine.roleScale(role))
	currentValue := func() int { return value }
	if strings.HasSuffix(role.Function, "BY_SELF_PARAM") {
		actor := engine.player(action.memberType)
		// Each recipient observes the current caster parameters after earlier
		// recipients have committed, including when the caster is itself a target.
		currentValue = func() int {
			return selfScaledParameterValueScaled(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[2]), engine.roleScale(role))
		}
	}
	boostFunction := "ATK_BREAK_BOOST"
	if strings.HasPrefix(role.Function, "GUARD_BREAK") {
		boostFunction = "GUARD_BREAK_BOOST"
	}
	fixed, rate := engine.parameterBoostTerms(action, role, boostFunction)
	if !combatParameterSupported(role.Parameters[1]) {
		return nil, fmt.Errorf("unsupported debuff parameter %q", role.Parameters[1])
	}
	results := make([]BattleResult, 0, engine.enemyCount*2)
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	for _, memberType := range engine.playerBuffTargets(action, role) {
		player := engine.player(memberType)
		amount := parameterDebuffValue(player.Effects, role.Function, role.Parameters[1], currentValue(), fixed, rate)
		ensurePlayerBaseParameters(player)
		player.Effects = append(player.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: amount, Delta: -amount, Kind: 2, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(memberType, player.Effects[len(player.Effects)-1])
		refreshAppliedPlayerParameter(player, role.Parameters[1], -amount)
		results = append(results,
			battleParameterBuffResult(memberType, role, battleBuffCodes[role.Function], 0),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(memberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)},
		)
	}
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		enemy := &engine.enemies[index]
		amount := parameterDebuffValue(enemy.Effects, role.Function, role.Parameters[1], currentValue(), fixed, rate)
		ensureEnemyBaseParameters(enemy)
		code := battleBuffCodes[role.Function]
		enemy.Effects = append(enemy.Effects, battleEffect{Function: role.Function, Parameter: role.Parameters[1], Value: amount, Delta: -amount, Kind: 2, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex})
		engine.recordAIStatusApplied(enemy.MemberType, enemy.Effects[len(enemy.Effects)-1])
		refreshAppliedEnemyParameter(enemy, role.Parameters[1], -amount)
		results = append(results,
			battleParameterBuffResult(enemy.MemberType, role, code, 0),
			BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)},
		)
	}
	return results, nil
}

func (engine *BattleEngine) executeSelfScaledHeal(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	actor := engine.player(action.memberType)
	value := selfScaledHealRoleValueScaled(role, action.cardLevel, chainCount, combatStatValue(actor, role.Parameters[0]), engine.roleScale(role))
	value = supportHealValue(actor.Effects, action.skill, value)
	return engine.healPlayerTargets(action, role, value)
}

func (engine *BattleEngine) healPlayerTargets(action battleAction, role CombatSkillRole, value int) ([]BattleResult, error) {
	return engine.healPlayerTargetsWithValue(action, role, func(int, int) int { return value })
}

func (engine *BattleEngine) executeTargetScaledHeal(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	return engine.healPlayerTargetsWithValue(action, role, func(hp, maxHP int) int {
		source := hp
		if role.Function == "HEAL_BY_TARGET_MAXHP" {
			source = maxHP
		}
		return targetHPHealRoleValue(role, action.cardLevel, chainCount, source)
	})
}

func (engine *BattleEngine) healPlayerTargetsWithValue(action battleAction, role CombatSkillRole, value func(int, int) int) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 8)
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := engine.player(memberType)
		oldHP := player.HP
		reported, hp := nativeHealCommit(player.HP, player.MaxHP, value(player.HP, player.MaxHP), player.Effects, true)
		player.HP = hp
		engine.turnStats.Heal += maxInt(0, reported)
		// 822a0 -> 4cbf8 records positive player-to-player direct healing by
		// recipient, including overheal. Regeneration has no such producer.
		engine.turnStats.HealReceived[memberType-1] += int64(maxInt(0, reported))
		engine.recordRecoveryAction(action.memberType, reported)
		engine.recordPlayerHealCommit(player, action.memberType, oldHP, reported, action.skill.HateRatio, role.HateLimit)
		results = append(results, battleHealResult(memberType, role.RoleIndex, reported, player.HP))
	}
	// 822a0 also accepts enemy targets for a player skill. Only player-to-player
	// healing generates hate; positive reported healing always enters the
	// caster's skill counter, and enemy parts share it with their living parent.
	if combatTargetIsPlayer(role.Target) {
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		target := &engine.enemies[index]
		rows := engine.commitDirectEnemyHeal(target, role.RoleIndex, value(target.HP, target.MaxHP))
		engine.turnStats.Heal += maxInt(0, int(rows[0].Args[2]))
		engine.recordRecoveryAction(action.memberType, int(rows[0].Args[2]))
		results = append(results, rows...)
	}
	return results, nil
}

func (engine *BattleEngine) recordPlayerHealCommit(player *battlePlayer, source int, oldHP, reported, hateRate, hateLimit int) {
	// 822a0/5f808 retain the requested positive amount for the result/skill
	// counters, but 57c40 receives only HP actually restored. 73e9f records
	// a reversed heal as damage; it never generates healing hate.
	if reported < 0 {
		recordPlayerDamage(player, -reported)
	} else if reported > 0 {
		engine.addPlayerRoleHate(source, maxInt(0, player.HP-oldHP), hateRate, hateLimit)
	}
}

func (engine *BattleEngine) discardSelectedCards() {
	for _, action := range engine.turnActions {
		if action.cardType > 0 && action.sphereSlot == 0 {
			engine.removeCardFromHand(action.memberType, action.cardType)
		}
	}
}

func (engine *BattleEngine) finishStoryUserAttack(results []BattleResult) ([]BattleResult, error) {
	engine.discardSelectedCards()
	return engine.finishStoryPhase(results, 1)
}
