package battlecore

import "strings"

const dotDamageLimit = 1_000_000_000

func dotValueUpValues(role CombatSkillRole, level int, chainCount int) (int, int, int) {
	turnAdd := combatParameterInt(role.Parameters[0])
	chainRate := 100
	if chainCount > 1 && role.ChainRate != 0 {
		chainRate += role.ChainRate * (chainCount - 1)
	}
	primaryRate := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	secondaryRate := combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level/1000
	primary := primaryRate * chainRate / 100
	secondary := secondaryRate * chainRate / 100
	return turnAdd, primary, secondary
}

func combatDOTIndex(function string) int {
	switch strings.ToUpper(function) {
	case "POISON":
		return 0
	case "BURN":
		return 1
	case "FREEZE":
		return 2
	case "BLEED":
		return 3
	case "ELECTRIC":
		return 4
	default:
		return -1
	}
}

func isCombatDOTFunction(function string) bool {
	return combatDOTIndex(function) >= 0
}

func combatDOTStatusResistanceIndex(function string) int {
	// FUN_0008bb40 maps the DOT buff codes to the target's bad-status
	// resistance table before it performs the role hit-rate roll.
	switch strings.ToUpper(function) {
	case "POISON":
		return 4
	case "BURN":
		return 5
	case "FREEZE":
		return 6
	case "BLEED":
		return 7
	case "ELECTRIC":
		return 10
	default:
		return -1
	}
}

func combatDOTAttribute(function string) string {
	// skill_role_dot_cast maps DOT kinds 1..5 to DARK/FIRE/ICE/WIND/LIGHT
	// when FUN_0008bb40 resolves the target-specific stored damage.
	switch strings.ToUpper(function) {
	case "POISON":
		return "DARK"
	case "BURN":
		return "FIRE"
	case "FREEZE":
		return "ICE"
	case "BLEED":
		return "WIND"
	case "ELECTRIC":
		return "LIGHT"
	default:
		return ""
	}
}

func hasActiveCombatEffect(effects []battleEffect, function string) bool {
	for _, effect := range effects {
		if effect.Remaining > 0 && strings.EqualFold(effect.Function, function) {
			return true
		}
	}
	return false
}

func (engine *BattleEngine) enemyResistsDOTStatus(enemy *battleEnemy, function string) bool {
	index := combatDOTStatusResistanceIndex(function)
	rate := 0
	if index >= 0 && index < len(enemy.Level.StatusResistances) {
		rate = enemy.Level.StatusResistances[index]
	}
	// The native consumer advances xor128 even for a zero resistance column.
	return engine.rollPercent10000(rate)
}

func registeredDOTValue(value int, attributeRate int, reductionPercent int) int {
	return registeredChainedDOTValue(value, attributeRate, 0, reductionPercent)
}

func registeredChainedDOTValue(value int, attributeRate int, chainPercent int, reductionPercent int) int {
	resolved := int64(value) * int64(maxInt(0, attributeRate)) / 100
	resolved = resolved * int64(100+chainPercent) / 100
	resolved = resolved * int64(100-reductionPercent) / 100
	if resolved < 1 {
		return 1
	}
	if resolved > 2147483647 {
		return 2147483647
	}
	return int(resolved)
}

func (engine *BattleEngine) dotEffectForEnemy(effect battleEffect, enemy *battleEnemy) battleEffect {
	return engine.dotEffectForEnemyWithChain(effect, enemy, 0)
}

func (engine *BattleEngine) dotEffectForEnemyWithChain(effect battleEffect, enemy *battleEnemy, chainPercent int) battleEffect {
	attribute := combatDOTAttribute(effect.Function)
	attributeRate := engine.enemyAttributeRateWithEffects(enemy, attribute)
	reduction := 0
	if index := combatDOTIndex(effect.Function); index >= 0 {
		reduction = enemy.Level.DOTReductions[index]
	}
	effect.Value = registeredChainedDOTValue(effect.Value, attributeRate, chainPercent, reduction)
	return effect
}

func dotValueUpResult(memberType int, role CombatSkillRole, effect battleEffect, turnAdd int, primary int, secondary int) BattleResult {
	buffMask := 0
	debuffMask := 0
	if kind := skillRoleKindBuff(effect); kind > 0 {
		buffMask = 1 << (kind & 31)
	}
	if kind := skillRoleKindDebuff(effect); kind > 0 {
		debuffMask = 1 << (kind & 31)
	}
	return BattleResult{Command: 201, Args: []int64{
		int64(memberType), int64(role.RoleIndex), int64(buffMask), int64(debuffMask),
		boolInt64(turnAdd > 0), boolInt64(primary > 0), boolInt64(secondary > 0),
	}}
}

func applyDOTValueUp(effects []battleEffect, memberType int, role CombatSkillRole, level int, chainCount int) ([]battleEffect, []BattleResult) {
	wanted := strings.ToUpper(role.Parameters[5])
	results := make([]BattleResult, 0, 1)
	turnAdd, rate, fixed := dotValueUpValues(role, level, chainCount)
	for index := range effects {
		if strings.ToUpper(effects[index].Function) != wanted || effects[index].Remaining <= 0 || effects[index].Value <= 0 {
			continue
		}
		// JP 976a0 scales the stored damage before adding the fixed segment.
		effects[index].Remaining = maxInt(0, effects[index].Remaining+turnAdd)
		value := int64(effects[index].Value)*int64(100+rate)/100 + int64(fixed)
		effects[index].Value = int(max(int64(1), min(int64(dotDamageLimit), value)))
		results = append(results, dotValueUpResult(memberType, role, effects[index], turnAdd, rate, fixed))
	}
	if len(results) == 0 {
		results = append(results, BattleResult{Command: 202, Args: []int64{int64(memberType), int64(role.RoleIndex)}})
	}
	return effects, results
}

func (engine *BattleEngine) executePlayerDOTValueUp(action battleAction, role CombatSkillRole, chainCount int) []BattleResult {
	results := make([]BattleResult, 0, engine.enemyCount)
	for _, member := range engine.playerRoleTargets(action, role) {
		player := engine.player(member)
		var projected []BattleResult
		player.Effects, projected = applyDOTValueUp(player.Effects, member, role, action.cardLevel, chainCount)
		results = append(results, projected...)
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		var projected []BattleResult
		engine.enemies[index].Effects, projected = applyDOTValueUp(engine.enemies[index].Effects, engine.enemies[index].MemberType, role, action.cardLevel, chainCount)
		results = append(results, projected...)
	}
	return results
}

func playerDrawEffectValue(player *battlePlayer, code int) int {
	value := 0
	for _, effect := range player.Effects {
		if effectCode, exists := battleBuffCodes[effect.Function]; exists && effectCode == code {
			value = maxInt(value, effect.Value)
		}
	}
	return value
}

// attributeDefenseRoleValues mirrors ATTR_DEF_UP/DOWN producers
// FUN_0009b6de/FUN_0009b4b4. The first group is a per-mille multiplier and
// the second is a fixed post-defense delta. ATTR_DEF_DOWN does not consume
// Chain: original 10301551 reaches 9b4b4 with scale 100 beside four ICE players.
// Buff 19 stores negative values while debuff 113 stays positive.
func attributeDefenseRoleValues(role CombatSkillRole, level int, chainCount int) (int, int) {
	primary := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	fixed := combatParameterInt(role.Parameters[3]) + combatParameterInt(role.Parameters[4])*level/1000
	if role.Function != "ATTR_DEF_DOWN" && chainCount > 1 && role.ChainRate != 0 {
		scale := 100 + role.ChainRate*(chainCount-1)
		primary = primary * scale / 100
		fixed = fixed * scale / 100
	}
	if role.Function == "ATTR_DEF_UP" {
		primary = -primary
		fixed = -fixed
	}
	return primary, fixed
}

