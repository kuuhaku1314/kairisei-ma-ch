package game

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestDeckSaveAcceptsNewlyUnlockedEmptySlotInSameRequest(t *testing.T) {
	s := &Account{
		deckSlots: 10, supportSlotCapacity: 10, buddySlots: 5,
		supportUnlockedSlots: []int8{0, 0, 0, 0},
		highestDeckRank:      3,
		cardDefinitions:      make(map[int]gamestate.Card),
		deckRankPolicy: gamestate.DeckRankPolicy{
			ConfigVersion:       1,
			ParameterThresholds: []int{0, 600, 650, 700, 750, 800, 850, 900, 1000, 1100},
			SkillThresholds:     []int{0, 2400, 2500, 2600, 2700, 2800, 2900, 3100, 3300, 3600},
			Cards:               make(map[int]gamestate.CardRankRule),
		},
	}
	empty := DeckInfo{
		ArthurType: 3, JobType: 3, Index: 1, Name: "侠客卡组2",
		CardUniqueIDs: make([]int64, 10), SupportCardUniqueIDs: make([]int64, 10),
		SphereUniqueIDs: make([]int64, 3), BuddyUniqueIDs: make([]int64, 5),
	}
	edited := cloneDeck(empty)
	edited.Index, edited.IsActive = 0, 1
	for i := 0; i < 10; i++ {
		id := i + 1
		s.cards = append(s.cards, CardInfo{UniqueID: int64(id), CardID: id, Level: 35})
		s.cardDefinitions[id] = gamestate.Card{SameCardID: id}
		s.deckRankPolicy.Cards[id] = gamestate.CardRankRule{
			MaximumParameters: [4]int{600, 60, 0, 0},
			SkillPoints:       [5]int{300, 300, 300, 300, 300},
		}
		edited.CardUniqueIDs[i] = int64(id)
	}
	s.decks = []DeckInfo{cloneDeck(edited)}
	// An invalid sibling must not commit either the new rank or the empty slot.
	locked := cloneDeck(empty)
	locked.Index = 2 // A, above the B earned by this edit.
	if _, _, err := s.SetDecks([]DeckInfo{edited, locked}); err == nil || s.highestDeckRank != 3 || len(s.decks) != 1 {
		t.Fatal("rejected batch changed rank or deck slots")
	}
	// The client may serialize the empty slot first. Its default deck_rank=0
	// must not prevent the valid first deck from unlocking B in this batch.
	if _, _, err := s.SetDecks([]DeckInfo{empty, edited}); err != nil {
		t.Fatal(err)
	}
	saved := s.Snapshot(gamestate.State{})
	if saved.User.ArthurRank != 8 || len(saved.Decks) != 2 || !reflect.DeepEqual(saved.Decks[1].CardUniqueIDs, empty.CardUniqueIDs) || saved.Decks[1].IsActive != 0 {
		t.Fatal("B unlock must retain the new inactive, unconfigured deck in the save")
	}
}

