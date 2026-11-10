package admin

import (
	"reflect"
	"testing"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestCustomGachaOwnedMigrationPreservesProfilesAndClaims(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	storage := accounts.Database()
	catalog, err := storage.CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	state := testfixture.RuntimeState(t)
	state.User.FriendPoint = 0
	gold := gamestate.Reward{Type: 4, Num: 7, CardSkillLevels: []int16{}}
	randomGold := gold
	randomGold.Num = 11
	friend := findProfile(catalog.Gachas, 60200101)
	friend.CardIDs, friend.CardWeights, friend.CardFames = []int{state.Cards[0].CardID}, []int{1}, nil
	friend.Gifts = []gamestate.GachaGiftRule{{FromPlay: 1, Rewards: []gamestate.Reward{gold}}}
	friend.RandomGifts = []gamestate.GachaRandomGiftRule{{FromPlay: 1, Pool: []gamestate.WeightedReward{{Reward: randomGold, Weight: 1}}}}
	profession := gamestate.CloneGachas([]gamestate.GachaProfile{*findProfile(catalog.Gachas, 60200201)})[0]
	profession.GachaID, profession.GroupID = 60209991, 60209991
	profession.CardIDs = append([]int(nil), profession.CardIDs[:3]...)
	profession.CardWeights = []int{2, 3, 5}
	profession.CardFames = map[int]int{profession.CardIDs[0]: 2, profession.CardIDs[1]: 3, profession.CardIDs[2]: 4}
	catalog.DeckRankPolicy.Cards = map[int]gamestate.CardRankRule{}
	for i, job := range []int8{0, 2, 1} {
		rule := catalog.DeckRankPolicy.Cards[profession.CardIDs[i]]
		rule.ArthurType = job
		catalog.DeckRankPolicy.Cards[profession.CardIDs[i]] = rule
	}
	catalog.Gachas = append(catalog.Gachas, profession)
	storage.SetCatalog(catalog)

	sources := []storedCustomGachaSource{
		{GachaID: 60300006, GroupID: 60300006, TemplateID: 60200001, RuleVersion: 1, Name: "stored group"},
		{GachaID: 60300007, GroupID: 60300006, TemplateID: 60200011, RuleVersion: 1, Name: "stored group"},
		{GachaID: 60300008, GroupID: 60300006, TemplateID: 60200012, RuleVersion: 1, Name: "stored group"},
		{GachaID: 60300009, GroupID: 60300009, Name: "current select", Rule: &gachaPoolRule{Type: "select", DrawCount: 1, UserSelectMax: 5}},
		{GachaID: 60300010, GroupID: 60300010, TemplateID: 60200101, Name: "old paid friend"},
		{GachaID: 60300011, GroupID: 60300011, TemplateID: 60200101, RuleVersion: 1, Name: "free friend"},
		{GachaID: 60300012, GroupID: 60300012, TemplateID: 60201302, RuleVersion: 1, Name: "step with gifts"},
		{GachaID: 60300013, GroupID: 60300013, TemplateID: 60201402, RuleVersion: 1, Name: "mixed with gifts"},
		{GachaID: 60300014, GroupID: 60300014, TemplateID: 60201102, RuleVersion: 1, Name: "unowned"},
		{GachaID: 60300015, GroupID: 60300015, TemplateID: 60209991, ArthurType: 2, Name: "profession", Deleted: true},
		{GachaID: 60300016, GroupID: 60300016, TemplateID: 60300006},
	}
	want := map[int]gamestate.GachaProfile{}
	rows := make([]any, 0, len(sources))
	for _, source := range sources {
		var profile gamestate.GachaProfile
		if source.Rule != nil {
			profile = source.Rule.profile()
		} else if source.TemplateID >= customGachaFirstID {
			profile = gamestate.CloneGachas([]gamestate.GachaProfile{want[source.TemplateID]})[0]
		} else {
			profile = gamestate.CloneGachas([]gamestate.GachaProfile{*findProfile(catalog.Gachas, source.TemplateID)})[0]
		}
		profile.GachaID, profile.GroupID, profile.PlayCount, profile.PublicationKey = source.GachaID, source.GroupID, 0, "operator"
		if source.Name != "" {
			profile.Name = source.Name
		}
		if source.ArthurType != 0 {
			profile.ArthurType = source.ArthurType
		}
		if source.Rule == nil && source.RuleVersion == 0 {
			profile.DailyFirstFree, profile.Gifts = false, nil
		}
		if source.GachaID == 60300015 {
			profile.CardIDs, profile.CardWeights = profile.CardIDs[:2], profile.CardWeights[:2]
			delete(profile.CardFames, profession.CardIDs[2])
		}
		want[source.GachaID] = profile
		rows = append(rows, source)
	}
	const nextID = 60300020
	registry, err := storage.WriteDocument(customGachaKey, 0, map[string]any{"pools": rows, "next_id": nextID}, "test")
	if err != nil {
		t.Fatal(err)
	}
	live := AdminGachaConfigFromProfile(want[60300012])
	live.Price, live.Steps[0].Price = 321, 9
	live.RandomGifts = friend.RandomGifts
	untouched := map[string]accountstore.Document{}
	for key, value := range map[string]any{
		"gacha-live:60300012":  live,
		"gacha-draft:60300012": map[string]any{"name": "unsaved content", "price": 456, "card_ids": []int{}},
		gachaPublicationKey:    map[string]any{"group_ids": []int{60300012}},
	} {
		untouched[key], err = storage.WriteDocument(key, 0, value, "test")
		if err != nil {
			t.Fatal(err)
		}
	}
	db, err := storage.Open()
	if err != nil {
		t.Fatal(err)
	}
	var snapshotBefore, snapshotAfter []byte
	if err := db.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&snapshotBefore); err != nil {
		t.Fatal(err)
	}
	o, err := NewOperations(storage, catalog.Gachas)
	if err != nil {
		t.Fatal(err)
	}
	for id, profile := range want {
		if !reflect.DeepEqual(o.gachaBases[id], profile) {
			t.Fatalf("migration changed full base profile %d", id)
		}
	}
	if !o.customGachas[60300015].Deleted || o.nextCustomGachaID() != nextID {
		t.Fatal("migration changed deletion state or allocation cursor")
	}
	configured := adminConfiguredGacha(o.gachaBases[60300012], live).Profile
	expected := adminConfiguredGacha(want[60300012], live).Profile
	for _, plays := range []int{0, 1, 2, 8} {
		configured.PlayCount, expected.PlayCount = plays, plays
		if !reflect.DeepEqual(configured.CurrentStep(), expected.CurrentStep()) || !reflect.DeepEqual(configured.CurrentGifts(), expected.CurrentGifts()) {
			t.Fatal("migration changed stage payment/reward or gift intervals", plays)
		}
	}
	for key, before := range untouched {
		after, err := storage.ReadDocument(key)
		if err != nil || !reflect.DeepEqual(after, before) {
			t.Fatal("migration rewrote another configuration", key, err)
		}
	}
	if err := db.QueryRow(`SELECT payload_json FROM cn_save_snapshot WHERE singleton=1`).Scan(&snapshotAfter); err != nil || !reflect.DeepEqual(snapshotAfter, snapshotBefore) {
		t.Fatal("migration changed player snapshot", err)
	}
	upgraded, err := storage.ReadDocument(customGachaKey)
	if err != nil || upgraded.Revision != registry.Revision+1 {
		t.Fatal("migration did not atomically advance the registry revision", err)
	}
	restarted, err := NewOperations(storage, catalog.Gachas)
	if err != nil || !reflect.DeepEqual(restarted.customGachas, o.customGachas) {
		t.Fatal("owned profiles changed after restart", err)
	}
	again, err := storage.ReadDocument(customGachaKey)
	var audits int
	if err != nil || !reflect.DeepEqual(again, upgraded) {
		t.Fatal("restart rewrote the migrated registry", err)
	}
	if err := db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE operation='gacha-custom-owned-migration'`).Scan(&audits); err != nil || audits != 1 {
		t.Fatal("migration audit was not committed once", audits, err)
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	account.ApplyGachaConfiguration(1, []game.GachaConfiguration{
		{Operator: true, Profile: o.gachaBases[60300010]},
		{Operator: true, Profile: o.gachaBases[60300011]},
	})
	if _, err := account.PlayGacha(60300010, 2, nil); err == nil {
		t.Fatal("version-0 paid pool became free")
	}
	draw, err := account.PlayGacha(60300011, 2, nil)
	if err != nil || len(draw.PresentGifts) != 2 || draw.PresentGifts[0].Num != 7 || draw.PresentGifts[1].Num != 11 {
		t.Fatalf("migrated free draw lost fixed/random gifts: %+v, %v", draw.PresentGifts, err)
	}
	if _, err := account.PlayGacha(60300011, 2, nil); err == nil {
		t.Fatal("migrated free draw was granted twice")
	}
}

func TestCustomGachaOwnedMigrationRollsBackWholeRegistry(t *testing.T) {
	for _, failure := range []string{"conversion", "audit"} {
		t.Run(failure, func(t *testing.T) {
			accounts := testfixture.NewFriendCapacityTestAccounts(t)
			storage := accounts.Database()
			catalog, err := storage.CatalogState()
			if err != nil {
				t.Fatal(err)
			}
			rows := []storedCustomGachaSource{{GachaID: 60300006, GroupID: 60300006, TemplateID: 60200001, Name: "valid", RuleVersion: 1}}
			if failure == "conversion" {
				rows = append(rows, storedCustomGachaSource{GachaID: 60300007, GroupID: 60300007, TemplateID: 999, Name: "unresolved", RuleVersion: 1})
			}
			before, err := storage.WriteDocument(customGachaKey, 0, map[string]any{"pools": rows, "next_id": 60300008}, "test")
			if err != nil {
				t.Fatal(err)
			}
			db, err := storage.Open()
			if err != nil {
				t.Fatal(err)
			}
			if failure == "audit" {
				if _, err := db.Exec(`CREATE TRIGGER fail_gacha_migration BEFORE INSERT ON cn_admin_audit WHEN NEW.operation='gacha-custom-owned-migration' BEGIN SELECT RAISE(ABORT,'migration audit failure'); END`); err != nil {
					t.Fatal(err)
				}
			}
			if _, err := NewOperations(storage, catalog.Gachas); err == nil {
				t.Fatal("failed migration started operations")
			}
			after, err := storage.ReadDocument(customGachaKey)
			if err != nil || !reflect.DeepEqual(after, before) {
				t.Fatal("failed migration rewrote the registry", err)
			}
			var audits int
			if err := db.QueryRow(`SELECT COUNT(*) FROM cn_admin_audit WHERE operation='gacha-custom-owned-migration'`).Scan(&audits); err != nil || audits != 0 {
				t.Fatal("failed migration left an audit", audits, err)
			}
		})
	}
}
