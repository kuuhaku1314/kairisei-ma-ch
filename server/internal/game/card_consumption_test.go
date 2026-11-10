package game

import (
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardDecomposeZeroDustAndRejectedTargets(t *testing.T) {
	makeAccount := func(radix int) *Account {
		s := ConsumptionTestStore(t)
		s.stive = 123
		s.cards[1].Fame = 3
		s.cardDefinitions[20] = gamestate.Card{CardID: 20, LevelMax: 1, FameMax: 100}
		s.cardDevelopmentRules = map[int]cardDevelopmentRule{20: {DevelopmentType: 1, DecomposeRadix: radix, FameMax: 100}}
		s.decks = []DeckInfo{{CardUniqueIDs: []int64{1, 2}, SupportCardUniqueIDs: []int64{2}, LeaderCardIndex: 0}}
		return s
	}
	for _, radix := range []int{0, 20} {
		for _, kind := range []int{cardDecomposeFame, cardDecomposeCard} {
			s := makeAccount(radix)
			if kind == cardDecomposeCard {
				s.decks[0].CardUniqueIDs[1] = 0
				s.decks[0].SupportCardUniqueIDs[0] = 0
			}
			balance, decks, err := s.DecomposeCard(2, kind)
			if err != nil {
				t.Fatalf("radix=%d kind=%d: %v", radix, kind, err)
			}
			consumed := 3
			if kind == cardDecomposeFame {
				consumed = 2
				if len(s.cards) != 2 || s.cards[1].Fame != 1 || len(decks) != 0 || s.decks[0].CardUniqueIDs[1] != 2 {
					t.Fatal("fame decomposition changed card ownership or decks")
				}
			} else if len(s.cards) != 1 || len(decks) != 0 || s.decks[0].CardUniqueIDs[1] != 0 || s.decks[0].SupportCardUniqueIDs[0] != 0 {
				t.Fatal("card decomposition left card or deck references")
			}
			if balance != 123+radix*consumed || s.gold != 100000 {
				t.Fatal("decomposition balance differs from native radix formula")
			}
		}
	}
	for _, reason := range []string{"main deck", "support deck", "locked", "missing", "not decomposable", "no extra fame"} {
		s := makeAccount(0)
		s.decks[0].CardUniqueIDs[1] = 0
		s.decks[0].SupportCardUniqueIDs[0] = 0
		uid, kind := int64(2), cardDecomposeCard
		switch reason {
		case "main deck":
			s.decks[0].CardUniqueIDs[1] = 2
		case "support deck":
			s.decks[0].SupportCardUniqueIDs[0] = 2
		case "locked":
			s.cards[1].IsLock = 1
		case "missing":
			uid = 99
		case "not decomposable":
			s.cardDevelopmentRules[20] = cardDevelopmentRule{}
		case "no extra fame":
			s.cards[1].Fame = 1
			kind = cardDecomposeFame
		}
		cards, decks := CloneCards(s.cards), CloneDecks(s.decks)
		_, _, err := s.DecomposeCard(uid, kind)
		var business *BusinessError
		if !errors.As(err, &business) || !reflect.DeepEqual(cards, CloneCards(s.cards)) || !reflect.DeepEqual(decks, CloneDecks(s.decks)) || s.stive != 123 {
			t.Fatalf("%s did not fail without consumption: %v", reason, err)
		}
		if strings.Contains(reason, "deck") && err != ErrCardInDeck {
			t.Fatalf("equipped card did not request removal from its deck: %v", err)
		}
	}
}

func TestCardConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell", "container sell"} {
		for _, state := range []string{"main deck", "support deck", "locked", "missing"} {
			if operation == "container sell" && strings.Contains(state, "deck") {
				continue
			}
			t.Run(operation+"/"+state, func(t *testing.T) {
				s := ConsumptionTestStore(t)
				want := ErrCardInDeck
				switch state {
				case "main deck":
					s.decks = []DeckInfo{{CardUniqueIDs: []int64{2}}}
				case "support deck":
					s.decks = []DeckInfo{{SupportCardUniqueIDs: []int64{2}}}
				case "locked":
					s.cards[1].IsLock = 1
					want = ErrCardLocked
				case "missing":
					s.cards = s.cards[:1]
					want = ErrCardUnavailable
				}
				if operation == "container sell" && len(s.cards) > 1 {
					s.containerCards, s.cards = s.cards[1:], s.cards[:1]
				}
				beforeCards, beforeContainer, beforeDecks := CloneCards(s.cards), CloneCards(s.containerCards), CloneDecks(s.decks)
				var err error
				switch operation {
				case "fusion":
					_, _, _, _, err = s.FuseCard(1, []int64{2}, nil, nil)
				case "evolution":
					_, _, _, err = s.EvolveCard(1, 11, []int64{2}, nil, nil, nil)
				case "sell":
					_, err = s.SellCards([]int64{2}, nil)
				case "container sell":
					_, err = s.SellContainerCards([]int64{2})
				}
				if err != want {
					t.Fatalf("consumption error = %v, want %v", err, want)
				}
				if s.gold != 100000 || !reflect.DeepEqual(beforeCards, CloneCards(s.cards)) || !reflect.DeepEqual(beforeContainer, CloneCards(s.containerCards)) || !reflect.DeepEqual(beforeDecks, CloneDecks(s.decks)) {
					t.Fatal("rejected consumption changed cards, decks or gold")
				}
			})
		}
	}
}

