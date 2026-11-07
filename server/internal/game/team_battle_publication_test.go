package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestOnboardingIceDragonIgnoresPublicationClosureOnlyForItsQuest(t *testing.T) {
	const bossID = 30010102
	raw := json.RawMessage(`{"9":[],"10":[
		{"0":700300101,"9":0,"10":[
			{"0":30010102,"14":1,"5":15,"10":0,"16":0},
			{"0":30010103,"14":1,"10":0,"16":0}]},
		{"0":700300201,"9":0,"10":[{"0":30020102,"14":1,"10":0,"16":0}]}
	],"11":[],"12":[{"0":700400010,"9":0,"10":[{"0":40001002,"14":0,"10":0,"16":0}]}]}`)
	disabled := map[int]bool{bossID: true, 30010103: true, 30020102: true}
	s := onboardingTestStore()
	s.teamBattleSolo = battleCatalogFixture(t, raw)
	s.ApplyBattleCatalogConfiguration(nil, disabled)
	s.cardDefinitions = map[int]gamestate.Card{1: {CardID: 1}}
	s.bp, s.bpMax, s.bpRecoveryInterval = 30, 30, 3*time.Minute
	helper := &s.playerProgression.Friends.HelperReward
	helper.OtherPerPartner, helper.FriendPerPartner, helper.MaximumPartners = 5, 10, 3
	profiles := []gamestate.TeamBattleRewardProfile{{BossID: bossID}}
	sourceProfiles := []gamestate.TeamBattleRewardProfile{{BossID: bossID,
		ResultRewards: []gamestate.Reward{{Type: 6, RewardTypeID: 1, Num: 1}}}}
	for _, step := range []int{cnOnboardingStepCount - 2, cnOnboardingStepCount - 1, cnOnboardingStepCount} {
		s.onboarding.Step = step
		wantOpen := step == cnOnboardingStepCount-1
		catalog, err := s.TeamBattleCatalog()
		if err != nil {
			t.Fatal(err)
		}
		if catalog.HasBoss(bossID) != wantOpen || catalog.BossAccessible(bossID, time.Now().Unix()) != wantOpen {
			t.Fatalf("step %d published/accessibility differs from tutorial exception", step)
		}
		if catalog.HasBoss(30010103) || catalog.HasBoss(30020102) {
			t.Fatalf("step %d reopened another closed difficulty", step)
		}
		if _, found := s.TeamBattleEntryRulesForBoss(bossID); found != wantOpen {
			t.Fatalf("step %d entry rules differ from publication", step)
		}
		rows, err := s.HowToGetCards([]int{1}, sourceProfiles, CardSourcePolicy{})
		if err != nil || len(rows) != 1 || len(rows[0].GetCards) != 1 {
			t.Fatalf("step %d card source query: %+v, %v", step, rows, err)
		}
		if source := rows[0].GetCards[0]; (source.Type == 10 && source.ContentID == bossID) != wantOpen {
			t.Fatalf("step %d card source differs from publication: %+v", step, source)
		}
		beforeBP := s.bp
		_, _, started, err := s.BeginTeamBattle(TeamBattleEntry{BossID: bossID,
			EnemyTypes: []int8{0}, Profiles: profiles, FameSeed: "onboarding-publication",
			FameSources: []TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}})
		if wantOpen {
			if err != nil || !started || s.bp != beforeBP-15 {
				t.Fatalf("tutorial entry did not debit normally: started=%t bp=%d err=%v", started, s.bp, err)
			}
			s.activeBattle = nil
		} else if err == nil || started || s.bp != beforeBP {
			t.Fatalf("step %d admitted or charged the closed boss", step)
		}
		if !disabled[bossID] || !s.disabledTeamBattleBossIDs[bossID] {
			t.Fatal("account exception mutated the shared Admin closure")
		}
	}
}

func publicationGroup(groupID, areaID, model int) json.RawMessage {
	bossID := groupID + 1
	if areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax {
		bossID = areaID*100 + 1
	}
	return publicationGroupWithBoss(groupID, areaID, bossID, model)
}

func publicationGroupWithBoss(groupID, areaID, bossID, model int) json.RawMessage {
	content, _ := json.Marshal(map[string]any{
		"0": groupID,
		"9": areaID,
		"10": []any{map[string]any{
			"0":  bossID,
			"14": model,
			"27": map[string]any{
				"0": "", "1": "", "2": "", "3": 0,
				"4": "", "5": "", "6": "", "7": "",
			},
		}},
	})
	return content
}

