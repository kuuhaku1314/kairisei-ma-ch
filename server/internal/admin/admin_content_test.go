package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestAdminContentAtomicConfigAndRestart(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{TeamBattleSolo: json.RawMessage(`{"9":[],"10":[{"0":1,"4":"测试","10":[{"0":11,"4":"中级","12":[{"0":1,"1":0}]}]}],"11":[],"12":[]}`),
		TeamBattleRewards: []gamestate.TeamBattleRewardProfile{{BossID: 11, ResultRewards: []gamestate.Reward{{Type: 0, Num: 2}, {Type: 4, Num: 50}}, EnemyDrops: []gamestate.TeamBattleEnemyDrop{{Reward: gamestate.Reward{Type: 4, Num: 50, CardSkillLevels: []int16{}}}}}},
		TradeShopProfiles: []gamestate.TradeShopProfile{{TradeShopID: 1, Name: "原兑换所", ShopType: 1, Lineups: []gamestate.TradeShopLineupProfile{{LineupID: 2, LineupName: "道具", StockNum: 3, Prices: []gamestate.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 10, PointCardCondition: []gamestate.TradeShopPointCardCondition{}}}, Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 1000, Num: 1, CardSkillLevels: []int16{}}}}}}}}
	if err := ops.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	row := ops.content.bosses[11]
	row.Targets = []dropTarget{{BattleIndex: 0, EnemyIndex: 0, Name: "本体"}}
	ops.content.bosses[11] = row
	admin := &API{operations: ops, catalogByKey: map[string]AdminCatalogEntry{"8:1000": {Name: "药水"}, "8:4000": {Name: "币"}}}
	router := chi.NewRouter()
	router.Put("/drops", admin.saveDropEditor)
	router.Put("/shops/{shopID}", admin.saveExchangeEditor)
	router.Delete("/shops/{shopID}", admin.changeExchangeDeletion)
	router.Post("/shops/{shopID}/restore", admin.changeExchangeDeletion)
	config := BossDrops{BossID: 11, Drops: []gamestate.TeamBattleEnemyDrop{{Reward: gamestate.Reward{Type: 8, RewardTypeID: 1000, Num: 3, CardSkillLevels: []int16{}}}}}
	config.FameRewards = []gamestate.Reward{{Type: 8, RewardTypeID: 4000, Num: 600, CardSkillLevels: []int16{}}}
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []BossDrops{config}}, 200)
	p := ops.content.configuration.State.TeamBattleRewards[0]
	if len(p.EnemyDrops) != 2 || p.EnemyDrops[0].Reward.Type != 4 || p.EnemyDrops[0].Reward.Num != 50 || p.ResultRewards[0].Type != 0 {
		t.Fatal("fixed gold/EXP lost")
	}
	// Eight editable rows plus the fixed gold would overflow EnemyData.drop1..8.
	// Reject before persisting or publishing any part of the new configuration.
	overflow := config
	overflow.Drops = make([]gamestate.TeamBattleEnemyDrop, 8)
	for i := range overflow.Drops {
		overflow.Drops[i] = config.Drops[0]
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 1, "configs": []BossDrops{overflow}}, 400)
	if ops.content.dropRevision != 1 || len(ops.content.configuration.State.TeamBattleRewards[0].EnemyDrops) != 2 {
		t.Fatal("native slot overflow was published")
	}
	boundary := overflow
	boundary.Drops = boundary.Drops[:7]
	boundary.Drops[0].Reward.Num = 100 // Quantity does not consume extra slots.
	if _, err := ops.content.project(map[int]BossDrops{11: boundary}, ops.content.shops, ops.content.rules); err != nil {
		t.Fatal("eight final slots should remain valid", err)
	}
	bad := config
	bad.BossID = 99
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 1, "configs": []BossDrops{config, bad}}, 400)
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []BossDrops{config}}, 409)
	if ops.content.dropRevision != 1 {
		t.Fatal("failed batch advanced revision")
	}
	shop := base.TradeShopProfiles[0]
	shop.Disabled = true
	testfixture.CallContentAdmin(t, router, "PUT", "/shops/1", map[string]any{"expected_revision": 0, "shop": shop}, 200)
	shop.Lineups = nil
	testfixture.CallContentAdmin(t, router, "PUT", "/shops/1", map[string]any{"expected_revision": 1, "shop": shop}, 400)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if restarted.content.dropRevision != 1 || restarted.content.shopRevision != 1 || !restarted.content.configuration.State.TradeShopProfiles[0].Disabled {
		t.Fatal("public config lost on restart")
	}
	if rewards := restarted.content.configuration.State.TeamBattleRewards[0].FameRewards; len(rewards) != 1 || rewards[0].Num != 600 {
		t.Fatal("fame pool lost on restart")
	}
	if base.TradeShopProfiles[0].Disabled || len(base.TeamBattleRewards[0].EnemyDrops) != 1 {
		t.Fatal("overrides mutated base")
	}
	testfixture.CallContentAdmin(t, router, "DELETE", "/shops/1", map[string]any{"expected_revision": 0}, 409)
	testfixture.CallContentAdmin(t, router, "DELETE", "/shops/1", map[string]any{"expected_revision": 1}, 200)
	if len(ops.content.configuration.State.TradeShopProfiles) != 0 || ops.content.exchangeShops()[0].Lineups[0].LineupID != 2 {
		t.Fatal("deletion retained a live shop or removed purchase identities")
	}
	testfixture.CallContentAdmin(t, router, "PUT", "/shops/1", map[string]any{"expected_revision": 2, "shop": base.TradeShopProfiles[0]}, 409)
	if err := restarted.InitializeContent(base, ""); err != nil || len(restarted.content.configuration.State.TradeShopProfiles) != 0 {
		t.Fatal("deleted shop resurrected on restart", err)
	}
	testfixture.CallContentAdmin(t, router, "POST", "/shops/1/restore", map[string]any{"expected_revision": 2}, 200)
	restored := ops.content.configuration.State.TradeShopProfiles
	if len(restored) != 1 || !restored[0].Disabled || restored[0].Lineups[0].LineupID != 2 {
		t.Fatal("restored shop was automatically opened or lost lineup identity")
	}
}