func configureAttributeDefenseEffect(effect *battleEffect, role CombatSkillRole, level int, chainCount int) {
	primary, fixed := attributeDefenseRoleValues(role, level, chainCount)
	effect.Value = fixed
	effect.Rate = primary
	effect.Parameters[0] = primary
	effect.Parameters[1] = fixed
}

// JP 3d4c5 accumulates the caster's fixed/rate boost for the role's attribute,
// not the card's attribute. Only fixed attribute defense changes; the primary
// per-mille defense coefficient stays intact. 391cb accepts either profile tag
// of the currently executing card, with two zeros meaning unrestricted.
func (engine *BattleEngine) boostAttributeDefenseEffect(action battleAction, effect *battleEffect) {
	card := engine.catalog.Cards[action.cardID]
	var fixed, rate int32
	for _, boost := range engine.player(action.memberType).Effects {
		if boost.Function != "ATTR_DEF_DOWN_BOOST" || boost.ListType < 0 || boost.ListType > 6 ||
			!supportAttributeMatches(boost.Attribute, effect.Attribute) {
			continue
		}
		if boost.ProfileTags != [2]int{} && !combatCardHasProfileTag(card, boost.ProfileTags[:]...) {
			continue
		}
		fixed += int32(boost.Value)
		rate += int32(boost.Rate)
	}
	// The common attribute-defense consumer clamps the pre-rate fixed sum
	// at zero, then uses a wide product; a negative rate may still reverse it.
	effect.Value = int(int64(maxInt(0, effect.Value+int(fixed))) * int64(1000+rate) / 1000)
	effect.Parameters[1] = effect.Value
}

func configureDOTEffect(effect *battleEffect, role CombatSkillRole, level int, chainCount int) {
	configureDOTEffectScaled(effect, role, level, chainCount, battleRoleScale{percent: 100})
}

func configureDOTEffectScaled(effect *battleEffect, role CombatSkillRole, level, chainCount int, scale battleRoleScale) {
	rate, fixed, coefficient, _ := dotRoleValuesScaled(role, level, chainCount, 0, scale)
	effect.Rate = rate
	effect.Parameters[0] = rate
	effect.Parameters[1] = fixed
	effect.Parameters[2] = coefficient / 10
	effect.Parameters[3] = 0
}

func persistentRoleValue(role CombatSkillRole, level int, actor *battlePlayer, chainCount int) int {
	switch role.Function {
	case "REGENERATE_FIXED":
		return fixedRegenerateRoleValue(role, level, chainCount, combatStatValue(actor, role.Parameters[5]))
	case "HEAL_CUT":
		return minInt(100, combatParameterInt(role.Parameters[1]))
	case "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC":
		_, _, _, value := dotRoleValues(role, level, chainCount, combatStatValue(actor, role.Parameters[7]))
		return value
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		_, fixed := attributeDefenseRoleValues(role, level, chainCount)
		return fixed
	case "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN":
		return combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		return attackBarrierRoleValue(role, level)
	case "COVERING":
		return coveringRoleValueScaled(role, level, chainCount, battleRoleScale{percent: 100})
	case "REFLECTION":
		return combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	case "CARD_TRAP_DAMAGE":
		return cardTrapDamageRoleValue(role, level, chainCount, combatStatValue(actor, role.Parameters[7]))
	case "CRITICAL_UP", "CRITICAL_DOWN", "WEAKNESS", "CRITICAL_DAMAGE_BOOST", "CARD_SEAL_REGIST":
		value := retainedRateRoleValue(role, level)
		if chainCount > 1 && role.ChainRate != 0 {
			value = value * (100 + role.ChainRate*(chainCount-1)) / 100
		}
		return value
	case "ENCHANT":
		return enchantRoleValue(role, level, chainCount)
	default:
		return combatParameterInt(role.Parameters[1])
	}
}

// JP enchant uses signed32 producer segments, with the neutral PvE stages
// preserved before its consumer combines retained entries.
func enchantRoleValue(role CombatSkillRole, level, chainCount int) int {
	return enchantRoleValueScaled(role, level, chainCount, battleRoleScale{percent: 100})
}

func enchantRoleValueScaled(role CombatSkillRole, level, chainCount int, scale battleRoleScale) int {
	p := func(i int) int32 { return int32(combatParameterInt(role.Parameters[i])) }
	first := p(1) * (p(2) + p(3)*int32(level)) / 1000
	first = scale.fixed(first)
	first = first * 100 / 100
	second := scale.fixed(p(4) * int32(level))
	second = second * 100 / 100
	value := first + second
	if chainCount > 1 && role.ChainRate != 0 {
		value = value * int32(100+role.ChainRate*(chainCount-1)) / 100
	}
	return int(value)
}

func combatTargetIsPlayer(target string) bool {
	if fixedUserSideArthurType(target) != 0 {
		return true
	}
	switch target {
	case "SELF", "SELECT", "USER_ONE", "USER_ALL", "FRIEND_ONE", "FRIEND_ALL", "MERCENARY", "MILLIONAIRE", "THIEF", "SINGER":
		return true
	default:
		return false
	}
}

func battleParameterBuffResult(memberType int, role CombatSkillRole, code, listType int) BattleResult {
	return battleBuffResultWithListType(
		memberType, role.RoleIndex, listType, code, combatParameterFlag(role.Parameters[1]), 1, 0, 0, 0, 0,
	)
}

func combatParameterFlag(parameter string) int {
	switch strings.ToUpper(parameter) {
	case "HP":
		return 1
	case "MAX_HP":
		return 2
	case "ATK":
		return 4
	case "INT":
		return 8
	case "MND":
		return 16
	case "DEF":
		return 32
	case "MDEF":
		return 64
	default:
		return 0
	}
}

func retainedEffectParameterFlags(effect battleEffect) int {
	// 45178 inspects the retained six parameter deltas. Only limit codes
	// 32/33 additionally publish their selector when their delta is zero.
	code := battleBuffCodes[effect.Function]
	if effect.Delta == 0 && code != 32 && code != 33 {
		return 0
	}
	return combatParameterFlag(effect.Parameter)
}

func battleBuffResultWithParameters(memberType int, roleIndex int, code int, parameterFlags int, attributeFlags int, parameter0 int, parameter1 int, parameter2 int, parameter3 int) BattleResult {
	return battleBuffResultWithListType(memberType, roleIndex, 0, code, parameterFlags, attributeFlags, parameter0, parameter1, parameter2, parameter3)
}

func battleBuffResultWithListType(memberType int, roleIndex int, listType int, code int, parameterFlags int, attributeFlags int, parameter0 int, parameter1 int, parameter2 int, parameter3 int) BattleResult {
	command := CommandBuff
	if listType == 1 || listType == 6 {
		command = CommandPassiveBuff
	}
	return BattleResult{Command: command, Args: []int64{
		int64(memberType), int64(roleIndex), int64(listType), int64(code), int64(battleBuffKind(code)),
		int64(parameterFlags), int64(attributeFlags), int64(parameter0), int64(parameter1), int64(parameter2), int64(parameter3),
	}}
}

func battleHealResult(memberType int, roleIndex int, amount int, hp int) BattleResult {
	return BattleResult{Command: 61, Args: []int64{int64(memberType), int64(roleIndex), int64(amount), int64(hp)}}
}

func battleHPCutResult(memberType int, roleIndex int, amount int, hp int) BattleResult {
	return BattleResult{Command: CommandHPCut, Args: []int64{int64(memberType), int64(roleIndex), int64(amount), int64(hp)}}
}

