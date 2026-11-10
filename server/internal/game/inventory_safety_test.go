package game

import (
	"errors"
	"math"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestAdminWithdrawnMailIsNotClaimHistory(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), presentHistories: []gamestate.Present{
		{PresentID: 1, State: gamestate.PresentStateAdminDeleted},
		{PresentID: 2, State: 1},
	}}
	_, history := s.PresentState()
	if len(history) != 1 || history[0].PresentID != 2 || len(s.presentHistories) != 2 {
		t.Fatal("withdrawal appeared as a received reward or lost its tombstone")
	}
}

func TestCardFusionFame(t *testing.T) {
	for _, name := range []string{"dual attribute caps fame", "wrong attribute", "manual leveled same family"} {
		t.Run(name, func(t *testing.T) {
			base := CardInfo{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 10, Fame: 90, BaseAddPrice: 100}
			s := &Account{onboarding: completedTestOnboarding(), gold: 1000, cards: []CardInfo{base},
				cardDefinitions: map[int]gamestate.Card{
					10: {CardID: 10, SameCardID: 10, RarityRank: 5, LevelMax: 2, ExperienceTableID: 1, FameMax: 100, FusionAttributes: 3},
					11: {CardID: 11, SameCardID: 10, RarityRank: 6},
				},
				cardExperience: map[int][]int{1: {10}},
				stackCards:     []gamestate.CardStack{{CardID: 20, Num: 3, AddExperience: 10}},
				cardProgression: gamestate.CardProgressionPolicy{
					FusionGoldPerMaterialPerBaseLevel: 100,
					FusionSuccessTypes:                []gamestate.CardFusionSuccessPolicy{{SuccessType: 0, Weight: 1, ExperiencePermille: 1000}},
					FameMaterials:                     map[int][5]int{20: {1, 5}},
				},
			}
			var uniqueIDs []int64
			s.missionCatalog = missionCatalogFixture(t, map[int]gamestate.MissionDefinition{1: {MissionID: 1, Published: true, Title: "强化", Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 1}}})
			s.missions = map[int]gamestate.MissionProgress{}
			uses := []gamestate.CardStackUse{{CardID: 20, Num: 2}}
			wantFame, wantGold, wantStack := 100, 800, 1
			if name == "wrong attribute" {
				definition := s.cardDefinitions[10]
				definition.FusionAttributes = 4
				s.cardDefinitions[10] = definition
			}
			if name == "manual leveled same family" {
				s.cards = append(s.cards, CardInfo{UniqueID: 2, CardID: 11, Level: 50, Fame: 7, AddExperience: 10})
				uniqueIDs, uses = []int64{2}, nil
				wantFame, wantGold, wantStack = 97, 900, 3
			}
			_, result, _, _, err := s.FuseCard(1, uniqueIDs, nil, uses)
			if name == "wrong attribute" {
				if err == nil || s.gold != 1000 || s.stackCards[0].Num != 3 || !EqualCardInfo(s.cards[0], base) || len(s.missions) != 0 {
					t.Fatal("ineffective fame material changed inventory")
				}
				return
			}
			if err != nil || result.Fame != wantFame || result.Experience != base.Experience || s.gold != wantGold || s.stackCards[0].Num != wantStack || len(s.cards) != 1 {
				t.Fatalf("fusion fame or consumption differs: result=%+v gold=%d stack=%v err=%v", result, s.gold, s.stackCards, err)
			}
			if s.missions[1].ProgressNow != 1 || s.missions[1].State != 1 || len(s.TakeMissionUpdates()) != 1 {
				t.Fatal("a successful fusion did not produce exactly one mission event")
			}
		})
	}
}

