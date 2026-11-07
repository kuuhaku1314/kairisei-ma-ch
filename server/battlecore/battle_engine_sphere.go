package battlecore

import (
	"errors"
	"fmt"
	"strings"
)

const (
	SphereTypeNormal  = "NORMAL"
	SphereTypeChalice = "CHALICE"
)

var ErrChaliceSphereUnavailable = errors.New("combat chalice sphere is unavailable")

func (engine *BattleEngine) sphereItemResults() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers*(deckSphereSlots+1))
	for _, player := range engine.playerStates() {
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			results = append(results, BattleResult{Command: CommandSphere, Args: []int64{
				int64(player.MemberType), int64(sphere.Slot), int64(sphere.SphereID),
			}})
		}
	}
	return results
}

// sphereStateResults projects the count, cost and display-power families that
// native refreshes after the identities have been installed. Play conditions
// are intentionally excluded: FUN_000cef63 publishes ResultCmd309 at the turn
// input boundary, which is sphereConditionResults below.
func (engine *BattleEngine) sphereStateResults() ([]BattleResult, error) {
	results, err := engine.sphereCountCostResults()
	if err != nil {
		return nil, err
	}
	for _, player := range engine.playerStates() {
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			display, err := engine.sphereDisplayState(player, sphere)
			if err != nil {
				return nil, err
			}
			sphere.Display = display
			results = append(results, sphereUpdateResult(player.MemberType, sphere.Slot, display.Power, display.Marker))
		}
	}
	return results, nil
}

// 63cc0 calls a59f6 then a5c9e: all sphere counts, then all costs. Full
// selection power (307) is emitted later by the same projection as hand cards.
func (engine *BattleEngine) sphereCountCostResults() ([]BattleResult, error) {
	var counts, costs []BattleResult
	for _, player := range engine.playerStates() {
		for j := range player.Spheres {
			sphere := &player.Spheres[j]
			if sphere.SphereID == 0 {
				continue
			}
			skill, _, err := engine.catalog.SphereSkill(sphere.SphereID)
			if err != nil {
				return nil, err
			}
			counts = append(counts, BattleResult{Command: CommandSphereCount, Args: []int64{int64(player.MemberType), int64(sphere.Slot), int64(sphere.Count), int64(sphere.Maximum)}})
			costs = append(costs, BattleResult{Command: CommandSphereCost, Args: []int64{int64(player.MemberType), int64(sphere.Slot), int64(skill.Cost)}})
		}
	}
	return append(counts, costs...), nil
}

func (engine *BattleEngine) sphereConditionResults() ([]BattleResult, error) {
	results := make([]BattleResult, 0, maxRoomMembers*deckSphereSlots)
	for _, player := range engine.playerStates() {
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID == 0 {
				continue
			}
			definition := engine.catalog.Spheres[sphere.SphereID]
			engine.refreshSphereCondition(sphere, definition)
			condition, err := spherePlayConditionCode(definition.PlayCondition)
			if err != nil {
				return nil, err
			}
			results = append(results, BattleResult{Command: CommandSpherePlayCondition, Args: []int64{
				int64(player.MemberType), int64(sphere.Slot), int64(condition), boolInt64(sphere.Playable), int64(sphere.Remaining),
			}})
		}
	}
	return results, nil
}

func (engine *BattleEngine) refreshSphereCondition(sphere *battleSphere, definition CombatSphereDefinition) {
	sphere.Remaining = 0
	switch definition.PlayCondition {
	case "", "NONE":
		sphere.Playable = true
	case "TURN_AFTER":
		sphere.Remaining = maxInt(0, definition.PlayParam+1-engine.turn)
		sphere.Playable = sphere.Remaining == 0
	default:
		sphere.Playable = false
	}
}

func spherePlayConditionCode(condition string) (int, error) {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "", "NONE":
		return 0, nil
	case "TURN_AFTER":
		return 1, nil
	default:
		return 0, fmt.Errorf("unsupported sphere play condition %q", condition)
	}
}

func boolInt64(value bool) int64 {
	if value {
		return 1
	}
	return 0
}

