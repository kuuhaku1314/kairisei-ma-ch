package admin

import (
	"encoding/json"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestActivityRewardsAtomicSaveRestartAndNativeSlots(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	o, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	p := &gamestate.TeamBattleScorePolicy{SourceState: "LOCAL_POLICY_DAMAGE_SCORE", EndTurn: 5, Rate: 100, MemberDeadEnd: true}
	for i := range 9 {
		p.Grades = append(p.Grades, gamestate.TeamBattleScoreGrade{Score: int64(i+1) * 100, Rewards: []gamestate.Reward{{Type: 10, Num: 10, CardSkillLevels: []int16{}}}})
	}
	base := gamestate.State{
		TeamBattleSolo:           json.RawMessage(`{"9":[],"10":[{"0":1,"1":13,"4":"圣剑杯","10":[{"0":11,"1":0,"4":"挑战","5":7,"6":4,"7":0,"24":0},{"0":111,"1":1,"4":"挑战","5":7,"6":4,"7":0,"24":1}]}],"11":[],"12":[]}`),
		TeamBattleOwnDeckSources: map[int]int{111: 11},
		TeamBattleRewards:        []gamestate.TeamBattleRewardProfile{{BossID: 11, ScorePolicy: p}, {BossID: 111, ScorePolicy: p}},
		TeamBattleReplays:        []gamestate.TeamBattleReplay{{BossID: 11, EndTurn: 5, CostInitial: 3}, {BossID: 111, EndTurn: 5, CostInitial: 3}},
		Explore:                  gamestate.ExploreProgressState{Events: []json.RawMessage{json.RawMessage(`{"dialogue":"preserve","symbols":[{"pos":17,"reward":[{"type":4,"num":20,"card_skill_lv":[]}]}],"treasureboxes":[]}`)}},
	}
	if err := o.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	a := &API{operations: o}
	router := chi.NewRouter()
	router.Put("/activities", a.saveActivityRewards)
	config, err := o.content.activityDefaults()
	if err != nil {
		t.Fatal(err)
	}
	config.Cups[11].EndTurn, config.Cups[11].Rate = 0, 150
	config.Cups[11].TeamCostInitial = 6
	config.Cups[11].MemberDeadEnd = false
	config.Explore[0].Rewards[0].Num = 200
	config.Explore[0].Chances = []int{250000}
	call := func(revision int, status int) {
		t.Helper()
		testfixture.CallContentAdmin(t, router, "PUT", "/activities", map[string]any{"expected_revision": revision, "config": config}, status)
	}
	call(0, 200)
	call(0, 409)
	for _, cost := range []int{-1, 2, 11} {
		config.Cups[11].TeamCostInitial = cost
		call(1, 400)
	}
	config.Cups[11].TeamCostInitial = 6
	config.Cups[11].Grades[2].Score = 0
	call(1, 400)
	config.Cups[11].Grades[2].Score = 300
	config.Explore[0].Rewards[0].RewardTypeID = 99
	call(1, 400)
	config.Explore[0].Rewards[0].RewardTypeID = 0
	config.Explore[0].Chances = []int{1000001}
	call(1, 400)
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := restarted.InitializeContent(base, ""); err != nil {
		t.Fatal(err)
	}
	s := restarted.content.configuration.State
	if restarted.content.activityRevision != 1 || s.TeamBattleReplays[0].EndTurn != 0 || s.TeamBattleReplays[1].EndTurn != 0 || s.TeamBattleRewards[1].ScorePolicy.Rate != 150 || s.TeamBattleRewards[1].ScorePolicy.MemberDeadEnd {
		t.Fatal("accepted rules did not survive restart or reach both entry modes")
	}
	if s.TeamBattleRewards[0].ScorePolicy.TeamCostInitial != 6 || s.TeamBattleReplays[0].CostInitial != 3 || s.TeamBattleReplays[1].CostInitial != 3 || p.TeamCostInitial != 0 {
		t.Fatal("multiplayer starting COST was lost or changed the solo/base replay")
	}
	var event struct {
		Dialogue string
		Symbols  []struct {
			Pos     int
			Reward  []gamestate.Reward
			Chances []int `json:"local_reward_chances"`
		}
	}
	if err := json.Unmarshal(s.Explore.Events[0], &event); err != nil {
		t.Fatal(err)
	}
	if event.Dialogue != "preserve" || event.Symbols[0].Pos != 17 || event.Symbols[0].Reward[0].Num != 200 || event.Symbols[0].Chances[0] != 250000 || p.Rate != 100 {
		t.Fatal("reward configuration changed native event data or base policy")
	}
}
