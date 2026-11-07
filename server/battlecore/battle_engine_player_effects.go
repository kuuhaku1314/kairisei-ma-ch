package battlecore

import (
	"fmt"
	"slices"
	"strings"
)

func (engine *BattleEngine) executePersistentEffect(action battleAction, role CombatSkillRole, level int, chainCount int) ([]BattleResult, error) {
	code, exists := battleBuffCodes[role.Function]
	if !exists {
		return nil, fmt.Errorf("persistent combat function %q has no official buff code", role.Function)
	}
	duration := maxInt(1, combatParameterInt(role.Parameters[0]))
	if role.Function == "ENCHANT" || role.Function == "GUTS" || isCountedDamageStatus(role.Function) {
		duration = combatParameterInt(role.Parameters[0])
	}
	value := persistentRoleValue(role, level, engine.player(action.memberType), chainCount)
	if role.Function == "COVERING" {
		value = coveringRoleValueScaled(role, level, chainCount, engine.roleScale(role))
	}
	if role.Function == "ENCHANT" {
		value = enchantRoleValueScaled(role, level, chainCount, engine.roleScale(role))
	}
	if role.Function == "REGENERATE_FIXED" {
		actor := engine.player(action.memberType)
		value = fixedRegenerateRoleValueScaled(role, level, 1, combatStatValue(actor, role.Parameters[5]), engine.roleScale(role))
		value = supportHealValue(actor.Effects, action.skill, value)
		value = scaleHealChain(maxInt(1, value), role.ChainRate, chainCount)
	}
	if isCombatDOTFunction(role.Function) {
		_, _, _, value = dotRoleValuesScaled(role, level, chainCount, combatStatValue(engine.player(action.memberType), role.Parameters[7]), engine.roleScale(role))
	}
	playerSide := engine.pvp || combatTargetIsPlayer(role.Target)
	kind := persistentEffectKind(role.Function)
	effect := persistentBattleEffect(role, value, duration, kind, engine.turn, action.memberType, level)
	effect.ListType, effect.BurstPassive = action.buffListType, action.buffListType == 6
	effect.HateRate, effect.HateLimit = action.skill.HateRatio, role.HateLimit
	engine.consumeRandomHandRole(role, &effect)
	if role.Function == "ATTR_DEF_UP" || role.Function == "ATTR_DEF_DOWN" {
		configureAttributeDefenseEffect(&effect, role, level, chainCount)
		if engine.pvp {
			effect.Value = int(engine.roleScale(role).fixed(int32(effect.Value)))
			effect.Parameters[1] = effect.Value
		}
		if role.Function == "ATTR_DEF_DOWN" {
			engine.boostAttributeDefenseEffect(action, &effect)
		}
	}
	if isCombatDOTFunction(role.Function) {
		configureDOTEffectScaled(&effect, role, level, chainCount, engine.roleScale(role))
	}
	results := make([]BattleResult, 0, 8)
	failed := make([]BattleResult, 0, 8)
	if playerSide {
		for _, memberType := range engine.playerBuffTargets(action, role) {
			if role.Function == "COST_BLOCK" {
				results = append(results, engine.registerPlayerCostBlock(engine.player(memberType), role, effect)...)
				continue
			}
			if applied, handled := engine.registerRetainedStatus(memberType, &engine.player(memberType).Effects, role, effect); handled {
				results = append(results, applied...)
				continue
			}
			if role.Function == "WEAKNESS" {
				results = append(results, engine.registerWeakness(memberType, role, effect)...)
				continue
			}
			if role.Function == "STAN" && !engine.stunHits(engine.player(memberType).Effects, 0, 0, role, level) {
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
				continue
			}
			// Hand-state application can rewrite counts into target-specific
			// masks. Do not feed one member's resolved mask into the next member.
			targetEffect := effect
			if isCombatDOTFunction(role.Function) {
				player := engine.player(memberType)
				// The native target consumer uses the same three resistance /
				// application draws for a player caster as for an enemy caster.
				if hasActiveCombatEffect(player.Effects, role.Function) || engine.rollPercent10000(0) ||
					!engine.persistentEffectHits(role, level) || engine.effectsResistDOT(player.Effects, role.Function) {
					results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
					continue
				}
				attributeRate, _ := engine.playerAttackAttribute(player, combatDOTAttribute(role.Function))
				targetEffect.Value = registeredChainedDOTValue(effect.Value, attributeRate, role.ChainRate*maxInt(0, chainCount-1), 0)
			}
			immediate, applied := engine.applyPlayerPersistentEffect(engine.player(memberType), role, &targetEffect)
			results = append(results, immediate...)
			if applied {
				results = append(results, battlePersistentResult(memberType, role, code, targetEffect))
			} else {
				// FUN_0008fb80 writes a failure row immediately for the current
				// target. A later FRIEND_ALL target succeeding does not erase it.
				results = append(results, battleDebuffFailedResult(memberType, role.RoleIndex, code))
			}
		}
		return results, nil
	}
	for _, index := range engine.playerRoleEnemyTargets(action, role) {
		enemy := &engine.enemies[index]
		if applied, handled := engine.registerRetainedStatus(enemy.MemberType, &enemy.Effects, role, effect); handled {
			results = append(results, applied...)
			continue
		}
		if role.Function == "WEAKNESS" {
			results = append(results, engine.registerWeakness(enemy.MemberType, role, effect)...)
			continue
		}
		if role.Function == "STAN" {
			if !engine.stunHits(enemy.Effects, enemy.StatusCooldown[1], enemy.Level.StatusResistances[1], role, level) {
				failed = append(failed, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
				continue
			}
			enemy.Effects = append(enemy.Effects, effect)
			engine.recordAIStatusApplied(enemy.MemberType, effect)
			results = append(results, battlePersistentResult(enemy.MemberType, role, code, effect))
			continue
		}
		if isCombatDOTFunction(role.Function) {
			// FUN_0008bb40 asks FUN_00046d30 for an existing DOT before it
			// consumes any resistance RNG. A matching active DOT rejects the
			// new entry instead of stacking or replacing it.
			if hasActiveCombatEffect(enemy.Effects, role.Function) ||
				engine.enemyResistsDOTStatus(enemy, role.Function) ||
				!engine.persistentEffectHits(role, level) ||
				engine.effectsResistDOT(enemy.Effects, role.Function) {
				failed = append(failed, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
				continue
			}
			targetEffect := engine.dotEffectForEnemyWithChain(effect, enemy, role.ChainRate*maxInt(0, chainCount-1))
			enemy.Effects = append(enemy.Effects, targetEffect)
			engine.recordAIStatusApplied(enemy.MemberType, targetEffect)
			results = append(results, battlePersistentResult(enemy.MemberType, role, code, targetEffect))
			continue
		}
		if !engine.persistentEffectHits(role, level) || engine.enemyResistsDebuff(enemy, role.Function) {
			failed = append(failed, battleDebuffFailedResult(enemy.MemberType, role.RoleIndex, code))
			continue
		}
		enemy.Effects = append(enemy.Effects, effect)
		engine.recordAIStatusApplied(enemy.MemberType, effect)
		results = append(results, battlePersistentResult(engine.enemies[index].MemberType, role, code, effect))
	}
	// The native result projector groups by role index. A multi-target role
	// displays DEBUFF_FAILED only if every target rejected that role; partial
	// success projects the successful ResultCmd62/69 rows without failure rows.
	if len(results) == 0 {
		return failed, nil
	}
	return results, nil
}

// FUN_00088a20 accepts BURST_PASSIVE entries only while the receiving member
// is in BURST. This is a target condition, not the source member's state.
func (engine *BattleEngine) playerBuffTargets(action battleAction, role CombatSkillRole) []int {
	targets := engine.playerRoleTargets(action, role)
	if action.buffListType != 6 {
		return targets
	}
	kept := targets[:0]
	for _, memberType := range targets {
		if engine.player(memberType).BurstState == burstGaugeBurst {
			kept = append(kept, memberType)
		}
	}
	return kept
}

func persistentEffectKind(function string) int {
	// Status direction belongs to the native function/buff identity, not to the
	// side receiving it. Player cards can apply BAD_STATUS to SELF (notably
	// COST_BLOCK); classifying every player-side target as a buff makes the
	// wrong release family remove it.
	if skillRoleKindDebuff(battleEffect{Function: function}) != 0 {
		return 2
	}
	return 1
}

func persistentBattleEffect(role CombatSkillRole, value int, duration int, kind int, turn int, source int, level int) battleEffect {
	effect := battleEffect{
		Function: role.Function, Parameter: role.Parameters[1], Attribute: role.Parameters[5], DamageKind: role.Parameters[3],
		Value: value, Kind: kind, Rate: combatParameterInt(role.Parameters[1]), Remaining: duration,
		AppliedTurn: turn, Source: source, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex,
	}
	switch role.Function {
	case "ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT":
		configureAttributeGuard(&effect, role, level)
	case "DAMAGE_BOOST":
		configureDamageBoostEffect(&effect, role, level)
	case "PHYSICS_DAMAGE_BOOST_COUNT", "MAGIC_DAMAGE_BOOST_COUNT":
		configureCountedDamage(&effect, role, level)
	case "ATTR_DEF_UP", "ATTR_DEF_DOWN":
		configureAttributeDefenseEffect(&effect, role, level, 1)
	case "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC":
		configureDOTEffect(&effect, role, level, 1)
	case "CARD_TRAP_DAMAGE":
		effect.Parameters[0] = combatParameterInt(role.Parameters[1])
		effect.Parameters[1] = value
		effect.Parameters[2] = combatParameterInt(role.Parameters[5]) + combatParameterInt(role.Parameters[6])*level
	case "CARD_SEAL":
		effect.Parameters = [4]int{
			combatParameterInt(role.Parameters[1]), combatParameterInt(role.Parameters[2]),
			combatParameterInt(role.Parameters[6]), combatParameterInt(role.Parameters[7]),
		}
	case "DEBUFF_REGIST":
		// Native role 99 stores p1 as the resistance rate and p2 as the
		// SKILL_ROLE_KIND_DEBUFF selector. Keep the selector symbolic in the
		// durable effect and project its exact enum only at ResultCmd time.
		effect.Parameter = role.Parameters[2]
		effect.Rate = value
		effect.Parameters[0] = value
		effect.Parameters[1] = skillRoleKindDebuff(battleEffect{Function: role.Parameters[2]})
	case "DAMAGE_UP", "DAMAGE_CUT", "DAMAGE_DOWN":
		// Native roles 20/22/33 store a duration, fixed + level-scaled rate,
		// attribute selector and physics selector. The active rate is applied by
		// the common damage consumer, not added to HP as a generic value.
		effect.Rate = combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
		effect.Attribute = role.Parameters[3]
		effect.DamageKind = role.Parameters[4]
		effect.Parameters[0] = effect.Rate
	case "COVERING":
		// Native role 40 stores p0 duration and the level-scaled p1+p2 value as
		// a per-mille damage-cut rate. p3/p4 filter attribute and physics. The
		// client status projection exposes the rate in tenths of one percent.
		effect.Rate = value
		effect.Value = value
		effect.Attribute = role.Parameters[3]
		effect.DamageKind = role.Parameters[4]
		effect.Parameters[0] = value / 10
	case "COST_BLOCK":
		// p0 is duration; p1 is the cost blocked at the next turn boundary.
		effect.Value = combatParameterInt(role.Parameters[1])
		effect.Rate = effect.Value
		effect.Parameters[0] = effect.Value
	case "ATTACK_BARRIER", "ATTACK_BARRIER_APPOINT_ATTR":
		// Native role 63/144 uses p1+p2*level as the absorbed-damage threshold,
		// p3 as hit count and p4 as the physics selector. Appointed barriers add
		// p5 as an attribute selector. Keep count and threshold separate so each
		// successful hit consumes exactly one barrier count.
		effect.Value = attackBarrierRoleValue(role, level)
		effect.Uses = maxInt(1, combatParameterInt(role.Parameters[3]))
		effect.DamageKind = role.Parameters[4]
		effect.Attribute = ""
		if role.Function == "ATTACK_BARRIER_APPOINT_ATTR" {
			effect.Attribute = role.Parameters[5]
		}
		effect.Parameters[0] = effect.Value
		effect.Parameters[1] = effect.Uses
	case "REFLECTION":
		// Reflection scales from p1+p2*level and filters by p3 physics type.
		effect.Rate = combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
		effect.DamageKind = role.Parameters[3]
		effect.Parameters[0] = effect.Rate
	case "HEAL_CUT":
		effect.Rate = minInt(100, combatParameterInt(role.Parameters[1]))
		effect.Value = effect.Rate
	case "HEAL_REVERSE":
		// Native role 80 stores p1+p2*level as the percentage of an incoming
		// heal which becomes a nonlethal negative HP delta. Multiple active
		// entries are accumulated by the common buff aggregator and capped at
		// 100 percent when the heal is committed. FUN_00096f30's percentage
		// preview is distinct from 8fb80's zero-valued initial ResultCmd62.
		effect.Rate = combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
		effect.Value = effect.Rate
		effect.Parameters[0] = effect.Rate
	case "ENDURE":
		// Role 121 stores p1+p2*level at buff +0x128. 45c04 takes the maximum
		// percentage, and 73e9f keeps that fraction of MaxHP at each HP commit.
		effect.Value = combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
		effect.Rate = effect.Value
		effect.Parameters[0] = effect.Value
	case "GUTS":
		// Native role 146 keeps the revival count and MaxHP percentage in
		// separate fields. A lethal commit consumes one count, not one turn.
		effect.Uses = combatParameterInt(role.Parameters[1])
		effect.Rate = combatParameterInt(role.Parameters[2])
		effect.Value = effect.Rate
		effect.Parameters[0] = effect.Uses
	case "ATTR_HIDE", "ATTR_SEE":
		// These statuses are duration-only. Keeping their payload zero avoids
		// turning p0 (duration) into a fabricated attribute or numeric modifier.
		effect.Value = 0
		effect.Rate = 0
	case "CRITICAL_UP", "CRITICAL_DOWN", "WEAKNESS":
		// Native roles retain the per-mille value internally. WEAKNESS's
		// rate/10 preview is separate from its zero-valued 8bb40 notification.
		effect.Value = value
		effect.Rate = value
		effect.Parameters[0] = value / 10
	case "CRITICAL_DAMAGE_BOOST":
		// p1 is the delta above the native 150% critical multiplier. The sole
		// official active row stores 50 and therefore changes criticals to 200%.
		effect.Value = combatParameterInt(role.Parameters[1])
		effect.Rate = effect.Value
		effect.Parameters[0] = effect.Value
	case "ENCHANT":
		// Enchant damage is a fixed per-hit value. The attack consumer sums all
		// active entries and emits a distinct ResultCmd60 with damage_type=1.
		effect.Value = value
		effect.Parameters[0] = value
	}
	return effect
}

func (engine *BattleEngine) applyPlayerPersistentEffect(player *battlePlayer, role CombatSkillRole, effect *battleEffect) (rows []BattleResult, applied bool) {
	defer func() {
		if applied {
			engine.recordAIStatusApplied(player.MemberType, *effect)
		}
	}()
	switch role.Function {
	case "BURN", "POISON", "FREEZE", "BLEED", "ELECTRIC":
		if hasActiveCombatEffect(player.Effects, role.Function) {
			return nil, false
		}
		player.Effects = append(player.Effects, *effect)
		return nil, true
	case "DARKNESS_APPOINT", "DARKNESS_RANDOM":
		effect.Mask = engine.newDarknessMask(player, role, effect.Parameters[0])
		effect.Parameters[0] = effect.Mask
		if effect.Mask == 0 {
			return nil, false
		}
		player.Effects = append(player.Effects, *effect)
		return nil, true
	case "CARD_TRAP_DAMAGE":
		candidates := make([]int, 0, len(player.Hand))
		submitted := engine.selectedPlays[player.MemberType]
		for _, deckSlot := range player.Hand {
			if deckSlot == 0 {
				continue
			}
			cardType := player.handSlotCard(deckSlot).CardType
			// 8fb80 -> 3c926 excludes every submitted card from the trap
			// pool, even while it is still held during UserAttack.
			if slices.Contains(submitted.CardTypes[:], cardType) {
				continue
			}
			if _, trapped := playerCardTrapEffect(player, cardType); trapped {
				continue
			}
			if _, sealed := playerCardSealEffect(player, cardType); sealed {
				continue
			}
			candidates = append(candidates, cardType)
		}
		count := effect.Parameters[0]
		count = minInt(maxInt(0, count), len(candidates))
		effect.Parameters[0] = count
		for draw := 0; draw < count && len(candidates) > 0; draw++ {
			selected := int(engine.rng.next() % uint32(len(candidates)))
			cardEffect := *effect
			cardEffect.CardType = candidates[selected]
			player.Effects = append(player.Effects, cardEffect)
			candidates[selected] = candidates[len(candidates)-1]
			candidates = candidates[:len(candidates)-1]
		}
		return nil, count > 0
	case "CARD_SEAL":
		candidates := make([]int, 0, len(player.Hand))
		submitted := engine.selectedPlays[player.MemberType]
		for _, deckSlot := range player.Hand {
			if deckSlot == 0 {
				continue
			}
			card := player.handSlotCard(deckSlot)
			if slices.Contains(submitted.CardTypes[:], card.CardType) {
				continue
			}
			skill, _, err := engine.handCardSkill(player, card)
			if err == nil && cardSealFilterMatches(skill, role) {
				candidates = append(candidates, card.CardType)
			}
		}
		wantedCount := effect.Parameters[0]
		wantedCount = minInt(maxInt(0, wantedCount), len(candidates))
		selectedCards := make([]int, 0, wantedCount)
		for draw := 0; draw < wantedCount && len(candidates) > 0; draw++ {
			selected := int(engine.rng.next() % uint32(len(candidates)))
			cardType := candidates[selected]
			candidates[selected] = candidates[len(candidates)-1]
			candidates = candidates[:len(candidates)-1]
			if _, sealed := playerCardSealEffect(player, cardType); sealed {
				continue
			}
			selectedCards = append(selectedCards, cardType)
		}
		appliedCount := 0
		for _, cardType := range selectedCards {
			if engine.playerResists(player, "CARD_SEAL_REGIST") {
				continue
			}
			cardEffect := *effect
			cardEffect.CardType = cardType
			cardEffect.Remaining, _ = engine.nativeInclusive(combatParameterInt(role.Parameters[0]), combatParameterInt(role.Parameters[1]))
			removed := removePlayerCardEffect(player, "CARD_TRAP_DAMAGE", cardEffect.CardType)
			if len(removed) > 0 {
				ensurePlayerBaseParameters(player)
				refreshPlayerBattleParameters(player)
				refreshPlayerAttribute(player)
				rows = appendNaturalExpiryRows(rows, player.MemberType, removed, player.Effects, player.Attribute)
				rows = append(rows, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
					player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
					player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery,
				)})
			}
			player.Effects = append(player.Effects, cardEffect)
			appliedCount++
		}
		effect.Parameters[0] = appliedCount
		return rows, appliedCount > 0
	default:
		player.Effects = append(player.Effects, *effect)
		return nil, true
	}
}

