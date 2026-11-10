package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// This runs only with the existing opt-in full-resource construction gate.
// Audit the shipped catalogs, including all own-deck variants and normal quests,
// without touching a player account, listening socket, or device.
func auditCompleteRuntimeDropCatalog(t *testing.T, state gamestate.State, cards masterdata.CardRuntimeMaster, battlePath string) {
	t.Helper()
	master, err := masterdata.LoadBattleRuntimeMaster(battlePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := masterdata.ApplyCardRuntimeMaster(&state, cards); err != nil {
		t.Fatal(err)
	}
	if err := masterdata.ApplyBattleRuntimeMaster(&state, master); err != nil {
		t.Fatal(err)
	}
	normal, err := masterdata.LoadNormalQuestRuntimeMaster(filepath.Join(filepath.Dir(battlePath), "cn602-normal-quest-runtime.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := masterdata.ApplyNormalQuestRuntimeMaster(&state, normal); err != nil {
		t.Fatal(err)
	}
	auditBattleAccountCatalogReuse(t, state, master, normal)
	cardIDs := func(rewards []gamestate.Reward, ordinaryOnly bool) []int {
		ids := []int{}
		for _, reward := range rewards {
			if reward.Type == 6 || (!ordinaryOnly && reward.Type == 13) {
				ids = append(ids, reward.RewardTypeID)
			}
		}
		slices.Sort(ids)
		return slices.Compact(ids)
	}
	byBoss := map[int][]int{}
	for _, profile := range state.TeamBattleRewards {
		if profile.EnemyDrops != nil {
			actual := []gamestate.Reward{}
			for _, drop := range profile.EnemyDrops {
				if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
					actual = append(actual, drop.Reward)
				}
			}
			if !slices.Equal(cardIDs(actual, false), cardIDs(profile.ResultRewards, false)) {
				t.Fatalf("boss %d aggregate cards differ from per-enemy drops", profile.BossID)
			}
		}
		byBoss[profile.BossID] = cardIDs(profile.ResultRewards, true)
	}
	var catalog map[string]json.RawMessage
	if err := json.Unmarshal(state.TeamBattleSolo, &catalog); err != nil {
		t.Fatal(err)
	}
	entryRules := make(map[int]game.TeamBattleEntryRules)
	entryGroups := make(map[int]int)
	for _, category := range []string{"9", "10", "11", "12"} {
		var groups []struct {
			ID     int                         `json:"0"`
			Bosses []game.TeamBattleEntryRules `json:"10"`
		}
		if err := json.Unmarshal(catalog[category], &groups); err != nil {
			t.Fatal(err)
		}
		for _, group := range groups {
			for _, rule := range group.Bosses {
				entryRules[rule.BossID], entryGroups[rule.BossID] = rule, group.ID
			}
		}
	}
	changedGroups, archiveBosses := 0, 0
	for i, raw := range state.TeamBattlePastBossGroups {
		var before, after masterdata.BattlePastBossGroupIdentity
		if json.Unmarshal(master.PastBossGroups[i], &before) != nil || json.Unmarshal(raw, &after) != nil {
			t.Fatal("bad archive")
		}
		if !reflect.DeepEqual(before.Cards, after.Cards) {
			changedGroups++
		}
		var archive struct {
			Bosses []game.TeamBattleEntryRules `json:"13"`
		}
		if err := json.Unmarshal(raw, &archive); err != nil {
			t.Fatal(err)
		}
		for _, rule := range archive.Bosses {
			if shared, found := entryRules[rule.BossID]; !found || !reflect.DeepEqual(shared, rule) || entryGroups[rule.BossID] != after.GroupID {
				t.Fatalf("archive boss %d differs from canonical entry rules", rule.BossID)
			}
		}
		union := []int{}
		for _, boss := range after.Bosses {
			union = append(union, byBoss[boss.BossID]...)
			archiveBosses++
		}
		slices.Sort(union)
		slices.Sort(after.Cards)
		if !slices.Equal(slices.Compact(union), after.Cards) {
			t.Fatalf("archive %d shows cards that do not drop", after.GroupID)
		}
	}
	t.Logf("drop audit: %d reward profiles (normal/event/key/tower/own-deck), %d archive groups, %d archive difficulties; %d group portraits corrected; all actual card IDs agree", len(state.TeamBattleRewards), len(state.TeamBattlePastBossGroups), archiveBosses, changedGroups)
}

// Compare the former full refresh with account preparation from the current
// catalog, including independent own-deck clears and stage-to-entrance progress.
func auditBattleAccountCatalogReuse(t *testing.T, catalog gamestate.State, battle masterdata.BattleRuntimeMaster, normal masterdata.NormalQuestRuntimeMaster) {
	t.Helper()
	encode := func(state gamestate.State) []byte {
		raw, err := json.Marshal(state)
		if err != nil {
			t.Fatal(err)
		}
		return raw
	}
	catalogBefore := encode(catalog)
	for account := 0; account < 2; account++ {
		state := catalog
		state.User.UserID += account + 1
		var solo map[string]json.RawMessage
		if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
			t.Fatal(err)
		}
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(solo["9"], &groups); err != nil {
			t.Fatal(err)
		}
		for groupIndex, group := range groups {
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group["10"], &bosses); err != nil {
				t.Fatal(err)
			}
			for bossIndex, boss := range bosses {
				boss["10"] = json.RawMessage(fmt.Sprint((groupIndex + bossIndex + account) % 3))
			}
			group["10"], _ = json.Marshal(bosses)
		}
		solo["9"], _ = json.Marshal(groups)
		state.TeamBattleSolo, _ = json.Marshal(solo)
		state.StageQuestAreas = slices.Clone(catalog.StageQuestAreas)
		var progress masterdata.NormalQuestProgress
		if err := json.Unmarshal(state.StageQuestAreas[0], &progress); err != nil {
			t.Fatal(err)
		}
		progress.StageQuest.Stages[account].IsClearDone = 1
		state.StageQuestAreas[0], _ = json.Marshal(progress)
		state.TeamBattleSchedule.SoloPushGroupIDs = []int{catalog.TeamBattleScheduleGroupIDs[0], -1, catalog.TeamBattleScheduleGroupIDs[0]}
		state.TeamBattleSchedule.MultiPushGroupIDs = []int{-1}
		tower := catalog.TowerQuestProfiles[0]
		state.TowerQuestProgress = []gamestate.TowerQuestProgress{
			{TowerID: tower.TowerID, Floor: -1, LastBattleFloor: -1, LoseCount: tower.LoseCountMax, LastResult: "invalid", LastRankUp: true, ClearedFloors: []int{2, 1, 2, -1}},
			{TowerID: tower.TowerID, Floor: 2},
			{TowerID: 987654321, Floor: 3, ClearedFloors: []int{1}},
		}
		// Give the reference path its own mutable account data; the optimized
		// path deliberately retains the shared catalog's maps and slices.
		var reference gamestate.State
		if err := json.Unmarshal(encode(state), &reference); err != nil {
			t.Fatal(err)
		}
		if err := masterdata.ApplyBattleRuntimeMaster(&reference, battle); err != nil {
			t.Fatal(err)
		}
		masterdata.NormalizeBattleAccountProgress(&state)
		for _, prepared := range []*gamestate.State{&reference, &state} {
			if err := masterdata.ApplyNormalQuestRuntimeMaster(prepared, normal); err != nil {
				t.Fatal(err)
			}
		}
		var want, got map[string]any
		if err := json.Unmarshal(encode(reference), &want); err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(encode(state), &got); err != nil {
			t.Fatal(err)
		}
		for key, value := range want {
			if !reflect.DeepEqual(value, got[key]) {
				t.Fatalf("account %d catalog reuse differs from full refresh in %s", account, key)
			}
		}
		if !bytes.Equal(catalogBefore, encode(catalog)) {
			t.Fatal("account preparation mutated the shared catalog")
		}
	}
}
