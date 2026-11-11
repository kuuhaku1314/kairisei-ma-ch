package admin

import (
	"encoding/json"
	"reflect"
	"strconv"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestGachaCopyPreservesFreeAndGiftsAsIndependentUnpublishedRules(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	source := gamestate.CloneGachas(catalog.Gachas[:1])[0]
	source.GachaID, source.GroupID, source.GachaType, source.PayType = 70001, 70001, 1, 2
	source.CardNum, source.CardNumMax, source.Price, source.PayTypeID = 1, 10, 200, 0
	source.DailyFirstFree, source.DailyFirstAvailable = true, true
	source.PlayCount, source.GroupPlayCount, source.DailyGroupPlays = 9, 9, 3
	source.Gifts = []gamestate.GachaGiftRule{{FromPlay: 1, ToPlay: 2, Rewards: []gamestate.Reward{{Type: 4, Num: 7, CardSkillLevels: []int16{}}}}}
	source.RandomGifts = []gamestate.GachaRandomGiftRule{{FromPlay: 1, EmptyWeight: 2, Pool: []gamestate.WeightedReward{{Weight: 3, Reward: gamestate.Reward{Type: 4, Num: 9, CardSkillLevels: []int16{}}}}}}
	o, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{source})
	if err != nil {
		t.Fatal(err)
	}
	liveConfig := AdminGachaConfigFromProfile(source)
	liveConfig.Price = 250
	loyaltyCard := source.CardIDs[0]
	liveConfig.CardLoves = map[int]int{loyaltyCard: 5000}
	liveConfig.RandomGifts = []gamestate.GachaRandomGiftRule{{FromPlay: 2, EmptyWeight: 1, Pool: []gamestate.WeightedReward{{Weight: 4, Reward: gamestate.Reward{Type: 4, Num: 19, CardSkillLevels: []int16{}}}}}}
	if _, err := o.storage.WriteDocument("gacha-live:"+strconv.Itoa(source.GachaID), 0, liveConfig, "gacha-live"); err != nil {
		t.Fatal(err)
	}
	entries := map[string]AdminCatalogEntry{}
	entries[adminCatalogKey(4, 0)] = AdminCatalogEntry{Kind: "currency", ResourceState: "ready"}
	for _, id := range source.CardIDs {
		entries[adminCatalogKey(6, id)] = AdminCatalogEntry{Rarity: 3, ResourceState: "ready", FameMax: 100, LoveMax: 10000}
	}
	a := &API{operations: o, catalogByKey: entries}
	router := chi.NewRouter()
	router.Post("/copy", a.createCustomGacha)
	var result struct {
		GroupID int   `json:"group_id"`
		IDs     []int `json:"gacha_ids"`
	}
	if err := json.Unmarshal(testfixture.CallContentAdmin(t, router, "POST", "/copy", map[string]any{"source_id": source.GachaID, "name": "copy", "expected_revision": 0}, 200), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.IDs) != 1 {
		t.Fatal(result)
	}
	id := result.IDs[0]
	check := func(ops *Operations) {
		t.Helper()
		p := ops.gachaBases[id]
		if p.Name != "copy" || p.GroupID != result.GroupID || !p.DailyFirstFree || p.CardNumMax != 10 || p.PayType != 2 || p.Price != 250 ||
			!reflect.DeepEqual(p.Gifts, source.Gifts) || !reflect.DeepEqual(p.RandomGifts, liveConfig.RandomGifts) || p.CardLoves[loyaltyCard] != 5000 {
			t.Fatalf("copy lost source rules: %+v", p)
		}
		if p.PlayCount != 0 || p.GroupPlayCount != 0 || p.DailyGroupPlays != 0 || p.DailyFirstAvailable {
			t.Fatal("copied player draw state", p)
		}
		live, err := ops.storage.ReadDocument("gacha-live:" + strconv.Itoa(id))
		if err != nil || live.Revision != 0 {
			t.Fatal("copy became published", err)
		}
		doc, err := ops.storage.ReadDocument("gacha-draft:" + strconv.Itoa(id))
		var draft AdminGachaConfig
		if err != nil || json.Unmarshal(doc.Payload, &draft) != nil || !reflect.DeepEqual(draft.RandomGifts, liveConfig.RandomGifts) || draft.CardLoves[loyaltyCard] != 5000 {
			t.Fatal("draft lost random gifts or loyalty", err)
		}
	}
	check(o)
	restarted, err := NewOperations(accounts.Database(), []gamestate.GachaProfile{source})
	if err != nil {
		t.Fatal(err)
	}
	check(restarted)
	cleared := AdminGachaConfigFromProfile(restarted.gachaBases[id])
	cleared.CardLoves = nil
	if _, err := o.storage.WriteDocument("gacha-live:"+strconv.Itoa(id), 0, cleared, "gacha-live"); err != nil {
		t.Fatal(err)
	}
	if err := restarted.reloadGachaConfigurations(); err != nil {
		t.Fatal(err)
	}
	config, err := restarted.effectiveGachaConfig(id)
	if err != nil || len(config.CardLoves) != 0 || len(restarted.gachaConfigurations) != 2 ||
		restarted.gachaConfigurations[1].Profile.GachaID != id || len(restarted.gachaConfigurations[1].Profile.CardLoves) != 0 {
		t.Fatal("cleared published loyalty inherited the copied base", config.CardLoves, err)
	}
	p := o.gachaBases[source.GachaID]
	p.Gifts[0].Rewards[0].Num = 100
	p.RandomGifts[0].Pool[0].Reward.Num = 200
	o.gachaBases[source.GachaID] = p
	if o.gachaBases[id].Gifts[0].Rewards[0].Num != 7 || o.gachaBases[id].RandomGifts[0].Pool[0].Reward.Num != 19 {
		t.Fatal("copy aliases source rewards")
	}
}