func battleDamageResult(memberType int, roleIndex int, amount int, hp int, difference int, attribute string, attributeRate int, critical int, damageType int, source int) BattleResult {
	// CN ResultCmd60 is [target,role-index,signed damage,HP,
	// attribute-difference,attribute,attribute-rate,critical,damage-type,source].
	// FUN_000927b0/FUN_00093461 define attribute-difference as the signed
	// delta introduced by the attribute multiplier before critical, defense and
	// damage effects. Damage type 0/1/2 is NORMAL/ENCHANT/REFLECTION.
	// Attack/enchant use pre-commit HP; reflection and DOT use post-commit HP.
	return BattleResult{Command: 60, Args: []int64{
		int64(memberType), int64(roleIndex), int64(amount), int64(hp), int64(difference),
		int64(combatAttributeCode(attribute)), int64(attributeRate), int64(critical), int64(damageType), int64(source),
	}}
}

func battleBurstGaugeResult(memberType int, roleIndex int, amount int, increase bool) BattleResult {
	action := 1
	if !increase {
		action = 2
	}
	// JP207 reports the signed per-action delta and UP/DOWN action identity;
	// the following506 commits the actual clamped gauge. Quick actions use flag0;
	// a BURST parameter producer sets flag1 on its separate self-targeted cost.
	return BattleResult{Command: 207, Args: []int64{int64(memberType), int64(roleIndex), int64(amount), int64(action), 0}}
}

func burstGaugeQuickValue(role CombatSkillRole, level int) int {
	// The JP producer ignores Chain and retains both neutral PvE percentage
	// stages with signed32 multiplication, even when both percentages are100.
	value := int32(combatParameterInt(role.Parameters[0])) + int32(combatParameterInt(role.Parameters[1]))*int32(level)
	value = value * 100 / 100
	value = value * 100 / 100
	return int(value)
}

func (engine *BattleEngine) addPlayerBurstGauge(memberType int, roleIndex int, amount int, increase bool) []BattleResult {
	player := engine.player(memberType)
	if !engine.addBurstGauge(player, amount) {
		return nil
	}
	return []BattleResult{
		battleBurstGaugeResult(memberType, roleIndex, amount, increase),
		{Command: CommandBurstGaugeState, Args: []int64{int64(memberType), int64(player.Burst)}},
	}
}

var skillRoleKindBuffByFunction = map[string]int{
	"DAMAGE_UP": 6, "DAMAGE_CUT": 7, "HEAL": 8, "REGENERATE_FIXED": 8, "REGENERATE_BY_SELF_PARAM": 8,
	"STEAL": 9, "ATTR": 10, "REWRITE": 10,
	"CARD_SEAL_REGIST": 11, "DEAL_BONUS": 12, "ATK_OP_ATTR_RATE_DOWN_INVALID": 13,
	"ATTR_HIDE": 14, "ATTACK_BARRIER": 15, "ATTACK_BARRIER_APPOINT_ATTR": 15,
	"COVERING": 16, "FIELD_ATTR_UP": 17, "FIELD_SKILL_UP": 18, "FIELD_HEAL_UP": 19,
	"FIELD_JAMMING_UP": 20, "FIELD_SUPPORT_DEFENCE_UP": 21, "PARAM_UP_BUFF_CONVERT": 22,
	"CRITICAL_UP": 23, "DARKNESS_REGIST": 24, "CRITICAL_UP_BY_SUPPORT": 25,
	"ROLE_VALUE_UP_BY_ROLE_OP": 26, "DECK_COMBO_RATE_UP": 27, "DEBUFF_REGIST": 28,
	"NEED_COST_DOWN": 29, "ROLE_VALUE_UP_BY_PVP_RATE": 30, "ENCHANT": 31,
	"ATK_UP_BY_MAX_HP": 32, "ATTR_DEF_UP": 33, "REFLECTION": 34, "DEBUFF_REGIST_LIMIT": 35, "ENDURE": 36,
	"DAMAGE_BOOST": 37, "ATK_UP_BOOST": 38, "DEF_UP_BOOST": 39,
	"ATK_BREAK_BOOST": 40, "GUARD_BREAK_BOOST": 41, "HEAL_BOOST": 42,
	"CRITICAL_BOOST": 43, "DAMAGE_CUT2": 44, "DAMAGE_BOOST_ORDER_TARGET_DEBUFF": 45,
	"DAMAGE_CUT_ORDER_TARGET_DEBUFF": 46, "BEGINNING_DRAW": 47, "GUTS": 51,
	"ATTR_CUT_ATTACK": 52, "ATTR_DRAIN_ATTACK": 53, "ATTR_CUT_ENCHANT": 54,
	"ATTR_DRAIN_ENCHANT": 55, "ATTR_CUT_DOT": 56, "ATTR_DRAIN_DOT": 57,
	"CRITICAL_DAMAGE_BOOST": 58, "NEED_COST_DOWN_BURST": 59, "ATTACK_MULTISTAGE": 60,
	"ADD_ATK_OP_PIERCING": 61, "DAMAGE_BOOST_ORDER_TRIBAL": 62,
	"ATK_UP_BOOST_ORDER_TRIBAL": 63, "DEF_UP_BOOST_ORDER_TRIBAL": 64,
	"ATK_BREAK_BOOST_ORDER_TRIBAL": 65, "HEAL_BOOST_ORDER_TRIBAL": 66,
	"CRITICAL_BOOST_ORDER_TRIBAL": 67, "BURST_GAUGE_REGENE_UP": 68,
	"ATK_BREAK_CUT": 78, "GUARD_BREAK_CUT": 79, "ATTR_DEF_DOWN_BOOST": 81,
	"PHYSICS_DAMAGE_BOOST_COUNT": 76, "MAGIC_DAMAGE_BOOST_COUNT": 77,
}

var skillRoleKindDebuffByFunction = map[string]int{
	"UNDERMINE": 6, "POISON": 7, "BURN": 8, "FREEZE": 9, "BLEED": 10,
	"CHARM": 11, "STAN": 12, "SILENCE": 13, "DAMAGE_DOWN": 14, "CARD_SEAL": 15,
	"ATTR_SEE": 16, "FIELD_ATTR_DOWN": 17, "FIELD_HEAL_DOWN": 18, "WEAKNESS": 19,
	"HEAL_REVERSE": 20, "DEAL_PENALTY": 21, "DEAL_PENALTY_TURN_APPOINT": 21,
	"ELECTRIC": 22, "DARKNESS": 23, "DARKNESS_APPOINT": 23, "DARKNESS_RANDOM": 23,
	"CARD_TRAP_DAMAGE": 24, "CARD_TRAP_DOT": 25, "COST_BLOCK": 26,
	"ATTR_DEF_DOWN": 27, "CRITICAL_DOWN": 28, "HEAL_CUT": 29,
}

