package game

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestMoveEquippedCardToContainerAutoFillsDeckAtomically(t *testing.T) {
	cardStore := cardMoveTestStore()

	moved, updatedDecks, err := cardStore.MoveCards(1, []int64{101})
	if err != nil {
		t.Fatalf("move equipped card: %v", err)
	}
	if len(moved) != 1 || moved[0].UniqueID != 101 || moved[0].Slot != 1 {
		t.Fatalf("unexpected moved cards: %#v", moved)
	}
	if len(updatedDecks) != 1 {
		t.Fatalf("expected one updated deck, got %#v", updatedDecks)
	}
	if got := updatedDecks[0].CardUniqueIDs; !reflect.DeepEqual(got, []int64{103, 102}) {
		t.Fatalf("vacated main slot was not deterministically filled: %v", got)
	}
	if got := cardStore.decks[0].CardUniqueIDs; !reflect.DeepEqual(got, []int64{103, 102}) {
		t.Fatalf("stored deck differs from response: %v", got)
	}
	if len(cardStore.cards) != 3 || cardIndexByUniqueID(cardStore.cards, 101) >= 0 {
		t.Fatalf("inventory mutation is incorrect: %#v", cardStore.cards)
	}
	if len(cardStore.containerCards) != 1 || cardStore.containerCards[0].UniqueID != 101 {
		t.Fatalf("container mutation is incorrect: %#v", cardStore.containerCards)
	}

	_, reverseDecks, err := cardStore.MoveCards(0, []int64{101})
	if err != nil {
		t.Fatalf("move card back to inventory: %v", err)
	}
	if len(reverseDecks) != 0 {
		t.Fatalf("container withdrawal must not rewrite decks: %#v", reverseDecks)
	}
	if cardIndexByUniqueID(cardStore.cards, 101) < 0 || len(cardStore.containerCards) != 0 {
		t.Fatalf("withdrawal did not restore inventory: cards=%#v container=%#v",
			cardStore.cards, cardStore.containerCards)
	}
}

func TestMoveEquippedCardWithoutReplacementLeavesStateUntouched(t *testing.T) {
	cardStore := &Account{onboarding: completedTestOnboarding(),
		cards: []CardInfo{{UniqueID: 101, CardID: 1001}},
		decks: []DeckInfo{{
			ArthurType:      1,
			Index:           0,
			CardUniqueIDs:   []int64{101},
			SphereUniqueIDs: make([]int64, DeckSphereSlots),
			BuddyUniqueIDs:  make([]int64, deckBuddySlots),
		}},
		cardDefinitions: map[int]gamestate.Card{
			1001: {CardID: 1001, SameCardID: 1, SameSupportCardID: 11},
		},
		cardMax:          10,
		cardContainerMax: 10,
	}
	beforeCards := CloneCards(cardStore.cards)
	beforeDecks := CloneDecks(cardStore.decks)

	if _, _, err := cardStore.MoveCards(1, []int64{101}); err == nil {
		t.Fatal("expected auto-fill failure")
	}
	if !reflect.DeepEqual(cardStore.cards, beforeCards) ||
		!reflect.DeepEqual(cardStore.decks, beforeDecks) ||
		len(cardStore.containerCards) != 0 {
		t.Fatalf("failed move mutated state: cards=%#v decks=%#v container=%#v",
			cardStore.cards, cardStore.decks, cardStore.containerCards)
	}
}

func TestMoveEquippedSupportCardAutoFillsSupportSlot(t *testing.T) {
	cardStore := cardMoveTestStore()
	cardStore.decks[0].SupportCardUniqueIDs = []int64{103}

	_, updatedDecks, err := cardStore.MoveCards(1, []int64{103})
	if err != nil {
		t.Fatalf("move equipped support card: %v", err)
	}
	if len(updatedDecks) != 1 ||
		!reflect.DeepEqual(updatedDecks[0].SupportCardUniqueIDs, []int64{104}) {
		t.Fatalf("vacated support slot was not filled: %#v", updatedDecks)
	}
}

func TestRepairIncompleteMainDecksFillsZeroAndContainerReferences(t *testing.T) {
	cardStore := cardMoveTestStore()
	cardStore.containerCards = []CardInfo{{UniqueID: 101, CardID: 1001, Slot: 1}}
	cardStore.cards = cardStore.cards[1:]
	cardStore.decks[0].CardUniqueIDs = []int64{101, 0}

	repaired, err := cardStore.repairIncompleteMainDecks()
	if err != nil {
		t.Fatalf("repair incomplete deck: %v", err)
	}
	if !repaired {
		t.Fatal("expected persisted deck repair")
	}
	if got := cardStore.decks[0].CardUniqueIDs; !reflect.DeepEqual(got, []int64{102, 103}) {
		t.Fatalf("unexpected repaired deck: %v", got)
	}
}

func TestRepairIncompleteMainDecksRejectsUnknownReferenceAtomically(t *testing.T) {
	cardStore := cardMoveTestStore()
	cardStore.decks[0].CardUniqueIDs = []int64{999, 0}
	before := append([]int64(nil), cardStore.decks[0].CardUniqueIDs...)

	if _, err := cardStore.repairIncompleteMainDecks(); err == nil {
		t.Fatal("expected unknown persisted card reference to fail")
	}
	if !reflect.DeepEqual(cardStore.decks[0].CardUniqueIDs, before) {
		t.Fatalf("failed repair mutated decks: %#v", cardStore.decks)
	}
}

func cardMoveTestStore() *Account {
	return &Account{onboarding: completedTestOnboarding(),
		cards: []CardInfo{
			{UniqueID: 101, CardID: 1001},
			{UniqueID: 102, CardID: 1002},
			{UniqueID: 103, CardID: 1003},
			{UniqueID: 104, CardID: 1004},
		},
		decks: []DeckInfo{{
			ArthurType:           1,
			IsActive:             1,
			Index:                0,
			LeaderCardIndex:      0,
			CardUniqueIDs:        []int64{101, 102},
			SupportCardUniqueIDs: []int64{},
		}},
		cardDefinitions: map[int]gamestate.Card{
			1001: {CardID: 1001, SameCardID: 1, SameSupportCardID: 11},
			1002: {CardID: 1002, SameCardID: 2, SameSupportCardID: 12},
			1003: {CardID: 1003, SameCardID: 3, SameSupportCardID: 13},
			1004: {CardID: 1004, SameCardID: 4, SameSupportCardID: 14},
		},
		cardMax:          10,
		cardContainerMax: 10,
	}
}