// 8bb40 rolls base resistance, then replaces COST_BLOCK in the incoming list.
// The old 72 is immediate; each successful role's 62 is still deferred.
func (engine *BattleEngine) registerPlayerCostBlock(player *battlePlayer, role CombatSkillRole, incoming battleEffect) []BattleResult {
	engine.rollPercent10000(0) // Player base resistance for status index 12 is zero.
	var rows []BattleResult
	if incoming.ListType != 2 && incoming.ListType != 4 {
		for _, current := range player.Effects {
			if current.ListType == incoming.ListType && current.Function == "COST_BLOCK" {
				_, release := naturalEffectReleaseResult(player.MemberType, current, battleBuffCodes["COST_BLOCK"])
				release.Args[1] = int64(incoming.ListType)
				rows = append(rows, release)
				break
			}
		}
		kept := player.Effects[:0]
		for _, current := range player.Effects {
			if current.ListType != incoming.ListType || current.Function != "COST_BLOCK" {
				kept = append(kept, current)
			}
		}
		player.Effects = kept
	}
	player.Effects = append(player.Effects, incoming)
	engine.recordAIStatusApplied(player.MemberType, incoming)
	return append(rows, battlePersistentResult(player.MemberType, role, battleBuffCodes["COST_BLOCK"], incoming))
}

