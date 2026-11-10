package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestExchangeConfigurationPreservesCountsAndChargesDisplayedPrice(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), items: map[int]gamestate.Item{4000: {ItemID: 4000, Num: 1000}}, itemDefinitions: map[int]gamestate.ItemDefinition{4000: {ItemID: 4000, MaxOwned: 9999}, 1000: {ItemID: 1000, MaxOwned: 9999}}, tradeShopPurchases: map[int]int{2: 2}}
	base := gamestate.State{TradeShopProfiles: []gamestate.TradeShopProfile{{TradeShopID: 1, Name: "交换所", Lineups: []gamestate.TradeShopLineupProfile{{LineupID: 2, LineupName: "药水", StockNum: 3, Prices: []gamestate.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 25, PointCardCondition: []gamestate.TradeShopPointCardCondition{}}}, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 1000, Num: 2, CardSkillLevels: []int16{}}}}}}}}
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	rows := s.TradeShopState()
	if len(rows) != 1 || rows[0].Lineups[0].StockRemain != 1 || rows[0].Lineups[0].Profile.Prices[0].Num != 25 {
		t.Fatal("display lost price or purchase count")
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err != nil || s.items[4000].Num != 975 || s.items[1000].Num != 2 || s.tradeShopPurchases[2] != 3 {
		t.Fatalf("wrong exchange %v", err)
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("limit not enforced")
	}
	base.TradeShopProfiles[0].Lineups[0].Disabled = true
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	if len(s.TradeShopState()) != 0 {
		t.Fatal("disabled offer displayed")
	}
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("stale page purchased disabled offer")
	}
	if s.tradeShopPurchases[2] != 3 {
		t.Fatal("operator changes reset player history")
	}
	s.ApplyTradeShopConfiguration(nil)
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.tradeShopPurchases[2] != 3 {
		t.Fatal("deleted shop accepted a stale purchase or lost history")
	}
	base.TradeShopProfiles[0].Lineups[0].Disabled = false
	s.ApplyTradeShopConfiguration(base.TradeShopProfiles)
	if _, err := s.BuyTradeShop(2, 1, nil); err == nil || s.items[4000].Num != 975 {
		t.Fatal("restoring shop reset its purchase limit")
	}
	// The same reward in a newly configured shop has its own limit identity.
	second := cloneTradeShopProfile(base.TradeShopProfiles[0])
	second.TradeShopID, second.Lineups[0].LineupID = 61000001, 62000001
	s.ApplyTradeShopConfiguration(append(base.TradeShopProfiles, second))
	if rows := s.TradeShopState(); rows[0].Lineups[0].StockRemain != 0 || rows[1].Lineups[0].StockRemain != 3 {
		t.Fatal("new shop inherited another lineup's exhausted stock")
	}
	if _, err := s.BuyTradeShop(62000001, 1, nil); err != nil || s.tradeShopPurchases[2] != 3 || s.tradeShopPurchases[62000001] != 1 || s.items[4000].Num != 950 {
		t.Fatal("same-item offers share purchase counts", err)
	}
	// A shop with a future opening time is neither listed nor purchasable until it opens.
	second.StartTime = int(time.Now().Add(time.Hour).Unix())
	s.ApplyTradeShopConfiguration(append(base.TradeShopProfiles, second))
	if rows := s.TradeShopState(); len(rows) != 1 || rows[0].Profile.TradeShopID != 1 {
		t.Fatal("scheduled shop listed before opening")
	}
	if _, err := s.BuyTradeShop(62000001, 1, nil); err == nil || s.tradeShopPurchases[62000001] != 1 {
		t.Fatal("scheduled shop sold before opening")
	}
}

