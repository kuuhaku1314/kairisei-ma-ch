package battlecore

import "strings"

func (engine *BattleEngine) recordReleasedSkillStatistics(member int, function string, removed []battleEffect) {
	// 73302(category, record=true) and 73420(typed) call 70bfc on the
	// recipient. OLD/RANDOM, natural expiry and revive use other removers.
	mode := releaseMode(function)
	if mode != "ALL" && mode != "ONE" {
		return
	}
	stats := engine.memberSkillStatistics(member)
	if stats == nil {
		return
	}
	for _, effect := range removed {
		if kind := branchStatusKind(effect, true); kind > 0 && kind < len(stats.ReleasedBuffs) {
			stats.ReleasedBuffs[kind] = true
		}
		if kind := branchStatusKind(effect, false); kind > 0 && kind < len(stats.ReleasedDebuffs) {
			stats.ReleasedDebuffs[kind] = true
		}
	}
}

func (engine *BattleEngine) branchReleaseStatistic(condition string, actor, target int, parameters [5]string) bool {
	good := strings.HasSuffix(condition, "_BUFF_RELEASE")
	matches := func(member int) bool {
		stats := engine.memberSkillStatistics(member)
		if stats == nil {
			return false
		}
		// d0741/d10cb read only three OR selectors, ignoring NULL.
		for _, value := range parameters[:3] {
			if good {
				if kind := combatTriggerBuffKind(value); kind > 0 && kind < len(stats.ReleasedBuffs) && stats.ReleasedBuffs[kind] {
					return true
				}
			} else if kind := combatTriggerDebuffKind(value); kind > 0 && kind < len(stats.ReleasedDebuffs) && stats.ReleasedDebuffs[kind] {
				return true
			}
		}
		return false
	}
	switch {
	case strings.HasPrefix(condition, "SELF_"):
		return matches(actor)
	case strings.HasPrefix(condition, "TARGET_"):
		return matches(target)
	case strings.HasPrefix(condition, "USER_SIDE_"):
		// Living players in the judging party; ordinary enemy callers inspect users.
		for _, player := range engine.playerParty(actor) {
			if player.MemberType != 0 && player.HP > 0 && matches(player.MemberType) {
				return true
			}
		}
	}
	return false
}