func (engine *BattleEngine) validateSphereSubmission(memberType int, submission cardPlaySubmission) (battleAction, error) {
	if submission.SphereSlot == 0 {
		return battleAction{}, nil
	}
	if submission.SphereSlot < 1 || submission.SphereSlot > deckSphereSlots {
		return battleAction{}, errors.New("combat sphere selection slot is invalid")
	}
	player := engine.player(memberType)
	sphere := &player.Spheres[submission.SphereSlot-1]
	if sphere.SphereID == 0 || sphere.Type != SphereTypeNormal || sphere.Count <= 0 || !sphere.Playable {
		return battleAction{}, errors.New("combat normal sphere selection is unavailable")
	}
	// Original 3b390 checks identity, remaining uses and play condition for a
	// sphere. It has no STAN rejection, just like ordinary PvE card selection.
	skill, roles, err := engine.catalog.SphereSkill(sphere.SphereID)
	if err != nil {
		return battleAction{}, err
	}
	target := submission.SphereTarget
	if target == 0 && skill.Target == "SELF" {
		target = memberType
	}
	if err := engine.validateTarget(skill.Target, memberType, target); err != nil {
		return battleAction{}, err
	}
	action := battleAction{
		memberType: memberType, sphereSlot: sphere.Slot, cardLevel: sphere.executionLevel(skill.ID),
		target: target, skill: skill, roles: roles,
	}
	if err := engine.attachSphereCallSkill(&action, sphere.SphereID); err != nil {
		return battleAction{}, err
	}
	return action, nil
}

func (engine *BattleEngine) attachSphereCallSkill(action *battleAction, sphereID int) error {
	callSkill, callRoles, err := engine.catalog.SphereCallSkill(sphereID)
	if err != nil {
		return err
	}
	action.callSkill = callSkill
	action.callRoles = callRoles
	if engine.playerMember(action.memberType) && action.sphereSlot >= 1 && action.sphereSlot <= deckSphereSlots {
		sphere := engine.player(action.memberType).Spheres[action.sphereSlot-1]
		action.callLevel, action.hasCallLevel = sphere.executionLevel(callSkill.ID), true
	}
	return nil
}

func (engine *BattleEngine) chalicePlayableResults() ([]BattleResult, error) {
	results := make([]BattleResult, 0, maxRoomMembers)
	chainCounts := engine.currentChainCounts()
	for _, player := range engine.playerStates() {
		if player.HP <= 0 {
			continue
		}
		for sphereIndex := range player.Spheres {
			sphere := &player.Spheres[sphereIndex]
			if sphere.SphereID != 0 && sphere.Type == SphereTypeChalice && sphere.Count > 0 && sphere.Playable {
				skill, _, err := engine.catalog.SphereSkill(sphere.SphereID)
				if err != nil {
					return nil, err
				}
				// 796a8/79d3e uses the owner's selected-card Chain for the
				// hold condition; this is not a zero-Chain sphere attack.
				action := battleAction{memberType: player.MemberType, cardLevel: sphere.executionLevel(skill.ID), skill: skill}
				if !engine.branchConditionSatisfied(skill.AppendCondition, skill.AppendParameters, action, engine.turnActions, chainCounts) {
					continue
				}
				sphere.ChalicePlayable = true
				results = append(results, BattleResult{Command: CommandChaliceSpherePlayable, Args: []int64{int64(player.MemberType), int64(sphere.Slot)}})
			}
		}
	}
	return results, nil
}

func (engine *BattleEngine) ReserveChaliceSphere(memberType int, slot int) ([]BattleResult, error) {
	// Original x86 13db0 checks the registered slot/type/playable flag, not
	// phase, future HP, continuation or the already-computed battle outcome.
	// A reservation only records intent while those results are animated.
	// Execution still observes KO/terminal state and consumes a use only when
	// the action runs; enemy cleanup revokes the published eligibility.
	if memberType < 1 || memberType > maxRoomMembers || slot < 0 || slot > deckSphereSlots {
		return nil, errors.New("combat chalice sphere reserve is invalid")
	}
	player := engine.player(memberType)
	if player.MemberType != memberType {
		return nil, errors.New("combat chalice sphere member is unavailable")
	}
	if slot != 0 {
		sphere := &player.Spheres[slot-1]
		if sphere.SphereID == 0 || sphere.Type != SphereTypeChalice || sphere.Count <= 0 || !sphere.ChalicePlayable {
			return nil, ErrChaliceSphereUnavailable
		}
	}
	player.ReservedChalice = slot
	return []BattleResult{{Command: CommandChaliceSphereReserve, Args: []int64{int64(memberType), int64(slot)}}}, nil
}