func TestDeckRankRetainsHistoricalUnlockAfterChangingToWeakerDeck(t *testing.T) {
	s := &Account{
		lastHomeDeckRank: 8,
		deckRankPolicy: gamestate.DeckRankPolicy{
			ConfigVersion:       1,
			ParameterThresholds: []int{0, 600, 650, 700, 750, 800, 850, 900, 1000, 1100, 1250, 1800, 2300, 2600, 4000, 5000, 6000, 7000},
			SkillThresholds:     []int{0, 2400, 2500, 2600, 2700, 2800, 2900, 3100, 3300, 3600, 4000, 4500, 5000, 5600, 5800, 6000, 6200, 6400},
			Cards:               map[int]gamestate.CardRankRule{},
		},
		unlockedFeatureIDs: map[uint]struct{}{0: {}},
		decks:              []DeckInfo{{ArthurType: 1, DeckRank: 17}},
	}
	for i := 0; i < 10; i++ {
		id := int64(i + 1)
		s.cards = append(s.cards, CardInfo{UniqueID: id, CardID: i + 1, Level: 1})
		s.decks[0].CardUniqueIDs = append(s.decks[0].CardUniqueIDs, id)
		s.deckRankPolicy.Cards[i+1] = gamestate.CardRankRule{
			MaximumParameters: [4]int{600, 60, 0, 0},
			SkillPoints:       [5]int{300, 400, 300, 300, 300},
		}
	}
	// Parameter score 1600 and skill score 4010 both reach A; the supplied
	// SSSS rank must neither be echoed nor raise the account's high-water mark.
	saved := s.Snapshot(gamestate.State{})
	if saved.User.ArthurRank != 10 || saved.Decks[0].DeckRank != 10 {
		t.Fatalf("rank must be computed: account=%d deck=%d", saved.User.ArthurRank, saved.Decks[0].DeckRank)
	}
	if first, again := s.HomeDeckRank(), s.HomeDeckRank(); first != 8<<16|10 || again != 10<<16|10 {
		t.Fatalf("unlock notification must report the previous Home rank once: %d / %d", first, again)
	}
	for feature := uint(0); feature < 4; feature++ {
		if !s.FeatureUnlocked(feature) {
			t.Fatalf("profession %d did not unlock at A", feature)
		}
	}
	// Switching to a nonmatching profession lowers skill evaluation to C.
	// The persisted account maximum and already-open professions must survive.
	s.decks[0].ArthurType = 2
	saved = s.Snapshot(saved)
	if saved.User.ArthurRank != 10 || saved.Decks[0].DeckRank != 6 {
		t.Fatalf("historical unlock regressed: account=%d deck=%d", saved.User.ArthurRank, saved.Decks[0].DeckRank)
	}
	if len(saved.User.UnlockedFeatureIDs) != 4 || requiredDeckRank(2) > saved.User.ArthurRank || requiredDeckRank(3) <= saved.User.ArthurRank {
		t.Fatal("saved A progress must retain professions and the first three deck slots")
	}
	if saved.User.LastHomeDeckRank != 10 {
		t.Fatal("last Home rank must survive save and reload")
	}
}

func TestDeckRankCardUseAllowsHiddenDeckMaterialBeforeA(t *testing.T) {
	s := &Account{
		deckRankPolicy: gamestate.DeckRankPolicy{ConfigVersion: 1}, highestDeckRank: 9,
		currentActiveArthur: 1,
		cards:               []CardInfo{{UniqueID: 1, CardID: 101}, {UniqueID: 2, CardID: 102}, {UniqueID: 3, CardID: 101}, {UniqueID: 4, CardID: 102}},
		cardDefinitions:     map[int]gamestate.Card{101: {SellGold: 10}, 102: {SellGold: 10}},
		decks: []DeckInfo{
			{ArthurType: 1, IsActive: 1, CardUniqueIDs: []int64{1}},
			{ArthurType: 2, IsActive: 1, CardUniqueIDs: []int64{2}, SupportCardUniqueIDs: []int64{2}},
			{ArthurType: 5, IsActive: 1, CardUniqueIDs: []int64{3}, SupportCardUniqueIDs: []int64{4}},
		},
	}
	if _, err := s.SellCards([]int64{1}, nil); err == nil {
		t.Fatal("selected deck card must remain protected before A")
	}
	for _, id := range []int64{3, 4} {
		if _, err := s.SellCards([]int64{id}, nil); err != ErrCardInDeck {
			t.Fatalf("Alchemist main and support cards must remain protected before A, uid %d: %v", id, err)
		}
	}
	s.highestDeckRank = 10
	if _, err := s.SellCards([]int64{2}, nil); err == nil {
		t.Fatal("all decks must be protected at A")
	}
	s.highestDeckRank = 9
	if _, err := s.SellCards([]int64{2}, nil); err != nil {
		t.Fatal(err)
	}
	if len(s.cards) != 3 || s.decks[1].CardUniqueIDs[0] != 0 || s.decks[1].SupportCardUniqueIDs[0] != 0 {
		t.Fatal("consumed hidden-deck cards must not leave dangling references")
	}
}
