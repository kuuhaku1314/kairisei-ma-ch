package battlecore

import "strings"

// Original EnemyData's four gauge fields. State and action counters are
// separate: 56efb clears only the reaction counters, never the retained gauge.
type battleEnemyTrance struct {
	State, Value, Remaining                          int
	StateChanges                                     uint32
	NormalLimit, OverheatLimit, Duration, Resistance int
	SkipCountdown, Extended                          bool
	Damage, OtherDamage                              int64
	Debuff, Covering                                 bool
	Reactive                                         *EnemyGaugeDefinition
}

func newEnemyTrance(def CombatEnemyDefinition) battleEnemyTrance {
	t := battleEnemyTrance{NormalLimit: def.TranceLimit, OverheatLimit: def.OverheatLimit,
		Duration: def.OverheatTurns, Resistance: def.OverheatResist}
	if def.TranceLimit > 0 {
		t.State = 1
	}
	if def.ReactiveGauge != nil {
		t.Reactive = copyEnemyGauge(def.ReactiveGauge)
		if t.State != 0 && t.Reactive.InitialState != 0 {
			t.State = t.Reactive.InitialState
			if t.State == 3 {
				t.Remaining = t.Duration // Initial overheat counts the first full turn.
			} else {
				limit := int32(t.limit())
				maximum := limit
				if t.State == 1 {
					maximum *= 3
				}
				t.Value = int(max(int32(0), min(maximum, limit*int32(t.Reactive.InitialRate)/100)))
			}
		}
	}
	return t
}

func (t *battleEnemyTrance) limit() int {
	if t.State == 1 {
		return t.NormalLimit
	}
	if t.State == 2 {
		return t.OverheatLimit
	}
	return 0
}

func (t *battleEnemyTrance) conditionState() int {
	if t.State == 0 {
		return 1
	}
	return t.State
}

func tranceState(value string) int {
	switch strings.ToUpper(strings.TrimSpace(value)) {
	case "", "0", "NONE", "NULL":
		return 0
	case "1", "NORMAL":
		return 1
	case "2", "TRANCE":
		return 2
	case "3", "OVER_HEAT":
		return 3
	default:
		return -1
	}
}

// d2de2/d2d7a/d2e7e query the existing enemy gauge state (581b7).
// NONE is the disabled state, not a wildcard. Direct target queries require
// an enemy; the side-wide predicate searches living enemy members.
func (engine *BattleEngine) branchEnemyTranceState(condition string, actor, target int, value string) bool {
	wanted := tranceState(value)
	if wanted < 0 {
		return false
	}
	if condition == "ENEMY_TRANCE_GAUGE_STATE" {
		for i := 0; i < engine.enemyCount; i++ {
			if e := &engine.enemies[i]; e.HP > 0 && e.Trance.State == wanted {
				return true
			}
		}
		return false
	}
	if condition == "SELF_TRANCE_GAUGE_STATE" {
		target = actor
	}
	index := target - maxRoomMembers - 1
	return index >= 0 && index < engine.enemyCount && engine.enemies[index].Trance.State == wanted
}

// e1280/56d5b compare the retained gauge percentage. A disabled gauge is
// NORMAL at zero percent here; NONE accepts NORMAL/TRANCE but not OVER_HEAT.
// The exact-state branch above intentionally keeps its distinct contract.
func enemyTranceValueMatches(enemy *battleEnemy, state, lower, upper string) bool {
	t := &enemy.Trance
	wanted := tranceState(state)
	current := t.conditionState()
	if wanted < 0 || wanted == 0 && current == 3 || wanted != 0 && wanted != current {
		return false
	}
	percent := 0
	if limit := int32(t.limit()); limit > 0 {
		percent = int(int32(t.Value) * 100 / limit)
	}
	return combatValueInRange(percent, lower, upper)
}

// 49640/4a780/4a9b0 search living enemies. NULL means any enemy, independent
// of the caller. State changes are an OR mask of every entered state this
// turn, not a comparison between the initial and final state.
func (engine *BattleEngine) enemyAITranceCondition(trigger string, parameters []string) bool {
	wanted := tranceState(parameters[1])
	if wanted < 0 {
		return false
	}
	matches := func(enemy *battleEnemy) bool {
		if enemy.HP <= 0 {
			return false
		}
		switch trigger {
		case "TRANCE_GAUGE_STATE":
			return enemy.Trance.conditionState() == wanted
		case "TRANCE_GAUGE_VALUE":
			return enemyTranceValueMatches(enemy, parameters[1], parameters[2], parameters[3])
		case "TRANCE_GAUGE_STATE_CHANGE":
			return wanted == 0 && enemy.Trance.StateChanges != 0 || enemy.Trance.StateChanges&(1<<uint32(wanted)) != 0
		}
		return false
	}
	if !combatNullValue(parameters[0]) {
		enemy, exists := engine.enemyByTriggerName(parameters[0])
		return exists && matches(enemy)
	}
	for i := 0; i < engine.enemyCount; i++ {
		if matches(&engine.enemies[i]) {
			return true
		}
	}
	return false
}

