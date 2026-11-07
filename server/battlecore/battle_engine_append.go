package battlecore

import (
	"fmt"
	"strings"
)

// battleBlessHold mirrors the APPEND_CARD_CURSE/BLESS entries kept separately
// from the ordinary buff list by CN libbattle5. CardType is 21 for a curse and
// 22 for a blessing. EnemySkill records which immutable skill table owns the
// stored pointer; SourceMember is the original caster. The containing player's
// member ID is the independent holder/trigger context (80510 -> 668b4).
type battleBlessHold struct {
	AppendIndex  int
	CardType     int
	SourceMember int
	EnemySkill   bool
	Skill        CombatSkillDefinition
	Roles        []CombatSkillRole
	CardLevel    int
	Target       int
	Remaining    int
	Repeat       bool
	Power        int
	Marker       int
	PowerKnown   bool
	ChainCount   int
	AppliedTurn  int
}

func (engine *BattleEngine) executePlayerBless(action battleAction, role CombatSkillRole, chainCount int) ([]BattleResult, error) {
	if action.callSkill.ID == 0 {
		return nil, nil
	}
	duration := combatParameterInt(role.Parameters[0])
	if duration <= 0 {
		duration = action.callSkill.AppendDuration
	}
	repeat := combatParameterInt(role.Parameters[2]) != 0
	holdSkill, holdRoles := action.callSkill, action.callRoles
	// a275b resolves CALL power once in the producer, with no target and
	// preview=0 (no RNG snapshot, but mode0 excludes RANDOM). 80510 does a separate lookup for
	// each recipient, with Chain=0. Neither is the raw base-role display.
	if !action.callProducerQueried[role.RoleIndex] {
		prototype := battleBlessHold{CardType: 22, SourceMember: action.memberType,
			Skill: holdSkill, Roles: holdRoles, CardLevel: action.callExecutionLevel()}
		if _, err := engine.resolveHoldDisplayState(engine.player(action.memberType), prototype, engine.appendChainCounts(action.memberType, holdSkill), false); err != nil {
			return nil, err
		}
	}
	results := make([]BattleResult, 0, maxRoomMembers)
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := engine.player(memberType)
		appendIndex, ok := engine.nextBlessAppendIndex(player)
		if !ok {
			continue
		}
		hold := battleBlessHold{
			AppendIndex: appendIndex, CardType: 22, SourceMember: action.memberType,
			Skill: holdSkill, Roles: append([]CombatSkillRole(nil), holdRoles...),
			CardLevel: action.callExecutionLevel(), Target: memberType, Remaining: duration, Repeat: repeat,
			ChainCount: chainCount, AppliedTurn: engine.turn,
		}
		state, err := engine.resolveHoldDisplayState(player, hold, nil, true)
		if err != nil {
			return nil, err
		}
		hold.Power, hold.Marker, hold.PowerKnown = state.Power, state.Marker, state.Known
		player.BlessHolds = append(player.BlessHolds, hold)
		results = append(results, blessHoldSetResult(memberType, role.RoleIndex, hold))
	}
	return results, nil
}

func (engine *BattleEngine) nextBlessAppendIndex(player *battlePlayer) (int, bool) {
	capacity := engine.holdMax
	if capacity <= 0 || capacity > maxHoldEntries {
		capacity = maxHoldEntries
	}
	if len(player.BlessHolds)+len(player.CardHolds) >= capacity {
		return 0, false
	}
	used := [maxHoldEntries]bool{}
	for _, hold := range player.BlessHolds {
		if hold.AppendIndex >= 0 && hold.AppendIndex < len(used) {
			used[hold.AppendIndex] = true
		}
	}
	for index := range used {
		if !used[index] {
			return index, true
		}
	}
	return 0, false
}

func blessHoldSetResult(memberType int, roleIndex int, hold battleBlessHold) BattleResult {
	// Native FUN_00043132 projects HOLD_SET as member, role index, append-card
	// type, sphere slot, skill id, append index, turns, skill level and power.
	return BattleResult{Command: CommandHoldSet, Args: []int64{
		int64(memberType), int64(roleIndex), int64(hold.CardType), 0, int64(hold.Skill.ID), int64(hold.AppendIndex),
		int64(hold.Remaining), int64(hold.CardLevel), int64(hold.Power),
	}}
}

