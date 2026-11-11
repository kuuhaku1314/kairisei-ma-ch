package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
	"kairisei.local/server/internal/testfixture"
)

// Called by the existing full-resource gate: one production handler, a
// disposable SQLite database, real session routing and no listening sockets.
func auditAlchemistSoloHTTP(t *testing.T, handler *cnDeploymentHandler, masterPath string) {
	master, err := masterdata.LoadBattleRuntimeMaster(masterPath)
	if err != nil || len(master.TowerProfiles) != 9 {
		t.Fatalf("complete tower input: %v", err)
	}
	identity, err := handler.accounts.ResolveInternalLogin("f1000000-0000-4000-8000-000000000005")
	if err != nil {
		t.Fatal(err)
	}
	initial, err := handler.accounts.LoadState(identity.UserID)
	if err != nil {
		t.Fatal(err)
	}
	initial.User.Name = "炼金单人HTTP验证"
	initial.User.BP = 100000
	initial.Onboarding = gamestate.OnboardingState{Step: 9}
	initial.TowerQuestProgress = nil
	for _, tower := range master.TowerProfiles {
		initial.TowerQuestProgress = append(initial.TowerQuestProgress, gamestate.TowerQuestProgress{
			TowerID: tower.TowerID, Floor: 2, ClearedFloors: []int{1},
		})
	}
	initial.Items = append(initial.Items, gamestate.Item{ItemID: 3022, Num: 1000})
	if err := handler.accounts.PersistState(identity.UserID, initial); err != nil {
		t.Fatal(err)
	}
	call := func(path string, payload any, success bool) map[string]json.RawMessage {
		t.Helper()
		raw, err := json.Marshal(payload)
		if err != nil {
			t.Fatal(err)
		}
		if path == "/HomeShow" {
			raw = nil // Session-only CN route, with no JSON business body.
		}
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, httptest.NewRequest("POST", path, strings.NewReader(identity.SessionKey+string(raw))))
		parts := strings.Split(strings.TrimSpace(w.Body.String()), "\n")
		var common struct {
			Code int `json:"res_code"`
		}
		ok := w.Code == 200 && len(parts) == 3 && json.Unmarshal([]byte(parts[0]), &common) == nil && common.Code == 0
		if ok != success {
			t.Fatalf("%s expected success=%v, HTTP %d: %s", path, success, w.Code, w.Body.String())
		}
		var body map[string]json.RawMessage
		if ok && json.Unmarshal([]byte(parts[1]), &body) != nil {
			t.Fatal("invalid business body")
		}
		return body
	}
	state := func() gamestate.State {
		t.Helper()
		value, err := handler.accounts.LoadState(identity.UserID)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	item := func(s gamestate.State, id int) int {
		for _, row := range s.Items {
			if row.ItemID == id {
				return row.Num
			}
		}
		return 0
	}
	call("/HomeShow", map[string]any{}, true)
	if !slices.Contains(state().User.UnlockedFeatureIDs, uint(37)) {
		t.Fatal("alchemy feature must be unlocked for the loadout check")
	}
	var supportSlots []struct {
		Arthur   int `json:"0"`
		Unlocked int `json:"1"`
	}
	show := call("/CardShow2", map[string]any{"0": 1}, true)
	if json.Unmarshal(show["5"], &supportSlots) != nil || len(supportSlots) != 5 || supportSlots[4].Arthur != 5 {
		t.Fatal("CardShow lacks alchemist support slot projection")
	}
	const ordinaryBoss = 20000001 // R755's ordinary Cherry material quest.
	call("/TeamBattleSoloStart", map[string]any{"bossid": ordinaryBoss, "deck_arthur_type": 5, "deck_arthur_type_idx": 0}, false)
	for arthur := 1; arthur <= 4; arthur++ {
		partners := []any{}
		for other := 1; other <= 4; other++ {
			if other != arthur {
				partners = append(partners, map[string]any{"userid": identity.UserID, "arthur_type": other, "deck_idx": 0})
			}
		}
		call("/TeamBattleSoloStart", map[string]any{"bossid": ordinaryBoss, "deck_arthur_type": arthur, "deck_arthur_type_idx": 0, "partner_deck_selects": partners}, true)
		call("/HomeShow", map[string]any{}, true)
	}
	foundActual := false
	for _, tower := range master.TowerProfiles {
		floor := tower.Floors[1]
		var boss struct {
			ID int `json:"0"`
		}
		if err := json.Unmarshal(floor.Boss, &boss); err != nil {
			t.Fatal(err)
		}
		if tower.TowerID == 71002 {
			if boss.ID != 710020002 {
				t.Fatal("actual second-floor identity changed")
			}
			foundActual = true
		}
		request := map[string]any{"bossid": boss.ID, "deck_arthur_type": 5, "deck_arthur_type_idx": 0}
		before := state()
		for _, bad := range []map[string]any{
			{"bossid": boss.ID, "deck_arthur_type": 1, "deck_arthur_type_idx": 0},
			{"bossid": boss.ID, "deck_arthur_type": 5, "deck_arthur_type_idx": 127},
			{"bossid": boss.ID, "deck_arthur_type": 5, "deck_arthur_type_idx": 0,
				"partner_deck_selects": []any{map[string]any{"userid": identity.UserID, "arthur_type": 1, "deck_idx": 0}}},
		} {
			call("/TeamBattleSoloStart", bad, false)
			if rejected := state(); rejected.User.BP != before.User.BP || item(rejected, floor.ItemID) != item(before, floor.ItemID) || rejected.ActiveTeamBattle != nil {
				t.Fatal("rejected entry charged or created a run")
			}
		}
		start := call("/TeamBattleSoloStart", request, true)
		var endTurn int
		if json.Unmarshal(start["end_turn"], &endTurn) != nil || endTurn != 20 {
			t.Fatalf("tower %d start.end_turn = %s, want 20", tower.TowerID, start["end_turn"])
		}
		var avatar struct {
			Costume int             `json:"costumeid"`
			Parts   json.RawMessage `json:"avatar_partsids"`
		}
		if json.Unmarshal(start["avatar"], &avatar) != nil || avatar.Costume != 34 || string(avatar.Parts) != "null" {
			t.Fatal("alchemist start uses the wrong fixed costume/parts contract")
		}
		accepted := state()
		active := accepted.ActiveTeamBattle
		if active == nil || active.TowerID != tower.TowerID || active.TowerFloor != 2 || len(active.SelectedPartners) != 0 ||
			accepted.User.BP != before.User.BP-floor.BPUse || item(accepted, floor.ItemID) != item(before, floor.ItemID)-floor.ItemUse ||
			len(active.ClearDecks) != 1 || active.ClearDecks[0].ArthurType != 5 {
			t.Fatalf("tower %d accepted context/debit differs", tower.TowerID)
		}
		type alchemistDeck struct {
			Arthur   int               `json:"arthur_type"`
			Job      int               `json:"job_type"`
			Burst    *int              `json:"is_burst"`
			HP       int               `json:"hp"`
			Support  []json.RawMessage `json:"support_deck"`
			Unlocked int               `json:"support_card_unlock_slot_num"`
		}
		var deck alchemistDeck
		if json.Unmarshal(active.ClearDecks[0].Deck, &deck) != nil || deck.Arthur != 5 || deck.Job != 5 || deck.HP <= 0 ||
			deck.Unlocked != len(deck.Support) || deck.Unlocked != supportSlots[4].Unlocked || deck.Burst == nil || *deck.Burst != 0 {
			t.Fatal("alchemy loadout/history parameters unavailable")
		}
		retry := call("/TeamBattleSoloStart", request, true)
		if again := state(); again.User.BP != accepted.User.BP || item(again, floor.ItemID) != item(accepted, floor.ItemID) ||
			!bytes.Equal(start["seed"], retry["seed"]) || !bytes.Equal(start["start_time"], retry["start_time"]) {
			t.Fatal("start retry charged or replaced the run")
		}
		// Exercise the publication contract without touching any live policy.
		if tower.TowerID == 71002 {
			testfixture.CallContentAdmin(t, handler.admin, "PUT", "/api/boss-policy?catalog=tower",
				map[string]any{"mode": "allowlist", "group_ids": []int{}, "expected_revision": 0}, 200)
			call("/TeamBattleSoloStart", request, false)
		}
		var battles []json.RawMessage
		if json.Unmarshal(start["battles"], &battles) != nil || len(battles) == 0 {
			t.Fatal("missing battle segments")
		}
		commands, dead := make([]string, len(battles)), make([]int, len(battles))
		for i := range commands {
			commands[i] = "0,22" + strings.Repeat(",0", 20)
			dead[i] = 15
		}
		end := map[string]any{"bossid": boss.ID, "progress": len(battles), "is_clear": 1, "input_cmd": commands, "enemy_dead_bit": dead}
		result := call("/TeamBattleSoloEnd", end, true)
		settled := state()
		var transitions []map[string]json.RawMessage
		if json.Unmarshal(result["scene_transition"], &transitions) != nil || len(transitions) != 1 || len(transitions[0]) != 8 {
			t.Fatal("tower result lacks its complete named transition")
		}
		for field, expected := range map[string]int{
			"stageid": 0, "stage_areaid": 0, "bossid": boss.ID, "is_owner": 1, "is_solo": 1,
			"towerid": tower.TowerID, "tower_floor": 3, "stage_type": int(gamestate.BattleStageAlchemist),
		} {
			var value int
			if json.Unmarshal(transitions[0][field], &value) != nil || value != expected {
				t.Fatalf("result transition %s = %s, want %d", field, transitions[0][field], expected)
			}
		}
		var currentUser map[string]json.RawMessage
		if json.Unmarshal(result["user"], &currentUser) != nil || len(currentUser) == 0 {
			t.Fatal("result lacks the outer user consumed by the existing transition wrapper")
		}
		progress := slices.IndexFunc(settled.TowerQuestProgress, func(p gamestate.TowerQuestProgress) bool { return p.TowerID == tower.TowerID })
		if progress < 0 || settled.TowerQuestProgress[progress].Floor != 3 || !slices.Contains(settled.TowerQuestProgress[progress].ClearedFloors, 2) {
			t.Fatal("accepted run did not commit its tower progress")
		}
		again := call("/TeamBattleSoloEnd", end, true)
		a, _ := json.Marshal(result)
		b, _ := json.Marshal(again)
		if !bytes.Equal(a, b) || state().User.BP != settled.User.BP {
			t.Fatal("settlement retry changed its receipt")
		}
		records, err := handler.accounts.RecentBattleClears(boss.ID, time.Now())
		if err != nil || len(records) != 1 || records[0].Count != 1 || len(records[0].Decks) != 1 || records[0].Decks[0].ArthurType != 5 {
			t.Fatalf("alchemy clear history did not commit exactly once: %v", err)
		}
		if tower.TowerID == 71002 {
			testfixture.CallContentAdmin(t, handler.admin, "PUT", "/api/boss-policy?catalog=tower",
				map[string]any{"mode": "all", "expected_revision": 1}, 200)
		}
		clear := call("/TeamBattleClearDeckShow", map[string]any{"bossid": boss.ID}, true)
		var shown []alchemistDeck
		if json.Unmarshal(clear["partner_deck"], &shown) != nil || len(shown) != 1 || shown[0].Arthur != 5 || shown[0].Job != 5 ||
			shown[0].Burst == nil || *shown[0].Burst != 0 {
			t.Fatalf("tower %d returned an invalid alchemist clear deck", tower.TowerID)
		}
		call("/HomeShow", map[string]any{}, true)
		t.Logf("tower %d floor2 boss %d: authenticated HTTP reject/start/retry/settle/history/return PASS", tower.TowerID, boss.ID)
	}
	if !foundActual {
		t.Fatal("actual tower71002 missing")
	}
}
