package battlecore

import "strings"

// d0eda/d0d16 and d3aba/d3930 inspect CardData's per-slot executed flag.
// They include earlier plays by a member who subsequently died. The native
// ten-card scan counts each slot once and chooses kind using its owner's job.
func (engine *BattleEngine) playedCardCondition(actor *battlePlayer, condition string, parameters [5]string) bool {
	byKind := strings.HasSuffix(condition, "SKILL_KIND")
	wanted := strings.ToUpper(parameters[0])
	if byKind {
		switch wanted {
		case "ATTACK", "SORCERY", "RECOVERY", "SUPPORT", "DEFENSE", "JAMMING", "SPECIAL":
		default:
			return false
		}
	}
	count := 0
	party := engine.playerParty(actor.MemberType)
	for i := range party {
		player := &party[i]
		if strings.HasPrefix(condition, "SELF_") && player.MemberType != actor.MemberType {
			continue
		}
		for _, card := range player.Deck {
			if card.CardType < 1 || card.CardType > 10 || !player.PlayedCardTypes[card.CardType] {
				continue
			}
			if byKind {
				skill, _, err := engine.catalog.CardSkill(card.CardID, player.ArthurType)
				if err == nil && skill.Kind == wanted {
					count++
				}
			} else if combatCardHasProfileTag(engine.catalog.Cards[card.CardID],
				combatParameterInt(parameters[0]), combatParameterInt(parameters[1]), combatParameterInt(parameters[2])) {
				count++
			}
		}
	}
	threshold := parameters[1]
	if !byKind {
		threshold = parameters[3]
	}
	return count >= combatParameterInt(threshold)
}

// 49e3e: any nonzero selector may match any of the eight profile tags.
// A card contributes once, even if several selectors/tags match.
func combatCardHasProfileTag(card CombatCardDefinition, wanted ...int) bool {
	for _, tag := range card.ProfileTags {
		if tag == 0 {
			continue
		}
		for _, selector := range wanted {
			if selector != 0 && selector == tag {
				return true
			}
		}
	}
	return false
}
