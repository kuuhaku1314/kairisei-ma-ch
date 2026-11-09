package cnbootstrap

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"

	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

// Full-resource HTTP contracts for editors; all requests use an isolated
// database and recorder. No listener, actual player or external network.
func auditCompleteAdminContent(t *testing.T, h http.Handler) {
	t.Helper()
	admin := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	var economy struct {
		Revision int `json:"revision"`
		Rules    []struct {
			Difficulty string `json:"difficulty"`
		} `json:"rules"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-economy", nil, 200), &economy); err != nil || len(economy.Rules) != 22 {
		t.Fatalf("activity/past economy scope: %d difficulties, error %v", len(economy.Rules), err)
	}
	// Normal quests also have zero StageQuestAreaID in the real catalog. The
	// production master directory, not that reward field, must exclude them.
	testfixture.CallContentAdmin(t, admin, "PUT", "/api/boss-economy", map[string]any{"expected_revision": economy.Revision, "rules": []any{map[string]any{"difficulty": "空想神殿 / 一握之星", "bp_use": 1, "experience": 1}}}, 400)
	t.Logf("boss economy: %d activity/past difficulties; ordinary main quest rejected", len(economy.Rules))
	var activity struct {
		Revision int `json:"revision"`
		Config   struct {
			Cups    map[int]*gamestate.TeamBattleScorePolicy `json:"cups"`
			Explore []json.RawMessage                        `json:"explore"`
		} `json:"config"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/activity-rewards", nil, 200), &activity); err != nil || len(activity.Config.Explore) == 0 {
		t.Fatal("missing exploration reward configuration", err)
	}
	testfixture.CallContentAdmin(t, admin, "PUT", "/api/activity-rewards", map[string]any{"expected_revision": activity.Revision, "config": activity.Config}, 200)
	t.Logf("activity rewards: %d cup policies / %d native exploration slots", len(activity.Config.Cups), len(activity.Config.Explore))
	var rules struct {
		Revision int                  `json:"revision"`
		Rules    []adminapi.BossRules `json:"rules"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-rules", nil, 200), &rules); err != nil || len(rules.Rules) == 0 {
		t.Fatal("missing difficulty rules", err)
	}
	for i := 0; i < len(rules.Rules); i += 100 {
		testfixture.CallContentAdmin(t, admin, "PUT", "/api/boss-rules", map[string]any{"expected_revision": rules.Revision, "rules": rules.Rules[i:min(i+100, len(rules.Rules))]}, 200)
		rules.Revision++
	}
	var cards struct {
		Entries []adminapi.AdminCatalogEntry `json:"entries"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/catalog?kind=card&attribute=FIRE&cost=2&limit=20", nil, 200), &cards); err != nil || len(cards.Entries) == 0 {
		t.Fatal("card detail filters empty", err)
	}
	for _, card := range cards.Entries {
		if card.Combat == nil || card.Combat.Cost != 2 || card.Parameters == nil {
			t.Fatal("card combat detail not attached")
		}
	}
	for _, kind := range []string{"costume", "stamp", "honor"} {
		var catalog struct {
			Total   int                          `json:"total"`
			Entries []adminapi.AdminCatalogEntry `json:"entries"`
		}
		if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/catalog?kind="+kind+"&limit=200", nil, 200), &catalog); err != nil {
			t.Fatal(err)
		}
		if catalog.Total == 0 {
			t.Fatalf("missing %s catalog", kind)
		}
		requests := make([]adminapi.AdminMailRequest, len(catalog.Entries))
		for i, entry := range catalog.Entries {
			requests[i] = adminapi.AdminMailRequest{RewardType: entry.RewardType, RewardTypeID: entry.RewardTypeID, Quantity: 1}
		}
		for i := 0; i < len(requests); i += 100 {
			testfixture.CallContentAdmin(t, admin, "POST", "/api/catalog/resolve", map[string]any{"rewards": requests[i:min(i+100, len(requests))]}, 200)
		}
		t.Logf("%s catalog: %d entries", kind, catalog.Total)
	}
	var list struct {
		Bosses []adminapi.DropBoss `json:"bosses"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-drops", nil, 200), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Bosses) < 100 {
		t.Fatal("missing boss editor catalog")
	}
	var detail struct {
		Config adminapi.BossDrops `json:"config"`
		Boss   adminapi.DropBoss  `json:"boss"`
	}
	// Verify every default can actually be edited; this catches reward/slot
	// schema mismatches that a page-only test would miss.
	for _, boss := range list.Bosses {
		if len(boss.Targets) == 0 {
			t.Fatalf("boss %d has no real targets", boss.BossID)
		}
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/boss-drops?boss_id=30100102", nil, 200), &detail); err != nil {
		t.Fatal(err)
	}
	if len(detail.Config.Drops) == 0 {
		t.Fatal("missing drop config")
	}
	for i := range detail.Config.Drops {
		if detail.Config.Drops[i].Reward.Type == 13 {
			detail.Config.Drops[i].Reward.Num++
			break
		}
	}
	testfixture.CallContentAdmin(t, admin, "PUT", "/api/boss-drops", map[string]any{"expected_revision": 0, "configs": []adminapi.BossDrops{detail.Config}}, 200)
	var shops struct {
		Shops      []gamestate.TradeShopProfile            `json:"shops"`
		NextShop   int                                     `json:"next_shop_id"`
		NextLineup int                                     `json:"next_lineup_id"`
		Templates  map[string][]adminapi.AdminCatalogEntry `json:"templates"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/exchanges", nil, 200), &shops); err != nil {
		t.Fatal(err)
	}
	if len(shops.Templates["evolution"]) < 20 || len(shops.Templates["fusion"]) < 2 {
		t.Fatal("incomplete material shortcuts")
	}
	shop := shops.Shops[0]
	shop.TradeShopID = shops.NextShop
	shop.Name = "测试复制兑换所"
	for i := range shop.Lineups {
		shop.Lineups[i].LineupID = shops.NextLineup + i
	}
	testfixture.CallContentAdmin(t, admin, "PUT", "/api/exchanges/"+strconv.Itoa(shop.TradeShopID), map[string]any{"expected_revision": 0, "shop": shop}, 200)
	var presets struct {
		Presets []adminapi.AdminGachaPreset `json:"presets"`
	}
	_ = json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/gacha-presets", nil, 200), &presets)
	baseFound := map[int]bool{}
	categories := map[int]string{60200001: "限定扭蛋", 60200101: "友情点扭蛋", 60200201: "超级扭蛋"}
	for _, p := range presets.Presets {
		baseFound[p.GroupID] = true
		if want, ok := categories[p.GroupID]; ok && p.Category != want {
			t.Fatalf("pool %d category = %d %q, want %q", p.GroupID, p.CategoryNum, p.Category, want)
		}
		for _, id := range p.GachaIDs {
			if id == 90000100 || id == 90000200 {
				t.Fatal("tutorial exposed")
			}
		}
	}
	for _, id := range []int{60200001, 60200101, 60200201} {
		if !baseFound[id] {
			t.Fatalf("base pool %d missing", id)
		}
	}
	var pools struct {
		Pools []struct {
			Config adminapi.AdminGachaConfig `json:"config"`
		} `json:"pools"`
	}
	_ = json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/gacha-editor", nil, 200), &pools)
	for _, pool := range pools.Pools {
		testfixture.CallContentAdmin(t, admin, "POST", "/api/gacha-editor/preview", map[string]any{"config": pool.Config}, 200)
	}
	t.Logf("operations: %d boss difficulties, %d exchange shops; %d fusion/%d evolution material options; %d gacha groups include permanent pools", len(list.Bosses), len(shops.Shops), len(shops.Templates["fusion"]), len(shops.Templates["evolution"]), len(presets.Presets))
}