func TestGodEvolutionRejectsInvalidFameWithoutConsumption(t *testing.T) {
	for _, reason := range []string{"low fame", "several low fame cards", "main deck", "support deck", "locked", "insufficient gold", "stack instead of instance"} {
		t.Run(reason, func(t *testing.T) {
			s := ConsumptionTestStore(t)
			s.cardActions.EvolutionTransitions[0].Type = 1
			s.cardActions.EvolutionTransitions[0].Materials[0].Fame = 3
			s.cards[1].Fame = 5
			ids, cardIDs := []int64{2}, []int(nil)
			switch reason {
			case "low fame":
				s.cards[1].Fame = 2
			case "several low fame cards":
				s.cards[1].Fame = 2
				s.cards = append(s.cards, CardInfo{UniqueID: 3, CardID: 20, Fame: 2})
				ids = append(ids, 3)
			case "main deck":
				s.decks = []DeckInfo{{CardUniqueIDs: []int64{2}}}
			case "support deck":
				s.decks = []DeckInfo{{SupportCardUniqueIDs: []int64{2}}}
			case "locked":
				s.cards[1].IsLock = 1
			case "insufficient gold":
				s.gold = 0
			case "stack instead of instance":
				ids, cardIDs = nil, []int{20}
				s.stackCards = []gamestate.CardStack{{CardID: 20, Num: 3}}
			}
			cards, decks, gold := CloneCards(s.cards), CloneDecks(s.decks), s.gold
			stacks := append([]gamestate.CardStack(nil), s.stackCards...)
			_, _, _, err := s.EvolveCard(1, 11, ids, nil, cardIDs, nil)
			if err == nil || !reflect.DeepEqual(cards, CloneCards(s.cards)) || !reflect.DeepEqual(decks, CloneDecks(s.decks)) || !reflect.DeepEqual(stacks, s.stackCards) || gold != s.gold || len(s.cardCollectionIDs) != 0 {
				t.Fatalf("invalid request changed inventory/decks/gold: %v", err)
			}
			if strings.Contains(reason, "deck") && err != ErrCardInDeck {
				t.Fatalf("equipped GOD material did not request removal: %v", err)
			}
		})
	}
}

