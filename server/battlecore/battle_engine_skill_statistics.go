package battlecore

import "strings"

// Ordinary TurnPhase clears these histories; PVP retains them across turns.
// Successful debuffs belong to the caster; positive parameter values belong
// to the receiver and retain the largest single value, not their sum.
type battleSkillStatistics struct {
	Debuffs         [skillRoleDebuffKindCount]uint32
	ParameterMax    [7]int32
	AttackDamage    [10][3]int64                   // 5a53c: attribute x PHYSICS/MAGIC/ALL.
	EnchantDamage   [10]int64                      // 5a664, separately added by d2f16.
	Recovery        int64                          // 70ac3: caster's direct healing, regeneration and drain.
	ReleasedBuffs   [skillRoleBuffKindCount]bool   // 7e870: recipient's typed release flags, set by 70bfc.
	ReleasedDebuffs [skillRoleDebuffKindCount]bool // 7e8fa; natural expiry and random/old release do not set them.
	AttackCount     uint32                         // 7e86c; committed after the complete skill, including enchant hits.
	Died            bool                           // 7e6c0; cleared by GUTS/TurnPhase, retained by ordinary REVIVE.
}

func (engine *BattleEngine) memberSkillStatistics(member int) *battleSkillStatistics {
	if player := engine.parameterState(member); player != nil {
		return &player.TurnSkillStats
	}
	if member >= 5 && member < 5+engine.enemyCount {
		return &engine.enemies[member-5].TurnSkillStats
	}
	return nil
}

func skillStatisticsParameter(parameter string) int {
	switch strings.ToUpper(parameter) {
	case "HP":
		return 0
	case "MAX_HP":
		return 1
	case "ATK":
		return 2
	case "INT":
		return 3
	case "MND":
		return 4
	case "DEF":
		return 5
	case "MDEF":
		return 6
	default:
		return -1
	}
}

func (engine *BattleEngine) recordSkillStatistics(member int, effect battleEffect) {
	code, known := battleBuffCodes[effect.Function]
	if !known {
		return
	}
	// DEAL_PENALTY uses the independent draw consumer, just as DEAL_BONUS
	// does not produce BUFF_EXEC. Enum presence alone does not count it.
	if code != 408 {
		kind := branchStatusKind(effect, false)
		if kind >= 0 && kind < skillRoleDebuffKindCount {
			if stats := engine.memberSkillStatistics(effect.Source); stats != nil {
				stats.Debuffs[kind]++
			}
		}
	}
	// Only the ordinary parameter consumers call 45070. Limit increases,
	// healing, regeneration, critical rates and status boosts do not.
	switch code {
	case 0, 1, 4, 5, 8, 100, 101, 102, 103, 104, 109, 110, 115:
		index := skillStatisticsParameter(effect.Parameter)
		value := int32(effect.Delta)
		// Accepted positive values still enter the statistic when HP's panel
		// mapping is inert. Debuffs retain their signed parameter delta.
		if effect.Kind == 1 {
			value = int32(effect.Value)
		}
		if index >= 0 && value > 0 {
			if stats := engine.memberSkillStatistics(member); stats != nil && value > stats.ParameterMax[index] {
				stats.ParameterMax[index] = value
			}
		}
	}
}

func (engine *BattleEngine) branchSkillStatistic(condition string, actor, target int, parameters [5]string) bool {
	if condition == "DEBUFF_EXEC" {
		stats := engine.memberSkillStatistics(actor)
		if stats == nil {
			return false
		}
		for _, value := range parameters {
			kind := combatTriggerDebuffKind(value)
			if kind > 0 && kind < len(stats.Debuffs) && int32(stats.Debuffs[kind]) > 0 {
				return true
			}
		}
		return false
	}
	index := skillStatisticsParameter(parameters[0])
	if index < 0 {
		return false
	}
	matches := func(member int) bool {
		stats := engine.memberSkillStatistics(member)
		return stats != nil && int(stats.ParameterMax[index]) >= combatParameterInt(parameters[1])
	}
	switch condition {
	case "SELF_PARAM_UP_VALUE":
		return matches(actor)
	case "TARGET_PARAM_UP_VALUE":
		return matches(target)
	case "USER_SIDE_PARAM_UP_VALUE":
		// Living players in the judging party; ordinary enemy callers inspect users.
		for _, p := range engine.playerParty(actor) {
			if p.MemberType != 0 && p.HP > 0 && matches(p.MemberType) {
				return true
			}
		}
	}
	return false
}