func TestPresentMultiReceiveUsesClientFilterLimitAndPartialResults(t *testing.T) {
	gold := func(id int64) gamestate.Present {
		return gamestate.Present{PresentID: id, Reward: gamestate.Reward{Type: 4, Num: 5}}
	}
	item := gamestate.Present{PresentID: 1, Reward: gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1}}
	for _, name := range []string{"twenty per batch", "full item keeps other rewards", "daily item filter", "nonclaimable keeps other rewards", "only received"} {
		t.Run(name, func(t *testing.T) {
			s := &Account{onboarding: completedTestOnboarding(), items: map[int]gamestate.Item{}, itemDefinitions: map[int]gamestate.ItemDefinition{
				10: {ItemID: 10, MaxOwned: 1},
			}}
			receiveTypes := []int{2}
			var wantIDs, wantFailed []int64
			wantGold := 0
			switch name {
			case "only received":
				received := gold(1)
				received.State = 1
				s.presents = []gamestate.Present{received}
			case "nonclaimable keeps other rewards":
				unclaimable := gold(1)
				unclaimable.State = 1
				s.presents = []gamestate.Present{unclaimable, gold(2)}
				wantIDs, wantGold = []int64{2}, 5
			case "twenty per batch":
				for id := int64(1); id <= 21; id++ {
					s.presents = append(s.presents, gold(id))
				}
				for id := int64(21); id >= 2; id-- {
					wantIDs = append(wantIDs, id)
				}
				wantGold = 100
			case "full item keeps other rewards":
				s.presents = []gamestate.Present{item, gold(2)}
				s.items[10] = gamestate.Item{ItemID: 10, Num: 1}
				wantIDs, wantFailed, wantGold = []int64{2}, []int64{1}, 5
			case "daily item filter":
				definition := s.itemDefinitions[10]
				definition.DailyLimited = 1
				s.itemDefinitions[10] = definition
				expiringGold := gold(2)
				expiringGold.LimitTime = 2147483647
				s.presents = []gamestate.Present{item, expiringGold}
				receiveTypes, wantIDs = []int{1}, []int64{1}
			}
			beforeCount := len(s.presents)
			result, err := s.ReceivePresents(receiveTypes, true)
			if err != nil || !slices.Equal(result.PresentID, wantIDs) || !slices.Equal(result.FailedID, wantFailed) || s.gold != wantGold || len(s.presents) != beforeCount || len(s.presentHistories) != 0 {
				t.Fatalf("gift selection/grant differs: ids=%v failed=%v gold=%d remaining=%d err=%v", result.PresentID, result.FailedID, s.gold, len(s.presents), err)
			}
			if name == "full item keeps other rewards" {
				full, err := s.ReceivePresent(1)
				if err != nil || !slices.Equal(full.FailedID, []int64{1}) || len(s.presents) != 2 {
					t.Fatalf("full gift must remain claimable: %+v %v", full, err)
				}
				s.items[10] = gamestate.Item{ItemID: 10}
				claimed, err := s.ReceivePresent(1)
				if err != nil || !slices.Equal(claimed.PresentID, []int64{1}) || s.items[10].Num != 1 {
					t.Fatalf("gift failed after freeing space: %+v %v", claimed, err)
				}
				retry, err := s.ReceivePresent(1)
				if err != nil || len(retry.PresentID) != 0 || len(retry.FailedID) != 0 || len(retry.Rewards) != 0 || s.items[10].Num != 1 || len(s.presentHistories) != 0 {
					t.Fatalf("retry duplicated reward/history: %+v %v", retry, err)
				}
			}
			if name == "nonclaimable keeps other rewards" {
				result, err := s.ReceivePresent(1)
				if err != nil || len(result.PresentID) != 0 || len(result.FailedID) != 0 || len(result.Rewards) != 0 || s.gold != 5 || len(s.presentHistories) != 0 {
					t.Fatalf("nonclaimable gift was granted: %+v %v", result, err)
				}
				repeated, err := s.ReceivePresents(receiveTypes, true)
				if err != nil || len(repeated.PresentID) != 0 || len(repeated.FailedID) != 0 || len(repeated.Rewards) != 0 || s.gold != 5 {
					t.Fatalf("received-only batch reported capacity failure or granted twice: %+v %v", repeated, err)
				}
				deleted, err := s.DeletePresents(1)
				if err != nil || !slices.Equal(deleted, []int64{1}) || s.gold != 5 || len(s.presents) != 1 || len(s.presentHistories) != 1 {
					t.Fatalf("discarding gift changed reward or failed: %v %v", deleted, err)
				}
				unavailable, err := s.ReceivePresent(1)
				if err != nil || len(unavailable.PresentID) != 0 || len(unavailable.FailedID) != 0 || len(unavailable.Rewards) != 0 || s.gold != 5 || len(s.presentHistories) != 1 {
					t.Fatalf("unavailable gift reported capacity failure or granted twice: %+v %v", unavailable, err)
				}
			}
		})
	}
}