func TestCardEvolutionKeepsEquippedBaseAndSurplusMaterial(t *testing.T) {
	s := ConsumptionTestStore(t)
	s.cards[0].CreateTime = 1700000000
	s.cards[1].Fame = 5
	s.decks = []DeckInfo{{CardUniqueIDs: []int64{1}, LeaderCardIndex: 0}}
	s.cardActions.EvolutionTransitions[0].Type = 1
	s.cardActions.EvolutionTransitions[0].Materials[0].Fame = 3
	result, _, decks, err := s.EvolveCard(1, 11, []int64{2}, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.UniqueID != 1 || result.CardID != 11 || len(s.cards) != 2 || s.cards[1].UniqueID != 2 || s.cards[1].Fame != 2 || !reflect.DeepEqual(decks, CloneDecks(s.decks)) || decks[0].CardUniqueIDs[0] != 1 || decks[0].LeaderCardIndex != 0 {
		t.Fatal("evolution changed the base identity/deck or lost surplus material fame")
	}
	if result.CreateTime != 1700000000 {
		t.Fatal("evolution reset the original acquisition date")
	}
	state := s.Snapshot(gamestate.State{})
	if len(state.Cards) != 2 || state.Cards[0].UniqueID != 1 || state.Cards[0].CardID != 11 || state.Cards[1].UniqueID != 2 || state.Cards[1].Fame != 2 || state.Decks[0].CardUniqueIDs[0] != 1 {
		t.Fatal("evolved base and remaining fame were not preserved in the save snapshot")
	}
}

func TestBuddyConsumptionReturnsBusinessErrors(t *testing.T) {
	for _, operation := range []string{"fusion", "evolution", "sell"} {
		for _, missing := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/missing=%v", operation, missing), func(t *testing.T) {
				s := ConsumptionTestStore(t)
				s.buddies = []gamestate.Buddy{{UniqueID: 1, BuddyID: 100, Level: 1, BaseAddPrice: 500}, {UniqueID: 2, BuddyID: 100, Level: 1, IsLock: 1}}
				s.buddyDefinitions = map[int]gamestate.BuddyDefinition{
					100: {BuddyID: 100, SameBuddyID: 100, MaxLevel: 2, ExperienceTableID: 1, EvolutionID: 101, Rarity: "MILLIONRARE"},
					101: {BuddyID: 101, SameBuddyID: 100, MaxLevel: 3, ExperienceTableID: 1, EvolutionCount: 1},
				}
				s.buddyExperience = map[int][]int{1: {10000, 10000}}
				s.buddyEvoPrices = map[string][]int{"MILLIONRARE": {100}}
				s.buddyProgression.MaximumMaterialCount = 100
				want := ErrBuddyLocked
				if missing {
					s.buddies = s.buddies[:1]
					want = ErrBuddyUnavailable
				}
				before := append([]gamestate.Buddy(nil), s.buddies...)
				var err error
				switch operation {
				case "fusion":
					_, err = s.FuseBuddy(1, []BuddyFusionInput{{InputType: 2, ID: 2, Num: 1}})
				case "evolution":
					_, err = s.EvolveBuddy(1, 2, 0)
				case "sell":
					_, _, _, _, err = s.SellBuddies([]int64{2})
				}
				if err != want {
					t.Fatalf("buddy error = %v, want %v", err, want)
				}
				if s.gold != 100000 || !reflect.DeepEqual(s.buddies, before) {
					t.Fatal("rejected buddy consumption changed state")
				}
			})
		}
	}
}

func TestAlchemyEvolutionConsumesItemsAndPreservesCardInventory(t *testing.T) {
	for _, name := range []string{"success", "insufficient first item", "missing second item", "wrong item", "stack channel", "instance channel", "ordinary evolution", "not full level", "insufficient gold"} {
		t.Run(name, func(t *testing.T) {
			s := ConsumptionTestStore(t)
			s.cards[0].Level = 1
			s.cards[0].CreateTime = 1700000000
			definition := s.cardDefinitions[10]
			definition.RarityPlus = 4
			s.cardDefinitions[10] = definition
			transition := &s.cardActions.EvolutionTransitions[0]
			transition.Type = 4
			transition.Materials = []gamestate.EvolutionMaterial{{CardID: 20, Num: 3}, {CardID: 21, Num: 2}}
			s.items = map[int]gamestate.Item{20: {ItemID: 20, Num: 5}, 21: {ItemID: 21, Num: 2}}
			s.stackCards = []gamestate.CardStack{{CardID: 20, Num: 99}}
			var uniqueIDs []int64
			var cardIDs []int
			itemIDs := []int{20, 21}
			switch name {
			case "insufficient first item":
				s.items[20] = gamestate.Item{ItemID: 20, Num: 2}
			case "missing second item":
				delete(s.items, 21)
			case "wrong item":
				itemIDs[1] = 22
			case "stack channel":
				cardIDs, itemIDs = itemIDs, nil
			case "instance channel":
				uniqueIDs = []int64{2}
			case "ordinary evolution":
				transition.Type = 0
			case "not full level":
				definition.RarityPlus = 0
				s.cardDefinitions[10] = definition
			case "insufficient gold":
				s.gold = 0
			}
			before := s.Snapshot(gamestate.State{})
			result, _, _, err := s.EvolveCard(1, 11, uniqueIDs, nil, cardIDs, itemIDs)
			if name != "success" {
				if err == nil || !reflect.DeepEqual(before, s.Snapshot(gamestate.State{})) {
					t.Fatalf("rejected item evolution changed saved state: %v", err)
				}
				return
			}
			if err != nil || result.CardID != 11 || result.UniqueID != 1 || result.CreateTime != 1700000000 || s.gold != before.User.Gold-transition.Gold {
				t.Fatalf("item evolution failed or changed base identity: %v", err)
			}
			if len(s.cards) != 2 || s.cards[1].UniqueID != 2 || s.stackCards[0].Num != 99 || s.items[20].Num != 2 || s.items[21].Num != 0 {
				t.Fatal("item consumption crossed inventories or used slot counts as quantities")
			}
			state := s.Snapshot(gamestate.State{})
			if len(state.Items) != 2 || state.Items[0].Num != 2 || state.Items[1].Num != 0 {
				t.Fatal("item consumption was not retained in the save snapshot")
			}
		})
	}
}

