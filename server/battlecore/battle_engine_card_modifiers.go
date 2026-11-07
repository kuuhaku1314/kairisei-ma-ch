package battlecore

import (
	"sort"
	"strings"
)

// All roles in a registered card skill start together, but native list7
// expires each role using its own first parameter. Keep one skill identity
// and its age rather than copying catalog roles into durable battle state.
type battleCardBurstSkill struct {
	SkillID     int
	BranchIndex int
	Age         int
	Serial      int
	Order       int
}

// The native list keeps the roles selected when BurstSkillExec ran. A later
// turn or HP change must not choose a new branch for an existing modifier.
func (engine *BattleEngine) cardModifierRoles(entry battleCardBurstSkill) []CombatSkillRole {
	variants := engine.catalog.Skills[entry.SkillID]
	if entry.BranchIndex < 0 || entry.BranchIndex >= len(variants) {
		return nil
	}
	return engine.catalog.SkillRoles[variants[entry.BranchIndex].FunctionID]
}

func (engine *BattleEngine) cardModifierResults() []BattleResult {
	var results []BattleResult
	for _, p := range engine.playerStates() {
		if p.MemberType == 0 || p.HP <= 0 {
			continue
		}
		type notification struct {
			serial, role, order int
			row                 BattleResult
		}
		var rows []notification
		for cardType, entries := range p.CardBurstSkills {
			for _, entry := range entries {
				for roleIndex, role := range engine.cardModifierRoles(entry) {
					if activeCardModifier(role, entry.Age) {
						rows = append(rows, notification{entry.Serial, roleIndex, entry.Order, BattleResult{Command: CommandAddCardBuff, Args: []int64{int64(p.MemberType), int64(cardType), int64(entry.SkillID), int64(entry.Serial)}}})
					}
				}
			}
		}
		sort.SliceStable(rows, func(i, j int) bool {
			a, b := rows[i], rows[j]
			if a.serial != b.serial {
				return a.serial < b.serial
			}
			if a.order != b.order {
				return a.order < b.order
			}
			return a.role < b.role
		})
		for _, row := range rows {
			results = append(results, row.row)
		}
		results = append(results, cardCostIncreaseResults(p)...)
	}
	return results
}

func activeCardModifier(role CombatSkillRole, age int) bool {
	return burstRoleRegistersCard(role.Function) && (age == 0 || age < combatParameterInt(role.Parameters[0]))
}

// 5eb20 -> 734c0 -> 43f96 silently ages CARD modifiers at turn end, before
// the next turn's AI preview. UserPhase must not clear longer-lived entries.
func (engine *BattleEngine) tickCardModifiers(player *battlePlayer) {
	for cardType, entries := range player.CardBurstSkills {
		var kept []battleCardBurstSkill
		costDown := 0
		for _, entry := range entries {
			entry.Age++
			active := false
			for _, role := range engine.cardModifierRoles(entry) {
				if !activeCardModifier(role, entry.Age) {
					continue
				}
				active = true
				if role.Function == "NEED_COST_DOWN_BURST" && engine.cardDiscountMatches(player, cardType, role) {
					costDown += maxInt(0, combatParameterInt(role.Parameters[1]))
				}
			}
			if active {
				kept = append(kept, entry)
			}
		}
		player.CardBurstSkills[cardType] = kept
		player.CardCostDown[cardType] = costDown
	}
}

// Resolve ordinary cards and tokens through their shared hand skill source;
// all configured attribute, kind, profile-group and cost filters still apply.
func (engine *BattleEngine) cardDiscountMatches(player *battlePlayer, cardType int, role CombatSkillRole) bool {
	if cardType < 1 || cardType > len(player.Deck) && !isTokenCardType(cardType) {
		return false
	}
	card := player.handSlotCard(cardType)
	skill, _, err := engine.handCardSkill(player, card)
	if err != nil {
		return false
	}
	if !combatNullValue(role.Parameters[2]) && !combatAttributeMatches(skill.Attribute, role.Parameters[2]) ||
		!combatNullValue(role.Parameters[3]) && !strings.EqualFold(role.Parameters[3], skill.Kind) {
		return false
	}
	if !combatValueInRange(skill.Cost, role.Parameters[6], role.Parameters[7]) {
		return false
	}
	a, b := combatParameterInt(role.Parameters[4]), combatParameterInt(role.Parameters[5])
	definition := engine.catalog.Cards[card.CardID]
	return a == 0 && b == 0 || a != 0 && combatCardHasProfileTag(definition, a) || b != 0 && combatCardHasProfileTag(definition, b)
}
