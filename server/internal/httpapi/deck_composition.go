package httpapi

import (
	"fmt"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/game"
)

// Own-profession and rented partner decks use the same immutable rules. This
// check precedes entry costs and does not rewrite another account's saved deck.
func (a *API) validatePartnerDeckComposition(view friendPointPartnerView, index int8) error {
	if a.combatDefinitions == nil {
		return nil
	}
	deck, found := game.ExactDeck(view.Decks, view.ArthurType, index)
	if !found {
		return fmt.Errorf("selected partner deck %d/%d is unavailable", view.ArthurType, index)
	}
	ids := make([]int, len(deck.CardUniqueIDs))
	for slot, uniqueID := range deck.CardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		card, exists := view.Cards[uniqueID]
		if !exists {
			return fmt.Errorf("partner card unique ID %d is unavailable", uniqueID)
		}
		ids[slot] = card.CardID
	}
	return battlecore.ValidateMainDeckComposition(int(view.ArthurType), ids, a.combatDefinitions)
}
