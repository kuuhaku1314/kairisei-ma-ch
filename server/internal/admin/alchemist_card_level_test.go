package admin

import (
	"bytes"
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

func TestAlchemistSkillCatalogFixesAdminRewardLevel(t *testing.T) {
	entries := []AdminCatalogEntry{
		{Kind: "card", RewardType: 6, RewardTypeID: 101, LevelMax: 15, FameMax: 100},
		{Kind: "card", RewardType: 6, RewardTypeID: 102, LevelMax: 15, FameMax: 100},
		{Kind: "card", RewardType: 6, RewardTypeID: 103, LevelMax: 15, FameMax: 100},
	}
	templates := []gamestate.Card{{CardID: 101, RarityPlus: 4}, {CardID: 102, RarityPlus: 4}, {CardID: 103}}
	details := map[int]multiplayer.OperationsCardInfo{
		101: {}, 102: {SummonID: 17}, 103: {},
	}
	if err := attachAdminCardDetails(entries, templates, details); err != nil {
		t.Fatal(err)
	}
	if entries[0].FixedCardLevel != 15 || entries[1].FixedCardLevel != 0 || entries[2].FixedCardLevel != 0 {
		t.Fatalf("catalog acquisition levels: %+v", entries)
	}
	payload, err := json.Marshal(entries[0])
	if err != nil || !bytes.Contains(payload, []byte(`"fixed_card_level":15`)) || bytes.Contains(payload, []byte(`"summon_id"`)) {
		t.Fatalf("admin catalog wire contract = %s (%v)", payload, err)
	}
	request := AdminMailRequest{RewardType: 6, RewardTypeID: 101, Quantity: 1, CardFame: 1}
	reward, err := adminCatalogReward(request, entries[0])
	if err != nil || reward.CardLevel != 15 {
		t.Fatalf("default skill reward = %+v (%v)", reward, err)
	}
	request.CardLevel = 1
	if _, err := adminCatalogReward(request, entries[0]); err == nil {
		t.Fatal("skill reward must reject a misleading displayed level")
	}
	request.RewardTypeID, request.CardLevel = 102, 3
	reward, err = adminCatalogReward(request, entries[1])
	if err != nil || reward.CardLevel != 3 {
		t.Fatalf("explicit summon reward = %+v (%v)", reward, err)
	}
	request.RewardTypeID = 103
	reward, err = adminCatalogReward(request, entries[2])
	if err != nil || reward.CardLevel != 3 {
		t.Fatalf("ordinary reward = %+v (%v)", reward, err)
	}
	delete(details, 101)
	if err := attachAdminCardDetails(entries, templates, details); err == nil {
		t.Fatal("missing alchemist definition must be reported")
	}
}
