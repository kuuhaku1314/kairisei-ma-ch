package masterdata

import (
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

func validateCardCollection(master CardRuntimeMaster) error {
	if len(master.CardCollectionPages) == 0 || len(master.CardCollectionPages) > len(master.CardTemplates)+len(master.StackCardTemplates) {
		return fmt.Errorf("CN card collection page count is invalid")
	}
	known := make(map[int]bool, len(master.CardTemplates)+len(master.StackCardTemplates))
	for _, card := range master.CardTemplates {
		known[card.CardID] = true
	}
	for _, card := range master.StackCardTemplates {
		known[card.CardID] = true
	}
	seen := make(map[int]bool)
	for i, page := range master.CardCollectionPages {
		if len(page) != 10 {
			return fmt.Errorf("CN card collection page %d requires ten slots", i+1)
		}
		count := 0
		for _, id := range page {
			if id == 0 {
				continue
			}
			if !known[id] || seen[id] {
				return fmt.Errorf("CN card collection page %d has unknown or duplicate card %d", i+1, id)
			}
			seen[id], count = true, count+1
		}
		if count == 0 {
			return fmt.Errorf("CN card collection page %d is empty", i+1)
		}
	}
	return nil
}

// History is separate from the immutable public directory. This also handles
// a card imported from inventory without erasing discoveries after a sale.
func normalizeCollectionLoveHistory(state *gamestate.State) bool {
	seen := make(map[int]bool, len(state.SupportDeck.CardCollectionLoveMaxIDs))
	for _, id := range state.SupportDeck.CardCollectionLoveMaxIDs {
		seen[id] = true
	}
	changed := false
	for _, cards := range [][]gamestate.Card{state.Cards, state.ContainerCards} {
		for _, card := range cards {
			if card.LoveMax > 0 && card.Love >= card.LoveMax && !seen[card.CardID] {
				seen[card.CardID], changed = true, true
				state.SupportDeck.CardCollectionLoveMaxIDs = append(state.SupportDeck.CardCollectionLoveMaxIDs, card.CardID)
			}
		}
	}
	return changed
}