func TestContentCatalogKeepsProgressWhenMovingGroups(t *testing.T) {
	account := json.RawMessage(`{"9":[],"10":[{"10":[{"0":7,"10":2,"12":[]}]}],"11":[],"12":[]}`)
	catalog := json.RawMessage(`{"9":[{"10":[{"0":7,"10":0,"12":[{"0":99}]}]}],"10":[],"11":[],"12":[]}`)
	configuration, err := NewContentConfiguration(1, gamestate.State{TeamBattleSolo: catalog})
	if err != nil {
		t.Fatal(err)
	}
	state := ApplyContentState(gamestate.State{TeamBattleSolo: account}, configuration)
	var top map[string][]struct {
		Bosses []struct {
			ID    int              `json:"0"`
			State int              `json:"10"`
			Cards []map[string]int `json:"12"`
		} `json:"10"`
	}
	if err := json.Unmarshal(state.TeamBattleSolo, &top); err != nil {
		t.Fatal(err)
	}
	if top["9"][0].Bosses[0].State != 2 || top["9"][0].Bosses[0].Cards[0]["0"] != 99 {
		t.Fatal("catalog update did not preserve progress and replace definitions")
	}
	s := &Account{onboarding: completedTestOnboarding(), teamBattleSolo: battleCatalogFixture(t, state.TeamBattleSolo)}
	s.ApplyBattleCatalogConfiguration(battleDefinitionsFixture(t, catalog), map[int]bool{7: true})
	if err := json.Unmarshal(filterBattleCatalog(s.teamBattleSolo.saved(), s.disabledTeamBattleBossIDs), &top); err != nil || len(top["9"][0].Bosses) != 0 {
		t.Fatal("closed entry is still published", err)
	}
	s.ApplyBattleCatalogConfiguration(battleDefinitionsFixture(t, catalog), nil)
	if err := json.Unmarshal(s.teamBattleSolo.saved(), &top); err != nil || top["9"][0].Bosses[0].State != 2 {
		t.Fatal("reopening difficulty lost clear progress", err)
	}
}

func TestBattleCatalogRequestOwnsPublicationAndPreservesClosedClears(t *testing.T) {
	raw := json.RawMessage(`{"future":9007199254740993,"9":[{"0":760000001,"9":0,"10":[{"0":30010101,"10":1,"14":1,"16":1},{"0":30010102,"10":2,"14":1,"16":0}]}],"10":[],"11":[],"12":[]}`)
	s := &Account{onboarding: completedTestOnboarding(), teamBattleSolo: battleCatalogFixture(t, raw), disabledTeamBattleBossIDs: map[int]bool{30010102: true}}
	view, err := s.TeamBattleCatalog()
	if err != nil {
		t.Fatal(err)
	}
	if !view.HasBoss(30010101) || view.HasBoss(30010102) {
		t.Fatal("published boss membership differs from disabled rules")
	}
	if !view.BossAccessible(30010101, time.Now().Unix()) || view.BossAccessible(30010102, time.Now().Unix()) {
		t.Fatal("entry lookup differs from refreshed publication and disabled rules")
	}
	group := view.Groups("10")[0].(map[string]any)
	boss := group["10"].([]any)[0].(map[string]any)
	if boss["10"] != json.Number("0") || boss["16"] != json.Number("0") {
		t.Fatal("expired buff was not reset in publication")
	}
	// Mutating the response cannot leak into a later request or saved progress.
	boss["10"] = json.Number("2")
	saved, err := decodeTeamBattleCatalog(s.teamBattleSolo.saved())
	if err != nil || saved.fields["future"] != json.Number("9007199254740993") || len(saved.groups["9"][0].bosses) != 2 {
		t.Fatal("expiry changed unmodeled fields or removed closed progress", err)
	}
	if saved.groups["9"][0].bosses[0].fields["10"] != json.Number("0") {
		t.Fatal("response mutation reached account state")
	}
	s.ApplyBattleCatalogConfiguration(battleDefinitionsFixture(t, raw), nil)
	reopened, err := s.TeamBattleCatalog()
	if err != nil || !reopened.HasBoss(30010102) {
		t.Fatal("reopened boss was not published", err)
	}
	bosses := reopened.Groups("10")[0].(map[string]any)["10"].([]any)
	if bosses[1].(map[string]any)["10"] != json.Number("2") {
		t.Fatal("reopening a difficulty reset its first clear")
	}
}