func (engine *BattleEngine) changePlayerBlessTurns(action battleAction, role CombatSkillRole, delta int) []BattleResult {
	return engine.changeBlessTurns(
		engine.playerRoleTargets(action, role), role, delta,
	)
}

func blessHoldLostResult(memberType int, hold battleBlessHold) BattleResult {
	return BattleResult{Command: CommandHoldLost, Args: []int64{int64(memberType), int64(hold.CardType), 0, int64(hold.AppendIndex)}}
}

func (engine *BattleEngine) executeEnemyCurse(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	return engine.executeEnemyCallSkillAppendCard(actor, selected, role, 21)
}

func (engine *BattleEngine) executeEnemyBless(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	return engine.executeEnemyCallSkillAppendCard(actor, selected, role, 22)
}

func (engine *BattleEngine) executeEnemyCallSkillAppendCard(actor *battleEnemy, selected int, role CombatSkillRole, cardType int) ([]BattleResult, error) {
	// In enemy context both APPEND_CARD_CURSE and APPEND_CARD_BLESS resolve p1
	// through FUN_00057ed8: the acting enemy's zero-based enemy_lvup call-skill
	// slot. FUN_00043132 then stores the target player and the resolved enemy
	// skill pointer. FUN_00080510 retains the originating enemy separately;
	// FUN_000668b4 checks conditions on the holder but executes with that caster.
	if cardType != 21 && cardType != 22 {
		return nil, fmt.Errorf("enemy append-card type %d is invalid", cardType)
	}
	results := make([]BattleResult, 0, 1)
	callIndex := combatParameterInt(role.Parameters[1])
	if callIndex < 0 || callIndex >= len(actor.Level.CallSkillIDs) {
		return nil, fmt.Errorf("enemy append-card call-skill index %d is invalid", callIndex)
	}
	callSkillID := actor.Level.CallSkillIDs[callIndex]
	if callSkillID == 0 {
		return nil, nil
	}
	skill, roles, ok := engine.enemySkillBase(callSkillID)
	if !ok || len(roles) == 0 {
		return nil, fmt.Errorf("enemy call skill %d is incomplete", callSkillID)
	}
	if cardType == 22 {
		// a275b performs this producer lookup for BLESS; 9955e (CURSE)
		// only writes zero display power and must not consume branch RNG here.
		view := enemyDisplaySource(actor)
		prototype := battleBlessHold{CardType: cardType, SourceMember: actor.MemberType,
			EnemySkill: true, Skill: skill, Roles: roles, CardLevel: 1}
		if _, err := engine.resolveHoldDisplayState(&view, prototype, nil, false); err != nil {
			return nil, err
		}
	}
	for _, memberType := range engine.enemyRolePlayerTargets(actor, selected, role) {
		player := engine.player(memberType)
		appendIndex, ok := engine.nextBlessAppendIndex(player)
		if !ok {
			continue
		}
		duration := combatParameterInt(role.Parameters[0])
		if duration <= 0 {
			duration = skill.AppendDuration
		}
		hold := battleBlessHold{
			AppendIndex: appendIndex, CardType: cardType, SourceMember: actor.MemberType, EnemySkill: true,
			Skill: skill, Roles: append([]CombatSkillRole(nil), roles...),
			CardLevel: 1, Target: memberType,
			Remaining: duration, Repeat: combatParameterInt(role.Parameters[2]) != 0,
			ChainCount: 1, AppliedTurn: engine.turn,
		}
		state, err := engine.resolveHoldDisplayState(player, hold, nil, true)
		if err != nil {
			return nil, err
		}
		hold.Power, hold.Marker, hold.PowerKnown = state.Power, state.Marker, state.Known
		player.BlessHolds = append(player.BlessHolds, hold)
		results = append(results, blessHoldSetResult(memberType, role.RoleIndex, hold))
	}
	return results, nil
}

func (engine *BattleEngine) changeEnemyBlessTurns(actor *battleEnemy, selected int, role CombatSkillRole, delta int) []BattleResult {
	return engine.changeBlessTurns(engine.enemyRolePlayerTargets(actor, selected, role), role, delta)
}

