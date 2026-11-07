package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestRarityFiveTicketGachaConsumesOfficialItemAndAddsCard(t *testing.T) {
	const (
		gachaID = 60200301
		itemID  = 9010
		cardID  = 5001
	)
	cardDefinition := gamestate.Card{
		CardID:            cardID,
		Name:              "五星测试卡",
		RarityRank:        5,
		LevelMax:          1,
		LoveMax:           0,
		FameMax:           100,
		ExperienceTableID: 1,
	}
	cardStore := &Account{
		gachas: []gamestate.GachaProfile{{
			GachaID: gachaID, PayType: 4, PayTypeID: itemID, Price: 1,
			CardNum: 1, CardNumMax: 1, CardIDs: []int{cardID}, CardWeights: []int{1},
			CardFames: map[int]int{cardID: 77},
		}},
		items: map[int]gamestate.Item{itemID: {ItemID: itemID, Num: 2}},
		itemDefinitions: map[int]gamestate.ItemDefinition{
			itemID: {ItemID: itemID, ItemType: "GACHA_TICKET", MaxOwned: 9999999},
		},
		cardDefinitions:   map[int]gamestate.Card{cardID: cardDefinition},
		cardTemplates:     map[int]CardInfo{cardID: {CardID: cardID, LevelMax: 1, Fame: 1, SkillLevels: []int16{1}}},
		cardExperience:    map[int][]int{1: {}},
		cardCollectionIDs: map[int]struct{}{},
		cardMax:           10,
		nextUniqueID:      1,
		cardProgression: gamestate.CardProgressionPolicy{
			ConfigVersion: 1, FusionGoldPerMaterialPerBaseLevel: 1,
		},
	}

	beforeDraw := int(time.Now().Unix())
	result, err := cardStore.PlayGacha(gachaID, 4, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Item.ItemID != itemID || result.Item.Num != 1 || cardStore.items[itemID].Num != 1 {
		t.Fatalf("ticket state = result %+v / store %+v", result.Item, cardStore.items[itemID])
	}
	if len(cardStore.cards) != 1 || cardStore.cards[0].CardID != cardID ||
		cardStore.cardDefinitions[cardStore.cards[0].CardID].RarityRank != 5 {
		t.Fatalf("rarity-five reward was not added: %+v", cardStore.cards)
	}
	if cardStore.cards[0].Fame != 77 {
		t.Fatalf("configured pool fame not applied to the drawn card: %d", cardStore.cards[0].Fame)
	}
	if card := cardStore.cards[0]; card.CreateTime < beforeDraw || card.CreateTime > int(time.Now().Unix()) || result.Reward.Cards[0].CreateTime != card.CreateTime {
		t.Fatalf("new card acquisition time is missing from inventory or draw response: %+v", card)
	}
	if saved := snapshotCards(cardStore.cards, nil); saved[0].CreateTime != cardStore.cards[0].CreateTime || CardsFromState(saved, 0)[0].CreateTime != cardStore.cards[0].CreateTime {
		t.Fatal("card acquisition time was lost between runtime and persisted state")
	}
	if pool := GachaPoolRewards(cardStore.gachas[0]); pool[0].CardFame != 77 {
		t.Fatalf("lineup shows fame %d, want configured 77", pool[0].CardFame)
	}
	if cardStore.gachas[0].PlayCount != 1 {
		t.Fatalf("play count = %d, want 1", cardStore.gachas[0].PlayCount)
	}
	if result.Expectancy != 4 {
		t.Fatalf("five-star draw expectancy=%d, want ULTRARARE(4)", result.Expectancy)
	}

	cardStore.items[itemID] = gamestate.Item{ItemID: itemID}
	beforeCards := len(cardStore.cards)
	if _, err := cardStore.PlayGacha(gachaID, 4, nil); err == nil {
		t.Fatal("expected insufficient ticket rejection")
	}
	if len(cardStore.cards) != beforeCards || cardStore.gachas[0].PlayCount != 1 {
		t.Fatal("failed draw mutated cards or play count")
	}
	// Selling the drawn card must not make its family "unowned" again.
	cardStore.cards = nil
	cardStore.items[itemID] = gamestate.Item{ItemID: itemID, Num: 1}
	cardStore.gachas[0].UnownedOnly = true
	// A different acquired form must also exclude the base family.
	sameFamily := cardDefinition
	sameFamily.CardID = cardID + 1
	cardStore.cardDefinitions[cardID+1] = sameFamily
	delete(cardStore.cardCollectionIDs, cardID)
	cardStore.cardCollectionIDs[cardID+1] = struct{}{}
	if len(cardStore.VisibleGachasLocked()) != 0 {
		t.Fatal("exhausted unowned pool remains published")
	}
	if _, err := cardStore.PlayGacha(gachaID, 4, nil); err == nil || cardStore.items[itemID].Num != 1 || cardStore.gachas[0].PlayCount != 1 {
		t.Fatal("unowned pool charged for a previously collected card")
	}
}

func TestUnavailableOnlyTicketSingleRemainsVisibleForItemLackTips(t *testing.T) {
	const (
		singleID = 60200301
		multiID  = 60200302
		itemID   = 9010
	)
	cardStore := &Account{
		gachas: []gamestate.GachaProfile{
			{GachaID: singleID, GroupID: singleID, CategoryNum: 5, PayType: 4, PayTypeID: itemID, Price: 1, CardNum: 1, CardNumMax: 1},
			{GachaID: multiID, GroupID: singleID, CategoryNum: 5, PayType: 4, PayTypeID: itemID, Price: 10, CardNum: 10, CardNumMax: 10},
		},
		items: map[int]gamestate.Item{itemID: {ItemID: itemID, Num: 0}},
	}

	visible := cardStore.GachaState()
	if len(visible) != 2 || visible[0].GachaID != singleID || visible[1].GachaID != multiID {
		t.Fatalf("visible gachas = %+v, want unavailable single followed by multi", visible)
	}
}

func TestUnavailableTicketSingleYieldsToUsableCrystalSingle(t *testing.T) {
	const (
		ticketID  = 60200201
		crystalID = 60200211
		multiID   = 60200212
		itemID    = 2000
	)
	cardStore := &Account{
		gachas: []gamestate.GachaProfile{
			{GachaID: ticketID, GroupID: ticketID, CategoryNum: 4, PayType: 4, PayTypeID: itemID, Price: 1, CardNum: 1, CardNumMax: 1},
			{GachaID: crystalID, GroupID: ticketID, CategoryNum: 4, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1},
			{GachaID: multiID, GroupID: ticketID, CategoryNum: 4, PayType: 3, Price: 50, CardNum: 10, CardNumMax: 10},
		},
		items: map[int]gamestate.Item{itemID: {ItemID: itemID, Num: 0}},
	}

	visible := cardStore.GachaState()
	if len(visible) != 2 || visible[0].GachaID != crystalID || visible[1].GachaID != multiID {
		t.Fatalf("visible gachas = %+v, want usable crystal single followed by multi", visible)
	}
}

func TestCrystalGachaTicketQuickBuyConsumesFreeCrystalAndStaysHidden(t *testing.T) {
	const (
		lineupID = 992001
		itemID   = 9010
	)
	cardStore := &Account{
		coin:     3,
		coinFree: 2,
		itemShopTabs: []gamestate.ItemShopTab{{
			TabType: 0,
			Lineup: []gamestate.ItemShopLineup{{
				LineupID: lineupID, PictID: 90010, LineupName: "水晶扭蛋币快捷兑换",
				PayType: 3, Price: 1, BuyNumMax: 5000, Hidden: true,
				Interiors: []gamestate.ItemShopInterior{{BuyType: 1, BuyTypeID: itemID, Num: 1}},
			}},
		}},
		items: map[int]gamestate.Item{itemID: {ItemID: itemID}},
		itemDefinitions: map[int]gamestate.ItemDefinition{
			itemID: {ItemID: itemID, ItemType: "GACHA_TICKET", MaxOwned: 9999999},
		},
	}

	updated, err := cardStore.BuyItemShop(lineupID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(updated.Items) != 1 || updated.Items[0].ItemID != itemID || updated.Items[0].Num != 2 ||
		cardStore.coin != 3 || cardStore.coinFree != 0 {
		t.Fatalf("quick buy result = %+v, paid/free = %d/%d", updated, cardStore.coin, cardStore.coinFree)
	}
}