func TestBossDifficultyModesShareDropsAndPersist(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	ops, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	base := gamestate.State{
		TeamBattleSolo:            json.RawMessage(`{"9":[],"10":[{"0":1,"4":"BOSS","10":[{"0":11,"1":0,"4":"上级","5":15,"6":8,"7":1,"10":0,"12":[],"24":0},{"0":111,"1":1,"4":"上级","5":15,"6":8,"7":1,"10":0,"12":[],"24":1}]}],"11":[],"12":[]}`),
		TeamBattleOwnDeckSources:  map[int]int{111: 11},
		DisabledTeamBattleBossIDs: map[int]bool{111: true},
		TeamBattleRewards:         []gamestate.TeamBattleRewardProfile{{BossID: 11}, {BossID: 111}},
	}
	if err := ops.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{11, 111} {
		row := ops.content.bosses[id]
		row.Targets = []dropTarget{{Name: "本体"}}
		ops.content.bosses[id] = row
	}
	if ops.content.ruleBases[11].OwnDeck || !ops.content.configuration.State.DisabledTeamBattleBossIDs[111] {
		t.Fatal("optional mode opened without an operator action")
	}
	a := &API{operations: ops, catalogByKey: map[string]AdminCatalogEntry{"8:1000": {Name: "药水"}}}
	router := chi.NewRouter()
	router.Put("/rules", a.saveBossRules)
	router.Put("/drops", a.saveDropEditor)
	testfixture.CallContentAdmin(t, router, "PUT", "/rules", map[string]any{"expected_revision": 0, "rules": []map[string]any{{"boss_id": 11}}}, 400)
	rule := BossRules{BossID: 11, Standard: false, OwnDeck: true, Continue: false}
	testfixture.CallContentAdmin(t, router, "PUT", "/rules", map[string]any{"expected_revision": 0, "rules": []BossRules{rule}}, 200)
	testfixture.CallContentAdmin(t, router, "PUT", "/rules", map[string]any{"expected_revision": 0, "rules": []BossRules{rule}}, 409)
	bad := rule
	bad.BossID = 99
	testfixture.CallContentAdmin(t, router, "PUT", "/rules", map[string]any{"expected_revision": 1, "rules": []BossRules{{BossID: 11, Standard: true}, bad}}, 400)
	projected := ops.content.configuration.State
	past, err := projectBossRuleGroup(json.RawMessage(`{"0":1,"13":[{"0":11,"7":1},{"0":111,"7":1}]}`), "13", map[int]bool{11: false, 111: false})
	var pastGroup struct {
		Bosses []struct {
			Revive int `json:"7"`
		} `json:"13"`
	}
	if err != nil || json.Unmarshal(past, &pastGroup) != nil || len(pastGroup.Bosses) != 2 || pastGroup.Bosses[0].Revive != 0 || pastGroup.Bosses[1].Revive != 0 {
		t.Fatal("past revival projection failed", err)
	}
	if !projected.DisabledTeamBattleBossIDs[11] || projected.DisabledTeamBattleBossIDs[111] || ops.content.ruleRevision != 1 {
		t.Fatal("mode selection or atomic rejection failed")
	}
	var top map[string][]struct {
		Bosses []struct {
			ID     int `json:"0"`
			Revive int `json:"7"`
			Start  int `json:"24"`
		} `json:"10"`
	}
	if err := json.Unmarshal(projected.TeamBattleSolo, &top); err != nil {
		t.Fatal(err)
	}
	for _, b := range top["10"][0].Bosses {
		if b.Revive != 0 || b.ID == 11 && b.Start != 0 || b.ID == 111 && b.Start != 1 {
			t.Fatal("revival changed entry mode or failed to reach both entries")
		}
	}
	drop := BossDrops{BossID: 111, Drops: []gamestate.TeamBattleEnemyDrop{{Reward: gamestate.Reward{Type: 8, RewardTypeID: 1000, Num: 3, CardSkillLevels: []int16{}}}}}
	testfixture.CallContentAdmin(t, router, "PUT", "/drops", map[string]any{"expected_revision": 0, "configs": []BossDrops{drop}}, 200)
	for _, p := range ops.content.configuration.State.TeamBattleRewards {
		if len(p.EnemyDrops) != 1 || p.EnemyDrops[0].Reward.Num != 3 {
			t.Fatal("two modes do not share difficulty drops")
		}
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	if restarted.content.ruleRevision != 1 || !restarted.content.configuration.State.DisabledTeamBattleBossIDs[11] || len(restarted.content.drops) != 1 {
		t.Fatal("difficulty settings lost on restart")
	}
}

func TestBuiltinGachaPublicationAndSchedule(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	base := gamestate.GachaProfile{GachaID: 60200011, GroupID: 60200001, Name: "常驻", Price: 50}
	profiles := []gamestate.GachaProfile{base, {GachaID: gamestate.OnboardingMultiGachaID, GroupID: gamestate.OnboardingMultiGachaID}, {GachaID: gamestate.OnboardingGachaID, GroupID: gamestate.OnboardingGachaID}}
	o, err := NewOperations(accounts.Database(), profiles)
	if err != nil {
		t.Fatal(err)
	}
	active, err := o.GachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if !o.GachaIDPublished(base.GachaID, active) || len(o.gachaBases) != 1 || len(o.managedGachaGroups) != 1 {
		t.Fatal("built-in pools or onboarding boundary incorrect")
	}
	zero := 0
	if _, err := o.setGachaPublication(gachaPublication{ExpectedRevision: &zero, GroupIDs: []int{}}); err != nil {
		t.Fatal(err)
	}
	active, err = o.GachaPublication()
	if err != nil {
		t.Fatal(err)
	}
	if o.GachaIDPublished(base.GachaID, active) || !o.GachaIDPublished(gamestate.OnboardingMultiGachaID, active) || !o.GachaIDPublished(gamestate.OnboardingGachaID, active) {
		t.Fatal("group switch did not respect onboarding exemption")
	}
	c := AdminGachaConfigFromProfile(base)
	c.EndUnix = 1
	if _, err := o.writeDocument("gacha-live:60200011", 0, c); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOperations(accounts.Database(), profiles)
	if err != nil {
		t.Fatal(err)
	}
	if restarted.GachaIDPublished(base.GachaID, map[int]struct{}{base.GroupID: {}}) {
		t.Fatal("expired schedule lost after restart")
	}
}
