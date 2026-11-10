package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestTowerContentUpdateKeepsProgressAndFirstClearOnce(t *testing.T) {
	profile := gamestate.TowerQuestProfile{TowerID: 7, Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, Rank: 1, Boss: json.RawMessage(`{"0":123}`)}}}
	s := &Account{onboarding: completedTestOnboarding(), towerQuestProfiles: map[int]gamestate.TowerQuestProfile{7: profile}, towerQuestProgress: map[int]gamestate.TowerQuestProgress{7: {TowerID: 7, Floor: 1}}}
	rewards := []gamestate.TeamBattleRewardProfile{{BossID: 123, TowerID: 7, TowerFloor: 1, ResultRewards: []gamestate.Reward{{Type: 4, Num: 5}}, FirstClearRewards: []gamestate.Reward{{Type: 4, Num: 100}}}}
	settle := func() {
		s.activeBattle = &TeamBattleContext{FameSeed: "tower-content", FameSources: []TeamBattleFameSource{{MemberType: 1}}, BossID: 123, TowerID: 7, TowerFloor: 1}
		if _, err := s.CompleteTeamBattle(123, true, rewards, TeamBattleDropReport{Turns: 10}); err != nil {
			t.Fatal(err)
		}
	}
	settle()
	before := cloneTowerQuestProgress(s.towerQuestProgress[7])
	profile.Floors[0].ClearRewards = []gamestate.Reward{{Type: 4, Num: 700}}
	s.ApplyTowerQuestConfiguration([]gamestate.TowerQuestProfile{profile})
	after := s.towerQuestProgress[7]
	if len(after.ClearedFloors) != len(before.ClearedFloors) || after.ClearRatings[1] != before.ClearRatings[1] || after.BestClearTurns[1] != before.BestClearTurns[1] {
		t.Fatal("definition update reset private progress")
	}
	rewards[0].FirstClearRewards[0].Num = 700
	settle()
	if s.gold != 110 {
		t.Fatal("edited first-clear reward was granted again", s.gold)
	}
}