func battleBuffReleaseResult(memberType int, role CombatSkillRole, effects []battleEffect) BattleResult {
	buffs := make([]int64, 3)
	debuffs := make([]int64, 3)
	buffCount := 0
	debuffCount := 0
	wantedKind := releaseWantedKind(role.Function)
	switch releaseMode(role.Function) {
	case "ALL":
		// FUN_000851e0/FUN_00087210 project the native all-buff/all-debuff
		// sentinels rather than enumerating the concrete effects removed.
		if wantedKind == 1 {
			buffs[0] = skillRoleBuffKindCount
		} else {
			debuffs[0] = skillRoleDebuffKindCount
		}
	case "ONE":
		// Both requested kinds are tested before either removal call. A
		// duplicated p2/p3 therefore remains duplicated in ResultCmd66.
		for _, kind := range releaseRoleKinds(role, wantedKind) {
			if !releasedEffectsContainKind(effects, kind, wantedKind) {
				continue
			}
			if wantedKind == 1 && buffCount < len(buffs) {
				buffs[buffCount] = int64(kind)
				buffCount++
			} else if wantedKind == 2 && debuffCount < len(debuffs) {
				debuffs[debuffCount] = int64(kind)
				debuffCount++
			}
		}
	default:
		for _, effect := range effects {
			if wantedKind == 1 && buffCount < len(buffs) {
				buffs[buffCount] = int64(maxInt(0, branchStatusKind(effect, true)))
				buffCount++
			} else if wantedKind == 2 && debuffCount < len(debuffs) {
				debuffs[debuffCount] = int64(maxInt(0, branchStatusKind(effect, false)))
				debuffCount++
			}
		}
	}
	return BattleResult{Command: CommandBuffRelease, Args: []int64{
		int64(memberType), int64(role.RoleIndex),
		buffs[0], buffs[1], buffs[2], debuffs[0], debuffs[1], debuffs[2],
	}}
}

func releasedEffectsContainKind(effects []battleEffect, kind int, wantedKind int) bool {
	for _, effect := range effects {
		if branchStatusKind(effect, wantedKind == 1) == kind {
			return true
		}
	}
	return false
}

func battleDebuffFailedResult(memberType int, roleIndex int, code int) BattleResult {
	// FUN_0007adb0 emits member, role index, BATTLE_BUFF and the native
	// BATTLE_BUFF_KIND classification.
	return BattleResult{Command: CommandDebuffFailed, Args: []int64{
		int64(memberType), int64(roleIndex), int64(code), int64(battleBuffKind(code)),
	}}
}

func battleBuffReleaseFailedResult(memberType int, roleIndex int) BattleResult {
	return BattleResult{Command: CommandBuffReleaseFailed, Args: []int64{int64(memberType), int64(roleIndex)}}
}

func skillRoleKindBuff(effect battleEffect) int {
	parameter := strings.ToUpper(effect.Parameter)
	function := strings.ToUpper(effect.Function)
	if kind, exists := skillRoleKindBuffByFunction[function]; exists {
		return kind
	}
	if strings.HasPrefix(function, "ATK_UP") {
		if parameter == "MAX_HP" {
			return 32
		}
		return mapCombatParameterKind(parameter, 1, 2, 3)
	}
	if strings.HasPrefix(function, "DEF_UP") {
		if parameter == "MDEF" {
			return 5
		}
		return 4
	}
	if strings.HasPrefix(function, "PARAM_LIMIT_BREAK") {
		return mapCombatParameterKind(parameter, 48, 49, 50)
	}
	return 0
}

func skillRoleKindDebuff(effect battleEffect) int {
	parameter := strings.ToUpper(effect.Parameter)
	function := strings.ToUpper(effect.Function)
	if kind, exists := skillRoleKindDebuffByFunction[function]; exists {
		return kind
	}
	if strings.HasPrefix(function, "ATK_BREAK") {
		return mapCombatParameterKind(parameter, 1, 2, 3)
	}
	if strings.HasPrefix(function, "GUARD_BREAK") {
		if parameter == "MDEF" {
			return 5
		}
		return 4
	}
	return 0
}

func mapCombatParameterKind(parameter string, attack int, magic int, recovery int) int {
	switch parameter {
	case "INT":
		return magic
	case "MND":
		return recovery
	default:
		return attack
	}
}

func battleBuffKind(code int) int {
	// 44ac0/44020: HEAL_REVERSE and DEAL_PENALTY are BAD_STATUS,
	// while positive draw and ENDURE are SPECIAL, not ordinary buffs.
	switch {
	case code == 413 || code == 2147483647:
		return 0
	case code == 108 || code == 408 || code >= 300 && code <= 312:
		return 5 // BATTLE_BUFF_KIND.BAD_STATUS
	case code >= 100 && code <= 115 || code == 123 || code == 124:
		return 2 // BATTLE_BUFF_KIND.DEBUFF
	case code >= 200 && code <= 218:
		return 4 // BATTLE_BUFF_KIND.GOOD_STATUS
	case code >= 400 && code <= 412:
		return 3 // BATTLE_BUFF_KIND.SPECIAL
	default:
		return 1 // BATTLE_BUFF_KIND.BUFF (native default)
	}
}

func (engine *BattleEngine) executeRelease(action battleAction, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 4)
	failed := make([]BattleResult, 0, 4)
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := engine.player(memberType)
		_, childRows := engine.releasePlayerEffectsWithRows(player, role, action.cardLevel)
		if len(childRows) > 0 {
			results = append(results, childRows...)
		} else {
			failed = append(failed, battleBuffReleaseFailedResult(memberType, role.RoleIndex))
		}
	}
	if !combatTargetIsPlayer(role.Target) {
		for _, index := range engine.playerRoleEnemyTargets(action, role) {
			enemy := &engine.enemies[index]
			_, childRows := engine.releaseEnemyEffectsWithRows(enemy, role, action.cardLevel)
			if len(childRows) > 0 {
				results = append(results, childRows...)
			} else {
				failed = append(failed, battleBuffReleaseFailedResult(enemy.MemberType, role.RoleIndex))
			}
		}
	}
	// Native buffers failures per target, but FUN_0007adb0 projects them only
	// when this role index produced no successful release on any target.
	if len(results) == 0 {
		return failed, nil
	}
	return results, nil
}

func (engine *BattleEngine) releasePlayerEffects(player *battlePlayer, role CombatSkillRole, level int) []battleEffect {
	removed, _ := engine.releasePlayerEffectsWithRows(player, role, level)
	return removed
}