func TestCardSalesRejectOverflowBeforeConsumingInventory(t *testing.T) {
	for _, container := range []bool{false, true} {
		s := &Account{onboarding: completedTestOnboarding(), gold: 1, cardDefinitions: map[int]gamestate.Card{9: {CardID: 9, SellGold: math.MaxInt}}}
		cards := []CardInfo{{UniqueID: 1, CardID: 9}}
		var err error
		if container {
			s.containerCards = cards
			_, err = s.SellContainerCards([]int64{1})
			if len(s.containerCards) != 1 {
				t.Fatal("failed sale removed a warehouse card")
			}
		} else {
			s.cards = cards
			_, err = s.SellCards([]int64{1}, nil)
			if len(s.cards) != 1 {
				t.Fatal("failed sale removed an inventory card")
			}
		}
		if err == nil || s.gold != 1 {
			t.Fatal("overflowing sale changed balance")
		}
	}
	if _, err := checkedCardSaleGold(0, 0, math.MaxInt, 2); err == nil {
		t.Fatal("stack quantity multiplication overflow accepted")
	}
	if total, err := checkedCardSaleGold(100, 10, 20, 3); err != nil || total != 70 {
		t.Fatal("ordinary sale was rejected")
	}
}

func TestFameTrainingCannotUnlockOrReplaceItsCard(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), cardContainerMax: 10, cards: []CardInfo{{UniqueID: 1, CardID: 9, IsLock: 1}}, cardFameTraining: &gamestate.CardFameTraining{UniqueID: 1}}
	if err := s.SetCardLock(1, 0, false); err == nil || s.cards[0].IsLock != 1 {
		t.Fatal("active training lock removed")
	}
	if _, _, _, _, err := s.FuseCard(1, []int64{2}, nil, nil); err == nil || !strings.Contains(err.Error(), "active fame training") {
		t.Fatal("training card accepted as fusion base")
	}
	if _, _, _, err := s.EvolveCard(1, 10, nil, nil, nil, nil); err == nil || !strings.Contains(err.Error(), "active fame training") {
		t.Fatal("training card accepted as evolution base")
	}
	if _, _, err := s.MoveCards(1, []int64{1}); err == nil || !strings.Contains(err.Error(), "active fame training") {
		t.Fatal("training card moved away from its owner")
	}
	s.cardFameTraining = nil
	if err := s.SetCardLock(1, 0, false); err != nil {
		t.Fatal("ordinary manual unlock rejected")
	}
}

