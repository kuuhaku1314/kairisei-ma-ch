package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestMultiplayerMissionUsesCompletionDayWithoutRewindingProgress(t *testing.T) {
	const bossID = 30010102
	beforeMidnight := time.Date(2026, 10, 23, 15, 59, 59, 0, time.UTC)
	afterMidnight := beforeMidnight.Add(time.Second)
	definition := gamestate.MissionDefinition{
		MissionID: 1, Published: true, Daily: true, Title: "每日组队", ProgressMax: 2,
		Rule: &gamestate.MissionRule{Event: gamestate.MissionQuestClear, QuestScope: gamestate.MissionQuestMultiplayer, Target: 2},
	}
	s := &Account{
		teamBattleSolo: battleCatalogFixture(t, json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"10":0}]}],"11":[],"12":[]}`)),
		missions:       map[int]gamestate.MissionProgress{},
		missionCatalog: missionCatalogFixture(t, map[int]gamestate.MissionDefinition{1: definition}),
	}
	s.SetMissionRequestTime(afterMidnight)
	context := TeamBattleContext{BossID: bossID, StageType: gamestate.BattleStageEvent, EntryRole: gamestate.BattleEntryMultiplayerGuest}
	plan := gamestate.TeamBattleSettlementPlan{Profile: gamestate.TeamBattleRewardProfile{BossID: bossID}, EnemyTypes: []int8{1}}
	complete := func(at time.Time) {
		t.Helper()
		if _, err := s.CompleteMultiplayerBattle(context, plan, true, TeamBattleDropReport{}, at); err != nil {
			t.Fatal(err)
		}
	}
	complete(beforeMidnight)
	if s.missions[1].ClearLimitTime != int(beforeMidnight.Unix()) || s.missions[1].ProgressNow != 1 || len(s.TakeMissionUpdates()) != 0 {
		t.Fatalf("late settlement counted for the request day or announced expired progress: %+v", s.missions[1])
	}
	complete(afterMidnight)
	complete(beforeMidnight)
	if s.missions[1].ClearLimitTime != missionDayEnd(afterMidnight.Unix()) || s.missions[1].ProgressNow != 1 {
		t.Fatalf("old settlement replaced current-day progress: %+v", s.missions[1])
	}
}

func TestQuestMissionsRespectScoreDefeatAndKeepScriptedVictoryRewards(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stage gamestate.BattleStageType
		dead  int
		want  int
	}{
		{"ordinary scripted victory", gamestate.BattleStageEvent, 0, 1},
		{"score survived", gamestate.BattleStageScore, 0, 0},
		{"score parts only", gamestate.BattleStageScore, 6, 0},
		{"score body defeated", gamestate.BattleStageScore, 1, 1},
		{"main score survived", gamestate.BattleStageMainScore, 0, 0},
		{"alchemist clear", gamestate.BattleStageAlchemist, 0, 1},
		{"alchemist special clear", gamestate.BattleStageAlchemistSP, 0, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const boss = 30010102
			s := &Account{activeBattle: &TeamBattleContext{BossID: boss, StageType: tc.stage, EntryRole: gamestate.BattleEntrySolo, BattleEnemyTypes: []int8{1}},
				teamBattleSolo: battleCatalogFixture(t, json.RawMessage(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"10":0}]}],"11":[],"12":[]}`)),
				missions:       map[int]gamestate.MissionProgress{}, missionCatalog: missionCatalogFixture(t, map[int]gamestate.MissionDefinition{
					1: {MissionID: 1, Published: true, Title: "通关", Rule: &gamestate.MissionRule{Event: gamestate.MissionQuestClear, Target: 1}},
					2: {MissionID: 2, Published: true, Title: "炼金通关", Rule: &gamestate.MissionRule{Event: gamestate.MissionQuestClear, Target: 1, QuestScope: gamestate.MissionQuestAlchemist}},
				})}
			profiles := []gamestate.TeamBattleRewardProfile{{BossID: boss, ResultRewards: []gamestate.Reward{{Type: 4, Num: 5}}}}
			if _, err := s.CompleteTeamBattle(boss, true, profiles, TeamBattleDropReport{EnemyDeadBits: []int{tc.dead}}); err != nil || s.gold != 5 || s.activeBattle != nil {
				t.Fatal("mission filtering changed the accepted victory or its inventory reward", err)
			}
			wantAlchemist := 0
			if tc.stage == gamestate.BattleStageAlchemist || tc.stage == gamestate.BattleStageAlchemistSP {
				wantAlchemist = tc.want
			}
			if s.missions[1].ProgressNow != tc.want || s.missions[2].ProgressNow != wantAlchemist || len(s.TakeMissionUpdates()) != tc.want+wantAlchemist {
				t.Fatal("score success was treated as body defeat, or ordinary scripted clear was lost")
			}
			if _, err := s.CompleteTeamBattle(boss, true, profiles); err == nil || s.missions[1].ProgressNow != tc.want {
				t.Fatal("duplicate settlement advanced mission progress")
			}
		})
	}
}