func (engine *BattleEngine) changeBlessTurns(targets []int, role CombatSkillRole, delta int) []BattleResult {
	if delta == 0 {
		return nil
	}
	attribute := strings.TrimSpace(role.Parameters[1])
	results := make([]BattleResult, 0, len(targets)*2)
	for _, memberType := range targets {
		player := engine.player(memberType)
		hasMatch := false
		for _, hold := range player.BlessHolds {
			if blessTurnHoldMatches(hold, attribute) {
				hasMatch = true
				break
			}
		}
		if !hasMatch {
			continue
		}
		// Native FUN_00080070 first projects APPEND_CARD_TURN_ADD and then
		// FUN_00042e00 mutates/removes every matching hold in vector order.
		results = append(results, BattleResult{Command: CommandAppendTurnAdd, Args: []int64{
			int64(memberType), int64(role.RoleIndex), 1, int64(combatAttributeCode(attribute)), int64(delta),
		}})
		kept := player.BlessHolds[:0]
		for _, hold := range player.BlessHolds {
			if !blessTurnHoldMatches(hold, attribute) {
				kept = append(kept, hold)
				continue
			}
			hold.Remaining += delta
			if hold.Remaining > 0 {
				kept = append(kept, hold)
			} else {
				results = append(results, blessHoldLostResult(memberType, hold))
			}
		}
		player.BlessHolds = kept
	}
	return results
}

func blessTurnHoldMatches(hold battleBlessHold, attribute string) bool {
	return hold.CardType == 22 && damageAttributeMatches(attribute, hold.Skill.Attribute)
}

type queuedBlessHold struct {
	owner int
	hold  battleBlessHold
}