func (engine *BattleEngine) releasePlayerEffectsWithRows(player *battlePlayer, role CombatSkillRole, level int) ([]battleEffect, []BattleResult) {
	ensurePlayerBaseParameters(player)
	selected := engine.selectReleasedEffectIndexes(player.Effects, role, level)
	var rows []BattleResult
	removed := applyReleaseGroups(player.Effects, selected, func(removed, kept []battleEffect) {
		engine.recordReleasedSkillStatistics(player.MemberType, role.Function, removed)
		for _, effect := range removed {
			revertPlayerEffect(player, effect)
		}
		player.Effects = kept
		refreshPlayerBattleParameters(player)
		refreshPlayerAttribute(player)
		rows = appendNaturalExpiryRows(rows, player.MemberType, removed, kept, player.Attribute)
		rows = append(rows, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(player.MemberType,
			player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery, player.Defense, player.MDefense,
			player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
	})
	return removed, selected.prependResult(player.MemberType, role, removed, rows)
}

func (engine *BattleEngine) releaseEnemyEffectsWithRows(enemy *battleEnemy, role CombatSkillRole, level int) ([]battleEffect, []BattleResult) {
	ensureEnemyBaseParameters(enemy)
	selected := engine.selectReleasedEffectIndexes(enemy.Effects, role, level)
	var rows []BattleResult
	removed := applyReleaseGroups(enemy.Effects, selected, func(removed, kept []battleEffect) {
		engine.recordReleasedSkillStatistics(enemy.MemberType, role.Function, removed)
		for _, effect := range removed {
			revertEnemyEffect(enemy, effect)
		}
		enemy.Effects = kept
		enemy.recordReleasedStatusCooldown(removed)
		refreshEnemyBattleParameters(enemy)
		refreshEnemyAttribute(enemy)
		rows = appendNaturalExpiryRows(rows, enemy.MemberType, removed, kept, enemy.Attribute)
		rows = append(rows, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(enemy.MemberType,
			enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery, enemy.Defense, enemy.MDefense,
			enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)})
	})
	return removed, selected.prependResult(enemy.MemberType, role, removed, rows)
}

// 73302/73420 invoke the release child after each category/typed selector.
// Preserve those intermediate states and parameter snapshots, not just the
// final flattened deletion. Indexes always refer to the original NORMAL list.
func applyReleaseGroups(effects []battleEffect, selection releasedEffectSelection, apply func([]battleEffect, []battleEffect)) []battleEffect {
	var allRemoved []battleEffect
	removedIndexes := make(map[int]bool)
	for _, group := range selection.groups {
		removed := make([]battleEffect, 0, len(group))
		for _, index := range group {
			removed = append(removed, effects[index])
			removedIndexes[index] = true
		}
		kept := make([]battleEffect, 0, len(effects)-len(removedIndexes))
		for index, effect := range effects {
			if !removedIndexes[index] {
				kept = append(kept, effect)
			}
		}
		apply(removed, kept)
		allRemoved = append(allRemoved, removed...)
	}
	return allRemoved
}

type releasedEffectSelection struct {
	indexes      map[int]struct{}
	order        []int
	groups       [][]int
	matchedKinds []int
}

func (selection releasedEffectSelection) prependResult(member int, role CombatSkillRole, removed []battleEffect, rows []BattleResult) []BattleResult {
	if len(removed) == 0 && len(selection.matchedKinds) == 0 {
		return nil
	}
	result := battleBuffReleaseResult(member, role, removed)
	if releaseMode(role.Function) == "ONE" {
		offset := 2
		if releaseWantedKind(role.Function) == 2 {
			offset = 5
		}
		for i := 0; i < 3; i++ {
			result.Args[offset+i] = 0
		}
		for i, kind := range selection.matchedKinds {
			result.Args[offset+i] = int64(kind)
		}
	}
	return append([]BattleResult{result}, rows...)
}

func releaseWantedKind(function string) int {
	if strings.HasPrefix(strings.ToUpper(function), "DEBUFF") {
		return 2
	}
	return 1
}

func releaseMode(function string) string {
	function = strings.ToUpper(function)
	switch {
	case strings.Contains(function, "_ONE"):
		return "ONE"
	case strings.HasSuffix(function, "_RANDOM"):
		return "RANDOM"
	case strings.HasSuffix(function, "_OLD"):
		return "OLD"
	default:
		return "ALL"
	}
}

func (engine *BattleEngine) selectReleasedEffectIndexes(effects []battleEffect, role CombatSkillRole, level int) releasedEffectSelection {
	wantedKind := releaseWantedKind(role.Function)
	mode := releaseMode(role.Function)
	selection := releasedEffectSelection{indexes: make(map[int]struct{})}
	chance := releaseRoleChance(role, level)
	roll := func() bool {
		// Native always advances xor128, including rates at or above 100% and
		// rates at or below zero.
		return int(engine.rng.next()%10000) < chance*100
	}
	if mode == "ALL" {
		if !roll() {
			return selection
		}
		for _, category := range []int{wantedKind, wantedKind + 3} {
			start := len(selection.order)
			for index, effect := range effects {
				if releaseCategoryEligible(effect, wantedKind) && battleBuffKind(battleBuffCodes[effect.Function]) == category {
					selection.indexes[index] = struct{}{}
					selection.order = append(selection.order, index)
				}
			}
			selection.appendGroup(start)
		}
		return selection
	}
	if mode == "ONE" {
		if !roll() {
			return selection
		}
		// 44ba0/44ed4 preflight can match NULL (a known zero-delta
		// parameter state), although 44140 deliberately never removes kind0.
		// Such a hit still emits 66, not 67. Preserve both preflight slots.
		for _, name := range role.Parameters[2:4] {
			kind := combatTriggerDebuffKind(name)
			if wantedKind == 1 {
				kind = combatTriggerBuffKind(name)
			}
			for _, effect := range effects {
				if effect.ListType == 0 && branchStatusKind(effect, wantedKind == 1) == kind {
					selection.matchedKinds = append(selection.matchedKinds, kind)
					break
				}
			}
		}
		limit := 128
		if strings.Contains(strings.ToUpper(role.Function), "_ONE_NUM") {
			limit = maxInt(1, combatParameterInt(role.Parameters[4]))
		}
		for _, kind := range releaseRoleKinds(role, wantedKind) {
			start := len(selection.order)
			removedForKind := 0
			for index, effect := range effects {
				if removedForKind >= limit {
					break
				}
				if _, alreadySelected := selection.indexes[index]; alreadySelected || effect.ListType != 0 || branchStatusKind(effect, wantedKind == 1) != kind {
					continue
				}
				selection.indexes[index] = struct{}{}
				selection.order = append(selection.order, index)
				removedForKind++
			}
			selection.appendGroup(start)
		}
		return selection
	}
	candidates := make([]int, 0, len(effects))
	for index, effect := range effects {
		if !releaseCategoryEligible(effect, wantedKind) {
			continue
		}
		candidates = append(candidates, index)
	}
	if len(candidates) == 0 {
		return selection
	}
	limit := minInt(len(candidates), maxInt(0, combatParameterInt(role.Parameters[2])))
	if mode == "RANDOM" {
		for attempt := 0; attempt < limit && len(candidates) > 0; attempt++ {
			if !roll() {
				continue
			}
			choice := int(engine.rng.next() % uint32(len(candidates)))
			index := candidates[choice]
			selection.indexes[index] = struct{}{}
			selection.order = append(selection.order, index)
			candidates = append(candidates[:choice], candidates[choice+1:]...)
		}
		selection.appendGroup(0)
		return selection
	}
	for _, index := range candidates[:limit] {
		if roll() {
			selection.indexes[index] = struct{}{}
			selection.order = append(selection.order, index)
		}
	}
	selection.appendGroup(0)
	return selection
}

func (selection *releasedEffectSelection) appendGroup(start int) {
	if start < len(selection.order) {
		selection.groups = append(selection.groups, selection.order[start:])
	}
}

// 851e0/87210 always start from NORMAL. ALL and unfiltered OLD/RANDOM
// select native coarse categories; ONE selects a typed kind and may therefore
// remove SPECIAL states such as DEAL_BONUS which ALL deliberately preserves.
func releaseCategoryEligible(effect battleEffect, wantedKind int) bool {
	code, ok := battleBuffCodes[effect.Function]
	if !ok || effect.ListType != 0 {
		return false
	}
	kind := battleBuffKind(code)
	return wantedKind == 1 && (kind == 1 || kind == 4) || wantedKind == 2 && (kind == 2 || kind == 5)
}

func releaseRoleKinds(role CombatSkillRole, wantedKind int) []int {
	kinds := make([]int, 0, 2)
	for _, name := range role.Parameters[2:4] {
		if name == "" || strings.EqualFold(name, "NULL") {
			continue
		}
		kind := combatTriggerDebuffKind(name)
		if wantedKind == 1 {
			kind = combatTriggerBuffKind(name)
		}
		if kind > 0 {
			kinds = append(kinds, kind)
		}
	}
	return kinds
}

func (engine *BattleEngine) executePlayerHPCut(action battleAction, role CombatSkillRole) ([]BattleResult, error) {
	results := make([]BattleResult, 0, 4)
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := engine.player(memberType)
		delta, _ := nativeHPCut(player.HP, combatParameterInt(role.Parameters[0]))
		player.HP = nativeHPCommit(player.HP, player.MaxHP, delta, player.Effects)
		recordPlayerDamage(player, -delta)
		results = append(results, battleHPCutResult(player.MemberType, role.RoleIndex, delta, player.HP))
	}
	if !combatTargetIsPlayer(role.Target) {
		for _, index := range engine.playerRoleEnemyTargets(action, role) {
			target := &engine.enemies[index]
			delta, _ := nativeHPCut(target.HP, combatParameterInt(role.Parameters[0]))
			target.HP = nativeHPCommit(target.HP, target.MaxHP, delta, target.Effects)
			recordEnemyDamageTotals(target, -delta)
			// 818d8 records special damage on the concrete target. Unlike
			// ordinary attacks, HP_CUT does not also commit to its parent.
			engine.recordSpecialEnemyDamage(target.MemberType, action.memberType, -delta)
			results = append(results, battleHPCutResult(target.MemberType, role.RoleIndex, delta, target.HP))
		}
	}
	return results, nil
}

