package battlecore

import "strings"

// battleParameterDeltas is the durable parameter projection owned by the
// native buff list. FUN_00071d48 aggregates retained effects and
// FUN_00071e60 rebuilds BATTLE_PARAM from the base member tuple plus those
// effects; it does not undo a clamped current value by adding the requested
// amount back later.
type battleParameterDeltas struct {
	maxHP    int
	attack   int
	magic    int
	recovery int
	defense  int
	mDefense int
}

// 71860 reads the base tuple plus PASSIVE(list1), not the full battle tuple.
// BURST_PASSIVE(list6) can survive a wave's end but must not leak into row5.
func playerBaseParameterResult(player *battlePlayer) BattleResult {
	view := *player
	ensurePlayerBaseParameters(&view)
	limit := initialParameterLimit(player.Alchemist)
	view.LimitAttack, view.LimitMagic, view.LimitRecovery = limit, limit, limit
	view.Effects = baseParameterEffects(player.Effects)
	refreshPlayerBattleParameters(&view)
	return BattleResult{Command: CommandBaseParam, Args: baseParameterArgsWithLimit(view.MemberType,
		view.MaxHP, view.Attack, view.Magic, view.Recovery, view.Defense, view.MDefense, limit)}
}

// Both actor families expose only PASSIVE contributions in their base query.
func enemyBaseParameterResult(enemy *battleEnemy, limit int) BattleResult {
	view := *enemy
	ensureEnemyBaseParameters(&view)
	view.Effects = baseParameterEffects(enemy.Effects)
	refreshEnemyBattleParameters(&view)
	return BattleResult{Command: CommandBaseParam, Args: baseParameterArgsWithLimit(view.MemberType,
		view.MaxHP, view.Attack, view.Magic, view.Recovery, view.Defense, view.MDefense, limit)}
}

func baseParameterEffects(effects []battleEffect) []battleEffect {
	var result []battleEffect
	for _, effect := range effects {
		if effect.ListType == 1 {
			result = append(result, effect)
		}
	}
	return result
}

func combatParameterSupported(parameter string) bool {
	switch strings.ToUpper(parameter) {
	case "MAX_HP", "ATK", "INT", "MND", "DEF", "MDEF":
		return true
	default:
		return false
	}
}

func retainedBattleParameterDeltas(effects []battleEffect) battleParameterDeltas {
	var deltas battleParameterDeltas
	for _, effect := range effects {
		if effect.Delta == 0 || effect.Function == "PARAM_LIMIT_BREAK_FIXED" {
			continue
		}
		switch strings.ToUpper(effect.Parameter) {
		case "MAX_HP":
			deltas.maxHP += effect.Delta
		case "ATK":
			deltas.attack += effect.Delta
		case "INT":
			deltas.magic += effect.Delta
		case "MND":
			deltas.recovery += effect.Delta
		case "DEF":
			deltas.defense += effect.Delta
		case "MDEF":
			deltas.mDefense += effect.Delta
		}
	}
	return deltas
}

func ensurePlayerBaseParameters(player *battlePlayer) {
	if player.BaseMaxHP != 0 {
		return
	}
	deltas := retainedBattleParameterDeltas(player.Effects)
	player.BaseMaxHP = maxInt(1, player.MaxHP-deltas.maxHP)
	player.BaseAttack = maxInt(0, player.Attack-deltas.attack)
	player.BaseMagic = maxInt(0, player.Magic-deltas.magic)
	player.BaseRecovery = maxInt(0, player.Recovery-deltas.recovery)
	player.BaseDefense = player.Defense - deltas.defense
	player.BaseMDefense = player.MDefense - deltas.mDefense
}

func ensureEnemyBaseParameters(enemy *battleEnemy) {
	if enemy.BaseMaxHP != 0 {
		return
	}
	deltas := retainedBattleParameterDeltas(enemy.Effects)
	enemy.BaseMaxHP = maxInt(1, enemy.MaxHP-deltas.maxHP)
	enemy.BaseAttack = maxInt(0, enemy.Attack-deltas.attack)
	enemy.BaseMagic = maxInt(0, enemy.Magic-deltas.magic)
	enemy.BaseRecovery = maxInt(0, enemy.Recovery-deltas.recovery)
	enemy.BaseDefense = enemy.Defense - deltas.defense
	enemy.BaseMDefense = enemy.MDefense - deltas.mDefense
}

