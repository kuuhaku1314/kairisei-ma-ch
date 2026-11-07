package game

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
	"time"
)

func TestAchievementChainReadsExistingStateWithoutGivingRelativeTasksHistory(t *testing.T) {
	defs := map[int]gamestate.MissionDefinition{}
	for i, goal := range []int{10, 30, 50} {
		id := i + 1
		parent := 0
		if i > 0 {
			parent = id - 1
		}
		defs[id] = gamestate.MissionDefinition{MissionID: id, Title: "等级成就", Published: true, PrerequisiteID: parent, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionPlayerLevel, Target: goal}}
	}
	defs[10] = gamestate.MissionDefinition{MissionID: 10, Title: "强化一次", Published: true, Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 1}}
	defs[11] = gamestate.MissionDefinition{MissionID: 11, Title: "累计登录", Published: true, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionLoginDays, Target: 30}}
	defs[12] = gamestate.MissionDefinition{MissionID: 12, Title: "连续登录", Published: true, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionLoginStreak, Target: 3}}
	s := &Account{currentLevel: 45, currentName: "player", loginBonusState: gamestate.LoginBonusState{TotalClaims: 37, LastClaimDay: "2026-10-20"}, missions: map[int]gamestate.MissionProgress{}, missionCatalog: missionCatalogFixture(t, defs)}
	s.markAllMissionMetricsLocked()
	s.RefreshStateMissions(time.Now())
	if s.missions[1].ProgressNow != 45 || s.missions[1].State != 1 || s.missions[2].State != 0 || s.missions[10].ProgressNow != 0 || s.missions[11].ProgressNow != 37 || s.missions[12].ProgressNow != 0 {
		t.Fatal("state/relative/history boundary changed", s.missions)
	}
	for _, id := range []int{1, 2} {
		if _, _, err := s.ReceiveMissionRewards([]int{id}); err != nil {
			t.Fatal(err)
		}
	}
	if s.missions[3].ProgressNow != 45 || s.missions[3].State != 0 {
		t.Fatal("successor restarted cumulative progress", s.missions[3])
	}
	s.ObserveMissionVisit(time.Now())
	s.RefreshStateMissions(time.Now())
	if s.missions[12].ProgressNow != 1 {
		t.Fatal("streak was inferred from historical reward claims")
	}
}

func TestMissionUTC8BoundaryAndScheduledStateActivation(t *testing.T) {
	before := time.Date(2026, 10, 23, 15, 59, 59, 0, time.UTC)
	after := before.Add(time.Second)
	defs := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, Title: "每日强化", Published: true, Daily: true, Rule: &gamestate.MissionRule{Event: gamestate.MissionCardFusion, Target: 1}},
		2: {MissionID: 2, Title: "排期等级", Published: true, StartsAt: after.Unix(), Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionPlayerLevel, Target: 10}},
	}
	s := &Account{currentLevel: 15, missions: map[int]gamestate.MissionProgress{}, missionCatalog: missionCatalogFixture(t, defs)}
	s.markAllMissionMetricsLocked()
	s.RefreshStateMissions(before)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, before)
	if s.missions[1].ClearLimitTime != int(before.Unix()) || s.missions[2].State != 0 {
		t.Fatal("wrong UTC+8 period")
	}
	s.RefreshStateMissions(after)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, after)
	if s.missions[1].ProgressNow != 1 || s.missions[1].ClearLimitTime != int(before.Unix()+86400) || s.missions[2].State != 1 {
		t.Fatal("daily reset or scheduled activation failed", s.missions)
	}
}

func TestMissionPVPRankShowsOnlyCompletion(t *testing.T) {
	defs := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, Title: "竞技场段位成就", Published: true, ProgressShow: 1, ProgressMax: 3,
			Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionPVPRank, Target: 3}},
	}
	s := &Account{missions: map[int]gamestate.MissionProgress{}, missionCatalog: missionCatalogFixture(t, defs)}
	s.SetMissionPVPRanks([]PVPRankConfig{
		{RankID: 2, PointMin: 100, PointMax: 199},
		{RankID: 3, PointMin: 200, PointMax: 299},
		{RankID: 4, PointMin: 300, PointMax: 399},
	})
	now := time.Now()
	for _, step := range []struct {
		points, savedRank, displayedProgress, notifications int
	}{
		{100, 2, 0, 0},
		{200, 3, 1, 1},
		{300, 4, 1, 0},
	} {
		s.pvpPoint = step.points
		s.markMissionMetricLocked(gamestate.MissionPVPRank)
		s.RefreshStateMissions(now)
		info := s.MissionInfos(false)
		if len(info) != 1 || info[0].ProgressNow != step.displayedProgress || info[0].ProgressMax != 1 || s.missions[1].ProgressNow != step.savedRank {
			t.Fatalf("points %d: incorrect rank projection or saved progress: %+v, %+v", step.points, info, s.missions[1])
		}
		updates := s.TakeMissionUpdates()
		if len(updates) != step.notifications || len(updates) == 1 && (updates[0].ProgressNow != 1 || updates[0].ProgressMax != 1) {
			t.Fatalf("points %d: unexpected progress notifications: %+v", step.points, updates)
		}
	}
}