func cardSealFilterMatches(skill CombatSkillDefinition, role CombatSkillRole) bool {
	// FUN_00081c60 applies p8 to every specified filter independently. Nonzero
	// means include: kind, attribute and the complete cost range must match.
	// Zero means exclude: every specified filter must not match. It is not a
	// final inversion of the cost predicate alone. The range reads the chosen
	// SkillData + 0x1c (cost), not the card profile's rarity.
	include := combatParameterInt(role.Parameters[8]) != 0
	if !strings.EqualFold(role.Parameters[4], "ALL") && role.Parameters[4] != "" {
		kindMatches := strings.EqualFold(role.Parameters[4], skill.Kind)
		if kindMatches != include {
			return false
		}
	}
	if !strings.EqualFold(role.Parameters[5], "ALL") && role.Parameters[5] != "" {
		attributeMatches := combatAttributeMatches(skill.Attribute, role.Parameters[5])
		if attributeMatches != include {
			return false
		}
	}
	lower := combatFilterBound(role.Parameters[6], -1)
	upper := combatFilterBound(role.Parameters[7], -1)
	// Native treats either absent bound as an unrestricted cost predicate.
	if lower < 0 || upper < 0 {
		return true
	}
	inside := skill.Cost >= lower && skill.Cost <= upper
	return inside == include
}

