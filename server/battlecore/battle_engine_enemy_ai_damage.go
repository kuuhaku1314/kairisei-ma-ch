package battlecore

import "strings"

// 7040d/70868 add independently filtered normal, enchant, DOT and special
// buckets. A DOT selector must not discard normal attacks or enchant damage;
// physics/attribute selectors must not discard DOT or special damage.
func nativeAIDamageEventMatches(event battleDamageEvent, attribute, physics, dot string, enchantOnly bool) bool {
	if enchantOnly && !event.Enchant {
		return false
	}
	if event.Special {
		return true
	}
	if event.DOT != "" {
		return combatNullValue(dot) || strings.EqualFold(event.DOT, dot)
	}
	// Damage buckets contain the resolved single attribute, not a card's
	// original dual-attribute declaration (701aa/70296).
	if !combatNullValue(attribute) && !strings.EqualFold(event.Attribute, attribute) {
		return false
	}
	return event.Enchant || strings.EqualFold(physics, "ALL") || strings.EqualFold(event.Physics, "ALL") || strings.EqualFold(event.Physics, physics)
}

func nativeAIDamageFiltersSupported(attribute, physics, dot string) bool {
	switch strings.ToUpper(attribute) {
	case "", "NULL", "FIRE", "ICE", "WIND", "LIGHT", "DARK", "EARTH", "THUNDER", "WATER", "NEUTRAL":
	default:
		return false
	}
	switch strings.ToUpper(physics) {
	case "PHYSICS", "MAGIC", "ALL":
	default:
		return false
	}
	switch strings.ToUpper(dot) {
	case "", "NULL", "POISON", "BURN", "FREEZE", "BLEED", "ELECTRIC", "TRAP":
		return true
	default:
		return false
	}
}

func (engine *BattleEngine) recordSpecialEnemyDamage(target, source, damage int) {
	if damage > 0 {
		engine.turnStats.DamageEvents = append(engine.turnStats.DamageEvents,
			battleDamageEvent{Target: target, Source: source, Value: damage, Special: true})
	}
}

func (engine *BattleEngine) damageEventsAtAge(age int) []battleDamageEvent {
	if age == 0 {
		return engine.turnStats.DamageEvents
	}
	if age < 1 || age > len(engine.damageHistory) {
		return nil
	}
	return engine.damageHistory[age-1]
}

func (engine *BattleEngine) damageByPlayerInTurns(target, source int, attribute, physics, dot string, first, last int, attackOnly bool) int64 {
	// 70868's indices are relative ages, inclusive, not absolute battle turns.
	if first < 0 || last < first || last > 29 {
		return 0
	}
	var total int64
	for age := first; age <= last; age++ {
		for _, event := range engine.damageEventsAtAge(age) {
			if attackOnly && (event.Special || event.DOT != "") {
				continue
			}
			if event.Target == target && event.Source == source && nativeAIDamageEventMatches(event, attribute, physics, dot, false) {
				total += int64(event.Value)
			}
		}
	}
	return total
}

func (engine *BattleEngine) allDamageTurnMatches(values []string) bool {
	return engine.damageTurnMatches(values, false)
}

func (engine *BattleEngine) damageTurnMatches(values []string, attackOnly bool) bool {
	if !nativeAIDamageFiltersSupported(values[3], values[4], values[5]) {
		return false
	}
	target := 0
	if !combatNullValue(values[0]) {
		if enemy, found := engine.enemyByTriggerName(values[0]); found {
			target = enemy.MemberType
		} else {
			target = -1
		}
	}
	var total, highest int64
	candidates := make([]int, 0, maxRoomMembers)
	for i := 0; i < engine.enemyCount; i++ {
		enemy := &engine.enemies[i]
		if enemy.HP <= 0 || target != 0 && enemy.MemberType != target {
			continue
		}
		for j := range engine.players {
			player := &engine.players[j]
			if player.MemberType == 0 || player.HP <= 0 {
				continue
			}
			value := engine.damageByPlayerInTurns(enemy.MemberType, player.MemberType, values[3], values[4], values[5], combatParameterInt(values[6]), combatParameterInt(values[7]), attackOnly)
			total += value
			// 50910 compares each enemy/player cell, not a per-player total
			// across all enemies. Ties deduplicate player IDs in encounter order.
			if value > highest {
				highest, candidates = value, candidates[:0]
			}
			if value == highest && !containsInt(candidates, player.MemberType) {
				candidates = append(candidates, player.MemberType)
			}
		}
	}
	lower, upper := int64(combatParameterInt(values[1])), int64(combatParameterInt(values[2]))
	if total < lower || upper != 0 && total > upper {
		return false
	}
	if len(candidates) > 0 {
		engine.retainEnemyTriggerTarget(candidates)
	}
	return true
}
