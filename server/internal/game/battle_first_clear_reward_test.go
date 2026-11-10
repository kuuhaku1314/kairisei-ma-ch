package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestQuestFirstClearCrystalsAreGrantedOncePerDifficulty(t *testing.T) {
	for _, stageQuest := range []bool{false, true} {
		name := "activity or material"
		if stageQuest {
			name = "normal stage"
		}
		t.Run(name, func(t *testing.T) {
			const firstBoss, secondBoss, area = 10000101, 10000102, 100001
			configuration := json.RawMessage(`{"9":[{"0":100001,"9":100001,"10":[{"0":10000101,"10":0},{"0":10000102,"10":0}]}],"10":[],"11":[],"12":[]}`)
			if !stageQuest {
				configuration = json.RawMessage(`{"9":[],"10":[{"0":100001,"9":0,"10":[{"0":10000101,"10":0},{"0":10000102,"10":0}]}],"11":[],"12":[]}`)
			}
			s := &Account{onboarding: completedTestOnboarding(), coinFree: 7, teamBattleSolo: battleCatalogFixture(t, configuration)}
			if stageQuest {
				s.stageQuests = map[int]json.RawMessage{area: json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[]}]},{"stageid":10000102,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000102,"state":0}]},"clear_reward":[]}]}]},"stage_clear":[],"new_clear_stage":[]}`)}
			}
			for _, step := range []struct {
				boss       int
				win, first bool
				balance    int
			}{
				{firstBoss, false, false, 7},
				{firstBoss, true, true, 57},
				{firstBoss, true, false, 57},
				{secondBoss, true, true, 107},
			} {
				context := TeamBattleContext{BossID: step.boss}
				profile := gamestate.TeamBattleRewardProfile{BossID: step.boss, FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 50, CardSkillLevels: []int16{}}}}
				if stageQuest {
					context.StageQuestAreaID, context.StageQuestStageID = area, step.boss
					profile.StageQuestAreaID, profile.StageQuestStageID = area, step.boss
				}
				s.activeBattle = &context
				result, err := s.CompleteTeamBattle(step.boss, step.win, []gamestate.TeamBattleRewardProfile{profile})
				if err != nil {
					t.Fatal(err)
				}
				if result.WasFirstClear != step.first || s.coinFree != step.balance {
					t.Fatalf("boss=%d win=%v: first=%v balance=%d, want %v/%d", step.boss, step.win, result.WasFirstClear, s.coinFree, step.first, step.balance)
				}
				if _, err := s.CompleteTeamBattle(step.boss, step.win, []gamestate.TeamBattleRewardProfile{profile}); err == nil || s.coinFree != step.balance {
					t.Fatal("duplicate settlement must not grant rewards again")
				}
				// Reconstruct the domain store from durable progress/coins. No
				// active run or result receipt can suppress a new run's reward.
				s = &Account{onboarding: completedTestOnboarding(), coinFree: s.coinFree, teamBattleSolo: battleCatalogFixture(t, s.teamBattleSolo.saved()), stageQuests: s.stageQuests}
			}
		})
	}
}
