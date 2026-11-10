package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestSavedMissionProgressDistinguishesHistoricalAndLockedTasks(t *testing.T) {
	now := time.Unix(1700000000, 0)
	definitions := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, SeriesID: 10, Published: true, Title: "首项", ProgressMax: 1},
		2: {MissionID: 2, SeriesID: 10, Published: true, PrerequisiteID: 1, Title: "后继", ProgressMax: 1},
		3: {MissionID: 3, SeriesID: 20, Title: "已停用", ProgressMax: 1},
		4: {MissionID: 4, SeriesID: 30, Published: true, StartsAt: now.Unix() + 60, Title: "未开始", ProgressMax: 1},
		5: {MissionID: 5, SeriesID: 40, Published: true, StartsAt: now.Unix() - 60, EndsAt: now.Unix() - 1, Title: "已结束", ProgressMax: 1},
	}
	catalog := missionCatalogFixture(t, definitions)
	saved := []gamestate.Mission{{Info: gamestate.MissionProgress{MissionID: 1, State: 1}}, {Info: gamestate.MissionProgress{MissionID: 3, State: 2}}}
	view := catalog.SavedProgress(saved, now)
	for index, want := range []string{"active", "locked", "inactive", "scheduled", "expired"} {
		if view[index].Availability != want {
			t.Fatalf("task %d availability = %q, want %q", index+1, view[index].Availability, want)
		}
	}
	if view[2].State != 2 {
		t.Fatal("operator history lost a claim from a disabled series")
	}
	encoded, err := json.Marshal(view[1])
	if err != nil {
		t.Fatal(err)
	}
	var row map[string]any
	if json.Unmarshal(encoded, &row) != nil || row["missionid"] != float64(2) || row["availability"] != "locked" {
		t.Fatal("Admin progress lost its flat mission DTO", string(encoded))
	}
	saved[0].Info.State = 2
	if catalog.SavedProgress(saved, now)[1].Availability != "active" {
		t.Fatal("claiming the prerequisite did not unlock the successor view")
	}
}

func TestMissionClaimUsesSharedDefinitionAndPreservesPrivateProgress(t *testing.T) {
	definitions := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, Published: true, Title: "强化任务", ProgressMax: 1,
			Rewards: []gamestate.Reward{{Type: 4, Num: 10}, {Type: 10, Num: 50}}},
		2: {MissionID: 2, Published: true, PrerequisiteID: 1, Title: "后续强化任务", ProgressMax: 10},
	}
	first := &Account{onboarding: completedTestOnboarding(), missionCatalog: missionCatalogFixture(t, definitions), missions: map[int]gamestate.MissionProgress{
		1: {MissionID: 1, State: 1, ProgressNow: 2}, 2: {MissionID: 2, ProgressNow: 2}, 99: {MissionID: 99, State: 2, ProgressNow: 8},
	}}
	second := &Account{onboarding: completedTestOnboarding(), missionCatalog: missionCatalogFixture(t, definitions), missions: map[int]gamestate.MissionProgress{}}
	if err := first.validateMissionsLocked(); err != nil {
		t.Fatal(err)
	}
	view := first.MissionInfos(true)
	if len(view) != 1 || view[0].ProgressNow != 2 || second.MissionNotice().Claimable {
		t.Fatal("private counters were clamped, shared or the successor was exposed early")
	}
	view[0].Rewards[0].Num = 999
	if first.MissionInfos(true)[0].Rewards[0].Num != 10 || second.MissionInfos(true)[0].ProgressNow != 0 {
		t.Fatal("response mutation changed shared definitions or another account")
	}
	if _, _, err := first.ReceiveMissionRewards([]int{1, 2}); err == nil || len(first.presents) != 0 || first.missions[1].State != 1 {
		t.Fatal("one invalid selection partially claimed a mission")
	}
	claimed, remaining, err := first.ReceiveMissionRewards([]int{1})
	if err != nil || len(claimed) != 1 || len(claimed[0].Rewards) != 2 || claimed[0].State != 2 || len(remaining) != 1 || remaining[0].MissionID != 2 || remaining[0].ProgressNow != 2 {
		t.Fatalf("claim did not expose the successor with its own progress: %+v %+v %v", claimed, remaining, err)
	}
	if len(first.presents) != 2 || first.presents[0].Reward.Num != 10 || first.presents[1].Reward.Num != 50 || first.presents[0].PresentID == first.presents[1].PresentID || first.gold != 0 || first.coinFree != 0 {
		t.Fatal("multi-reward mission did not create exactly its reward mails")
	}
	if _, _, err := first.ReceiveMissionRewards([]int{1}); err == nil || len(first.presents) != 2 {
		t.Fatal("claimed mission granted again")
	}
	if len(first.MissionInfos(false)) != 1 || len(first.MissionInfos(true)) != 2 {
		t.Fatal("MissionShow is_reward filtering differs from the captured lists")
	}
	snapshot := first.Snapshot(gamestate.State{})
	raw, err := json.Marshal(snapshot.Engagement.Missions)
	if err != nil {
		t.Fatal(err)
	}
	var saved []gamestate.Mission
	if err := json.Unmarshal(raw, &saved); err != nil || len(saved) != 3 || saved[0].Info.State != 2 || saved[0].Info.ProgressNow != 2 || saved[2].Info.MissionID != 99 {
		t.Fatal("private save lost a claim, overflow counter or unpublished historical ID", err)
	}
	snapshot.Engagement.Missions[0].Info.State = 0
	if first.missions[1].State != 2 || second.missions[1].State != 0 {
		t.Fatal("snapshot mutation leaked into either account")
	}
}

func TestMissionRewardValidationPrecedesEveryClaimInBatch(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), missions: map[int]gamestate.MissionProgress{1: {MissionID: 1, State: 1}, 2: {MissionID: 2, State: 1}},
		missionCatalog: missionCatalogFixture(t, map[int]gamestate.MissionDefinition{
			1: {MissionID: 1, Published: true, Title: "有效奖励", Rewards: []gamestate.Reward{{Type: 4, Num: 10}}},
			2: {MissionID: 2, Published: true, Title: "缺少定义的奖励", Rewards: []gamestate.Reward{{Type: 8, RewardTypeID: 999, Num: 1}}},
		})}
	if _, _, err := s.ReceiveMissionRewards([]int{1, 2}); err == nil || len(s.presents) != 0 || s.missions[1].State != 1 || s.missions[2].State != 1 {
		t.Fatal("invalid final reward partially claimed the batch")
	}
}

func missionCatalogFixture(t *testing.T, definitions map[int]gamestate.MissionDefinition) *MissionCatalog {
	t.Helper()
	catalog, err := NewMissionCatalog(definitions)
	if err != nil {
		t.Fatal(err)
	}
	return catalog
}