func (engine *BattleEngine) executeBlessHolds(timing string, cardTypeFilter ...int) ([]BattleResult, error) {
	filter := 0
	if len(cardTypeFilter) > 0 {
		filter = cardTypeFilter[0]
	}
	var queued []queuedBlessHold
	var lost []BattleResult
	// FUN_000668b4 snapshots all eligible holds and removes one-shots before
	// executing any skill. An earlier append cannot enable a later condition
	// in this same pass. Keep vector order, not the reusable append ID order.
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 || player.HP <= 0 {
			continue
		}
		kept := player.BlessHolds[:0]
		for _, hold := range player.BlessHolds {
			if (filter != 0 && hold.CardType != filter) || !engine.appendConditionSatisfied(timing, player, hold) {
				kept = append(kept, hold)
				continue
			}
			queued = append(queued, queuedBlessHold{owner: player.MemberType, hold: hold})
			if hold.Repeat {
				kept = append(kept, hold)
			} else {
				lost = append(lost, blessHoldLostResult(player.MemberType, hold))
			}
		}
		player.BlessHolds = kept
	}
	// 42a31 -> c6e10/3af23: priority, then random ties across different
	// holders only. Same-holder equal-priority entries preserve vector order.
	for boundary := 0; boundary < len(queued)-1; boundary++ {
		for i := len(queued) - 2; i >= boundary; i-- {
			left, right := queued[i], queued[i+1]
			swap := left.hold.Skill.PriorityPVE > right.hold.Skill.PriorityPVE
			if left.hold.Skill.PriorityPVE == right.hold.Skill.PriorityPVE && left.owner != right.owner {
				swap = engine.rng.next()&1 != 0
			}
			if swap {
				queued[i], queued[i+1] = right, left
			}
		}
	}
	results := make([]BattleResult, 0, len(queued)*4)
	for _, entry := range queued {
		owner := engine.player(entry.owner)
		hold := entry.hold
		if engine.endType != 0 || owner.HP <= 0 {
			continue
		}
		source := hold.SourceMember
		if source == 0 {
			source = entry.owner
		}
		action := battleAction{memberType: source, cardType: hold.CardType, cardLevel: hold.CardLevel,
			target: entry.owner, skill: hold.Skill, roles: hold.Roles}
		chain := engine.appendExecutionChainCount(entry.owner, hold.Skill)
		if hold.EnemySkill {
			if source < 5 || source >= 5+engine.enemyCount {
				return nil, fmt.Errorf("append skill %d source enemy %d is unavailable", hold.Skill.ID, source)
			}
			var matched bool
			action.skill, action.roles, action.branchIndex, matched = engine.selectEnemySkillBranchWithIndex(&engine.enemies[source-5], hold.Skill.ID, entry.owner)
			if !matched || len(action.roles) == 0 {
				owner.recordAppendExecution(hold)
				continue
			}
		} else {
			if !engine.playerMember(source) {
				return nil, fmt.Errorf("append skill %d source player %d is unavailable", hold.Skill.ID, source)
			}
			engine.player(source).ExecutionSource.setPhysical(hold.CardType, 0)
			if len(engine.catalog.Skills[hold.Skill.ID]) > 0 {
				action.skill, action.branchIndex = engine.selectCombatSkillBranchWithIndex(action, engine.turnActions, engine.appendChainCounts(entry.owner, hold.Skill))
				action.roles = engine.catalog.SkillRoles[action.skill.FunctionID]
				if action.skill.ID == 0 {
					owner.recordAppendExecution(hold)
					continue
				}
				if len(action.roles) == 0 {
					return nil, fmt.Errorf("append skill role %d is unavailable", action.skill.FunctionID)
				}
			}
		}
		targetCode, ok := combatSkillTargetCode(hold.Skill.Target)
		if !ok {
			return nil, fmt.Errorf("append skill %d target %q is unsupported", hold.Skill.ID, hold.Skill.Target)
		}
		// 7adb0 mode 7 preserves the holder as the header target, while role
		// SELECT uses the skill target set (79ee0), including USER/ENEMY_ALL.
		results = append(results, BattleResult{Command: CommandHoldSkill, Args: []int64{
			int64(source), int64(hold.CardType), int64(hold.AppendIndex), int64(hold.Skill.ID), int64(entry.owner),
			int64(hold.CardLevel), int64(targetCode), int64(chain), int64(action.skill.FunctionID), int64(action.branchIndex), 0,
		}})
		origin := "BLESS"
		if hold.CardType == 21 {
			origin = "CURSE"
		}
		results = append(results, engine.reactiveGaugeSkillStart(action, origin)...)
		action.skillBonus = &battleSkillBonus{}
		skillResults, err := engine.executeSkillRoleSet(source, entry.owner, action.skill, action.roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
			role.SourceSkillID = action.skill.ID
			var rows []BattleResult
			var err error
			if hold.EnemySkill {
				if role.Target == "SELECT" && (hold.Skill.Target == "USER_ALL" || hold.Skill.Target == "ENEMY_ALL") {
					role.Target = hold.Skill.Target
				}
				rows, err = engine.executeEnemyRole(&engine.enemies[source-5], entry.owner, role, action.roles)
			} else {
				rows, err = engine.executePlayerRole(action, role, chain)
			}
			if err != nil {
				return nil, fmt.Errorf("append skill %d role %d (%s): %w", hold.Skill.ID, role.RoleIndex, role.Function, err)
			}
			return rows, nil
		})
		if err != nil {
			return nil, err
		}
		results = append(results, skillResults...)
		engine.nativeSkillSerial++
		if engine.endType == 0 {
			// 7adb0 refreshes 314/315 before 5d008 checks the terminal flag,
			// including a lethal CALL that also changes its caster's power.
			display, err := engine.refreshBattleDisplayPowers()
			if err != nil {
				return nil, err
			}
			results = append(results, display...)
		}
		// 668b4/68948 records the queued base CALL only after 7adb0's
		// display refresh. The next completed skill can expose the new stamp.
		// Branches with no executable roles count too; waiting holds do not.
		owner.recordAppendExecution(hold)
		if engine.allEnemyBodiesDead() {
			engine.endType = engine.enemyWaveEndType()
		}
	}
	// JP64490 removes one-shots before execution. Its terminal path temporarily
	// enables the result writer for their 316 rows; only59 is suppressed.
	results = append(results, lost...)
	if engine.endType == 0 {
		// 668b4 does not refresh cards at the delimiter. Actual CALL
		// execution refreshes above; newly stored ordinary holds alone do not.
		results = append(results, BattleResult{Command: CommandHoldSkillEnd})
	}
	return results, nil
}

// FUN_000796a8(holder, callSkill, 1, 0) recomputes Chain at execution and
// includes the holder even if it selected no matching card this turn.
func (engine *BattleEngine) appendExecutionChainCount(owner int, skill CombatSkillDefinition) int {
	if !engine.playerMember(owner) {
		return 0
	}
	return battleActionChainCount(engine.partyActions(engine.turnActions, owner), skill.Attribute, owner)
}