func TestCardFusionMaterialCount(t *testing.T) {
	policy := cardFusionTestPolicy()
	for _, input := range []struct {
		name                               string
		stack, inventory, container, kinds int
		gold                               int
		rejected                           bool
	}{
		{"eight", 8, 0, 0, 1, 20000, false},
		{"nine", 9, 0, 0, 1, 20000, false},
		{"hundred", 100, 0, 0, 1, 20000, false},
		{"grouped 101", 101, 0, 0, 1, 20000, false},
		{"mixed 101", 99, 1, 1, 1, 20000, false},
		{"total limit", 9999, 0, 0, 1, 1000000, false},
		{"mixed total limit", 9997, 1, 1, 1, 1000000, false},
		{"over total", 10000, 0, 0, 1, 1000100, true},
		{"mixed over total", 9998, 1, 1, 1, 1000100, true},
		{"instance limit", 0, 60, 40, 0, 20000, false},
		{"over instance limit", 0, 101, 0, 0, 20000, true},
		{"mixed 101 instances", 0, 60, 41, 0, 20000, false},
		{"both instance limits", 0, 100, 100, 0, 20000, false},
		{"over warehouse limit", 0, 0, 101, 0, 20000, true},
		{"type limit", 1, 0, 0, 100, 20000, false},
		{"over type limit", 1, 0, 0, 101, 20000, true},
		{"insufficient gold", 9, 0, 0, 1, 899, false},
	} {
		t.Run(input.name, func(t *testing.T) {
			base := CardInfo{UniqueID: 1, CardID: 10, Level: 1, LevelMax: 2, BaseAddPrice: 100}
			s := &Account{onboarding: completedTestOnboarding(), gold: input.gold, cards: []CardInfo{base}, cardProgression: policy,
				cardDefinitions: map[int]gamestate.Card{10: {CardID: 10, LevelMax: 2, ExperienceTableID: 1}, 11: {CardID: 11, SameCardID: 11}},
				cardExperience:  map[int][]int{1: {1000000}},
			}
			uniqueIDs, containerIDs, stackIDs := []int64{}, []int64{}, []gamestate.CardStackUse{}
			for i := range input.inventory {
				uid := int64(i + 2)
				s.cards = append(s.cards, CardInfo{UniqueID: uid, CardID: 11, AddExperience: 10})
				uniqueIDs = append(uniqueIDs, uid)
			}
			for i := range input.container {
				uid := int64(i + 10000)
				s.containerCards = append(s.containerCards, CardInfo{UniqueID: uid, CardID: 11, AddExperience: 10})
				containerIDs = append(containerIDs, uid)
			}
			for i := range input.kinds {
				id := 20 + i
				s.stackCards = append(s.stackCards, gamestate.CardStack{CardID: id, Num: 10001, AddExperience: 10})
				stackIDs = append(stackIDs, gamestate.CardStackUse{CardID: id, Num: input.stack})
			}
			beforeCards := append([]CardInfo(nil), s.cards...)
			beforeContainer := append([]CardInfo(nil), s.containerCards...)
			beforeStacks := append([]gamestate.CardStack(nil), s.stackCards...)
			_, _, successType, _, err := s.FuseCard(1, uniqueIDs, containerIDs, stackIDs)
			if input.rejected || input.name == "insufficient gold" {
				if err == nil {
					t.Fatal("invalid fusion was accepted")
				}
				if input.name == "insufficient gold" && err != ErrInsufficientGold {
					t.Fatalf("gold error = %v", err)
				}
				if s.gold != input.gold || !slices.EqualFunc(s.cards, beforeCards, EqualCardInfo) || !slices.EqualFunc(s.containerCards, beforeContainer, EqualCardInfo) || !slices.Equal(s.stackCards, beforeStacks) {
					t.Fatal("rejected fusion changed inventory, experience or gold")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			count := input.stack*input.kinds + input.inventory + input.container
			multiplier := []int{1000, 1500, 2500}[successType]
			if s.gold != input.gold-100*count || len(s.cards) != 1 || len(s.containerCards) != 0 || s.cards[0].Experience != count*10*multiplier/1000 {
				t.Fatalf("fusion consumption/result differs: gold=%d cards=%+v", s.gold, s.cards)
			}
			for _, stack := range s.stackCards {
				if stack.Num != 10001-input.stack {
					t.Fatal("stack quantity differs")
				}
			}
		})
	}
}

func TestCardEvolutionRecipes(t *testing.T) {
	for _, input := range []struct {
		name                      string
		kind, count, fame         int
		keep, container, rejected bool
	}{
		{"normal resets level", 0, 3, 0, false, false, false},
		{"operator closes direct edge", 0, 3, 0, false, false, true},
		{"closed reverse leaves forward open", 0, 3, 0, false, false, false},
		{"limit keeps level with 25+25+3 materials", 3, 53, 0, true, false, false},
		{"limit consumes last mixed materials", 3, 53, 0, true, false, false},
		{"limit insufficient materials", 3, 53, 0, true, false, true},
		{"limit insufficient gold", 3, 53, 0, true, false, true},
		{"knights accepts 800 materials", 2, 800, 0, false, false, false},
		{"god keeps level", 1, 1, 3, true, false, false},
		{"god low fame", 1, 1, 2, true, false, true},
		{"god warehouse material", 1, 1, 3, true, true, false},
		{"god warehouse low fame", 1, 1, 2, true, true, true},
		{"god retains surplus fame", 1, 1, 5, true, false, false},
		{"god retains warehouse surplus fame", 1, 1, 5, true, true, false},
	} {
		t.Run(input.name, func(t *testing.T) {
			base := CardInfo{UniqueID: 1, CardID: 10, Level: 2, LevelMax: 2, Experience: 10, Fame: 3, Love: 5, IsLock: 1}
			transition := gamestate.EvolutionTransition{FromCardID: 10, ToCardID: 11, Type: input.kind, Gold: 100, KeepLevel: input.keep,
				Materials: []gamestate.EvolutionMaterial{{CardID: 20, Num: input.count, Fame: 1}}}
			if input.kind == 3 {
				transition.Materials = []gamestate.EvolutionMaterial{{CardID: 20, Num: 25, Fame: 1}, {CardID: 21, Num: 25, Fame: 1}, {CardID: 22, Num: 3, Fame: 1}}
			}
			s := &Account{onboarding: completedTestOnboarding(), gold: 500, cards: []CardInfo{base}, cardCollectionIDs: map[int]struct{}{},
				cardActions:     gamestate.CardActionState{EvolutionTransitions: []gamestate.EvolutionTransition{transition}},
				cardDefinitions: map[int]gamestate.Card{11: {CardID: 11, LevelMax: 3, ExperienceTableID: 2, LoveMax: 10000, FameMax: 100}},
				cardTemplates:   map[int]CardInfo{11: {CardID: 11, Level: 1, LevelMax: 3}},
				cardExperience:  map[int][]int{2: {20, 30}},
				cardProgression: gamestate.CardProgressionPolicy{FusionGoldPerMaterialPerBaseLevel: 100},
				missionCatalog: missionCatalogFixture(t, map[int]gamestate.MissionDefinition{
					1: {MissionID: 1, Published: true, Title: "进化为指定卡", Rule: &gamestate.MissionRule{Event: gamestate.MissionCardEvolution, SubjectID: 11, Target: 1}},
				}),
				missions: map[int]gamestate.MissionProgress{},
			}
			var uniqueIDs, containerIDs []int64
			var materialIDs []int
			if input.kind == 1 {
				s.cardActions.EvolutionTransitions[0].Materials[0].Fame = 3
				material := CardInfo{UniqueID: 2, CardID: 20, Fame: input.fame}
				if input.container {
					s.containerCards = []CardInfo{material}
					containerIDs = []int64{2}
				} else {
					s.cards = append(s.cards, material)
					uniqueIDs = []int64{2}
				}
			} else {
				for _, material := range transition.Materials {
					s.stackCards = append(s.stackCards, gamestate.CardStack{CardID: material.CardID, Num: material.Num + 1})
					materialIDs = append(materialIDs, material.CardID)
				}
				if input.name == "limit insufficient materials" {
					s.stackCards[1].Num = 24
				}
				if input.name == "limit consumes last mixed materials" {
					for i := range s.stackCards {
						s.stackCards[i].Num--
					}
				}
			}
			if input.name == "limit insufficient gold" {
				s.gold = transition.Gold - 1
			}
			beforeGold := s.gold
			beforeStacks := slices.Clone(s.stackCards)
			beforeCards := len(s.cards) + len(s.containerCards)
			if input.name == "operator closes direct edge" {
				s.ApplyEvolutionRestrictions(NewEvolutionRestrictions([]EvolutionPath{{FromCardID: 10, ToCardID: 11}}))
			}
			if input.name == "closed reverse leaves forward open" {
				s.ApplyEvolutionRestrictions(NewEvolutionRestrictions([]EvolutionPath{{FromCardID: 11, ToCardID: 10}}))
			}
			_, _, _, err := s.EvolveCard(1, 11, uniqueIDs, containerIDs, materialIDs, nil)
			if input.rejected {
				wantCode := -1
				if input.name == "limit insufficient materials" {
					wantCode = -1200
				}
				if input.name == "limit insufficient gold" {
					wantCode = -1030
				}
				var business *BusinessError
				if !errors.As(err, &business) || business.Code != wantCode {
					t.Fatalf("evolution rejection = %v, want code %d", err, wantCode)
				}
				if s.gold != beforeGold || !EqualCardInfo(s.cards[0], base) || len(s.cards)+len(s.containerCards) != beforeCards || !slices.Equal(s.stackCards, beforeStacks) || len(s.cardCollectionIDs) != 0 || len(s.missions) != 0 {
					t.Fatal("invalid evolution changed state")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			result := s.cards[0]
			if s.missions[1].ProgressNow != 1 || s.missions[1].State != 1 {
				t.Fatal("successful evolution did not count the result card once")
			}
			wantLevel, wantExp := 1, 0
			if input.keep {
				wantLevel, wantExp = 2, 20
			}
			wantCards, wantContainer := 1, 0
			if input.kind == 1 && input.fame > 3 {
				remaining := s.cards[1:]
				if input.container {
					wantContainer = 1
					remaining = s.containerCards
				} else {
					wantCards++
				}
				if len(remaining) != 1 || remaining[0].UniqueID != 2 || remaining[0].Fame != input.fame-3 {
					t.Fatal("GOD evolution did not retain the surplus material fame")
				}
			}
			if result.CardID != 11 || result.UniqueID != base.UniqueID || result.Level != wantLevel || result.Experience != wantExp || result.Love != base.Love || result.Fame != base.Fame || result.IsLock != base.IsLock || s.gold != 400 || len(s.cards) != wantCards || len(s.containerCards) != wantContainer {
				t.Fatalf("evolution result or consumption differs: %+v", result)
			}
			if input.kind != 1 {
				remaining := 1
				if input.name == "limit consumes last mixed materials" {
					remaining = 0
					if slices.ContainsFunc(s.StackState(), func(card gamestate.CardStack) bool { return card.Num > 0 }) {
						t.Fatal("depleted evolution materials reappeared after inventory refresh")
					}
				}
				for _, material := range s.stackCards {
					if material.Num != remaining {
						t.Fatal("evolution consumed wrong stack quantity")
					}
				}
			}
			if _, ok := s.cardCollectionIDs[11]; !ok {
				t.Fatal("evolution target was not collected")
			}
		})
	}
}