func ConsumptionTestStore(t *testing.T) *Account {
	t.Helper()
	return &Account{onboarding: completedTestOnboarding(),
		gold: 100000, cardProgression: cardFusionTestPolicy(),
		cards: []CardInfo{{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 1000000, BaseAddPrice: 200},
			{UniqueID: 2, CardID: 20, Level: 1, AddExperience: 10}},
		cardDefinitions: map[int]gamestate.Card{
			10: {CardID: 10, LevelMax: 2, ExperienceTableID: 1},
			11: {CardID: 11, LevelMax: 3, ExperienceTableID: 2},
			20: {CardID: 20, SameCardID: 20, RarityRank: 6, SellGold: 100},
		},
		cardActions: gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{{
			FromCardID: 10, ToCardID: 11, Gold: 100, Materials: []gamestate.EvolutionMaterial{{CardID: 20, Num: 1}},
		}}},
		cardTemplates:     map[int]CardInfo{11: {CardID: 11, Level: 1, LevelMax: 3}},
		cardExperience:    map[int][]int{1: {1000000}, 2: {1000000, 1000000}},
		cardCollectionIDs: map[int]struct{}{},
	}
}

func cardFusionTestPolicy() gamestate.CardProgressionPolicy {
	return gamestate.CardProgressionPolicy{
		MaximumCardMaterialCount:          100,
		FusionGoldPerMaterialPerBaseLevel: 100,
		FusionSuccessTypes: []gamestate.CardFusionSuccessPolicy{
			{SuccessType: 0, Weight: 90, ExperiencePermille: 1000},
			{SuccessType: 1, Weight: 9, ExperiencePermille: 1500},
			{SuccessType: 2, Weight: 1, ExperiencePermille: 2500},
		},
	}
}

func TestEquippedBaseAcceptsManyMRMaterials(t *testing.T) {
	for _, count := range []int{9, 100} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			s := ConsumptionTestStore(t)
			s.cards[0].Level, s.cards[0].Experience, s.cards[0].IsLock = 1, 0, 1
			s.cards = s.cards[:1]
			s.decks = []DeckInfo{{CardUniqueIDs: []int64{1}}}
			selected := make([]int64, count)
			for i := range count {
				id := int64(i + 2)
				selected[i] = id
				s.cards = append(s.cards, CardInfo{UniqueID: id, CardID: 20, Level: 1, AddExperience: 10})
			}
			_, result, _, _, err := s.FuseCard(1, selected, nil, nil)
			if err != nil || result.Experience <= 0 || result.IsLock != 1 || len(s.cards) != 1 || s.gold != 100000-200*count || s.decks[0].CardUniqueIDs[0] != 1 {
				t.Fatalf("equipped base with %d MR cards: result=%+v err=%v", count, result, err)
			}
		})
	}
}
