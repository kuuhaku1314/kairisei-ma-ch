package battlecore

import "strings"

func isAttributeGuard(function string) bool {
	switch function {
	case "ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK", "ATTR_CUT_ENCHANT", "ATTR_DRAIN_ENCHANT", "ATTR_CUT_DOT", "ATTR_DRAIN_DOT":
		return true
	default:
		return false
	}
}

func attributeGuardRate(role CombatSkillRole, level int) int {
	// The six producers use ordinary level growth and ignore Chain.
	return int(int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(level))
}

func attributeGuardPower(function string, rate int) int {
	if strings.HasPrefix(function, "ATTR_CUT_") {
		return rate / 10
	}
	return rate
}

func configureAttributeGuard(effect *battleEffect, role CombatSkillRole, level int) {
	effect.Parameter, effect.Attribute, effect.DamageKind = "", "", ""
	effect.Rate = attributeGuardRate(role, level)
	effect.Value = effect.Rate
	effect.Uses = combatParameterInt(role.Parameters[1])
	if strings.HasSuffix(role.Function, "_DOT") {
		effect.Parameter = strings.ToUpper(role.Parameters[4])
	} else {
		effect.Attribute = strings.ToUpper(role.Parameters[4])
	}
	if strings.HasSuffix(role.Function, "_ATTACK") {
		effect.DamageKind = strings.ToUpper(role.Parameters[5])
	}
}

func attributeGuardDotCode(effect battleEffect) int {
	if effect.Parameter == "TRAP" {
		return 6
	}
	return combatDOTIndex(effect.Parameter) + 1
}

func attributeGuardInitialParameters(effect battleEffect) [4]int {
	parameters := [4]int{}
	switch effect.Function {
	case "ATTR_CUT_ATTACK", "ATTR_DRAIN_ATTACK":
		parameters[0] = combatPhysicsIndex(effect.DamageKind)
	case "ATTR_DRAIN_ENCHANT":
		parameters[0] = 2
	case "ATTR_CUT_DOT":
		parameters[1] = attributeGuardDotCode(effect)
	case "ATTR_DRAIN_DOT":
		parameters[0], parameters[1] = 2, attributeGuardDotCode(effect)
	}
	return parameters
}

// A zero use counter is unlimited; a negative counter never matches. Natural
// duration and the finite hit counter are independent. Matching is by damage
// family, so an ordinary cut never consumes an enchant or retained DOT hit.
func consumeAttributeGuard(effects *[]battleEffect, function string, attribute string, physics string, dot string) (battleEffect, *battleEffect, bool) {
	for index := range *effects {
		effect := &(*effects)[index]
		if effect.Function != function || effect.Remaining <= 0 || effect.Uses < 0 {
			continue
		}
		if strings.HasSuffix(function, "_DOT") {
			if effect.Parameter != "" && effect.Parameter != "NULL" && effect.Parameter != dot {
				continue
			}
		} else if !damageAttributeMatches(effect.Attribute, attribute) || !damagePhysicsMatches(effect.DamageKind, physics) {
			continue
		}
		var expired *battleEffect
		if effect.Uses > 0 {
			effect.Uses--
			if effect.Uses == 0 {
				copy := *effect
				expired = &copy
			}
		}
		matched := *effect
		if expired != nil {
			*effects = append((*effects)[:index], (*effects)[index+1:]...)
		}
		return matched, expired, true
	}
	return battleEffect{}, nil, false
}

func cutAttributeGuardDamage(effects *[]battleEffect, damage int, suffix string, attribute string, physics string, dot string) (int, *battleEffect) {
	guard, expired, matched := consumeAttributeGuard(effects, "ATTR_CUT_"+suffix, attribute, physics, dot)
	if !matched {
		return damage, nil
	}
	return maxInt(1, int(int64(damage)*int64(1000-guard.Rate)/1000)), expired
}

func drainAttributeGuardDamage(effects *[]battleEffect, damage int, hp int, suffix string, attribute string, physics string, dot string) (int, *battleEffect) {
	guard, expired, matched := consumeAttributeGuard(effects, "ATTR_DRAIN_"+suffix, attribute, physics, dot)
	if !matched {
		return damage, nil
	}
	requested := maxInt(0, int(int64(damage)*int64(guard.Rate)/100))
	if suffix == "DOT" {
		return -nativeHealReverseDelta(hp, requested, *effects), expired
	}
	return -nativeHealDelta(hp, requested, *effects), expired
}

func resolveDOTAttributeGuards(effects *[]battleEffect, damage int, hp int, dot string) incomingDamageResolution {
	resolution := incomingDamageResolution{Damage: damage}
	var cutExpired, drainExpired *battleEffect
	resolution.Damage, cutExpired = cutAttributeGuardDamage(effects, damage, "DOT", "", "", dot)
	resolution.Damage, drainExpired = drainAttributeGuardDamage(effects, resolution.Damage, hp, "DOT", "", "", dot)
	if drainExpired != nil {
		resolution.Released = append(resolution.Released, *drainExpired)
	}
	if cutExpired != nil {
		resolution.Released = append(resolution.Released, *cutExpired)
	}
	return resolution
}