// nativeHPCut mirrors libbattle5 FUN_000818d8. It truncates the percentage
// after adding the native double 0.9, caps it to current HP-1, commits a signed
// negative delta, and sends that same delta and committed HP in ResultCmd64
// without an additional ResultCmd3.
func nativeHPCut(currentHP int, rate int) (delta int, hp int) {
	if currentHP <= 0 {
		return 0, 0
	}
	cut := int(float64(int64(currentHP)*int64(maxInt(0, rate)))/100 + 0.9)
	cut = minInt(cut, currentHP-1)
	return -cut, currentHP - cut
}

// 45c04 selects the maximum ENDURE percentage from lists 0..6. 73e9f
// applies that MaxHP floor to the signed HP commit, without changing damage rows.
func nativeHPCommit(currentHP int, maxHP int, delta int, effects []battleEffect) int {
	if maxHP <= 0 {
		return 0
	}
	hp := int64(currentHP) + int64(delta)
	rate := 0
	for _, effect := range effects {
		if effect.Function == "ENDURE" && effect.Remaining > 0 && effect.ListType >= 0 && effect.ListType <= 6 {
			rate = maxInt(rate, effect.Value)
		}
	}
	if rate > 0 && int32(hp*100/int64(maxHP)) < int32(rate) {
		hp = int64(int32(int64(maxHP) * int64(rate) / 100))
	}
	return minInt(maxHP, maxInt(0, int(hp)))
}

// nativeHealCommit mirrors the common FUN_000452d0/FUN_00073e9f heal commit.
// Positive overheal is clamped only in the authoritative HP value; ResultCmd61
// keeps the requested amount. HEAL_REVERSE instead converts the accumulated
// rate (capped at 100) into a signed, nonlethal negative HP delta.
func nativeHealCommit(currentHP int, maxHP int, requested int, effects []battleEffect, minimumOne bool) (delta int, hp int) {
	if minimumOne {
		requested = maxInt(1, requested)
	}
	if requested <= 0 {
		return 0, minInt(maxInt(0, currentHP), maxInt(0, maxHP))
	}
	delta = nativeHealDelta(currentHP, requested, effects)
	return delta, nativeHPCommit(currentHP, maxHP, delta, effects)
}

// The original absorption consumer uses the same reversal calculation while
// deferring HP publication until the complete attack role has finished.
func nativeHealDelta(currentHP int, requested int, effects []battleEffect) int {
	if requested <= 0 {
		return 0
	}
	for _, effect := range effects {
		if effect.Function == "HEAL_CUT" && effect.Remaining > 0 && effect.Rate >= 0 {
			if effect.Rate > 0 {
				requested = maxInt(1, requested-int(int64(requested)*int64(effect.Rate)/100))
			}
			break
		}
	}
	return nativeHealReverseDelta(currentHP, requested, effects)
}

// Retained DOT absorption applies reversal without the healing-cut consumer
// used by direct healing, regeneration and ordinary/enchant absorption.
func nativeHealReverseDelta(currentHP int, requested int, effects []battleEffect) int {
	if requested <= 0 {
		return 0
	}
	reverseRate := 0
	for _, effect := range effects {
		if effect.Function == "HEAL_REVERSE" && effect.Remaining > 0 {
			reverseRate += maxInt(0, effect.Rate)
		}
	}
	reverseRate = minInt(100, reverseRate)
	if reverseRate > 0 {
		reversed := int(int64(requested) * int64(reverseRate) / 100)
		reversed = minInt(reversed, maxInt(0, currentHP-1))
		return -reversed
	}
	return requested
}

// battleDOTOrder is the managed SKILL_ROLE_DOT 1..5 order consumed by native
// FUN_000615b0/FUN_000616e0. Value 6 is TRAP; the active CN card-bound trap is
// consumed by card play and is not a retained HP tick in the Go domain model.
var battleDOTOrder = [...]string{"POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC"}

func activeBattleDOT(effects []battleEffect, function string) (battleEffect, bool) {
	for _, effect := range effects {
		if effect.Remaining > 0 && strings.EqualFold(effect.Function, function) {
			return effect, true
		}
	}
	return battleEffect{}, false
}

// tickEnemyDOTEffects mirrors FUN_000616e0, called by
// battle5_api_user_attack after the complete player action list. Native owns
// DOT order by type first and member second, then performs a GUTS/death sweep
// after every type before it can advance to the next one.
func (engine *BattleEngine) tickEnemyDOTEffects() ([]BattleResult, error) {
	// FUN_000616e0 returns immediately when a previous user-attack action has
	// already set the GameMaster end flag. The outer API still calls it; it just
	// produces no retained-DOT rows.
	if engine.endType != 0 {
		return nil, nil
	}
	results := make([]BattleResult, 0, engine.enemyCount*4)
	engine.clearTranceReactions()
	for _, function := range battleDOTOrder {
		for index := 0; index < engine.enemyCount; index++ {
			enemy := &engine.enemies[index]
			if enemy.HP <= 0 {
				continue
			}
			effect, exists := activeBattleDOT(enemy.Effects, function)
			if !exists {
				continue
			}
			resolution := resolveDOTAttributeGuards(&enemy.Effects, effect.Value, enemy.HP, function)
			damage := resolution.Damage
			enemy.HP = nativeHPCommit(enemy.HP, enemy.MaxHP, -damage, enemy.Effects)
			recordEnemyDamage(enemy, damage, "")
			enemy.Trance.OtherDamage += int64(maxInt(0, damage))
			engine.recordDamageEvent(enemy.MemberType, effect.Source, damage, effect.Attribute, "", effect.Function, false)
			results = append(results,
				buffStatusEffectResult(enemy.MemberType, battleBuffCodes[effect.Function]),
				BattleResult{Command: 60, Args: []int64{int64(enemy.MemberType), 0, int64(-damage), int64(enemy.HP), 0, 0, 100, 0, 0, 0}},
			)
			results = append(results, damageEffectResults(enemy.MemberType, resolution)...)
			if enemy.Parent > 0 && enemy.Parent <= engine.enemyCount {
				// FUN_00060730 forwards the selected part's final post-consumer
				// amount. The body's HP commit also applies ENDURE; only
				// ResultCmd3(flag=1) projects its HP, without a second damage row.
				parent := &engine.enemies[enemy.Parent-1]
				parentDamage := maxInt(0, damage)
				parent.HP = nativeHPCommit(parent.HP, parent.MaxHP, -parentDamage, parent.Effects)
				recordEnemyDamage(parent, parentDamage, "")
				parent.Trance.OtherDamage += int64(parentDamage)
				results = append(results, BattleResult{Command: CommandHP, Args: []int64{
					int64(parent.MemberType), int64(parent.MaxHP), int64(parent.HP), 1,
				}})
			}
		}

		// FUN_0005feb6 precedes FUN_0004ccfc/FUN_0004cd4a after every
		// SKILL_ROLE_DOT value. A GUTS recovery must therefore happen before
		// the next DOT type and before the corresponding break/death action.
		for index := 0; index < engine.enemyCount; index++ {
			enemy := &engine.enemies[index]
			var guts []BattleResult
			enemy.HP, guts = resolveGuts(enemy.MemberType, enemy.MaxHP, enemy.HP, &enemy.Effects)
			if len(guts) > 0 {
				enemy.TurnSkillStats.Died = false
			}
			results = append(results, guts...)
		}
		for index := 0; index < engine.enemyCount; index++ {
			var err error
			results, err = engine.resolveEnemyDeath(results, &engine.enemies[index])
			if err != nil {
				return nil, err
			}
		}
		if engine.allEnemyBodiesDead() {
			engine.endType = engine.enemyWaveEndType()
			break
		}
	}
	results = append(results, engine.finishTranceReactions(1)...)
	return results, nil
}

