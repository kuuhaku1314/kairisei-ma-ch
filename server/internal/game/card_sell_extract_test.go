package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardSellExtractUsesBaseRarityAndSharesSaleSettlement(t *testing.T) {
	policy := &gamestate.CardSellExtractPolicy{
		ItemID:            8000,
		CountByBaseRarity: map[int]int{4: 10, 5: 50, 6: 1},
		BaseCardIDs:       map[int]int{101: 100, 102: 100, 300: 400},
		BaseRarities:      map[int]int{100: 5, 400: 4},
	}
	newAccount := func(owned, max int) *Account {
		return &Account{
			cards: []CardInfo{{UniqueID: 1, CardID: 101, Fame: 1}, {UniqueID: 2, CardID: 102, Fame: 100}},
			cardDefinitions: map[int]gamestate.Card{
				100: {CardID: 100, RarityRank: 1},
				101: {CardID: 101, RarityRank: 6, SellGold: 5},
				102: {CardID: 102, RarityRank: 6, SellGold: 5},
			},
			stackCards:      []gamestate.CardStack{{CardID: 300, Num: 3, BaseAddPrice: 2}},
			items:           map[int]gamestate.Item{8000: {ItemID: 8000, Num: owned}},
			itemDefinitions: map[int]gamestate.ItemDefinition{8000: {ItemID: 8000, MaxOwned: max}},
			cardSellExtract: policy,
		}
	}

	s := newAccount(0, 999999)
	result, err := s.SellCards([]int64{1, 2}, []gamestate.CardStackUse{{CardID: 300, Num: 3}})
	if err != nil || result.GetGold != 16 || result.Gold != 16 || len(s.cards) != 0 || s.stackCards[0].Num != 0 ||
		s.items[8000].Num != 130 || result.Extract.InPresentBox || len(result.Extract.Rewards) != 1 ||
		result.Extract.Rewards[0].Reward.Num != 130 {
		t.Fatalf("sale and extract differ: result=%+v account=%+v error=%v", result, s, err)
	}

	s = newAccount(999999, 999999)
	result, err = s.SellCards([]int64{1}, nil)
	if err != nil || result.GetGold != 5 || len(s.cards) != 1 || s.items[8000].Num != 999999 ||
		!result.Extract.InPresentBox || len(s.presents) != 1 || s.presents[0].Reward.Num != 50 ||
		len(result.Extract.Rewards) != 1 || !result.Extract.Rewards[0].InPresentBox {
		t.Fatalf("full inventory lost sale extract: result=%+v account=%+v error=%v", result, s, err)
	}
	if _, err := s.SellCards([]int64{2, 2}, nil); err == nil || len(s.cards) != 1 || len(s.presents) != 1 {
		t.Fatal("failed sale changed cards or duplicated extract")
	}

	s = newAccount(0, 999999)
	s.containerCards = s.cards[:1]
	s.cards = nil
	result, err = s.SellContainerCards([]int64{1})
	if err != nil || result.GetGold != 5 || len(s.containerCards) != 0 || s.items[8000].Num != 50 ||
		result.Extract.InPresentBox || len(s.presents) != 0 || len(result.Extract.Rewards) != 1 ||
		result.Extract.Rewards[0].Reward.Num != 50 {
		t.Fatalf("warehouse sale omitted extract: result=%+v error=%v", result, err)
	}

	s = newAccount(999999, 999999)
	s.containerCards = s.cards[:1]
	s.cards = nil
	if _, err := s.SellContainerCards([]int64{1, 1}); err == nil || len(s.containerCards) != 1 || len(s.presents) != 0 {
		t.Fatal("failed warehouse sale changed cards or granted extract")
	}
	result, err = s.SellContainerCards([]int64{1})
	if err != nil || result.GetGold != 5 || len(s.containerCards) != 0 || s.items[8000].Num != 999999 ||
		!result.Extract.InPresentBox || len(s.presents) != 1 || s.presents[0].Reward.Num != 50 {
		t.Fatalf("full inventory lost warehouse extract: result=%+v error=%v", result, err)
	}
}