func combatFilterBound(value string, fallback int) int {
	if value == "" || strings.EqualFold(value, "ALL") {
		return fallback
	}
	return combatParameterInt(value)
}

func combatRarityCode(rarity string) int {
	switch strings.ToUpper(rarity) {
	case "NORMAL":
		return 1
	case "HIGHNORMAL":
		return 2
	case "RARE":
		return 3
	case "SUPERRARE":
		return 4
	case "ULTRARARE":
		return 5
	case "MILLIONRARE":
		return 6
	case "EXRARE":
		return 7
	case "LEGEND":
		return 8
	default:
		return 0
	}
}

func (engine *BattleEngine) nativeInclusive(lower int, upper int) (int, bool) {
	value := upper
	width := upper + 1 - lower
	if width <= 0 {
		return value, false
	}
	return lower + int(engine.rng.next()%uint32(width)), true
}

// Consumers use the count produced before the skill's first role. Direct
// role calls without a skill context produce their own count.
func (engine *BattleEngine) consumeRandomHandRole(role CombatSkillRole, effect *battleEffect) {
	if context := engine.skillTargets; context != nil {
		if count, exists := context.randomHandCounts[role.RoleIndex]; exists {
			effect.Parameters[0] = count
			return
		}
	}
	engine.prepareRandomHandRole(role, effect)
}