func (engine *BattleEngine) ExecuteChaliceUserPhase() ([]BattleResult, error) {
	if engine.phase != PhaseUserAttack {
		return nil, errors.New("combat chalice user phase is unavailable")
	}
	results, err := engine.executeReservedChaliceSpheres()
	if err != nil {
		return nil, err
	}
	gaugeRows, err := engine.advanceReactiveTranceStates()
	if err != nil {
		return nil, err
	}
	results = append(results, gaugeRows...)
	return engine.settleActionPhase(results, PhaseChaliceUser, false)
}

// Both original chalice APIs execute the same reserved-action routine (65b9a).
// The enemy API then expires effects, unless the summons ended the battle.
func (engine *BattleEngine) executeReservedChaliceSpheres() ([]BattleResult, error) {
	actions := make([]battleAction, 0, maxRoomMembers)
	for _, player := range engine.playerStates() {
		if player.ReservedChalice == 0 || player.HP <= 0 {
			continue
		}
		sphere := &player.Spheres[player.ReservedChalice-1]
		if sphere.SphereID == 0 || sphere.Type != SphereTypeChalice || sphere.Count <= 0 || !sphere.ChalicePlayable {
			return nil, fmt.Errorf("member %d reserved chalice sphere became unavailable", player.MemberType)
		}
		skill, roles, err := engine.catalog.SphereSkill(sphere.SphereID)
		if err != nil {
			return nil, err
		}
		// 65b9a builds the reserved action with target zero; 7adb0 resolves
		// SELF/whole-side effects without changing the 56 display target.
		action := battleAction{
			memberType: player.MemberType, sphereSlot: sphere.Slot, cardLevel: sphere.executionLevel(skill.ID),
			skill: skill, roles: roles,
		}
		if err := engine.attachSphereCallSkill(&action, sphere.SphereID); err != nil {
			return nil, err
		}
		actions = append(actions, action)
	}
	// FUN_00065b9a builds manual-flag actions and uses the same native sort.
	engine.sortPlayerActions(actions)
	results := make([]BattleResult, 0, len(actions)*12)
	for _, action := range actions {
		if engine.player(action.memberType).HP <= 0 {
			continue
		}
		engine.retargetPlayerAction(&action)
		// Like normal spheres, branch at execution, not before earlier actions.
		// 65b9a reuses 3d342: branch conditions read the ordinary selections.
		branch, branchIndex := engine.selectCombatSkillBranchWithIndex(action, engine.turnActions, engine.currentChainCounts())
		roles := engine.catalog.SkillRoles[branch.FunctionID]
		if branch.ID == 0 || len(roles) == 0 {
			return nil, fmt.Errorf("chalice sphere skill role %d is unavailable", branch.FunctionID)
		}
		branch.Target = action.skill.Target
		action.skill, action.branchIndex, action.roles = branch, branchIndex, roles
		actionResults, err := engine.executeSphereAction(action, 0)
		if err != nil {
			return nil, err
		}
		results = append(results, actionResults...)
		if engine.endType != 0 {
			break
		}
	}
	for _, player := range engine.playerStates() {
		if player.ReservedChalice == 0 {
			continue
		}
		player.ReservedChalice = 0
		for sphereIndex := range player.Spheres {
			player.Spheres[sphereIndex].ChalicePlayable = false
		}
	}
	return results, nil
}

