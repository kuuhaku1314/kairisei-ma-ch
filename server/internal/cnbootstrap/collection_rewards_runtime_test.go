package cnbootstrap

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"kairisei.local/server/internal/accountstore"
	adminapi "kairisei.local/server/internal/admin"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

func auditCompleteCollectionRewards(t *testing.T, h http.Handler, savePath, seedPath, stampPath, honorPath string, cards masterdata.CardRuntimeMaster) {
	t.Helper()
	storage, err := accountstore.OpenDatabase(savePath, seedPath, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := storage.Close(); err != nil {
			t.Error(err)
		}
	})
	testfixture.AttachProbeCardCatalog(t, storage, cards)
	accounts := testfixture.TestAccountRepository(t, storage)
	identity, err := accounts.ResolveInternalLogin("00000000-0000-4000-8491-000000000001")
	if err != nil {
		t.Fatal(err)
	}
	state, err := accounts.LoadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	state.InventorySequence = gamestate.InventorySequenceState{Card: 900001, Sphere: 900002, Buddy: 900003}
	stamps, err := masterdata.LoadStampRuntimeMaster(stampPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyStampRuntimeMaster(&state, stamps); err != nil {
		t.Fatal(err)
	}
	honors, err := masterdata.LoadHonorRuntimeMaster(honorPath)
	if err != nil {
		t.Fatal(err)
	}
	if err = masterdata.ApplyHonorRuntimeMaster(&state, honors); err != nil {
		t.Fatal(err)
	}
	admin := h.(interface{ AdminHandler() http.Handler }).AdminHandler()
	ownership := map[string][]int{"costume": {1}, "stamp": state.Stamps.StampIDs, "honor": state.Honors.HonorIDs}
	want := map[string]int{}
	before := map[string]int{}
	state.Engagement.Presents = nil
	for _, kind := range []string{"costume", "stamp", "honor", "sphere", "buddy", "card", "item", "material"} {
		var catalog struct {
			Entries []adminapi.AdminCatalogEntry `json:"entries"`
		}
		if err := json.Unmarshal(testfixture.CallContentAdmin(t, admin, "GET", "/api/catalog?kind="+kind+"&limit=200", nil, 200), &catalog); err != nil {
			t.Fatal(err)
		}
		found := false
		for _, entry := range catalog.Entries {
			if slices.Contains(ownership[kind], entry.RewardTypeID) || entry.ResourceState == "unavailable" {
				continue
			}
			reward := gamestate.Reward{Type: entry.RewardType, RewardTypeID: entry.RewardTypeID, Num: 1, CardSkillLevels: []int16{}}
			if kind == "sphere" {
				reward.Num = 50
			}
			if kind == "card" {
				reward.CardLevel, reward.CardFame, reward.CardSkillLevels = 1, 1, []int16{1}
			}
			state.Engagement.Presents = append(state.Engagement.Presents, gamestate.Present{PresentID: int64(491000 + len(state.Engagement.Presents)), Title: "运营奖励", Reward: reward})
			want[kind] = entry.RewardTypeID
			before[kind] = inventoryRewardCount(t, state, reward)
			if !gamestate.IsCollectionReward(reward.Type) {
				reward.Num = 2
			}
			state.Engagement.Presents = append(state.Engagement.Presents, gamestate.Present{PresentID: int64(491000 + len(state.Engagement.Presents)), Title: "同类再次赠送", Reward: reward})
			if kind == "sphere" {
				for _, other := range catalog.Entries {
					if other.RewardTypeID != reward.RewardTypeID && other.ResourceState != "unavailable" {
						reward.RewardTypeID = other.RewardTypeID
						state.Engagement.Presents = append(state.Engagement.Presents, gamestate.Present{Title: "另一种秘石", Reward: reward})
						break
					}
				}
			}
			found = true
			break
		}
		if !found {
			t.Fatalf("no unowned %s sample", kind)
		}
	}
	presents := state.Engagement.Presents
	state.Engagement.Presents = nil
	if err = accounts.PersistState(identity.UserID, state); err != nil {
		t.Fatal(err)
	}
	call := func(route string, payload any) map[string]json.RawMessage {
		t.Helper()
		data, _ := json.Marshal(payload)
		if payload == nil {
			data = nil
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, httptest.NewRequest("POST", route, strings.NewReader(identity.SessionKey+string(data))))
		lines := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		var result map[string]json.RawMessage
		if w.Code != 200 || len(lines) != 3 || json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(lines[1]), &result) != nil {
			t.Fatalf("%s: %d %s", route, w.Code, w.Body.String())
		}
		return result
	}
	stampReceived := false
	sphereReceived := make(map[int]int)
	for index, present := range presents {
		// Deliver through Admin so every subsequent gift rebuilds the cached
		// handler, as it does for a player receiving separate operator grants.
		batch := adminapi.AdminMailBatch{
			ID: fmt.Sprintf("collection-reload-%d", index), Title: present.Title, Message: "重复领取与重载检查",
			UserIDs: []int{identity.UserID},
			Rewards: []adminapi.AdminMailRequest{{RewardType: present.Reward.Type, RewardTypeID: present.Reward.RewardTypeID, Quantity: present.Reward.Num}},
		}
		testfixture.CallContentAdmin(t, admin, "POST", "/api/mail-batches", batch, 200)
		var delivery struct {
			Results []struct {
				OK     bool `json:"ok"`
				Result struct {
					PresentIDs []int64 `json:"present_ids"`
				} `json:"result"`
			} `json:"results"`
		}
		body := testfixture.CallContentAdmin(t, admin, "POST", "/api/mail-batches/"+batch.ID+"/run", map[string]any{"user_ids": batch.UserIDs}, 200)
		if json.Unmarshal(body, &delivery) != nil || len(delivery.Results) != 1 || !delivery.Results[0].OK || len(delivery.Results[0].Result.PresentIDs) != 1 {
			t.Fatalf("Admin gift delivery failed: %s", body)
		}
		present.PresentID = delivery.Results[0].Result.PresentIDs[0]
		result := call("/PresentBoxRecv", map[string]int64{"presentid": present.PresentID})
		if present.Reward.Type == 16 {
			var ids []int
			if json.Unmarshal(result["new_stampids"], &ids) != nil || (!stampReceived && !slices.Contains(ids, want["stamp"])) || (stampReceived && len(ids) != 0) {
				t.Fatalf("native stamp %d ownership delta absent: %s (prior: %v)", want["stamp"], result["new_stampids"], state.Stamps.StampIDs)
			}
			stampReceived = true
		}
		call("/PresentBoxRecv", map[string]int64{"presentid": present.PresentID})
		if present.Reward.Type == 15 {
			id := present.Reward.RewardTypeID
			if _, tracked := sphereReceived[id]; !tracked {
				sphereReceived[id] = inventoryRewardCount(t, state, present.Reward)
			}
			sphereReceived[id] += present.Reward.Num
		}
	}
	var costumes []int
	if json.Unmarshal(call("/CostumeShow", nil)["costumeids"], &costumes) != nil || !slices.Contains(costumes, want["costume"]) {
		t.Fatal("costume window did not refresh ownership")
	}
	reloaded, err := accounts.LoadPersistentState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	var costume struct {
		IDs []int `json:"costumeids"`
	}
	_ = json.Unmarshal(reloaded.Costume, &costume)
	if !slices.Contains(costume.IDs, want["costume"]) || !slices.Contains(reloaded.Stamps.StampIDs, want["stamp"]) || !slices.Contains(reloaded.Honors.HonorIDs, want["honor"]) {
		t.Fatal("unlock lost after SQL reload")
	}
	if len(sphereReceived) != 2 {
		t.Fatal("sphere regression did not cover a different reward ID")
	}
	for id, expected := range sphereReceived {
		if got := inventoryRewardCount(t, reloaded, gamestate.Reward{Type: 15, RewardTypeID: id}); got != expected {
			t.Fatalf("sphere %d lost after other Admin gifts: got %d, want %d", id, got, expected)
		}
	}
	for kind, rewardType := range map[string]int{"card": 6, "item": 8, "material": 13, "buddy": 19} {
		expected := before[kind] + 3
		if got := inventoryRewardCount(t, reloaded, gamestate.Reward{Type: rewardType, RewardTypeID: want[kind]}); got != expected {
			t.Fatalf("%s repeat Admin gift/retry: got %d, expected %d", kind, got, expected)
		}
	}
	for _, card := range reloaded.Cards {
		if card.CardID == want["card"] && card.UniqueID >= 900001 {
			want["card"] = 0
			break
		}
	}
	for _, sphere := range reloaded.Spheres {
		if sphere.SphereID == want["sphere"] && sphere.UniqueID >= 900002 {
			want["sphere"] = 0
			break
		}
	}
	for _, buddy := range reloaded.Buddies {
		if buddy.BuddyID == want["buddy"] && buddy.UniqueID >= 900003 {
			want["buddy"] = 0
			break
		}
	}
	if want["card"] != 0 || want["sphere"] != 0 || want["buddy"] != 0 {
		t.Fatal("instance IDs regressed after account reload")
	}
	t.Log("Admin gift/cache reload/retry: sphere 50+2=52 plus 2 of another kind; card/buddy instances, additive item/material stacks, unique unlocks, SQLite and monotonic IDs passed")
}

func inventoryRewardCount(t *testing.T, state gamestate.State, reward gamestate.Reward) int {
	t.Helper()
	total := 0
	seen := make(map[int64]bool)
	addInstance := func(uniqueID int64) {
		if seen[uniqueID] {
			t.Fatalf("reward type %d repeats instance %d", reward.Type, uniqueID)
		}
		seen[uniqueID] = true
		total++
	}
	switch reward.Type {
	case 6:
		for _, card := range state.Cards {
			if card.CardID == reward.RewardTypeID {
				addInstance(card.UniqueID)
			}
		}
	case 15:
		for _, sphere := range state.Spheres {
			if sphere.SphereID == reward.RewardTypeID {
				addInstance(sphere.UniqueID)
			}
		}
	case 19:
		for _, buddy := range state.Buddies {
			if buddy.BuddyID == reward.RewardTypeID {
				addInstance(buddy.UniqueID)
			}
		}
	case 8:
		for _, item := range state.Items {
			if item.ItemID == reward.RewardTypeID {
				total += item.Num
			}
		}
	case 13:
		for _, stack := range state.StackCards {
			if stack.CardID == reward.RewardTypeID {
				total += stack.Num
			}
		}
	}
	return total
}