func refreshPlayerBattleParameters(player *battlePlayer) {
	ensurePlayerBaseParameters(player)
	deltas := retainedBattleParameterDeltas(player.Effects)
	// 71860/71e60 clamp the projected player tuple, not its raw base.
	// Enemy HP has a separate minimum of one and no player ceiling.
	minimum := 500
	if player.MemberType >= storyHelpMember {
		minimum = 1
	}
	player.MaxHP = maxInt(minimum, minInt(999999, player.BaseMaxHP+deltas.maxHP))
	player.Attack = minInt(activeParameterLimit(player.LimitAttack), maxInt(0, player.BaseAttack+deltas.attack))
	player.Magic = minInt(activeParameterLimit(player.LimitMagic), maxInt(0, player.BaseMagic+deltas.magic))
	player.Recovery = minInt(activeParameterLimit(player.LimitRecovery), maxInt(0, player.BaseRecovery+deltas.recovery))
	// 74292/74344 retain negative defense. Only players have the symmetric
	// +/-999999 cap; attack consumers deliberately request signed values.
	player.Defense = maxInt(-999999, minInt(999999, player.BaseDefense+deltas.defense))
	player.MDefense = maxInt(-999999, minInt(999999, player.BaseMDefense+deltas.mDefense))
	// Projection/expiry only clamp HP. The application consumer separately
	// adds a positive MAX_HP delta once (88a20 -> 73e9f).
	player.HP = minInt(player.HP, player.MaxHP)
}

func refreshAppliedPlayerParameter(player *battlePlayer, parameter string, delta int) {
	hp := player.HP
	refreshPlayerBattleParameters(player)
	// 8f4e9 calls 73e9f/722ee only for a nonzero MAX_HP delta. Other
	// parameter buffs leave current HP intact, even above the visible cap.
	if !strings.EqualFold(parameter, "MAX_HP") || delta == 0 {
		player.HP = hp
	} else if delta > 0 {
		player.HP = nativeHPCommit(hp, player.MaxHP, delta, player.Effects)
	}
}

func refreshAppliedEnemyParameter(enemy *battleEnemy, parameter string, delta int) {
	hp := enemy.HP
	refreshEnemyBattleParameters(enemy)
	if !strings.EqualFold(parameter, "MAX_HP") || delta == 0 {
		enemy.HP = hp
	} else if delta > 0 {
		enemy.HP = nativeHPCommit(hp, enemy.MaxHP, delta, enemy.Effects)
	}
}

func refreshEnemyBattleParameters(enemy *battleEnemy) {
	ensureEnemyBaseParameters(enemy)
	deltas := retainedBattleParameterDeltas(enemy.Effects)
	enemy.MaxHP = maxInt(1, enemy.BaseMaxHP+deltas.maxHP)
	enemy.Attack = maxInt(0, enemy.BaseAttack+deltas.attack)
	enemy.Magic = maxInt(0, enemy.BaseMagic+deltas.magic)
	enemy.Recovery = maxInt(0, enemy.BaseRecovery+deltas.recovery)
	enemy.Defense = enemy.BaseDefense + deltas.defense
	enemy.MDefense = enemy.BaseMDefense + deltas.mDefense
	// The same native ceiling projection is shared by player and enemy members.
	enemy.HP = minInt(enemy.HP, enemy.MaxHP)
}

// The parameter consumer owns the actual effect list before refreshing stats.
// Complete-skill projection later defers statuses and suppresses startup 6 rows.
func playerParameterStatusResults(player *battlePlayer, role CombatSkillRole, code, listType int) []BattleResult {
	var results []BattleResult
	if listType == 1 {
		results = append(results, playerBaseParameterResult(player))
	}
	return append(results, battleParameterBuffResult(player.MemberType, role, code, listType),
		BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
			player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
}

// JP cut and attribute-defense boost passives retain fixed/rate terms and neutral
// PvE stages. Cancelling 100/100 changes the original overflow behavior.
func fixedRatePassiveValues(role CombatSkillRole, level int) (int, int) {
	p := func(i int) int32 { return int32(combatParameterInt(role.Parameters[i])) }
	fixed := p(1) + p(2)*int32(level)/1000
	rate := p(3) + p(4)*int32(level)
	fixed, rate = fixed*100/100, rate*100/100
	fixed, rate = fixed*100/100, rate*100/100
	return int(fixed), int(rate)
}

func parameterEffectMagnitude(value, fixed, rate int) int {
	// JP 948dc caps the wide producer scalar before the shared 949c2 consumer.
	// Fixed addition and percentage multiplication wrap independently in int32;
	// a negative fixed sum is clipped before the rate, then magnitude is at least 1.
	sum := int32(minInt(2147483647, value)) + int32(fixed)
	if sum < 0 {
		sum = 0
	}
	return maxInt(1, int(sum*int32(1000+rate)/1000))
}

// Receiver cuts and the caster's boost belong to one additive fixed/rate pair,
// not two successive percentage operations. List membership owns applicability;
// even a zero-duration NORMAL entry applies until its first expiry boundary.
func parameterDebuffValue(effects []battleEffect, function, parameter string, value, fixed, rate int) int {
	cut := "ATK_BREAK_CUT"
	if strings.HasPrefix(function, "GUARD_BREAK") {
		cut = "GUARD_BREAK_CUT"
	}
	f, q := int32(fixed), int32(rate)
	for _, effect := range effects {
		if effect.Function == cut && effect.ListType >= 0 && effect.ListType <= 6 && strings.EqualFold(effect.Parameter, parameter) {
			f -= int32(effect.Value)
			q -= int32(effect.Rate)
		}
	}
	return parameterEffectMagnitude(value, int(f), int(q))
}
