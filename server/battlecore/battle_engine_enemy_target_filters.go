package battlecore

import (
	"sort"
	"strings"
)

func isEnemyFilteredTarget(kind string) bool {
	switch kind {
	case "USER_PARAM_BUFF", "USER_PARAM_DEBUFF", "USER_HP", "USER_HP_RATE", "USER_HAND_NUM", "USER_DEBUFF_DUPLICATE_SELECT":
		return true
	}
	return false
}

// Original selectors 52880/52aa0/52d00/537a4/53d6c/55a34 filter living
// players, then use their own fallback and RNG. They are separate from the
// similarly named trigger predicates, which retain a trigger target.
func (engine *BattleEngine) selectEnemyFilteredTarget(action CombatEnemyAction, kind string) (int, bool) {
	values := action.TargetParams
	candidates := engine.playerTargetCandidates(false)
	if kind == "USER_DEBUFF_DUPLICATE_SELECT" {
		filtered := make([]int, 0, len(candidates))
		wanted := combatTriggerDebuffKind(values[0])
		for _, member := range candidates {
			for _, effect := range engine.players[member-1].Effects {
				status := branchStatusKind(effect, false)
				if effect.ListType == 0 && (wanted == 0 && status > 0 || wanted != 0 && status == wanted) {
					filtered = append(filtered, member)
					break
				}
			}
		}
		if len(filtered) > 0 {
			candidates = filtered
		}
		selection := strings.ToUpper(values[1])
		if selection == "" || selection == "RANDOM" {
			return engine.randomFinalizedPlayerTarget(candidates, false)
		}
		parameter := strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(selection, "HIGH_"), "LOW_"), "_USER")
		if len(candidates) == 0 || (!strings.HasPrefix(selection, "HIGH_") && !strings.HasPrefix(selection, "LOW_")) || parameter != "HP" && !combatParameterSupported(parameter) {
			return 0, false
		}
		// ce96a(..., true) uses random low bits for equal metric values.
		// Its c6e10 right-to-left bubble comparisons must be preserved:
		// choosing a random extremum once consumes a different RNG stream.
		engine.sortPlayerMetricTargets(candidates, func(member int) int {
			return combatStatValue(&engine.players[member-1], parameter)
		})
		member := candidates[0]
		if strings.HasPrefix(selection, "HIGH_") {
			member = candidates[len(candidates)-1]
		}
		return engine.finalizeEnemyPlayerTarget(member, false)
	}

	parameterMode := kind == "USER_PARAM_BUFF" || kind == "USER_PARAM_DEBUFF"
	parameter := strings.ToUpper(values[0])
	job := combatArthurType(values[0])
	if parameterMode {
		if parameter != "" && parameter != "NULL" && !combatParameterSupported(parameter) {
			return engine.enemyDefaultRandomTarget(action)
		}
	} else if job == 0 && parameter != "" && parameter != "NULL" {
		return engine.enemyDefaultRandomTarget(action)
	}
	metric := func(member int) int64 {
		player := &engine.players[member-1]
		switch kind {
		case "USER_HP":
			return int64(player.HP)
		case "USER_HP_RATE":
			if player.MaxHP <= 0 {
				return 0
			}
			return max(int64(0), int64(player.HP)*100/int64(player.MaxHP))
		case "USER_HAND_NUM":
			return int64(combatHandCount(player))
		default:
			// 45123 reads the retained signed parameter columns of NORMAL
			// entries, excluding limits; each sum is a native signed int32.
			var total int32
			for _, effect := range player.Effects {
				if effect.ListType != 0 || !strings.EqualFold(effect.Parameter, parameter) || effect.Function == "PARAM_LIMIT_BREAK_FIXED" {
					continue
				}
				delta := int32(effect.Delta)
				if kind == "USER_PARAM_BUFF" && delta > 0 || kind == "USER_PARAM_DEBUFF" && delta < 0 {
					total += delta
				}
			}
			if kind == "USER_PARAM_DEBUFF" {
				total = -total
			}
			return int64(total)
		}
	}
	rank := combatParameterInt(values[3])
	ranked := int64(0)
	if (kind == "USER_HP" || kind == "USER_HP_RATE") && rank > 0 {
		if len(candidates) == 0 {
			return engine.enemyDefaultRandomTarget(action)
		}
		scores := make([]int64, 0, len(candidates))
		for _, member := range candidates {
			scores = append(scores, metric(member))
		}
		sort.Slice(scores, func(i, j int) bool { return scores[i] > scores[j] })
		ranked = scores[minInt(rank-1, len(scores)-1)]
	} else {
		rank = 0
	}
	low, high := int64(combatParameterInt(values[1])), int64(combatParameterInt(values[2]))
	filtered := make([]int, 0, len(candidates))
	for _, member := range candidates {
		if !parameterMode && job != 0 && engine.players[member-1].ArthurType != job {
			continue
		}
		value := metric(member)
		if value >= low && (high <= 0 || value <= high) && (rank == 0 || value == ranked) {
			filtered = append(filtered, member)
		}
	}
	if len(filtered) == 0 {
		return engine.enemyDefaultRandomTarget(action)
	}
	return engine.randomFinalizedPlayerTarget(filtered, false)
}

// JP de907..dec86 compares ordinary user parameters, drawing one low bit
// whenever adjacent values are equal. Preserve d6b50's right-to-left passes:
// ties outside the final extremum consume RNG too.
func (engine *BattleEngine) sortPlayerMetricTargets(candidates []int, metric func(int) int) {
	for boundary := 0; boundary < len(candidates)-1; boundary++ {
		for i := len(candidates) - 2; i >= boundary; i-- {
			left, right := metric(candidates[i]), metric(candidates[i+1])
			swap := left > right
			if left == right {
				swap = engine.rng.next()&1 != 0
			}
			if swap {
				candidates[i], candidates[i+1] = candidates[i+1], candidates[i]
			}
		}
	}
}