func (engine *BattleEngine) prepareRandomHandRole(role CombatSkillRole, effect *battleEffect) bool {
	switch role.Function {
	case "DARKNESS_RANDOM":
		// FUN_000a3812 produces one count before any target consumer runs, so a
		// FRIEND_ALL role shares this value instead of re-rolling per member.
		effect.Parameters[0], _ = engine.nativeInclusive(combatParameterInt(role.Parameters[1]), combatParameterInt(role.Parameters[2]))
	case "CARD_TRAP_DAMAGE":
		// FUN_000a354a has the same one-per-role p1..p2 producer contract.
		effect.Parameters[0], _ = engine.nativeInclusive(combatParameterInt(role.Parameters[1]), combatParameterInt(role.Parameters[2]))
	case "CARD_SEAL":
		// FUN_000a3931 produces p2..p3 and then performs a second xor128 call
		// whenever that native inclusive range is valid. The second value is not
		// stored in the action record, but it is observable in all later draws.
		var rolled bool
		effect.Parameters[0], rolled = engine.nativeInclusive(combatParameterInt(role.Parameters[2]), combatParameterInt(role.Parameters[3]))
		if rolled {
			engine.rng.next()
		}
	default:
		return false
	}
	return true
}

func (engine *BattleEngine) newDarknessMask(player *battlePlayer, role CombatSkillRole, randomCount int) int {
	if engine.playerResists(player, "DARKNESS_REGIST") {
		return 0
	}
	existing := 0
	for _, effect := range player.Effects {
		if strings.HasPrefix(effect.Function, "DARKNESS") && effect.Remaining > 0 {
			existing |= effect.Mask
		}
	}
	if role.Function == "DARKNESS_APPOINT" {
		requested := 0
		for slot := 0; slot < 5; slot++ {
			if combatParameterInt(role.Parameters[slot+1]) != 0 {
				requested |= 1 << (4 - slot)
			}
		}
		return requested &^ existing
	}
	// FUN_0008fb80 builds available native bit positions in ascending order
	// (bit 0 through bit 4), selects rng%remaining, replaces that element with
	// the last candidate and then shortens the list. This differs observably
	// from a forward partial Fisher-Yates shuffle: both the selected mask and
	// every later room RNG draw would otherwise drift.
	bits := make([]int, 0, 5)
	for bitIndex := 0; bitIndex < 5; bitIndex++ {
		bit := 1 << bitIndex
		if existing&bit == 0 {
			bits = append(bits, bit)
		}
	}
	randomCount = minInt(maxInt(0, randomCount), len(bits))
	mask := 0
	for draw := 0; draw < randomCount && len(bits) > 0; draw++ {
		selected := int(engine.rng.next() % uint32(len(bits)))
		mask |= bits[selected]
		bits[selected] = bits[len(bits)-1]
		bits = bits[:len(bits)-1]
	}
	return mask
}

