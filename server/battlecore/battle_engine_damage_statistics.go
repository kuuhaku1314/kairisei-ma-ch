package battlecore

import "strings"

func (engine *BattleEngine) recordSkillDamage(member, value int, attribute, physics string, enchant bool) {
	stats := engine.memberSkillStatistics(member)
	if stats == nil || value <= 0 {
		return
	}
	attr := combatAttributeCode(attribute)
	if enchant {
		stats.EnchantDamage[attr] += int64(value)
	} else {
		stats.AttackDamage[attr][combatPhysicsIndex(physics)] += int64(value)
	}
}

func (engine *BattleEngine) recordRecoveryAction(source, value int) {
	if value > 0 {
		if stats := engine.memberSkillStatistics(source); stats != nil {
			stats.Recovery += int64(value)
		}
	}
}

func statisticRange(value int64, lower, upper string) bool {
	return value >= int64(combatParameterInt(lower)) && (combatParameterInt(upper) == 0 || value <= int64(combatParameterInt(upper)))
}

func (engine *BattleEngine) receivedSkillDamage(member int, attribute, physics string, enchantOnly bool) (int64, bool) {
	stats := engine.memberSkillStatistics(member)
	if stats == nil {
		return 0, false
	}
	attribute, physics = strings.ToUpper(strings.TrimSpace(attribute)), strings.ToUpper(strings.TrimSpace(physics))
	if physics == "" {
		physics = "PHYSICS"
	} // Managed empty enum defaults to 0.
	if !nativeAIDamageFiltersSupported("NULL", physics, "NULL") {
		return 0, false
	}
	if !nativeAIDamageFiltersSupported(attribute, physics, "NULL") {
		// Valid dual ATTR enums never equal 701aa/70296's ten scalar buckets.
		switch attribute {
		case "FIRE_ICE", "FIRE_WIND", "FIRE_LIGHT", "FIRE_DARK", "ICE_WIND", "ICE_LIGHT", "ICE_DARK", "WIND_LIGHT", "WIND_DARK", "LIGHT_DARK":
			return 0, true
		default:
			return 0, false
		}
	}
	var value int64
	wanted, physical := combatAttributeCode(attribute), combatPhysicsIndex(physics)
	for attr, direct := range stats.AttackDamage {
		if wanted != 0 && wanted != attr {
			continue
		}
		value += stats.EnchantDamage[attr]
		if !enchantOnly {
			for kind, amount := range direct {
				if physical == 2 || kind == 2 || kind == physical {
					value += amount
				}
			}
		}
	}
	return value, true
}

func (engine *BattleEngine) branchDamageStatistic(condition string, actor int, parameters [5]string) bool {
	if condition == "RECOVERY_ACTION" {
		stats := engine.memberSkillStatistics(actor)
		return stats != nil && statisticRange(stats.Recovery, parameters[0], parameters[1])
	}
	if condition == "SELF_ENCHANT_DAMAGE" {
		value, ok := engine.receivedSkillDamage(actor, parameters[0], "ALL", true)
		return ok && statisticRange(value, parameters[1], parameters[2])
	}
	matches := func(member int) bool {
		value, ok := engine.receivedSkillDamage(member, parameters[1], parameters[0], false)
		return ok && statisticRange(value, parameters[2], parameters[3])
	}
	if condition == "SELF_DAMAGE" {
		return matches(actor)
	}
	if condition == "FRIEND_DAMAGE" {
		// Living players in the judging party; ordinary enemy callers inspect users.
		for _, p := range engine.playerParty(actor) {
			if p.MemberType != 0 && p.HP > 0 && matches(p.MemberType) {
				return true
			}
		}
	}
	return false
}
