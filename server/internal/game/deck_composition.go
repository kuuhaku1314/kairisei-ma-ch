package game

import (
	"fmt"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

type accountOptions struct {
	missions          *MissionCatalog
	battleCatalog     *TeamBattleDefinitions
	combatDefinitions *battlemodel.Definitions
	cardCatalog       *CardCatalog
	alchemistMain     AlchemistMainParameters
}

type AccountOption func(*accountOptions)

// WithCombatDefinitions attaches the application's immutable catalog without
// copying master fields into owned cards or persisting another definition map.
// Old saved decks remain editable; changed composition rules apply on saving,
// evolution and battle admission, rather than preventing account login.
func WithCombatDefinitions(definitions *battlemodel.Definitions) AccountOption {
	return func(options *accountOptions) { options.combatDefinitions = definitions }
}

// AlchemistMainParameters is a read-only view of the shared card parameter
// catalog. The account requests one main-card family without owning formulas.
type AlchemistMainParameters interface {
	AlchemistMain(cardID, level, love, fame int) (gamestate.CardParameter, bool, error)
}

func WithAlchemistMainParameters(parameters AlchemistMainParameters) AccountOption {
	return func(options *accountOptions) { options.alchemistMain = parameters }
}

// ownedCardIDs is local to an operation. Reuse it while the inventory is stable;
// never keep an account-wide index that needs invalidation after card changes.
func ownedCardIDs(cards []CardInfo) map[int64]int {
	owned := make(map[int64]int, len(cards))
	for _, card := range cards {
		owned[card.UniqueID] = card.CardID
	}
	return owned
}

func (s *Account) validateDeckCardFamilies(deck DeckInfo, owned map[int64]int, replacementUniqueID int64, replacementCardID int, forBattle bool) error {
	if err := s.validateDeckFamilies(deck, owned, replacementUniqueID, replacementCardID); err != nil {
		return err
	}
	return s.validateDeckComposition(deck, owned, replacementUniqueID, replacementCardID, forBattle || deck.IsActive != 0)
}

func (s *Account) validateDeckComposition(deck DeckInfo, owned map[int64]int, replacementUniqueID int64, replacementCardID int, requireActiveRules bool) error {
	if s.combatDefinitions == nil {
		// Isolated inventory tools/tests can omit combat definitions. Production
		// HTTP accounts receive the same catalog as battle preparation.
		return nil
	}
	ids := make([]int, len(deck.CardUniqueIDs))
	for index, uniqueID := range deck.CardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		id, exists := owned[uniqueID]
		if !exists {
			return fmt.Errorf("unknown main card unique ID %d", uniqueID)
		}
		if uniqueID == replacementUniqueID {
			id = replacementCardID
		}
		ids[index] = id
	}
	if requireActiveRules && deck.ArthurType == 5 {
		if err := battlecore.ValidateAlchemistMainDeckProfession(ids, s.combatDefinitions); err != nil {
			return err
		}
	}
	return battlecore.ValidateMainDeckComposition(int(deck.ArthurType), ids, s.combatDefinitions)
}

// ValidateBattleDeckComposition checks a selected, owned deck before charging
// entry costs. Existing family/ownership checks also protect pre-update saves.
func (s *Account) ValidateBattleDeckComposition(arthurType, deckIndex int8) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	deck, found := ExactDeck(s.decks, arthurType, deckIndex)
	if !found {
		return fmt.Errorf("selected deck %d/%d is unavailable", arthurType, deckIndex)
	}
	return s.validateDeckCardFamilies(deck, ownedCardIDs(s.cards), 0, 0, true)
}
