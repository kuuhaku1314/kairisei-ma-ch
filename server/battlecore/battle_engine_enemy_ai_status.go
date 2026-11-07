package battlecore

// 5a1be/5a5c8 and the side-wide variants scan actor lists 0..6; the
// by-user-one predicates 50d80/54cf0 deliberately scan NORMAL only.
// Presence is authoritative: no Remaining or coarse effect.Kind filter.
func enemyAIHasCurrentStatus(effects []battleEffect, good bool, values []string, normalOnly, requireAll bool) bool {
	lastList := 6
	if normalOnly {
		lastList = 0
	}
	return enemyAIHasStatusInLists(effects, good, values, 0, lastList, requireAll)
}

func enemyAIHasStatusInLists(effects []battleEffect, good bool, values []string, firstList, lastList int, requireAll bool) bool {
	wanted := make(map[int]bool, len(values))
	for _, value := range values {
		kind := combatTriggerDebuffKind(value)
		if good {
			kind = combatTriggerBuffKind(value)
		}
		if requireAll && kind == 0 {
			// 59a82 premarks NULL as satisfied; ordinary OR selectors can
			// match a known parameter entry with an actual zero delta.
			continue
		}
		wanted[kind] = false
	}
	for _, effect := range effects {
		if effect.ListType < firstList || effect.ListType > lastList {
			continue
		}
		kind := branchStatusKind(effect, good)
		if _, exists := wanted[kind]; kind >= 0 && exists {
			if !requireAll {
				return true
			}
			wanted[kind] = true
		}
	}
	if !requireAll {
		return false
	}
	for _, found := range wanted {
		if !found {
			return false
		}
	}
	return true
}

func (engine *BattleEngine) userHandCountMatches(job, lower, upper string) bool {
	wantedArthur := combatArthurType(job)
	candidates := make([]int, 0, maxRoomMembers)
	for _, player := range engine.players {
		if player.HP <= 0 || wantedArthur != 0 && player.ArthurType != wantedArthur {
			continue
		}
		count := combatHandCount(&player)
		if count >= combatParameterInt(lower) && count <= combatParameterInt(upper) {
			candidates = append(candidates, player.MemberType)
		}
	}
	return engine.retainEnemyTriggerTarget(candidates)
}