// 58606 resets both gauges and the duration. A disabled enemy cannot be
// enabled by a card; changing to the current state preserves its value.
func changeEnemyTrance(enemy *battleEnemy, state int) []BattleResult {
	t := &enemy.Trance
	if t.State == 0 || state < 1 || state > 3 || t.State == state {
		return nil
	}
	t.StateChanges |= 1 << uint32(state) // 551a8; initialization does not set it.
	t.State, t.Value, t.Remaining = state, 0, 0
	if state == 2 && t.Reactive != nil {
		t.Value = t.OverheatLimit
	}
	if state == 3 {
		t.Remaining, t.SkipCountdown, t.Extended = t.Duration, true, false
	}
	return []BattleResult{{Command: 87, Args: []int64{int64(enemy.MemberType), int64(state), int64(t.limit()), int64(t.Remaining)}}}
}

// 5948e uses signed int32 arithmetic before division. In TRANCE, UP
// reduces the overheat gauge while DOWN increases it. Emit even a zero delta.
func controlEnemyTrance(enemy *battleEnemy, role CombatSkillRole, level int) []BattleResult {
	t := &enemy.Trance
	if role.Function == "TRANCE_GAUGE_STATE_CHANGE" {
		return changeEnemyTrance(enemy, tranceState(role.Parameters[0]))
	}
	filter := tranceState(role.Parameters[0])
	limit := int32(t.limit())
	if t.State != 1 && t.State != 2 || limit <= 0 || filter < 0 || filter != 0 && filter != t.State {
		return nil
	}
	if t.Reactive != nil && t.State == 2 && t.Value <= 0 {
		return nil // Original depleted TRANCE cannot be refilled by this producer.
	}
	percent := int32(combatParameterInt(role.Parameters[1])) + int32(level)*int32(combatParameterInt(role.Parameters[2]))
	if role.Function == "TRANCE_GAUGE_VALUE_DOWN" {
		percent = -percent
	}
	delta := limit * percent / 100
	if t.State == 2 && t.Reactive == nil {
		delta = -delta
	}
	maximum := limit
	if t.Reactive != nil && t.State == 1 {
		maximum *= 3
	}
	t.Value = int(max(int32(0), min(maximum, int32(t.Value)+delta)))
	return []BattleResult{{Command: 205, Args: []int64{int64(enemy.MemberType), int64(t.Value), int64(role.RoleIndex)}}}
}

func (engine *BattleEngine) executeEnemyTranceRole(actor *battleEnemy, selected int, role CombatSkillRole) []BattleResult {
	var rows []BattleResult
	for _, index := range engine.enemyRoleEnemyTargets(actor, selected, role, false) {
		rows = append(rows, engine.applyTranceRole(&engine.enemies[index], role, 1)...)
	}
	return rows
}

func (engine *BattleEngine) executePlayerTranceRole(action battleAction, role CombatSkillRole) []BattleResult {
	var rows []BattleResult
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		rows = append(rows, engine.applyTranceRole(&engine.enemies[index], role, action.cardLevel)...)
	}
	return rows
}

// 80810 rolls resistance before the producer's level-scaled success rate,
// even for an enemy outside OVER_HEAT. 59779 allows one extension per cycle.
func (engine *BattleEngine) applyTranceRole(enemy *battleEnemy, role CombatSkillRole, level int) []BattleResult {
	if role.Function != "TRANCE_GAUGE_OVER_HEAT_TURN_ADD" {
		return controlEnemyTrance(enemy, role, level)
	}
	t := &enemy.Trance
	chance := int(int32(combatParameterInt(role.Parameters[1])) + int32(level)*int32(combatParameterInt(role.Parameters[2])))
	args := []int64{int64(enemy.MemberType), int64(role.RoleIndex)}
	if engine.rollPercent10000(t.Resistance) || !engine.rollPercent10000(chance) || t.State != 3 || t.Extended {
		return []BattleResult{{Command: 204, Args: args}}
	}
	t.Extended = true
	t.Remaining = int(max(int32(0), int32(t.Remaining)+int32(combatParameterInt(role.Parameters[0]))))
	return []BattleResult{{Command: 203, Args: args},
		{Command: 89, Args: []int64{int64(enemy.MemberType), int64(t.Remaining)}}}
}

