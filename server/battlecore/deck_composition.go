package battlecore

import (
	"errors"
	"fmt"

	"kairisei.local/server/battlemodel"
)

// The original CardCsvData.isRoundTable checks this profile group identity.
// It does not infer the restriction from a card's name, rarity or family.
const RoundTableCardGroup = 20020001

var (
	ErrMultipleRoundTableCards = errors.New("a main deck can contain only one Round Table knight")
	ErrTooManySummonCards      = errors.New("an alchemist main deck can contain at most five summon knights")
	ErrAlchemistDeckProfession = errors.New("an alchemist main deck can contain only common or alchemist knights")
)

// ValidateMainDeckComposition reads the same immutable definitions as battle
// preparation. Empty slots are allowed while editing; capacity, ownership,
// family conflicts and support-card rules belong to their existing validators.
func ValidateMainDeckComposition(arthurType int, cardIDs []int, definitions *battlemodel.Definitions) error {
	if definitions == nil {
		return errors.New("deck composition definitions are unavailable")
	}
	roundTables, summons := 0, 0
	for _, id := range cardIDs {
		if id == 0 {
			continue
		}
		card, exists := definitions.Cards[id]
		if !exists {
			return fmt.Errorf("deck composition card %d is unavailable", id)
		}
		// Deck.IsExitDeckScene uses GetSameCardGroupID, which counts
		// matching profile entries rather than only matching cards.
		for _, group := range card.ProfileTags {
			if group == RoundTableCardGroup {
				roundTables++
			}
		}
		if card.SummonID != 0 {
			summons++
		}
	}
	if roundTables > 1 {
		return ErrMultipleRoundTableCards
	}
	if arthurType == 5 && summons > 5 {
		return ErrTooManySummonCards
	}
	return nil
}

// ValidateAlchemistMainDeckProfession is the active-deck predicate from the
// client. Resolve profession through the base Arthur skill, as CardCsvData
// does, without copying another profession field into cards or player saves.
// Callers apply it on activation or battle entry, not to inactive edit drafts.
func ValidateAlchemistMainDeckProfession(cardIDs []int, definitions *battlemodel.Definitions) error {
	if definitions == nil {
		return errors.New("deck composition definitions are unavailable")
	}
	for _, id := range cardIDs {
		if id == 0 {
			continue // Slot completeness belongs to the existing deck validator.
		}
		card, exists := definitions.Cards[id]
		if !exists {
			return fmt.Errorf("deck composition card %d is unavailable", id)
		}
		variants := definitions.Skills[card.ArthurSkillID]
		if card.ArthurSkillID == 0 || len(variants) == 0 {
			continue // CardCsvData.GetArthurType returns NULL when absent.
		}
		if job := variants[0].Job; job != "NULL" && job != "ALCHEMIST" {
			return ErrAlchemistDeckProfession
		}
	}
	return nil
}