func (engine *BattleEngine) playerResists(player *battlePlayer, function string) bool {
	rate := minInt(100, maxInt(0, engine.playerEffectValue(player, function)))
	return int(engine.rng.next()%100) < rate
}

func (engine *BattleEngine) persistentEffectHits(role CombatSkillRole, level int) bool {
	// Native STAN and the five common DOT producers calculate their application
	// rate from p1+p2*level and compare xor128()%10000 against rate*100.
	if role.Function != "STAN" && !isCombatDOTFunction(role.Function) {
		return true
	}
	rate := combatParameterInt(role.Parameters[1]) + combatParameterInt(role.Parameters[2])*level
	return engine.rollPercent10000(rate)
}

func (engine *BattleEngine) rollPercent10000(rate int) bool {
	rate = minInt(100, maxInt(0, rate))
	return int(engine.rng.next()%10000) < rate*100
}

func (engine *BattleEngine) enemyResistsDebuff(enemy *battleEnemy, function string) bool {
	return engine.effectsResistDebuff(enemy.Effects, function, false)
}

func (engine *BattleEngine) effectsResistDOT(effects []battleEffect, function string) bool {
	// FUN_0008bb40 performs this RNG draw even when no matching resistance is
	// active, so the zero-rate roll is retained for deterministic parity.
	return engine.effectsResistDebuff(effects, function, true)
}

