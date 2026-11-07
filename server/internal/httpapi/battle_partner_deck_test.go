package httpapi

import (
	"errors"
	"reflect"
	"strconv"
	"testing"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestAccountReloadPreservesInactiveDeckDrafts(t *testing.T) {
	for _, arthur := range []int8{1, 5} {
		t.Run(strconv.Itoa(int(arthur)), func(t *testing.T) {
			state := testfixture.RuntimeState(t)
			draft := state.Decks[0]
			draft.ArthurType, draft.JobType, draft.Index, draft.IsActive = arthur, arthur, 1, 0
			draft.CardUniqueIDs = make([]int64, len(draft.CardUniqueIDs))
			draft.SupportCardUniqueIDs = make([]int64, len(draft.SupportCardUniqueIDs))
			draft.SphereUniqueIDs = make([]int64, len(draft.SphereUniqueIDs))
			draft.BuddyUniqueIDs = make([]int64, len(draft.BuddyUniqueIDs))
			state.Decks = append(state.Decks, draft)
			definitions := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{}, Skills: map[int][]battlemodel.CombatSkillDefinition{}}
			for _, card := range state.Cards {
				definitions.Cards[card.CardID] = battlemodel.CombatCardDefinition{ID: card.CardID, ArthurSkillID: card.CardID}
				definitions.Skills[card.CardID] = []battlemodel.CombatSkillDefinition{{ID: card.CardID, Job: "MERCENARY"}}
			}
			for load := 0; load < 2; load++ {
				account, err := game.New(state, game.WithCombatDefinitions(definitions))
				if err != nil {
					t.Fatalf("load %d: %v", load, err)
				}
				state = account.Snapshot(state)
				for _, deck := range state.Decks {
					if deck.ArthurType == arthur && deck.Index == draft.Index &&
						(!reflect.DeepEqual(deck.CardUniqueIDs, draft.CardUniqueIDs) || deck.IsActive != 0) {
						t.Fatalf("load %d rewrote the empty inactive draft: %+v", load, deck)
					}
				}
			}
		})
	}
}

func TestAccountReloadKeepsInvalidCompositionEditableAndRepairsWithCatalog(t *testing.T) {
	state := testfixture.RuntimeState(t)
	deck := state.Decks[0]
	deck.ArthurType, deck.JobType, deck.IsActive = 5, 5, 1
	deck.SupportCardUniqueIDs = make([]int64, len(deck.SupportCardUniqueIDs))
	deck.SphereUniqueIDs = make([]int64, len(deck.SphereUniqueIDs))
	deck.BuddyUniqueIDs = make([]int64, len(deck.BuddyUniqueIDs))
	deck.CardUniqueIDs = append([]int64(nil), deck.CardUniqueIDs...)
	state.Decks = append(state.Decks, deck)
	definitions := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{}, Skills: map[int][]battlemodel.CombatSkillDefinition{}}
	for _, card := range state.Cards {
		job := "ALCHEMIST"
		if card.UniqueID == deck.CardUniqueIDs[0] {
			job = "MERCENARY"
		}
		definitions.Cards[card.CardID] = battlemodel.CombatCardDefinition{ID: card.CardID, ArthurSkillID: card.CardID}
		definitions.Skills[card.CardID] = []battlemodel.CombatSkillDefinition{{ID: card.CardID, Job: job}}
	}
	account, err := game.New(state, game.WithCombatDefinitions(definitions))
	if err != nil {
		t.Fatalf("a saved foreign-profession deck must remain loadable: %v", err)
	}
	saved := account.Snapshot(state)
	if !reflect.DeepEqual(saved.Decks[len(saved.Decks)-1].CardUniqueIDs, deck.CardUniqueIDs) {
		t.Fatal("loading silently rewrote the existing deck")
	}
	if err := account.ValidateBattleDeckComposition(5, deck.Index); !errors.Is(err, battlecore.ErrAlchemistDeckProfession) {
		t.Fatalf("loaded definitions must protect battle admission: %v", err)
	}
	// When an equipped slot really needs restoring, initialization must use
	// the same profession rules as subsequent card movement and battle entry.
	state.Decks[len(state.Decks)-1].CardUniqueIDs[0] = 0
	account, err = game.New(state, game.WithCombatDefinitions(definitions))
	if err != nil {
		t.Fatalf("repair equipped deck with canonical definitions: %v", err)
	}
	saved = account.Snapshot(state)
	if saved.Decks[len(saved.Decks)-1].CardUniqueIDs[0] == deck.CardUniqueIDs[0] {
		t.Fatal("initial repair selected the first ordinary-profession card")
	}
	if err := account.ValidateBattleDeckComposition(5, deck.Index); err != nil {
		t.Fatalf("initial repair left an unusable alchemist deck: %v", err)
	}
}

func TestTeamBattlePartnerDeckWireKeepsLockedSupportSlotsNonEmpty(t *testing.T) {
	deck := game.DeckInfo{
		ArthurType:           1,
		Index:                0,
		JobType:              1,
		CardUniqueIDs:        []int64{1},
		SupportCardUniqueIDs: []int64{0, 0, 0},
		SphereUniqueIDs:      []int64{0, 0, 0},
		BuddyUniqueIDs:       []int64{0, 0, 0, 0, 0},
		Name:                 "test",
	}
	cards := map[int64]game.CardInfo{
		1: {
			UniqueID:    1,
			CardID:      10000001,
			Level:       1,
			SkillLevels: []int16{1},
			Fame:        1,
		},
	}

	wire, err := teamBattlePartnerDeckWire(
		1000001,
		1,
		deck,
		cards,
		gamestate.JobParameter{},
		gamestate.Avatar{AvatarPartIDs: []int{0, 0, 0, 0, 0, 0, 0}},
		map[int64]gamestate.Sphere{},
		map[int64]gamestate.Buddy{},
		0,
		0,
		nil,
	)
	if err != nil {
		t.Fatalf("project partner deck: %v", err)
	}

	support, ok := wire["support_deck"].([]any)
	if !ok {
		t.Fatalf("support_deck type = %T, want []any", wire["support_deck"])
	}
	if len(support) != len(deck.SupportCardUniqueIDs) {
		t.Fatalf("support_deck slots = %d, want %d", len(support), len(deck.SupportCardUniqueIDs))
	}
	if got := wire["support_card_unlock_slot_num"]; got != int8(0) {
		t.Fatalf("support unlock count = %v, want 0", got)
	}
	for index, value := range support {
		card, ok := value.(map[string]any)
		if !ok {
			t.Fatalf("support_deck[%d] type = %T, want map[string]any", index, value)
		}
		if got := card["cardid"]; got != 0 {
			t.Fatalf("support_deck[%d].cardid = %v, want 0", index, got)
		}
	}
}