// 58382 dispatches SELF_* against the actor and ENEMY_* against all live
// enemies. A disabled gauge is not NORMAL.
func (engine *BattleEngine) enemyTranceCondition(enemy *battleEnemy, value string) bool {
	value = strings.ToUpper(strings.TrimSpace(value))
	switch value {
	case "", "NULL", "0":
		return true
	case "SELF_NORMAL", "1":
		return enemy.Trance.State == 1
	case "SELF_TRANCE", "2":
		return enemy.Trance.State == 2
	case "SELF_OVER_HEAT", "3":
		return enemy.Trance.State == 3
	}
	state := map[string]int{"ENEMY_NORMAL": 1, "4": 1, "ENEMY_TRANCE": 2, "5": 2, "ENEMY_OVER_HEAT": 3, "6": 3}[value]
	for i := 0; state != 0 && i < engine.enemyCount; i++ {
		if engine.enemies[i].HP > 0 && engine.enemies[i].Trance.State == state {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) clearTranceReactions() {
	for i := 0; i < engine.enemyCount; i++ {
		t := &engine.enemies[i].Trance
		t.Damage, t.OtherDamage, t.Debuff, t.Covering = 0, 0, false, false
	}
}

// 4cdac -> 58c8e runs for every live enemy, including one not targeted by
// the action. The greatest reaction wins; damage and debuffs are not summed.
func (engine *BattleEngine) finishTranceReactions(cost int) []BattleResult {
	var rows []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		if t.Reactive != nil {
			continue // Per-enemy reactions happen before the skill role set.
		}
		// 58c8e also emits an inert zero for a disabled gauge with a
		// nonzero overheat limit; 62f63 returns zero rates for state NONE.
		if enemy.HP > 0 && t.State == 0 && t.OverheatLimit > 0 {
			rows = append(rows, BattleResult{Command: 88, Args: []int64{int64(enemy.MemberType), 0}})
			continue
		}
		if enemy.HP <= 0 || t.State != 1 && t.State != 2 || t.limit() <= 0 {
			continue
		}
		limit := int32(t.limit())
		rate := func(kind int) int64 {
			return int64(int32(engine.catalog.TranceRates[kind][t.State-1][max(0, min(4, cost-1))]) * limit / 100)
		}
		amount := max(int64(0), min(t.Damage, rate(0)), min(t.OtherDamage, rate(1)))
		if t.Debuff {
			amount = max(amount, rate(2))
		}
		if t.Covering && t.State == 1 {
			amount = max(amount, rate(3))
		}
		t.Value = int(min(limit, int32(t.Value)+int32(amount)))
		rows = append(rows, BattleResult{Command: 88, Args: []int64{int64(enemy.MemberType), int64(t.Value)}})
	}
	engine.clearTranceReactions()
	return rows
}

// 588a0 runs at the user/enemy phase boundary, not after each card.
func (engine *BattleEngine) advanceTranceStates() []BattleResult {
	var rows []BattleResult
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		t := &enemy.Trance
		if t.Reactive != nil || enemy.HP <= 0 || t.State != 1 && t.State != 2 || t.limit() <= 0 || t.Value < t.limit() {
			continue
		}
		rows = append(rows, BattleResult{Command: 501, Args: []int64{int64(enemy.MemberType)}})
		rows = append(rows, changeEnemyTrance(enemy, t.State+1)...)
	}
	return rows
}

// 59098 skips the cleanup in which OVER_HEAT began, then counts full turns.
func tickEnemyTrance(enemy *battleEnemy) []BattleResult {
	t := &enemy.Trance
	if t.State != 3 || t.Duration <= 0 {
		return nil
	}
	if t.SkipCountdown {
		t.SkipCountdown = false
		return nil
	}
	t.Remaining = int(int32(t.Remaining) - 1)
	rows := []BattleResult{{Command: 89, Args: []int64{int64(enemy.MemberType), int64(t.Remaining)}}}
	if t.Remaining < 1 {
		rows = append(rows, BattleResult{Command: 501, Args: []int64{int64(enemy.MemberType)}})
		rows = append(rows, changeEnemyTrance(enemy, 1)...)
	}
	return rows
}

func resumeEnemyTrance(enemy *battleEnemy) []BattleResult {
	t := &enemy.Trance
	if t.State == 0 || enemy.HP <= 0 {
		return nil
	}
	return []BattleResult{{Command: 500, Args: []int64{int64(enemy.MemberType), int64(t.State), int64(t.Value), int64(t.limit()), int64(t.Remaining)}}}
}