func TestMissionOwnedSetRefreshesAfterSphereMaterialConsumption(t *testing.T) {
	definition := gamestate.MissionDefinition{
		MissionID: 1, Published: true, Title: "持有指定素材", ProgressMax: 2,
		Rule: &gamestate.MissionRule{Kind: gamestate.MissionTargetSet, Metric: gamestate.MissionCardOwned, Targets: []int{50, 51}, Target: 2},
	}
	s := &Account{
		gold: 100, spheres: []gamestate.Sphere{{UniqueID: 1, SphereID: 10, Level: 1}},
		sphereDefinitions: map[int]gamestate.SphereDefinition{
			10: {SphereID: 10, SameSphereID: 10, EvolutionID: 11, MaxLevel: 1},
			11: {SphereID: 11, SameSphereID: 10, EvolutionCount: 1, MaxLevel: 1, Rarity: "1", MaterialAddExperience: 1},
		},
		sphereEvoPrices: map[string][]int{"1": {1}},
		stackCards:      []gamestate.CardStack{{CardID: 50, Num: 1, MaterialType: sphereEvolutionMaterialType}},
		missionCatalog:  missionCatalogFixture(t, map[int]gamestate.MissionDefinition{1: definition}),
		missions:        map[int]gamestate.MissionProgress{},
	}
	now := time.Now()
	s.markAllMissionMetricsLocked()
	s.RefreshStateMissions(now)
	if got := s.MissionInfos(false)[0].ProgressNow; got != 1 {
		t.Fatalf("initial owned progress = %d, want 1", got)
	}
	if _, _, _, _, err := s.EvolveSphere(1, 0, 50); err != nil {
		t.Fatal(err)
	}
	s.RefreshStateMissions(now)
	if got := s.MissionInfos(false)[0].ProgressNow; got != 0 {
		t.Fatalf("owned progress after consuming last copy = %d, want 0", got)
	}
}

func TestStoryAchievementsReadSavedReadingAndLearningCompletion(t *testing.T) {
	const readingID = 13001001
	defs := map[int]gamestate.MissionDefinition{
		1: {MissionID: 1, Title: "已读剧情", Published: true, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionStoryCleared, SubjectID: readingID, Target: 1}},
		2: {MissionID: 2, Title: "圣剑学习第一节", Published: true, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionStoryCleared, SubjectID: 45001000, Target: 1}},
		3: {MissionID: 3, Title: "圣剑学习第二节", Published: true, Rule: &gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionStoryCleared, SubjectID: 45001010, Target: 1}},
	}
	s := &Account{questStoryCleared: map[int]bool{readingID: true}, missions: map[int]gamestate.MissionProgress{}, missionCatalog: missionCatalogFixture(t, defs)}
	s.markAllMissionMetricsLocked()
	s.RefreshStateMissions(time.Now())
	if s.missions[1].State != 1 || s.missions[2].State != 0 || s.missions[3].State != 0 {
		t.Fatal("stored reading completion or unplayed learning state was misread", s.missions)
	}
	s.burstProgress[0] = 1
	s.markMissionMetricLocked(gamestate.MissionStoryCleared)
	s.RefreshStateMissions(time.Now())
	if s.missions[2].State != 1 || s.missions[3].State != 0 {
		t.Fatal("learning completion did not respect the saved chapter cursor", s.missions)
	}
	s.burstProgress[0] = 2
	s.markMissionMetricLocked(gamestate.MissionStoryCleared)
	s.RefreshStateMissions(time.Now())
	if s.missions[3].State != 1 {
		t.Fatal("second learning chapter did not complete its achievement")
	}
}