func (engine *BattleEngine) appendChainCounts(owner int, skill CombatSkillDefinition) map[string]int {
	return map[string]int{strings.ToUpper(skill.Attribute): engine.appendExecutionChainCount(owner, skill)}
}

// Read-only numeric view for the shared display calculator, never an actor
// substitution in skill execution. The enemy remains the original source.
func enemyDisplaySource(enemy *battleEnemy) battlePlayer {
	return battlePlayer{
		MemberType: enemy.MemberType, HP: enemy.HP, MaxHP: enemy.MaxHP,
		Attack: enemy.Attack, Magic: enemy.Magic, Recovery: enemy.Recovery,
		Defense: enemy.Defense, MDefense: enemy.MDefense,
	}
}

func (engine *BattleEngine) appendConditionSatisfied(timing string, player *battlePlayer, hold battleBlessHold) bool {
	trigger := strings.ToUpper(strings.TrimSpace(hold.Skill.AppendTrigger))
	if trigger == "" {
		trigger = "USER_ATTACK_END"
	}
	if trigger != timing {
		return false
	}
	condition := strings.ToUpper(strings.TrimSpace(hold.Skill.AppendCondition))
	if condition == "" || condition == "NONE" {
		return true
	}
	action := battleAction{
		memberType: player.MemberType, cardType: hold.CardType, cardLevel: hold.CardLevel,
		target: hold.Target, skill: hold.Skill, roles: hold.Roles,
	}
	return engine.branchConditionSatisfied(condition, hold.Skill.AppendParameters, action, engine.turnActions, engine.currentChainCounts())
}

func (engine *BattleEngine) currentChainCounts() map[string]int {
	return battleActionChainCounts(engine.turnActions)
}

func (engine *BattleEngine) tickBlessHolds() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers)
	for _, player := range engine.playerStates() {
		results = append(results, tickPlayerBlessHolds(player)...)
	}
	return results
}

func tickPlayerBlessHolds(player *battlePlayer) []BattleResult {
	if player.GameOver {
		return nil
	}
	var results []BattleResult
	kept := player.BlessHolds[:0]
	for _, hold := range player.BlessHolds {
		hold.Remaining--
		if hold.Remaining > 0 {
			kept = append(kept, hold)
		} else {
			results = append(results, blessHoldLostResult(player.MemberType, hold))
		}
	}
	player.BlessHolds = kept
	return results
}

func ValidateAppendSkillDefinition(source string, skill CombatSkillDefinition) error {
	trigger := strings.ToUpper(strings.TrimSpace(skill.AppendTrigger))
	if trigger != "" && trigger != "USER_ATTACK_END" && trigger != "ENEMY_ACTION_END" {
		return fmt.Errorf("official %s skill %d append trigger %q is unsupported", source, skill.ID, trigger)
	}
	if !SupportsAppendCondition(skill.AppendCondition) {
		return fmt.Errorf("official %s skill %d append condition %q is unsupported", source, skill.ID, skill.AppendCondition)
	}
	return nil
}

func SupportsAppendCondition(condition string) bool {
	switch strings.ToUpper(strings.TrimSpace(condition)) {
	case "", "NONE", "TURN", "ENEMY_SIDE_DEBUFF", "DECK_COMBO_COUNT", "SELF_OTHER_PLAY_ATTR", "SELF_OTHER_PLAY_SKILL_KIND", "SELF_OTHER_PLAY_RARITY",
		"SELF_OTHER_PLAY_TAG", "SELF_MAIN_DECK_TAG", "USER_SIDE_DEBUFF", "ENEMY_TRANCE_GAUGE_STATE", "TARGET_CARD_ONLY_SELF_PLAY",
		"SELF_HP_PER", "SELF_OTHER_PLAY_NUM", "SELF_PLAY_MOST_LOW_COST", "SELF_PLAY_COST_TOTAL",
		"SELF_BUFF", "SELF_BLESS", "SELF_PLAY_COST_NUM", "SELF_NOT_PLAY_HAND_NUM", "BUFF_EXEC":
		return true
	default:
		return false
	}
}