func publicationGroupWithBosses(groupID, areaID, model int, bossIDs ...int) json.RawMessage {
	bosses := make([]any, len(bossIDs))
	for index, bossID := range bossIDs {
		bosses[index] = map[string]any{
			"0": bossID, "14": model,
			"27": map[string]any{
				"0": "", "1": "", "2": "", "3": 0,
				"4": "", "5": "", "6": "", "7": "",
			},
		}
	}
	content, _ := json.Marshal(map[string]any{
		"0":  groupID,
		"9":  areaID,
		"10": bosses,
	})
	return content
}

func TestProjectCNTeamBattlePublicationOnlyFirstActivityFamilyDuringTutorial(t *testing.T) {
	configuration, err := json.Marshal(map[string]any{
		"9": []json.RawMessage{
			publicationGroupWithBosses(760000012, 0, 1, 30010102, 30010104, 30010103),
			publicationGroupWithBosses(760000013, 0, 1, 30020102, 30020103),
			publicationGroupWithBosses(710000012, 0, 0, 40001002, 40001004),
			publicationGroupWithBosses(710000013, 0, 0, 40002002, 40002003),
		},
		"10": []any{}, "11": []any{}, "12": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectCNTeamBattlePublication(configuration, nil, nil, false, true)
	if err != nil {
		t.Fatalf("project publication: %v", err)
	}
	var top map[string][]struct {
		GroupID int `json:"0"`
		Bosses  []struct {
			BossID int `json:"0"`
		} `json:"10"`
	}
	if err := json.Unmarshal(projected, &top); err != nil {
		t.Fatal(err)
	}
	if len(top["10"]) != 1 || len(top["10"][0].Bosses) != 1 {
		t.Fatalf("projected 3D bosses = %+v", top["10"])
	}
	if got := top["10"][0].Bosses[0].BossID; got != 30010102 {
		t.Fatalf("tutorial 3D boss = %d, want 30010102", got)
	}
	if len(top["12"]) != 1 || len(top["12"][0].Bosses) != 1 {
		t.Fatalf("projected 2D bosses = %+v", top["12"])
	}
	if got := top["12"][0].Bosses[0].BossID; got != 40001002 {
		t.Fatalf("tutorial 2D boss = %d, want 40001002", got)
	}
}

func TestProjectCNTeamBattlePublicationKeepsFullActivityFamiliesOutsideTutorial(t *testing.T) {
	configuration, err := json.Marshal(map[string]any{
		"9": []json.RawMessage{
			publicationGroupWithBosses(760000012, 0, 1, 30010102, 30010104, 30010103),
			publicationGroupWithBosses(760000013, 0, 1, 30020102, 30020103),
			publicationGroupWithBosses(710000012, 0, 0, 40001002, 40001004),
			publicationGroupWithBosses(710000013, 0, 0, 40002002, 40002003),
		},
		"10": []any{}, "11": []any{}, "12": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectCNTeamBattlePublication(configuration, nil, nil, false, false)
	if err != nil {
		t.Fatalf("project publication: %v", err)
	}
	var top map[string][]struct {
		Bosses []json.RawMessage `json:"10"`
	}
	if err := json.Unmarshal(projected, &top); err != nil {
		t.Fatal(err)
	}
	if len(top["10"]) != 2 || len(top["10"][0].Bosses) != 3 || len(top["10"][1].Bosses) != 2 {
		t.Fatalf("full 3D activity family = %+v", top["10"])
	}
	if len(top["12"]) != 2 || len(top["12"][0].Bosses) != 2 || len(top["12"][1].Bosses) != 2 {
		t.Fatalf("full 2D activity family = %+v", top["12"])
	}
}

func TestProjectCNTeamBattlePublicationOrdersByFirstContentAppearance(t *testing.T) {
	// Generated group IDs follow pict_id and are intentionally opposite to the
	// official boss/content identities in this fixture.
	groups := []json.RawMessage{
		publicationGroupWithBoss(710000300, 0, 40003002, 0),
		publicationGroupWithBoss(710000100, 0, 40009002, 0),
		publicationGroupWithBoss(760000100, 0, 30090102, 1),
		publicationGroupWithBoss(760000300, 0, 30020103, 1),
	}
	configuration, err := json.Marshal(map[string]any{
		"9": groups, "10": []any{}, "11": []any{}, "12": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectCNTeamBattlePublication(configuration, nil, nil, false, false)
	if err != nil {
		t.Fatalf("project publication: %v", err)
	}
	var top map[string][]struct {
		GroupID int `json:"0"`
	}
	if err := json.Unmarshal(projected, &top); err != nil {
		t.Fatal(err)
	}
	want3D := []int{760000300, 760000100}
	want2D := []int{710000300, 710000100}
	for index, expected := range want3D {
		if top["10"][index].GroupID != expected {
			t.Fatalf("3D group %d = %d, want %d", index, top["10"][index].GroupID, expected)
		}
	}
	for index, expected := range want2D {
		if top["12"][index].GroupID != expected {
			t.Fatalf("2D group %d = %d, want %d", index, top["12"][index].GroupID, expected)
		}
	}
}

func publicationArea(areaID int, clears ...int) json.RawMessage {
	stages := make([]any, len(clears))
	for index, clear := range clears {
		bossID := areaID*100 + index + 1
		stages[index] = map[string]any{
			"stageid":        bossID,
			"is_release_now": 1,
			"is_clear_done":  clear,
			"raid_boss": []any{map[string]any{
				"boss_group": map[string]any{
					"bosses": []any{map[string]any{"bossid": bossID, "bp_use": index + 1}},
				},
			}},
		}
	}
	content, _ := json.Marshal(map[string]any{
		"stage_quest": map[string]any{
			"areaid":       areaID,
			"stage_object": stages,
		},
		"stage_clear":     []any{},
		"new_clear_stage": []any{},
	})
	return content
}

func TestProjectCNTeamBattlePublicationSeparatesCategoriesAndUnlocksNormalAreas(t *testing.T) {
	groups := []json.RawMessage{
		publicationGroupWithBosses(100001, 100001, 0, 10000101, 10000102),
		publicationGroupWithBosses(100002, 100002, 0, 10000201, 10000202, 10000203),
		publicationGroupWithBosses(100003, 100003, 0, 10000301, 10000302),
		publicationGroup(200000, 0, 1),
		publicationGroup(710000010, 0, 0),
		publicationGroup(710000020, 0, 0),
		publicationGroup(760000010, 0, 1),
		publicationGroup(760000020, 0, 1),
	}
	configuration, err := json.Marshal(map[string]any{
		"9": groups, "10": []any{}, "11": []any{}, "12": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}
	projected, err := projectCNTeamBattlePublication(configuration, map[int]json.RawMessage{
		100001: publicationArea(100001, 1, 1),
		100002: publicationArea(100002, 1, 0, 0),
		100003: publicationArea(100003, 0, 0),
	}, []int{710000010}, false, false)
	if err != nil {
		t.Fatalf("project publication: %v", err)
	}
	var top map[string][]struct {
		GroupID   int `json:"0"`
		StageType int `json:"1"`
		Bosses    []struct {
			BossID int `json:"0"`
			State  int `json:"10"`
			IsLock int `json:"26"`
			Unlock *struct {
				Name string `json:"0"`
			} `json:"27"`
		} `json:"10"`
	}
	if err := json.Unmarshal(projected, &top); err != nil {
		t.Fatal(err)
	}
	assertGroupIDs := func(key string, expected ...int) {
		t.Helper()
		actual := top[key]
		if len(actual) != len(expected) {
			t.Fatalf("category %s length = %d, want %d", key, len(actual), len(expected))
		}
		for index := range expected {
			if actual[index].GroupID != expected[index] {
				t.Fatalf("category %s group %d = %d, want %d", key, index, actual[index].GroupID, expected[index])
			}
		}
	}
	assertGroupIDs("9", 100001, 100002)
	assertGroupIDs("10", 760000010, 760000020)
	assertGroupIDs("11", 710000010)
	assertGroupIDs("12", 710000020)
	if top["9"][0].StageType != 0 || top["9"][1].StageType != 0 {
		t.Fatalf("normal quest groups did not use regular stage type: %+v", top["9"])
	}
	// The stock stage scene expects the complete area's boss topology. StageQuest
	// release state and the boss lock bit gate the current node without removing
	// future nodes from this DTO.
	wantSecondArea := []struct {
		boss  int
		state int
		lock  int
	}{{10000201, 2, 0}, {10000202, 0, 0}, {10000203, 0, 1}}
	if len(top["9"][1].Bosses) != len(wantSecondArea) {
		t.Fatalf("second area visible bosses = %d, want %d", len(top["9"][1].Bosses), len(wantSecondArea))
	}
	for index, expected := range wantSecondArea {
		boss := top["9"][1].Bosses[index]
		if boss.BossID != expected.boss || boss.State != expected.state || boss.IsLock != expected.lock {
			t.Fatalf("normal boss %d identity/state/lock = %d/%d/%d, want %d/%d/%d", index, boss.BossID, boss.State, boss.IsLock, expected.boss, expected.state, expected.lock)
		}
		if boss.Unlock == nil {
			t.Fatalf("normal boss %d omitted unlock DTO", index)
		}
	}
}

func TestProjectCNNormalQuestGroupPublishesCompleteLockedTopologyForFreshArea(t *testing.T) {
	projected, err := projectNormalQuestGroupForTest(
		publicationGroupWithBosses(100001, 100001, 0, 10000101, 10000102, 10000103),
		publicationArea(100001, 0, 0, 0),
		false,
	)
	if err != nil {
		t.Fatalf("project fresh normal area: %v", err)
	}
	var group struct {
		StageType int `json:"1"`
		Bosses    []struct {
			BossID int `json:"0"`
			State  int `json:"10"`
			IsLock int `json:"26"`
			Unlock *struct {
				Name string `json:"0"`
			} `json:"27"`
		} `json:"10"`
	}
	if err := json.Unmarshal(projected, &group); err != nil {
		t.Fatal(err)
	}
	if group.StageType != 0 || len(group.Bosses) != 3 {
		t.Fatalf("fresh normal area projection = %+v", group)
	}
	wantLocks := []int{0, 1, 1}
	for index := range group.Bosses {
		boss := group.Bosses[index]
		if boss.BossID != 10000101+index || boss.State != 0 ||
			boss.IsLock != wantLocks[index] || boss.Unlock == nil {
			t.Fatalf("fresh normal boss %d = %+v", index, boss)
		}
	}
}

func TestProjectCNNormalQuestGroupPublishesOnlyFixedFirstRowDuringTutorial(t *testing.T) {
	projected, err := projectNormalQuestGroupForTest(
		publicationGroupWithBosses(100001, 100001, 0, 10000101, 10000102, 10000103),
		publicationArea(100001, 0, 0, 0),
		true,
	)
	if err != nil {
		t.Fatalf("project tutorial normal area: %v", err)
	}
	var group struct {
		StageType int `json:"1"`
		Bosses    []struct {
			BossID int `json:"0"`
			State  int `json:"10"`
			IsLock int `json:"26"`
			Unlock *struct {
				Name string `json:"0"`
			} `json:"27"`
		} `json:"10"`
	}
	if err := json.Unmarshal(projected, &group); err != nil {
		t.Fatal(err)
	}
	if group.StageType != 0 || len(group.Bosses) != 1 {
		t.Fatalf("tutorial normal area projection = %+v", group)
	}
	boss := group.Bosses[0]
	if boss.BossID != 10000101 || boss.State != 0 || boss.IsLock != 0 || boss.Unlock == nil {
		t.Fatalf("tutorial normal boss = %+v", boss)
	}
}

func TestProjectCNStageQuestPublicationUnlocksOneStageAtATime(t *testing.T) {
	projected, err := projectCNStageQuestPublication(publicationArea(100001, 1, 0, 0))
	if err != nil {
		t.Fatalf("project stage quest: %v", err)
	}
	var value struct {
		StageQuest struct {
			Stages []struct {
				Released int `json:"is_release_now"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(projected, &value); err != nil {
		t.Fatal(err)
	}
	want := []int{1, 1, 0}
	for index, expected := range want {
		if value.StageQuest.Stages[index].Released != expected {
			t.Fatalf("stage %d release = %d, want %d", index, value.StageQuest.Stages[index].Released, expected)
		}
	}
	stageID, bpUse, found, err := releasedStageQuestBattleForBoss(projected, 100001, 10000102)
	if err != nil || !found || stageID != 10000102 || bpUse != 2 {
		t.Fatalf("released stage lookup = %d/%d/%t/%v", stageID, bpUse, found, err)
	}
	stageID, bpUse, found, err = releasedStageQuestBattleForBoss(projected, 100001, 10000103)
	if err != nil || found || stageID != 0 || bpUse != 0 {
		t.Fatalf("locked stage lookup = %d/%d/%t/%v", stageID, bpUse, found, err)
	}
}

func projectNormalQuestGroupForTest(group, progress json.RawMessage, tutorial bool) (json.RawMessage, error) {
	configuration, err := json.Marshal(map[string]any{"9": []json.RawMessage{group}})
	if err != nil {
		return nil, err
	}
	projected, err := projectCNTeamBattlePublication(configuration, map[int]json.RawMessage{100001: progress}, nil, tutorial, false)
	if err != nil {
		return nil, err
	}
	var lists map[string][]json.RawMessage
	if err := json.Unmarshal(projected, &lists); err != nil {
		return nil, err
	}
	return lists["9"][0], nil
}
