package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestExploreFreezesRewardsIncludingEmptyAfterPersistence(t *testing.T) {
	for _, amount := range []int{0, 20} {
		s := onboardingTestStore()
		s.missionCatalog = missionCatalogFixture(t, map[int]gamestate.MissionDefinition{1: {MissionID: 1, Published: true, Title: "探索", Rule: &gamestate.MissionRule{Event: gamestate.MissionExploreClear, Target: 1}}})
		s.missions = map[int]gamestate.MissionProgress{}
		s.ap, s.apMax, s.apRecoveryInterval = 2, 3, time.Hour
		s.avatars = []gamestate.Avatar{{}}
		s.cards = []CardInfo{{UniqueID: 1, CardID: 10000010}}
		s.decks = []DeckInfo{{ArthurType: 1, CardUniqueIDs: []int64{1}}}
		s.exploreStages = []gamestate.ExploreStage{{ExploreStageID: 1}}
		rewards := []gamestate.Reward{}
		if amount != 0 {
			rewards = append(rewards, gamestate.Reward{Type: 4, Num: amount, CardSkillLevels: []int16{}})
		}
		if _, _, _, _, ok := s.BeginExplore(1, 0, rewards); !ok {
			t.Fatal("start rejected")
		}
		if amount != 0 {
			rewards[0].Num = 900
		}
		raw, err := json.Marshal(s.Snapshot(gamestate.State{}).Explore)
		if err != nil {
			t.Fatal(err)
		}
		var restored gamestate.ExploreProgressState
		if err := json.Unmarshal(raw, &restored); err != nil {
			t.Fatal(err)
		}
		if restored.ActiveRewards == nil {
			t.Fatal("frozen empty plan became legacy fallback")
		}
		s.exploreActiveRewards = restored.ActiveRewards
		_, _, done, _, err := s.EndExplore([]gamestate.Reward{{Type: 4, Num: 999, CardSkillLevels: []int16{}}})
		if err != nil || !done || s.gold != amount {
			t.Fatalf("frozen=%d gold=%d err=%v", amount, s.gold, err)
		}
		if _, _, done, _, err := s.EndExplore(rewards); err != nil || done || s.gold != amount {
			t.Fatal("replayed exploration awarded twice", err)
		}
		if s.missions[1].ProgressNow != 1 || s.missions[1].State != 1 || len(s.TakeMissionUpdates()) != 1 {
			t.Fatal("exploration replay duplicated or lost mission progress")
		}
	}
}