// tickPlayerDOTEffects mirrors FUN_000615b0, called by
// battle5_api_enemy_phase after the complete enemy action list. It shares the
// native type-major order and per-type GUTS sweep, but player game-over and END
// projection remain owned by the enclosing phase.
func (engine *BattleEngine) tickPlayerDOTEffects() []BattleResult {
	results := make([]BattleResult, 0, maxRoomMembers*3)
	for _, function := range battleDOTOrder {
		for _, player := range engine.playerStates() {
			if player.HP <= 0 {
				continue
			}
			effect, exists := activeBattleDOT(player.Effects, function)
			if !exists {
				continue
			}
			resolution := resolveDOTAttributeGuards(&player.Effects, effect.Value, player.HP, function)
			damage := resolution.Damage
			player.HP = engine.playerHPAfterDamage(player, damage)
			recordPlayerDamage(player, damage)
			results = append(results,
				buffStatusEffectResult(player.MemberType, battleBuffCodes[effect.Function]),
				BattleResult{Command: 60, Args: []int64{int64(player.MemberType), 0, int64(-damage), int64(player.HP), 0, 0, 100, 0, 0, 0}},
			)
			results = append(results, damageEffectResults(player.MemberType, resolution)...)
			if effect.Source >= 5 && effect.Source < 5+engine.enemyCount {
				results = append(results, engine.burstDamageGaugeResults(player, damage)...)
			}
		}
		for _, player := range engine.playerStates() {
			var guts []BattleResult
			guts = resolvePlayerGuts(player)
			results = append(results, guts...)
		}
		if engine.allPlayersDead() {
			// Stop DOT passes; the enclosing phase owns continuation, caller
			// retirement and defeat settlement, including a bare all-KO party.
			break
		}
	}
	return results
}

// tickPersistentEffects implements 5eb20 -> 734c0 at the end of the
// chalice-enemy phase, not at the beginning of the next TurnPhase.
// Lists 1/2/4/6 have no turn countdown; lists 3/7 expire silently.
func (engine *BattleEngine) tickPersistentEffects() ([]BattleResult, error) {
	results := make([]BattleResult, 0, 32)
	for _, player := range engine.playerStates() {
		if player.MemberType == 0 || player.GameOver {
			continue
		}
		results = engine.tickMemberPersistentEffects(results, player)
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		// Immediate DESTRUCT leaves the actor registered after its visible
		// death. Ordinary break settlement alone removes it from this sweep.
		if enemy.MemberType == 0 || enemy.HP <= 0 && enemy.Broken && !enemy.SelfDestructed {
			continue
		}
		ensureEnemyBaseParameters(enemy)
		expiredAny := false
		for _, listType := range [...]int{0, 3, 5, 7} {
			var expired []battleEffect
			enemy.Effects, expired = expireEffectList(enemy.Effects, listType)
			expiredAny = expiredAny || len(expired) > 0
			for _, effect := range expired {
				revertEnemyEffect(enemy, effect)
			}
			refreshEnemyBattleParameters(enemy)
			refreshEnemyAttribute(enemy)
			if listType == 0 || listType == 5 {
				enemy.recordReleasedStatusCooldown(expired)
				results = appendNaturalExpiryRows(results, enemy.MemberType, expired, enemy.Effects, enemy.Attribute)
				results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
					enemy.MemberType, enemy.HP, enemy.MaxHP, enemy.Attack, enemy.Magic, enemy.Recovery,
					enemy.Defense, enemy.MDefense, enemy.LimitAttack, enemy.LimitMagic, enemy.LimitRecovery)})
			}
		}
		if expiredAny {
			results = append(results, engine.publishMemberStatusProjection(enemy.MemberType)...)
		}
		enemy.tickStatusCooldown()
		gaugeState := enemy.Trance.State
		results = append(results, tickEnemyTrance(enemy)...)
		if enemy.Trance.Reactive != nil && enemy.Trance.State != gaugeState {
			rows, err := engine.executeGaugeStateSkill(enemy)
			if err != nil {
				return nil, err
			}
			results = append(results, rows...)
		}
	}
	if engine.storyHelp != nil {
		results = engine.tickMemberPersistentEffects(results, &engine.storyHelp.Actor)
	}
	// The native actor list also contains the two neutral field members.
	// They have no active skills in this PvE profile, but 72a18 still emits
	// their NORMAL/EVENT parameter snapshots, as it does for real actors.
	for member := 10; member <= 11; member++ {
		limit := initialParameterLimit(engine.alchemist)
		for range 2 {
			results = append(results, BattleResult{Command: CommandBattleParam,
				Args: battleParameterArgs(member, 0, 1, 0, 0, 0, 0, 0, limit, limit, limit)})
		}
	}
	return results, nil
}

func expireEffectList(effects []battleEffect, listType int) ([]battleEffect, []battleEffect) {
	kept := effects[:0]
	var expired []battleEffect
	for _, effect := range effects {
		if effect.ListType == listType {
			effect.Remaining--
			if effect.Remaining <= 0 {
				// Native retains the invalid negative-counter guard for resume,
				// but silently drops it when its duration ends.
				if !isAttributeGuard(effect.Function) || effect.Uses >= 0 {
					expired = append(expired, effect)
				}
				continue
			}
		}
		kept = append(kept, effect)
	}
	return kept, expired
}

