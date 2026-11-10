package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestItemShopOperatorPolicyControlsDisplayAndCharge(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(),
		gold: 30000,
		itemShopTabs: []gamestate.ItemShopTab{{TabType: 0, Lineup: []gamestate.ItemShopLineup{{
			LineupID: 602004, PayType: 1, Price: 10000, BuyNumMax: 99,
			Interiors: []gamestate.ItemShopInterior{{BuyType: 1, BuyTypeID: 3999, Num: 1}},
		}}}},
		items:           map[int]gamestate.Item{},
		itemDefinitions: map[int]gamestate.ItemDefinition{3999: {ItemID: 3999, MaxOwned: 9999}},
	}
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: true, Price: 12000}}})
	tabs, _ := s.ItemShopState()
	visible := tabs[0].Lineup
	if len(visible) != 1 || visible[0].Price != 12000 {
		t.Fatalf("operator price not displayed: %+v", visible)
	}
	if _, err := s.BuyItemShop(602004, 2); err != nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatalf("operator purchase: gold=%d items=%+v err=%v", s.gold, s.items, err)
	}
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: []ItemShopSetting{{LineupID: 602004, Enabled: false, Price: 12000}}})
	tabs, _ = s.ItemShopState()
	if !tabs[0].Lineup[0].Disabled {
		t.Fatal("disabled offer remained visible")
	}
	if _, err := s.BuyItemShop(602004, 1); err == nil || s.gold != 6000 || s.items[3999].Num != 2 {
		t.Fatal("disabled offer changed player balances")
	}
	if base := s.itemShopTabs[0].Lineup[0]; base.Disabled || base.Price != 10000 {
		t.Fatal("operator override mutated the seed copied into persistence")
	}
}

func TestItemShopPeriodsAndNativeProductKinds(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), gold: 10000, cardMax: 5990, cardContainerMax: 6000, items: map[int]gamestate.Item{}, itemDefinitions: map[int]gamestate.ItemDefinition{1000: {ItemID: 1000, Name: "恢复药", PictID: 10080, MaxOwned: 999, Description: "恢复体力。"}}, collectionRewardIDs: map[[2]int]struct{}{{16, 123}: {}}, stampIDs: map[int]struct{}{}}
	for i := 0; i < 5; i++ {
		s.itemShopTabs = append(s.itemShopTabs, gamestate.ItemShopTab{TabType: i})
	}
	rows := []ItemShopSetting{
		{LineupID: 70000001, Enabled: true, Price: 10, ItemID: 1000, BuyType: 1, Name: "恢复药", PayType: 1, Quantity: 5, BuyNumMax: 10, TotalLimit: 3, Period: "day", PeriodLimit: 2},
		{LineupID: 70000002, Enabled: true, Price: 20, ItemID: 123, BuyType: 2, Name: "表情", TabType: 2, PayType: 1, Quantity: 1, BuyNumMax: 1},
		{LineupID: 70000003, Enabled: true, Price: 30, BuyType: 4, Name: "持有扩容", TabType: 4, PayType: 1, Quantity: 5, BuyNumMax: 1},
	}
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: rows})
	if _, err := s.BuyItemShop(70000001, 2); err != nil || s.items[1000].Num != 10 || s.gold != 9980 {
		t.Fatal("item grant/payment", err)
	}
	if _, err := s.BuyItemShop(70000001, 1); err == nil || s.gold != 9980 || s.itemShopPurchases[70000001] != 2 {
		t.Fatal("daily limit charged or counted rejected purchase")
	}
	rows[0].Period = "week"
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: rows})
	if _, err := s.BuyItemShop(70000001, 1); err == nil {
		t.Fatal("changing day to week reset current purchases")
	}
	rows[0].Period = "month"
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: rows})
	if _, err := s.BuyItemShop(70000001, 1); err == nil {
		t.Fatal("changing week to month reset current purchases")
	}
	beforeTabs, _ := s.ItemShopState()
	if beforeTabs[0].Lineup[0].Note != "恢复体力。" || beforeTabs[4].Lineup[0].Note != "卡牌持有上限增加5格。" {
		t.Fatal("shop descriptions missing or overwritten by quota settings")
	}
	if len(beforeTabs[2].Lineup) != 1 || beforeTabs[2].Lineup[0].PictID != 0 {
		t.Fatal("unowned stamp missing or routed through item icon")
	}
	stamp, err := s.BuyItemShop(70000002, 1)
	if err != nil || len(stamp.StampIDs) != 1 || stamp.StampIDs[0] != 123 || len(stamp.Items) != 0 {
		t.Fatal("native stamp delivery", err)
	}
	before := s.gold
	if _, err := s.BuyItemShop(70000002, 1); err == nil || s.gold != before {
		t.Fatal("owned stamp charged twice")
	}
	if _, err := s.BuyItemShop(70000003, 1); err != nil || s.cardMax != 5995 {
		t.Fatal("capacity purchase", err)
	}
	tabs, _ := s.ItemShopState()
	if len(tabs[0].Lineup) != 1 || len(tabs[2].Lineup) != 0 || len(tabs[4].Lineup) != 1 {
		t.Fatal("native product tabs or owned stamp visibility")
	}
	// Sep 30 23:59 China time crosses a month, then a week. Counters track
	// all dimensions even when no periodic limit was selected at purchase.
	clock := time.Date(2026, 9, 30, 15, 59, 0, 0, time.UTC)
	s.itemShopPurchases[70000001] = 0
	delete(s.itemShopPeriods, 70000001)
	s.recordItemShopPurchase(70000001, 2, clock)
	base := s.itemShopBaseTabs()[0].Lineup[0]
	for _, tc := range []struct {
		period    string
		now       time.Time
		remaining int
	}{
		{"day", clock, 0}, {"day", clock.Add(2 * time.Minute), 1},
		{"week", clock.Add(24 * time.Hour), 0}, {"week", clock.Add(5 * 24 * time.Hour), 1},
		{"month", clock, 0}, {"month", clock.Add(2 * time.Minute), 1},
	} {
		rows[0].Period = tc.period
		s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: rows})
		if got := s.itemShopLineupAt(base, tc.now).StockRemain; got != tc.remaining {
			t.Fatalf("period %s at %s: %d", tc.period, tc.now, got)
		}
	}
	rows[0].TotalLimit = 1
	s.ApplyRuntimeSettings(RuntimeSettings{ItemShop: rows})
	if s.itemShopLineupAt(base, clock.Add(40*24*time.Hour)).StockRemain != 0 {
		t.Fatal("lowering total limit reset history")
	}
}