func (engine *BattleEngine) ExecuteChaliceEnemyPhase() ([]BattleResult, error) {
	if engine.phase != PhaseEnemy {
		return nil, errors.New("combat chalice enemy phase is unavailable")
	}
	results, err := engine.executeReservedChaliceSpheres()
	if err != nil {
		return nil, err
	}
	gaugeRows, err := engine.advanceReactiveTranceStates()
	if err != nil {
		return nil, err
	}
	results = append(results, gaugeRows...)
	// JP 14f40 rechecks continuation (66310) even with no reserved sphere.
	// A partial-party offer still permits expiry; an empty phase must not
	// synthesize automatic retirement for callers that do not offer Continue.
	if len(results) != 0 || engine.continueAllowed {
		results, err = engine.settleActionPhase(results, PhaseChaliceEnemy, false)
		if err != nil {
			return nil, err
		}
	}
	// 66310 stops enemy-tail expiry for an all-KO continuation offer too.
	if engine.endType != 0 || engine.continuePending && engine.allPlayersDead() {
		return results, nil
	}
	// Original 52e14 limit is consumed at the end of the enemy-chalice phase.
	// Timeout is native ENEMY_WIN (2); the SCORE scene still awards score.
	if engine.endTurn > 0 && engine.turn >= engine.endTurn {
		engine.endType, engine.phase = 2, PhaseEnded
		return engine.appendWaveBuffCleanup(append(results, battleEndResult(2))), nil
	}
	for playerIndex := range engine.players {
		for sphereIndex := range engine.players[playerIndex].Spheres {
			engine.players[playerIndex].Spheres[sphereIndex].ChalicePlayable = false
		}
	}
	engine.phase = PhaseChaliceEnemy
	return engine.appendEffectTurn(results)
}

// Both normal turns and keep-buff handover use the same expiry/publication.
func (engine *BattleEngine) appendEffectTurn(results []BattleResult) ([]BattleResult, error) {
	// Native battle5_api_chalice_sphr_exec_enemy_phase calls FUN_0005eb20
	// whenever its terminal check remains clear. FUN_0005eb20 always starts
	// the resulting direction group with zero-argument BUFF_PARTITION(70),
	// even when it has no following cleanup rows.
	rows, err := engine.tickPersistentEffects()
	if err != nil {
		return nil, err
	}
	display, err := engine.refreshBattleDisplayPowers()
	if err != nil {
		return nil, err
	}
	rows = append(rows, display...)
	results = append(results, BattleResult{Command: CommandBuffPartition})
	return append(results, rows...), nil
}

func (engine *BattleEngine) executeSphereAction(action battleAction, chainCount int) ([]BattleResult, error) {
	if !engine.playerMember(action.memberType) || action.sphereSlot < 1 || action.sphereSlot > deckSphereSlots {
		return nil, errors.New("combat sphere action is invalid")
	}
	sphere := &engine.player(action.memberType).Spheres[action.sphereSlot-1]
	if sphere.SphereID == 0 || sphere.Count <= 0 {
		return nil, errors.New("combat sphere action is unavailable")
	}
	// Reserved chalices carry target zero. Original JP 7a210 skips a
	// single-target skill with no selected recipient before its header,
	// gauge reaction or use count; SELF and whole-side skills still execute.
	if action.target == 0 && (action.skill.Target == "USER_ONE" || action.skill.Target == "ENEMY_ONE") {
		return nil, nil
	}
	engine.player(action.memberType).ExecutionSource.setPhysical(0, action.sphereSlot)
	header, err := playerSphereSkillResult(action, chainCount)
	if err != nil {
		return nil, err
	}
	results := []BattleResult{header}
	gaugeRows := engine.reactiveGaugeSkillStart(action, "SPHR")
	results = append(results, gaugeRows...)
	display, err := engine.sphereDisplayState(engine.player(action.memberType), sphere)
	if err != nil {
		return nil, err
	}
	results = append(results, sphereUpdate2Result(action.memberType, action.sphereSlot, display.Power, display.Marker))
	engine.recordExecutedPlayerSkill(action, chainCount)
	action.skillBonus = &battleSkillBonus{}
	skillRows, err := engine.executeSkillRoleSet(action.memberType, action.target, action.skill, action.roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
		return engine.executePlayerRole(action, role, chainCount)
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
	engine.addPlayerHate(action.memberType, int64(maxInt(0, action.skill.HateRatio)))
	sphere.Count--
	if engine.allEnemyBodiesDead() && engine.endType == 0 {
		engine.endType = engine.enemyWaveEndType()
	}
	return results, nil
}
