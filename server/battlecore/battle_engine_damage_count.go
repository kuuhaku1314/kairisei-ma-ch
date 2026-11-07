package battlecore

import "strings"

// COUNT2 retains one of the two original BATTLE_BUFF identities. The public
// SkillRole and shared catalog keep DAMAGE_BOOST_COUNT2 unchanged.
func countedDamageStatusRole(role CombatSkillRole) CombatSkillRole {
	role.Function = "MAGIC_DAMAGE_BOOST_COUNT"
	if strings.EqualFold(role.Parameters[5], "PHYSICS") {
		role.Function = "PHYSICS_DAMAGE_BOOST_COUNT"
	}
	return role
}

func isCountedDamageStatus(function string) bool {
	return function == "PHYSICS_DAMAGE_BOOST_COUNT" || function == "MAGIC_DAMAGE_BOOST_COUNT"
}

func countedDamageRate(role CombatSkillRole, level int) int {
	return int(int32(combatParameterInt(role.Parameters[2])) + int32(combatParameterInt(role.Parameters[3]))*int32(level))
}

func configureCountedDamage(effect *battleEffect, role CombatSkillRole, level int) {
	effect.Parameter = ""
	effect.Uses = int(int32(combatParameterInt(role.Parameters[1])))
	effect.Rate = countedDamageRate(role, level)
	effect.Value = effect.Rate
	effect.Attribute = strings.ToUpper(role.Parameters[4])
	effect.DamageKind = strings.ToUpper(role.Parameters[5])
}

func countedDamageSnapshot(effects []battleEffect, attribute, physics string) []battleEffect {
	var snapshot []battleEffect
	for _, effect := range effects {
		if isCountedDamageStatus(effect.Function) &&
			supportAttributeMatches(effect.Attribute, attribute) && damagePhysicsMatches(effect.DamageKind, physics) {
			// Membership is active even at duration0 before the expiry boundary.
			snapshot = append(snapshot, effect)
		}
	}
	return snapshot
}

func countedDamageHitRate(snapshot []battleEffect, hit int) int {
	var rate int32
	for _, effect := range snapshot {
		if effect.ListType == 0 && effect.Uses > 0 && hit >= effect.Uses {
			continue
		}
		rate += int32(effect.Rate)
	}
	return int(rate)
}

// Native3ad86/3b705 retain the largest used hit ordinal across recipients, then
// commit it once at the attack-role boundary. Passive counters are not consumed.
func commitCountedDamage(player *battlePlayer, snapshot []battleEffect, hits int) []BattleResult {
	var results []BattleResult
	var expired []battleEffect
	for _, used := range snapshot {
		if used.ListType != 0 || used.Uses <= 0 {
			continue
		}
		for i := range player.Effects {
			current := &player.Effects[i]
			if current.ListType != used.ListType || current.Function != used.Function {
				continue
			}
			current.Uses = maxInt(0, current.Uses-minInt(hits, used.Uses))
			results = append(results, BattleResult{Command: 200, Args: []int64{
				int64(player.MemberType), int64(battleBuffCodes[current.Function]), int64(current.Uses), int64(combatAttributeCode(current.Attribute)),
			}})
			if current.Uses == 0 {
				expired = append(expired, *current)
				player.Effects = append(player.Effects[:i], player.Effects[i+1:]...)
			}
			break
		}
	}
	if len(expired) > 0 {
		results = appendNaturalExpiryRows(results, player.MemberType, expired, player.Effects, player.Attribute)
		results = append(results, BattleResult{Command: CommandBattleParam, Args: battleParameterArgs(
			player.MemberType, player.HP, player.MaxHP, player.Attack, player.Magic, player.Recovery,
			player.Defense, player.MDefense, player.LimitAttack, player.LimitMagic, player.LimitRecovery)})
	}
	return results
}