// 47190 suppresses the grouped UI release while the NORMAL/EVENT lists
// still supply the same buff kind. The per-entry 71 is always emitted.
func appendNaturalExpiryRows(results []BattleResult, memberType int, expired, remaining []battleEffect, attribute string) []BattleResult {
	released := make(map[int]bool)
	parameterMasks := make(map[int]int)
	attributeMasks := make(map[int]int)
	for _, effect := range expired {
		code, exists := battleBuffCodes[effect.Function]
		if !exists {
			continue
		}
		buffKind, debuffKind := branchStatusKind(effect, true), branchStatusKind(effect, false)
		stillActive := false
		for _, other := range remaining {
			// The HP selector has no retained parameter operator. Original
			// expiry publishes72 even while another such status remains.
			if effect.Delta == 0 && strings.EqualFold(effect.Parameter, "HP") {
				break
			}
			if other.ListType != 0 && other.ListType != 2 {
				continue
			}
			if code == 19 || code == 113 {
				// 47190 -> 476b2 matches attribute defense by exact code and
				// attribute, independently of retained parameter deltas.
				stillActive = code == battleBuffCodes[other.Function] && effect.Attribute == other.Attribute
			} else if effect.Parameter != "" || other.Parameter != "" {
				// 47190 matches the same code, then 44ba0/44ed4 classifies
				// its retained signed deltas. Different producers expire separately.
				// A clamped zero-delta entry supplies NULL, not its producer's kind.
				stillActive = code == battleBuffCodes[other.Function] &&
					(buffKind >= 0 && buffKind == branchStatusKind(other, true) ||
						debuffKind >= 0 && debuffKind == branchStatusKind(other, false))
			} else {
				stillActive = code == battleBuffCodes[other.Function] && effect.Attribute == other.Attribute
			}
			if stillActive {
				break
			}
		}
		key, row := naturalEffectReleaseResult(memberType, effect, code)
		row.Args[1] = int64(effect.ListType)
		parameterFlags, attributeFlags := key[1], key[2]
		duplicate := released[code] && (parameterFlags != 0 && parameterMasks[code]&parameterFlags != 0 ||
			parameterFlags == 0 && (attributeFlags == 1 || attributeMasks[code]&attributeFlags != 0))
		if !stillActive && !duplicate {
			results = append(results, row)
			released[code] = true
			parameterMasks[code] |= parameterFlags
			// JP groups both attribute and parameter masks by buff code. Each
			// expired entry still receives its own71 after the grouped72 rows.
			attributeMasks[code] |= attributeFlags
		}
	}
	for _, effect := range expired {
		if code, ok := battleBuffCodes[effect.Function]; ok {
			results = append(results, BattleResult{Command: CommandBuffLostOne,
				Args: []int64{int64(memberType), int64(code), int64(effect.SourceSkillID)}})
			if effect.Function == "REWRITE" {
				results = append(results, rewriteResult(memberType, attribute))
			}
		}
	}
	return results
}

// 63cc0 -> 5fe48(0), 5fe48(1) heals both sides at UserPhase entry.
// 5f808 consumes lists 0..6 in order, never revives an HP-zero member,
// and combines all regeneration entries into one 53/61 pair per target.
func (engine *BattleEngine) regenerateMembers() []BattleResult {
	var results []BattleResult
	regenerate := func(memberType int, hp *int, maxHP int, effects []battleEffect) {
		if *hp <= 0 {
			return
		}
		total := 0
		for listType := 0; listType <= 6; listType++ {
			for _, effect := range effects {
				if effect.ListType != listType || effect.Value < 0 ||
					(effect.Function != "REGENERATE_FIXED" && effect.Function != "REGENERATE_BY_SELF_PARAM") {
					continue
				}
				oldHP := *hp
				delta, nextHP := nativeHealCommit(*hp, maxHP, effect.Value, effects, false)
				*hp = nextHP
				total += delta
				engine.recordRecoveryAction(effect.Source, delta)
				if engine.playerMember(memberType) {
					engine.recordPlayerHealCommit(engine.player(memberType), effect.Source, oldHP, delta, effect.HateRate, effect.HateLimit)
				} else if memberType >= 5 && memberType < 5+engine.enemyCount && delta < 0 {
					recordEnemyDamageTotals(&engine.enemies[memberType-5], -delta)
				}
			}
		}
		if total != 0 {
			results = append(results, buffStatusEffectResult(memberType, battleBuffCodes["REGENERATE_FIXED"]),
				battleHealResult(memberType, 0, total, *hp))
		}
	}
	for _, player := range engine.playerStates() {
		if player.MemberType != 0 && !player.GameOver {
			regenerate(player.MemberType, &player.HP, player.MaxHP, player.Effects)
		}
	}
	for index := 0; index < engine.enemyCount; index++ {
		enemy := &engine.enemies[index]
		regenerate(enemy.MemberType, &enemy.HP, enemy.MaxHP, enemy.Effects)
	}
	return results
}

func naturalEffectReleaseResult(memberType int, effect battleEffect, code int) ([3]int, BattleResult) {
	parameterFlags := retainedEffectParameterFlags(effect)
	attributeFlags := 1
	if effect.Function == "ATTR_DEF_UP" || effect.Function == "ATTR_DEF_DOWN" || effect.Function == "ATTACK_BARRIER_APPOINT_ATTR" || effect.Function == "ENCHANT" || effect.Function == "REWRITE" || isAttributeGuard(effect.Function) {
		attributeFlags = 1 << combatAttributeCode(effect.Attribute)
	}
	if effect.Function == "ENCHANT" || effect.Function == "ATTR_DEF_DOWN_BOOST" || isCountedDamageStatus(effect.Function) {
		attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
	}
	key := [3]int{code, parameterFlags, attributeFlags}
	return key, BattleResult{Command: 72, Args: []int64{int64(memberType), 0, int64(code), int64(parameterFlags), int64(attributeFlags)}}
}

func refreshPlayerAttribute(player *battlePlayer) {
	for index := len(player.Effects) - 1; index >= 0; index-- {
		if player.Effects[index].Function == "REWRITE" {
			player.Attribute = player.Effects[index].Attribute
			return
		}
	}
	player.Attribute = player.BaseAttribute
	if player.Attribute == "" {
		player.Attribute = "NEUTRAL"
	}
}

func refreshEnemyAttribute(enemy *battleEnemy) {
	for index := len(enemy.Effects) - 1; index >= 0; index-- {
		if enemy.Effects[index].Function == "REWRITE" {
			enemy.Attribute = enemy.Effects[index].Attribute
			return
		}
	}
	enemy.Attribute = enemy.BaseAttribute
	if enemy.Attribute == "" {
		enemy.Attribute = "NULL"
	}
}

func revertPlayerEffect(player *battlePlayer, effect battleEffect) {
	if effect.Delta == 0 {
		return
	}
	if effect.Function == "PARAM_LIMIT_BREAK_FIXED" {
		adjustPlayerParameterLimit(player, effect.Parameter, -effect.Delta)
	}
}

func revertEnemyEffect(enemy *battleEnemy, effect battleEffect) {
	if effect.Delta == 0 {
		return
	}
	if effect.Function == "PARAM_LIMIT_BREAK_FIXED" {
		adjustEnemyParameterLimit(enemy, effect.Parameter, -effect.Delta)
	}
}

func coveringRoleValueScaled(role CombatSkillRole, level, chainCount int, scale battleRoleScale) int {
	value := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	if scale.pvp {
		value = int(scale.fixed(int32(value)))
	}
	if chainCount > 1 && role.ChainRate != 0 {
		value = value * (100 + role.ChainRate*(chainCount-1)) / 100
	}
	return value
}

func (engine *BattleEngine) tickMemberPersistentEffects(results []BattleResult, player *battlePlayer) []BattleResult {
	ensurePlayerBaseParameters(player)
	expiredAny := false
	for _, listType := range [...]int{0, 3, 5, 7} {
		var expired []battleEffect
		player.Effects, expired = expireEffectList(player.Effects, listType)
		expiredAny = expiredAny || len(expired) > 0
		for _, effect := range expired {
			revertPlayerEffect(player, effect)
		}
		refreshPlayerBattleParameters(player)
		refreshPlayerAttribute(player)
		if listType == 0 || listType == 5 {
			results = appendNaturalExpiryRows(results, player.MemberType, expired, player.Effects, player.Attribute)
			// 72a18 emits a parameter snapshot even for an empty expired list.
			results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
				player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
				player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
		}
	}
	// JP728f1 checks the four removed-entry counts before publishing507/508.
	// A KO member's unreported status is not itself an expiry-triggered update.
	if expiredAny {
		results = append(results, engine.publishMemberStatusProjection(player.MemberType)...)
	}
	tickCardCostIncreases(player)
	engine.tickCardModifiers(player)
	results = append(results, engine.tickPlayerCardHolds(player)...)
	results = append(results, tickPlayerBlessHolds(player)...)
	tickPlayerBurstCounters(player)
	return results
}
