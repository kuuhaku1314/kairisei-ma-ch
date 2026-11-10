package game

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestFollowCapacityGrowthSurvivesLevelUpAndSnapshot(t *testing.T) {
	raw, err := os.ReadFile("../../config/cn602-player-progression-runtime.json")
	if err != nil {
		t.Fatal(err)
	}
	var policy gamestate.PlayerProgressionPolicy
	if err := json.Unmarshal(raw, &policy); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ level, maximum int }{{1, 100}, {199, 100}, {200, 100}, {201, 101}, {250, 150}, {299, 199}, {300, 200}, {301, 200}, {350, 200}} {
		if got := policy.FollowMaximum(tc.level); got != tc.maximum {
			t.Fatalf("level %d follow capacity %d, want %d", tc.level, got, tc.maximum)
		}
	}
	for _, level := range []int{47, 199, 200, 299, 300} {
		s := &Account{onboarding: completedTestOnboarding(), playerProgression: policy, currentLevel: level, currentFollowMax: policy.FollowMaximum(level),
			bp: 10, bpMax: policy.BattlePointMaximum(level), bpRecoveryInterval: 3 * time.Minute, apRecoveryInterval: 2 * time.Hour}
		s.applyPlayerExperienceLocked(policy.ExperienceRequired(level))
		view := s.PlayerProgressionState()
		if view.Level != level+1 || view.FollowMax != policy.FollowMaximum(level+1) || s.FollowMaximum() != view.FollowMax {
			t.Fatalf("level-up follow capacity: %+v", view)
		}
		saved := s.Snapshot(gamestate.State{})
		if saved.User.FollowMax != view.FollowMax || saved.User.Level != view.Level {
			t.Fatal("follow capacity not persisted")
		}
	}
}
