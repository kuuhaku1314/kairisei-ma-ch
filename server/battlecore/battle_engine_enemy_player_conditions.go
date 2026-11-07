package battlecore

import (
	"sort"
	"strings"
)

// These predicates use zero as an omitted bound, unlike draw/hand/card counts
// where zero is an actual quantity. Do not merge their interval contracts.
func nativeOptionalRange(value int, lower, upper string) bool {
	low, high := combatParameterInt(lower), combatParameterInt(upper)
	return (low == 0 || value >= low) && (high == 0 || value <= high)
}

func (engine *BattleEngine) userParameterRankMatches(parameter string, rank int, lower, upper string) bool {
	if combatNullValue(parameter) {
		parameter = "HP"
	}
	if rank < 1 {
		return false
	}
	members := engine.playerTargetCandidates(false)
	if len(members) == 0 {
		return false
	}
	// ce887 orders ascending by value then member ID and takes n-rank;
	// unlike USER_PARAM_ONE, this retains one exact member without RNG.
	sort.Slice(members, func(i, j int) bool {
		a, b := combatStatValue(&engine.players[members[i]-1], parameter), combatStatValue(&engine.players[members[j]-1], parameter)
		if a == b {
			return members[i] < members[j]
		}
		return a < b
	})
	member := members[maxInt(0, len(members)-rank)]
	if !nativeOptionalRange(combatStatValue(&engine.players[member-1], parameter), lower, upper) {
		return false
	}
	engine.enemyTriggerTarget = member
	return true
}

func (engine *BattleEngine) userHPRateMatches(job, lower, upper string, rank int) bool {
	members := engine.playerTargetCandidates(false)
	metric := func(member int) int {
		p := &engine.players[member-1]
		if p.MaxHP <= 0 {
			return 0
		}
		return maxInt(0, int(int64(p.HP)*100/int64(p.MaxHP)))
	}
	rankValue := 0
	if rank > 0 {
		if len(members) == 0 {
			return false
		}
		values := make([]int, 0, len(members))
		for _, member := range members {
			values = append(values, metric(member))
		}
		sort.Sort(sort.Reverse(sort.IntSlice(values)))
		rankValue = values[minInt(rank-1, len(values)-1)]
	}
	wanted := combatArthurType(job)
	matching := make([]int, 0, len(members))
	for _, member := range members {
		p := &engine.players[member-1]
		value := metric(member)
		if (wanted == 0 || p.ArthurType == wanted) && (rank <= 0 || value == rankValue) && nativeOptionalRange(value, lower, upper) {
			matching = append(matching, member)
		}
	}
	return engine.retainEnemyTriggerTarget(matching)
}

func (engine *BattleEngine) userDrawRankMatches(job, lower, upper string, rank int) bool {
	members := engine.playerTargetCandidates(false)
	rankValue := 0
	if rank > 0 {
		// 560d0 rejects a missing rank. In particular, the native LOW wrapper
		// asks for rank four and does not fall back to a shorter living list.
		if rank > len(members) {
			return false
		}
		values := make([]int, 0, len(members))
		for _, member := range members {
			values = append(values, playerAIDrawChange(&engine.players[member-1]))
		}
		sort.Sort(sort.Reverse(sort.IntSlice(values)))
		rankValue = values[rank-1]
	}
	wanted := combatArthurType(job)
	matching := make([]int, 0, len(members))
	for _, member := range members {
		p := &engine.players[member-1]
		value := playerAIDrawChange(p)
		if (wanted == 0 || p.ArthurType == wanted) && (rank <= 0 || value == rankValue) && value >= combatParameterInt(lower) && value <= combatParameterInt(upper) {
			matching = append(matching, member)
		}
	}
	return engine.retainEnemyTriggerTarget(matching)
}

func (engine *BattleEngine) userTotalCardCountMatches(attribute, kind, lower, upper string) bool {
	count := 0
	for _, action := range engine.turnActions {
		member := action.memberType
		if member < 1 || member > maxRoomMembers || engine.players[member-1].HP <= 0 || action.cardType == 0 || action.sphereSlot != 0 {
			continue
		}
		if _, submitted := engine.selectedPlays[member]; !submitted {
			continue
		}
		if !combatNullValue(kind) && !strings.EqualFold(action.skill.Kind, kind) {
			continue
		}
		if !combatNullValue(attribute) && !combatAttributeMatches(action.skill.Attribute, attribute) {
			continue
		}
		count++
	}
	return count >= combatParameterInt(lower) && count <= combatParameterInt(upper)
}
