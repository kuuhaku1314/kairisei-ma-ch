package battlecore

import (
	"fmt"
	"strings"
)

// JP NEED_COST_UP attaches each accepted role to a physical hand card. It is
// independent of the Buddy modifier list and does not set its CARD_STATE bit.
type battleCardCostIncrease struct {
	Amount, Remaining int
	SkillID, Serial   int
}

func (player *battlePlayer) cardCostIncrease(cardType int) int {
	if cardType < 1 || cardType >= len(player.CardCostIncreases) {
		return 0
	}
	total := 0
	for _, entry := range player.CardCostIncreases[cardType] {
		total += entry.Amount
	}
	return total
}

func tickCardCostIncreases(player *battlePlayer) {
	for cardType, entries := range player.CardCostIncreases {
		kept := entries[:0]
		for _, entry := range entries {
			entry.Remaining--
			if entry.Remaining > 0 {
				kept = append(kept, entry)
			}
		}
		player.CardCostIncreases[cardType] = kept
	}
}

func (engine *BattleEngine) executeEnemyCardCostIncrease(actor *battleEnemy, selected int, role CombatSkillRole) ([]BattleResult, error) {
	if role.Target != "SELECT_TARGET_HAND_ALL" {
		return nil, fmt.Errorf("unsupported enemy cost increase target %q", role.Target)
	}
	amount := combatParameterInt(role.Parameters[1])
	if amount <= 0 {
		return nil, nil // Original 9657a rejects before notification and RNG.
	}
	// The role selects cards from each member already chosen by the outer
	// skill. Preserve that shared target resolution, including all-player skills.
	targetRole := role
	targetRole.Target = "SELECT"
	var results []BattleResult
	for _, member := range engine.enemyRolePlayerTargets(actor, selected, targetRole) {
		player := engine.player(member)
		candidates := make([]int, 0, len(player.Hand))
		for _, slot := range player.Hand {
			if slot == 0 {
				continue
			}
			card := player.handSlotCard(slot)
			skill, _, err := engine.handCardSkill(player, card)
			if err != nil {
				return nil, err
			}
			// 3bbf8 checks attribute, kind, either tag and original card cost.
			if !combatNullValue(role.Parameters[2]) && !combatAttributeMatches(skill.Attribute, role.Parameters[2]) ||
				!engine.cardDiscountMatches(player, card.CardType, role) {
				continue
			}
			candidates = append(candidates, card.CardType)
		}
		if len(candidates) == 0 {
			continue
		}
		wanted := minInt(maxInt(0, combatParameterInt(role.Parameters[8])), len(candidates))
		chosen := make([]int, 0, wanted)
		for i := 0; i < wanted; i++ {
			// 9673a consumes one xor128 value even for the last candidate.
			index := int(engine.rng.next() % uint32(len(candidates)))
			chosen = append(chosen, candidates[index])
			candidates[index] = candidates[len(candidates)-1]
			candidates = candidates[:len(candidates)-1]
		}
		results = append(results, BattleResult{Command: CommandCardNeedCostUp, Args: []int64{
			int64(member), int64(role.RoleIndex), int64(combatAttributeCode(role.Parameters[2])),
			int64(combatSkillKindCode(role.Parameters[3])),
			int64(combatParameterInt(role.Parameters[4])), int64(combatParameterInt(role.Parameters[5])),
			int64(combatParameterInt(role.Parameters[6])), int64(combatParameterInt(role.Parameters[7])),
		}})
		for _, cardType := range chosen {
			player.CardCostIncreases[cardType] = append(player.CardCostIncreases[cardType], battleCardCostIncrease{
				Amount: amount, Remaining: combatParameterInt(role.Parameters[0]),
				SkillID: role.SourceSkillID, Serial: engine.nativeSkillSerial,
			})
			// Immediate notification names the actor; UserPhase refresh names
			// the holder. Both are distinct original native result contexts.
			results = append(results, BattleResult{Command: CommandAddCardBuff, Args: []int64{
				int64(actor.MemberType), int64(cardType), int64(role.SourceSkillID), int64(engine.nativeSkillSerial),
			}})
		}
	}
	return results, nil
}

func cardCostIncreaseResults(player *battlePlayer) []BattleResult {
	var results []BattleResult
	for _, slot := range player.Hand {
		if slot == 0 {
			continue
		}
		cardType := player.handSlotCard(slot).CardType
		for _, entry := range player.CardCostIncreases[cardType] {
			results = append(results, BattleResult{Command: CommandAddCardBuff, Args: []int64{
				int64(player.MemberType), int64(cardType), int64(entry.SkillID), int64(entry.Serial),
			}})
		}
	}
	return results
}

func combatSkillKindCode(value string) int {
	return map[string]int{"ATTACK": 1, "SORCERY": 2, "RECOVERY": 3, "SUPPORT": 4, "DEFENSE": 5, "JAMMING": 6, "SPECIAL": 7}[strings.ToUpper(value)]
}

// Ordinary NEED_COST_DOWN is a member status, separate from the CARD-list
// modifiers cleared when an individual card leaves the hand.
func passiveCostDiscountEffect(role CombatSkillRole, turn, source int) battleEffect {
	return battleEffect{Function: role.Function, Value: combatParameterInt(role.Parameters[1]),
		Remaining: combatParameterInt(role.Parameters[0]), Kind: 1,
		Attribute: role.Parameters[2], SkillKind: role.Parameters[3],
		Parameters: [4]int{combatParameterInt(role.Parameters[4]), combatParameterInt(role.Parameters[5])},
		CostMin:    combatParameterInt(role.Parameters[6]), CostMax: combatParameterInt(role.Parameters[7]),
		AppliedTurn: turn, Source: source, SourceSkillID: role.SourceSkillID, RoleIndex: role.RoleIndex}
}

func (engine *BattleEngine) cardCostReduction(player *battlePlayer, cardType, baseCost int) int {
	reduction := player.CardCostDown[cardType]
	if cardType < 1 || cardType > len(player.Deck) && !isTokenCardType(cardType) {
		return reduction
	}
	card := player.handSlotCard(cardType)
	skill, _, err := engine.handCardSkill(player, card)
	if err != nil {
		return reduction
	}
	definition := engine.catalog.Cards[card.CardID]
	for _, effect := range player.Effects {
		if effect.Function != "NEED_COST_DOWN" || effect.ListType != 1 && effect.Remaining < 0 ||
			effect.CostCardType != 0 && effect.CostCardType != cardType ||
			!supportAttributeMatches(effect.Attribute, skill.Attribute) ||
			!combatNullValue(effect.SkillKind) && effect.SkillKind != skill.Kind || !supportCostMatches(effect, baseCost) {
			continue
		}
		// The first VALUE is the original filter_type_cast Arthur filter,
		// not a second card tag. NULL skill Arthur is matched by filter5.
		filter, tag := effect.Parameters[0], effect.Parameters[1]
		if filter != 0 && (combatNullValue(skill.ArthurJob) && filter != 5 ||
			!combatNullValue(skill.ArthurJob) && (filter > 5 || filter < 1 || ArthurJob(filter) != skill.ArthurJob)) {
			continue
		}
		if tag == 0 || combatCardHasProfileTag(definition, tag) {
			reduction += effect.Value
		}
	}
	return reduction
}

func (engine *BattleEngine) effectiveCardCost(player *battlePlayer, cardType, baseCost int) int {
	if cardType < 0 || cardType >= len(player.CardCostDown) {
		return maxInt(0, baseCost)
	}
	return maxInt(0, baseCost-engine.cardCostReduction(player, cardType, baseCost)+player.cardCostIncrease(cardType))
}
