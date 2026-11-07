package battlecore

import (
	"fmt"
	"strings"
)

// Support slots are CARD_TYPE 11..20, independent of the ten-card draw pool.
// Preserve holes: an equipped third support card must remain CARD_TYPE 13.
func ValidateSupportCards(cards []BattleCard) error {
	var occupied [10]bool
	for _, card := range cards {
		if card.CardType < 11 || card.CardType > 20 || card.CardID <= 0 || card.Level <= 0 || card.Love < 0 {
			return fmt.Errorf("invalid support card slot %d", card.CardType)
		}
		if occupied[card.CardType-11] {
			return fmt.Errorf("duplicate support card slot %d", card.CardType)
		}
		occupied[card.CardType-11] = true
	}
	return nil
}

// CardCsvData.getLovePer -> BattlePluginMgr.SupportSkillIndexGet -> native
// 29837. The partitions come from support_skill_lvup.csv, not card rarity.
func (c *CombatCatalog) CardSupportSkill(card BattleCard) (CombatSkillDefinition, []CombatSkillRole, error) {
	definition, ok := c.Cards[card.CardID]
	if !ok {
		return CombatSkillDefinition{}, nil, fmt.Errorf("support card %d is unavailable", card.CardID)
	}
	loveRate := c.cardLoveRate(card)
	index := NativeSupportSkillIndex(c.SupportLoveRates, loveRate)
	return c.cardSupportSkillByID(definition.SupportSkillIDs[index])
}

// CardData.love_rate is shared by support partitions and PVP ordering.
func (c *CombatCatalog) cardLoveRate(card BattleCard) int {
	if card.HasLoveRate {
		return card.LoveRate
	}
	definition := c.Cards[card.CardID]
	if definition.LoveMax <= 0 {
		return 100
	}
	return minInt(100, maxInt(0, int(float32(card.Love*100)/float32(definition.LoveMax))))
}

// NativeSupportSkillIndex shares the original threshold selection with the
// client. Catalog/ABI callers validate the three supplied master thresholds.
func NativeSupportSkillIndex(partitions [3]int, loveRate int) int {
	index := 0
	for _, threshold := range partitions {
		if loveRate < threshold {
			break
		}
		index++
	}
	return index
}

func (c *CombatCatalog) cardSupportSkillByID(id int) (CombatSkillDefinition, []CombatSkillRole, error) {
	if id == 0 {
		return CombatSkillDefinition{}, nil, nil
	}
	variants := c.Skills[id]
	if len(variants) != 1 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("support skill %d requires one native passive definition", id)
	}
	skill := variants[0]
	roles := c.SkillRoles[skill.FunctionID]
	if len(roles) == 0 {
		return CombatSkillDefinition{}, nil, fmt.Errorf("support skill %d has no roles", id)
	}
	return skill, roles, nil
}

// Gate only the four EX definitions actually referenced by card master.
// The support table also contains unreferenced roles from other systems.
func (c *CombatCatalog) ValidateCardSupportFunctionCoverage() error {
	seen := make(map[int]bool)
	for _, card := range c.Cards {
		for _, id := range card.SupportSkillIDs {
			if id == 0 || seen[id] {
				continue
			}
			seen[id] = true
			skill, roles, err := c.cardSupportSkillByID(id)
			if err != nil {
				return err
			}
			if (skill.Target != "SELF" && skill.Target != "USER_ALL") || skill.BranchCondition != "" || skill.BranchCondition2 != "" {
				return fmt.Errorf("support skill %d has unsupported target/branch", id)
			}
			for _, role := range roles {
				if role.Target != "SELECT" || role.ExcludeSelf {
					return fmt.Errorf("support skill %d role %s has unsupported target", id, role.Function)
				}
				effect, err := sphereSupportEffect(role, 1, 0, 1)
				if err != nil {
					return fmt.Errorf("support skill %d: %w", id, err)
				}
				if _, ok := battleBuffCodes[role.Function]; !ok || sphereSupportAttributeFlags(effect.Attribute) == 0 {
					return fmt.Errorf("support skill %d role %s has unsupported buff/attribute", id, role.Function)
				}
			}
		}
	}
	return nil
}

// Native 663a4/5e700 installs each user's EX cards in slot order, followed
// by that user's CHALICE. All are durable PASSIVE-list effects, not stats.
func (engine *BattleEngine) executePlayerSupportPassives() ([]BattleResult, error) {
	var results []BattleResult
	for _, owner := range engine.playerStates() {
		if owner.HP <= 0 || owner.GameOver {
			continue
		}
		for _, card := range owner.SupportDeck {
			if card.CardID == 0 {
				continue
			}
			skill, roles, err := engine.catalog.CardSupportSkill(card)
			if err != nil {
				return nil, err
			}
			if skill.ID == 0 {
				continue
			}
			target := owner.MemberType
			if strings.EqualFold(skill.Target, "USER_ALL") {
				target = 0
			}
			rows, err := engine.executePlayerCardPassive(owner, battleAction{
				memberType: owner.MemberType, cardType: card.CardType, cardID: card.CardID,
				cardLevel: card.ExecutionLevel(skill.ID), target: target, skill: skill, roles: roles,
			})
			if err != nil {
				return nil, err
			}
			results = append(results, rows...)
		}
		sphereResults, err := engine.executeSphereSupportPassive(owner)
		if err != nil {
			return nil, err
		}
		results = append(results, sphereResults...)
		cardResults, err := engine.executeMainCardPassives(owner)
		if err != nil {
			return nil, err
		}
		results = append(results, cardResults...)
	}
	return results, nil
}

// 434ed is inclusive at both ends; an upper bound of zero means unbounded.
func supportCostMatches(effect battleEffect, cost int) bool {
	return cost >= effect.CostMin && (effect.CostMax == 0 || cost <= effect.CostMax)
}

// 4350b treats either ATTR_NULL operand as unrestricted. The enemy's damage
// resistance matcher is intentionally separate from this skill filter.
func supportAttributeMatches(filter, attribute string) bool {
	return attribute == "" || strings.EqualFold(attribute, "NULL") || damageAttributeMatches(filter, attribute)
}

// Native 45d30/822a0 applies the caster's HEAL_BOOST before the recipient's
// reverse/cut effects. Unlike damage boost, this accumulator has no 3000 cap.
func supportHealValue(effects []battleEffect, skill CombatSkillDefinition, value int) int {
	fixed, rate := 0, 0
	for _, effect := range effects {
		if effect.Function == "HEAL_BOOST" && effect.ListType == 1 && effect.Remaining > 0 &&
			supportAttributeMatches(effect.Attribute, skill.Attribute) && supportCostMatches(effect, skill.Cost) {
			fixed += effect.Value
			rate += effect.Rate
		}
	}
	return int(int64(value+fixed) * int64(1000+rate) / 1000)
}