func (engine *BattleEngine) effectsResistDebuff(effects []battleEffect, function string, alwaysRoll bool) bool {
	wantedKind := skillRoleKindDebuff(battleEffect{Function: function})
	if wantedKind == 0 {
		return false
	}
	rate := 0
	for _, effect := range effects {
		if effect.Function != "DEBUFF_REGIST" || effect.Remaining <= 0 {
			continue
		}
		resistedKind := skillRoleKindDebuff(battleEffect{Function: effect.Parameter})
		if resistedKind == wantedKind {
			rate += maxInt(0, effect.Value)
		}
	}
	if rate <= 0 && !alwaysRoll {
		return false
	}
	return engine.rollPercent10000(rate)
}

func battlePersistentResult(memberType int, role CombatSkillRole, code int, effect battleEffect) BattleResult {
	attributeFlags := 1
	parameters := effect.Parameters
	if isAttributeGuard(role.Function) {
		attributeFlags = 1 << combatAttributeCode(effect.Attribute)
		parameters = attributeGuardInitialParameters(effect)
	}
	if role.Function == "DAMAGE_BOOST" {
		attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
		parameters = [4]int{0, combatPhysicsIndex(effect.DamageKind), 0, 0}
	}
	if isCountedDamageStatus(role.Function) {
		attributeFlags = sphereSupportAttributeFlags(effect.Attribute)
		parameters = [4]int{effect.Uses, 0, 0, 0}
	}
	switch code {
	case 106:
		// 8bb40 publishes the producer's kind/attribute filters, a bitmask
		// of the inclusive cost interval, and its include flag.
		kindMask, attributeMask := -1, -1
		if !strings.EqualFold(role.Parameters[4], "ALL") {
			kind := combatSkillKindCode(role.Parameters[4])
			kindMask = 1 << kind
		}
		if !strings.EqualFold(role.Parameters[5], "ALL") && role.Parameters[5] != "" {
			attributeMask = 1 << combatAttributeCode(role.Parameters[5])
		}
		lower, upper := combatFilterBound(role.Parameters[6], -1), combatFilterBound(role.Parameters[7], -1)
		costMask := int32(-1)
		if lower != -1 && upper != -1 {
			costMask = 0
			// x86 masks each shift to five bits; one full cycle sets all bits.
			for cost, count := lower, 0; cost <= upper && count < 32; cost, count = cost+1, count+1 {
				costMask |= int32(uint32(1) << (uint(cost) & 31))
			}
		}
		parameters = [4]int{kindMask, attributeMask, int(costMask), combatParameterInt(role.Parameters[8])}
	case 10, 14, 200, 202, 203, 204, 205, 206, 207, 208, 209, 216, 409:
		// 8d7e0 publishes only local_5ac/local_5a8, not the retained
		// threshold, regeneration, resistance, covering or critical power.
		// For these reachable good-status codes it agrees with 453cc.
		parameters = nativeStatusUIParameters(code, effect)
	}
	if isCombatDOTFunction(role.Function) || role.Function == "CRITICAL_DOWN" || role.Function == "WEAKNESS" || role.Function == "COST_BLOCK" || role.Function == "STAN" || role.Function == "CARD_TRAP_DAMAGE" || role.Function == "DARKNESS_APPOINT" || role.Function == "DARKNESS_RANDOM" || role.Function == "HEAL_REVERSE" || role.Function == "HEAL_CUT" {
		// 8bb40/8fb80's four notification fields stay zero; a077a's numerical
		// preview (likewise 96ba7/978b8) is not the ResultCmd62 payload.
		// Confirmed in original x86, independently of retained effect values.
		parameters = [4]int{}
	}
	if role.Function == "ATTR_DEF_UP" || role.Function == "ATTR_DEF_DOWN" || role.Function == "ATTACK_BARRIER_APPOINT_ATTR" || role.Function == "ENCHANT" {
		attributeFlags = 1 << combatAttributeCode(role.Parameters[5])
	}
	if role.Function == "ENCHANT" {
		attributeFlags = sphereSupportAttributeFlags(role.Parameters[5])
	}
	if role.Function == "COVERING" {
		// 96165 -> 8d7e0 preserves its p3 attribute in the initial status too.
		attributeFlags = 1 << combatAttributeCode(effect.Attribute)
	}
	if role.Function == "ATTR_DEF_UP" || role.Function == "ATTR_DEF_DOWN" || role.Function == "ENCHANT" {
		// 88a20/8fb80 retain defense/enchant values but leave the four initial
		// notification values zero; producer previews are separate outputs.
		parameters = [4]int{}
	}
	return battleBuffResultWithListType(
		memberType, role.RoleIndex, effect.ListType, code, 0, attributeFlags,
		parameters[0], parameters[1], parameters[2], parameters[3],
	)
}

