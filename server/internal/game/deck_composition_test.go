package game

import (
	"errors"
	"reflect"
	"testing"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

func TestDeckCompositionRejectsSaveAndAdmissionWithoutChangingDecks(t *testing.T) {
	store := &Account{
		deckSlots: 10, supportSlotCapacity: 10, buddySlots: 5,
		supportUnlockedSlots: []int8{10, 10, 10, 10},
		cardDefinitions:      make(map[int]gamestate.Card),
		combatDefinitions:    &battlemodel.Definitions{Cards: make(map[int]battlemodel.CombatCardDefinition)},
	}
	deck := DeckInfo{ArthurType: 1, JobType: 1, CardUniqueIDs: make([]int64, 10), SupportCardUniqueIDs: make([]int64, 10), SphereUniqueIDs: make([]int64, 3), BuddyUniqueIDs: make([]int64, 5)}
	for id := 1; id <= 12; id++ {
		store.cards = append(store.cards, CardInfo{UniqueID: int64(id), CardID: id, Level: 1, Fame: 1})
		store.cardDefinitions[id] = gamestate.Card{CardID: id, SameCardID: id, SameSupportCardID: id}
		definition := battlemodel.CombatCardDefinition{ID: id}
		if id >= 11 {
			definition.ProfileTags[0] = battlecore.RoundTableCardGroup
		}
		store.combatDefinitions.Cards[id] = definition
		if id <= 10 {
			deck.CardUniqueIDs[id-1] = int64(id)
		}
	}
	store.decks = []DeckInfo{cloneDeck(deck)}
	invalid := cloneDeck(deck)
	invalid.CardUniqueIDs[0], invalid.CardUniqueIDs[1] = 11, 12
	if _, _, err := store.SetDecks([]DeckInfo{invalid}); !errors.Is(err, battlecore.ErrMultipleRoundTableCards) {
		t.Fatalf("save should reject two distinct Round Table cards: %v", err)
	}
	if !reflect.DeepEqual(store.decks, []DeckInfo{deck}) {
		t.Fatal("rejected save mutated the selected deck")
	}
	// Existing saves can still be opened and edited after new rules are attached,
	// but neither solo nor multiplayer admission may use the invalid old deck.
	store.decks[0] = invalid
	if err := store.ValidateBattleDeckComposition(1, 0); !errors.Is(err, battlecore.ErrMultipleRoundTableCards) {
		t.Fatalf("solo admission: %v", err)
	}
	if _, err := store.BattleLoadout(1, 1, 0); !errors.Is(err, battlecore.ErrMultipleRoundTableCards) {
		t.Fatalf("multiplayer admission: %v", err)
	}
	// An EX copy is outside this main-deck limit, and lowering the main count
	// repairs an invalid pre-update deck without deleting either owned card.
	valid := cloneDeck(deck)
	valid.CardUniqueIDs[0], valid.SupportCardUniqueIDs[0] = 11, 12
	if _, _, err := store.SetDecks([]DeckInfo{valid}); err != nil {
		t.Fatalf("one main and one support Round Table card: %v", err)
	}
	if len(store.cards) != 12 {
		t.Fatal("editing composition changed ownership")
	}
	// The same definition check is used before evolving a card in place.
	valid.SupportCardUniqueIDs[0] = 0
	if err := store.validateDeckCardFamilies(valid, ownedCardIDs(store.cards), 2, 12, false); !errors.Is(err, battlecore.ErrMultipleRoundTableCards) {
		t.Fatalf("evolution must check the future main-card definition: %v", err)
	}
	// The original exit validator counts every matching profile-group entry,
	// including a duplicate within one definition.
	duplicate := store.combatDefinitions.Cards[11]
	duplicate.ProfileTags[1] = battlecore.RoundTableCardGroup
	store.combatDefinitions.Cards[11] = duplicate
	if _, _, err := store.SetDecks([]DeckInfo{valid}); !errors.Is(err, battlecore.ErrMultipleRoundTableCards) {
		t.Fatalf("repeated Round Table profile group: %v", err)
	}
}

func TestAlchemistSummonDeckLimitUsesCanonicalDefinitions(t *testing.T) {
	definitions := &battlemodel.Definitions{Cards: make(map[int]battlemodel.CombatCardDefinition)}
	for id := 1; id <= 7; id++ {
		definitions.Cards[id] = battlemodel.CombatCardDefinition{ID: id, SummonID: 100 + id}
	}
	for _, test := range []struct {
		arthur int
		ids    []int
		want   error
	}{
		{5, []int{1, 2, 3, 4, 5, 0, 0}, nil},
		{5, []int{1, 2, 3, 4, 5, 6, 0}, battlecore.ErrTooManySummonCards},
		{1, []int{1, 2, 3, 4, 5, 6, 0}, nil},
	} {
		if err := battlecore.ValidateMainDeckComposition(test.arthur, test.ids, definitions); !errors.Is(err, test.want) {
			t.Fatalf("arthur %d, cards %v: %v", test.arthur, test.ids, err)
		}
	}
}

func TestAlchemistProfessionChecksDraftActivationEvolutionAndAdmission(t *testing.T) {
	store := &Account{
		deckSlots: 10, supportSlotCapacity: 10, buddySlots: 5,
		supportUnlockedSlots: []int8{10, 10, 10, 10},
		cardDefinitions:      make(map[int]gamestate.Card),
		combatDefinitions:    &battlemodel.Definitions{Cards: make(map[int]battlemodel.CombatCardDefinition), Skills: make(map[int][]battlemodel.CombatSkillDefinition)},
	}
	deck := DeckInfo{ArthurType: 5, JobType: 5, CardUniqueIDs: make([]int64, 10), SupportCardUniqueIDs: make([]int64, 10), SphereUniqueIDs: make([]int64, 3), BuddyUniqueIDs: make([]int64, 5)}
	for id := 1; id <= 12; id++ {
		profession := "ALCHEMIST"
		if id == 1 {
			profession = "NULL"
		} else if id >= 11 {
			profession = "MERCENARY"
		}
		store.cards = append(store.cards, CardInfo{UniqueID: int64(id), CardID: id, Level: 1, Fame: 1})
		store.cardDefinitions[id] = gamestate.Card{CardID: id, SameCardID: id, SameSupportCardID: id}
		store.combatDefinitions.Cards[id] = battlemodel.CombatCardDefinition{ID: id, ArthurSkillID: id}
		store.combatDefinitions.Skills[id] = []battlemodel.CombatSkillDefinition{{ID: id, Job: profession}}
		if id <= 10 {
			deck.CardUniqueIDs[id-1] = int64(id)
		}
	}
	deck.CardUniqueIDs[1] = 11
	if _, _, err := store.SetDecks([]DeckInfo{deck}); err != nil {
		t.Fatalf("inactive editing draft: %v", err)
	}
	before := CloneDecks(store.decks)
	active := cloneDeck(deck)
	active.IsActive = 1
	if _, _, err := store.SetDecks([]DeckInfo{active}); !errors.Is(err, battlecore.ErrAlchemistDeckProfession) {
		t.Fatalf("activation of a foreign-profession main card: %v", err)
	}
	if !reflect.DeepEqual(before, store.decks) {
		t.Fatal("rejected activation mutated the saved draft")
	}
	if err := store.ValidateBattleDeckComposition(5, 0); !errors.Is(err, battlecore.ErrAlchemistDeckProfession) {
		t.Fatalf("solo must validate the selected draft as a battle deck: %v", err)
	}
	if _, err := store.BattleLoadout(1, 5, 0); !errors.Is(err, battlecore.ErrAlchemistDeckProfession) {
		t.Fatalf("multiplayer must validate the selected draft as a battle deck: %v", err)
	}
	active.CardUniqueIDs[1], active.SupportCardUniqueIDs[0] = 2, 11
	if _, _, err := store.SetDecks([]DeckInfo{active}); err != nil {
		t.Fatalf("common/alchemist main cards with a different-profession EX card: %v", err)
	}
	if err := store.ValidateBattleDeckComposition(5, 0); err != nil {
		t.Fatalf("valid active alchemist deck: %v", err)
	}
	if err := store.validateDeckCardFamilies(active, ownedCardIDs(store.cards), 2, 12, false); !errors.Is(err, battlecore.ErrAlchemistDeckProfession) {
		t.Fatalf("evolution must validate the target's profession before mutation: %v", err)
	}
}

func TestAlchemistAutomaticMainFillUsesEligibleCardsForInactiveDeck(t *testing.T) {
	store := &Account{
		cardDefinitions: make(map[int]gamestate.Card),
		combatDefinitions: &battlemodel.Definitions{
			Cards:  make(map[int]battlemodel.CombatCardDefinition),
			Skills: make(map[int][]battlemodel.CombatSkillDefinition),
		},
	}
	for index, profession := range []string{"MERCENARY", "NULL", "ALCHEMIST"} {
		id := index + 1
		store.cards = append(store.cards, CardInfo{UniqueID: int64(id), CardID: id})
		store.cardDefinitions[id] = gamestate.Card{CardID: id, SameCardID: id, SameSupportCardID: id}
		store.combatDefinitions.Cards[id] = battlemodel.CombatCardDefinition{ID: id, ArthurSkillID: id}
		store.combatDefinitions.Skills[id] = []battlemodel.CombatSkillDefinition{{ID: id, Job: profession}}
	}
	deck := DeckInfo{ArthurType: 5, JobType: 5, CardUniqueIDs: []int64{0, 0}, SupportCardUniqueIDs: []int64{0}}
	if err := store.fillVacatedDeckSlots(&deck, store.cards, []int{0, 1}, false); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(deck.CardUniqueIDs, []int64{2, 3}) || deck.IsActive != 0 {
		t.Fatalf("automatic fill crossed professions or activated the draft: %+v", deck)
	}
	if err := store.fillVacatedDeckSlots(&deck, store.cards, []int{0}, true); err != nil {
		t.Fatal(err)
	}
	if deck.SupportCardUniqueIDs[0] != 1 {
		t.Fatalf("ordinary profession must remain eligible for EX: %+v", deck)
	}
	unfilled := DeckInfo{ArthurType: 5, CardUniqueIDs: []int64{0}}
	if err := store.fillVacatedDeckSlots(&unfilled, store.cards[:1], []int{0}, false); err == nil || unfilled.CardUniqueIDs[0] != 0 {
		t.Fatalf("ordinary-only candidates must not be used as alchemist main cards: %+v, %v", unfilled, err)
	}
}