// nativeStatusUIParameters mirrors 453cc (resume) and the corresponding
// 8d7e0 initial good-status notifications. Other producer families can have
// different initial fields and must not use this as a universal buff encoder.
func nativeStatusUIParameters(code int, effect battleEffect) [4]int {
	parameters := [4]int{}
	switch code {
	case 22: // DAMAGE_BOOST retains the physics selector in its second UI field.
		parameters[1] = combatPhysicsIndex(effect.DamageKind)
	case 205: // Barrier type and remaining hit count, not its damage threshold.
		parameters[0] = combatPhysicsIndex(effect.DamageKind)
		parameters[1] = effect.Uses
	case 208, 210, 211: // Reflection / attribute cut / attribute drain type.
		parameters[0] = combatPhysicsIndex(effect.DamageKind)
	case 209: // GUTS restores remaining revivals, not HP percentage.
		parameters[0] = effect.Uses
	case 217, 218:
		parameters[0] = effect.Uses
	case 215: // DOT absorption restores its kind; CUT_DOT does not.
		parameters[0], parameters[1] = 2, attributeGuardDotCode(effect)
	}
	return parameters
}

func (engine *BattleEngine) executeDealChange(action battleAction, role CombatSkillRole, penalty bool) ([]BattleResult, error) {
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
	for _, memberType := range engine.playerRoleTargets(action, role) {
		player := engine.player(memberType)
		effect := battleEffect{Function: role.Function, Value: value, Kind: kind, Remaining: duration, AppliedTurn: engine.turn, Source: action.memberType, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
		if !applyPlayerDrawEffect(player, effect, code) {
			continue
		}
		engine.recordAIStatusApplied(memberType, effect)
		results = append(results, battleBuffResultWithParameters(memberType, role.RoleIndex, code, 0, 1, value, 0, 0, 0))
	}
	return results, nil
}

func applyPlayerDrawEffect(player *battlePlayer, effect battleEffect, code int) bool {
	// CN libbattle5 keeps one status per draw buff code. A weaker replacement is
	// rejected while the existing status has more than one internal turn left;
	// otherwise the old code entry is removed and the new role becomes owner.
	// The native internal duration includes its application turn, hence the
	// ordinary DEAL_BONUS/DEAL_PENALTY value of two affects the next UserPhase.
	for _, current := range player.Effects {
		currentCode, isDrawEffect := battleBuffCodes[current.Function]
		if isDrawEffect && currentCode == code && effect.Value <= current.Value && current.Remaining > 1 {
			return false
		}
	}
	kept := player.Effects[:0]
	for _, current := range player.Effects {
		currentCode, isDrawEffect := battleBuffCodes[current.Function]
		if !isDrawEffect || currentCode != code {
			kept = append(kept, current)
		}
	}
	player.Effects = append(kept, effect)
	return true
}
