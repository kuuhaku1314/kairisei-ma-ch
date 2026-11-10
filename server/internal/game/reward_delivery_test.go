package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestExperienceRewardRefillsPointsOnlyOnLevelUp(t *testing.T) {
	policy := gamestate.PlayerProgressionPolicy{MaxLevel: 3}
	policy.Experience.Base = 100
	policy.BattlePoints.Base, policy.BattlePoints.LevelsPerPoint, policy.BattlePoints.Maximum = 20, 2, 21
	policy.Friends.Base, policy.Friends.IncreaseFromLevel, policy.Friends.Maximum = 100, 201, 200
	policy.JobParameters.MaxStatusLevel = 80
	policy.JobParameters.Maximum = make([]gamestate.JobParameter, 5)
	for _, tc := range []struct {
		name          string
		amount, level int
		refill        bool
	}{
		{"no-level-up", 50, 1, false}, {"level-up", 100, 2, true}, {"maximum-level", 1000, 3, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			clock := time.Unix(1000, 0)
			s := &Account{onboarding: completedTestOnboarding(), playerProgression: policy, currentLevel: 1, ap: 1, apMax: 3, bp: 2, bpMax: 20,
				apNextRecovery: clock, bpNextRecovery: clock}
			s.applyPlayerExperienceLocked(tc.amount)
			if s.currentLevel != tc.level {
				t.Fatalf("level = %d", s.currentLevel)
			}
			if tc.refill {
				if s.ap != s.apMax || s.bp != s.bpMax || !s.apNextRecovery.IsZero() || !s.bpNextRecovery.IsZero() {
					t.Fatal("level-up did not refill points and reset recovery clocks")
				}
			} else if s.ap != 1 || s.bp != 2 || s.apNextRecovery != clock || s.bpNextRecovery != clock {
				t.Fatal("ordinary EXP gain refilled points")
			}
		})
	}
}

func TestSettlementOverflowPreservesProgressAndClaimableReward(t *testing.T) {
	for _, kind := range []string{"battle", "explore", "story", "gacha", "item-gacha"} {
		t.Run(kind, func(t *testing.T) {
			reward := gamestate.Reward{Type: 8, RewardTypeID: 10, Num: 1, CardSkillLevels: []int16{}}
			s := &Account{onboarding: completedTestOnboarding(), items: map[int]gamestate.Item{10: {ItemID: 10, Num: 1}},
				itemDefinitions: map[int]gamestate.ItemDefinition{10: {ItemID: 10, MaxOwned: 1}},
			}
			var complete func() (PresentReceiveResult, error)
			switch kind {
			case "battle":
				s.teamBattleSolo = battleCatalogFixture(t, json.RawMessage(`{"9":[],"10":[{"0":100001,"9":0,"10":[{"0":10000101,"10":0}]}],"11":[],"12":[]}`))
				s.activeBattle = &TeamBattleContext{BossID: 10000101}
				profile := gamestate.TeamBattleRewardProfile{BossID: 10000101,
					ResultRewards:     []gamestate.Reward{{Type: 4, Num: 5}, reward},
					FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 50}},
				}
				complete = func() (PresentReceiveResult, error) {
					result, err := s.CompleteTeamBattle(10000101, true, []gamestate.TeamBattleRewardProfile{profile})
					return result.Result, err
				}
			case "explore":
				s.exploreActive, s.exploreStartedAt = true, time.Now()
				s.decks = []DeckInfo{{ArthurType: 1, CardUniqueIDs: []int64{}}}
				complete = func() (PresentReceiveResult, error) {
					result, _, _, _, err := s.EndExplore([]gamestate.Reward{reward})
					return result, err
				}
			case "story":
				s.storyRewardPolicy.MainFirstClear = reward
				s.storyMainParts = []gamestate.StoryMainPart{{Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 1, StateFlag: 1}}}}}}
				s.activeMainStoryID = 1
				complete = func() (PresentReceiveResult, error) { return s.EndMainStory(true) }
			case "gacha":
				s.coinFree = 5
				s.gachas = []gamestate.GachaProfile{{GachaID: 7, PayType: 3, Price: 5, CardNum: 1, CardNumMax: 1,
					RewardPool: []gamestate.WeightedReward{{Weight: 1, Reward: reward}}}}
				complete = func() (PresentReceiveResult, error) {
					result, err := s.PlayGacha(7, 3, nil)
					return result.Reward, err
				}
			case "item-gacha":
				s.items[20] = gamestate.Item{ItemID: 20, Num: 1}
				s.itemDefinitions[20] = gamestate.ItemDefinition{ItemID: 20, ItemType: "GACHA", Function: "GACHA_EXEC", MaxOwned: 10}
				s.itemGachaProfiles = map[int]gamestate.ItemGachaProfile{20: {ItemID: 20, Rewards: []gamestate.Reward{reward}}}
				complete = func() (PresentReceiveResult, error) {
					result, err := s.PlayItemGacha(20, 1)
					return result.Reward, err
				}
			}
			result, err := complete()
			if err != nil {
				t.Fatalf("full inventory blocks completed %s: %v", kind, err)
			}
			if !result.InPresentBox || len(result.Items) != 0 {
				t.Fatal("mailed reward was reported as added inventory")
			}
			if s.activeBattle != nil || s.exploreActive || s.activeMainStoryID != 0 || len(s.presents) != 1 || s.items[10].Num != 1 {
				t.Fatal("settlement did not finish with exactly one unclaimed reward")
			}
			if kind == "battle" && (s.gold != 5 || s.coinFree != 50) {
				t.Fatal("full inventory blocked ordinary currency rewards")
			}
			if (kind == "gacha" && s.coinFree != 0) || (kind == "item-gacha" && s.items[20].Num != 0) {
				t.Fatal("overflow draw did not consume payment")
			}
			if kind == "gacha" || kind == "item-gacha" {
				if len(result.Rewards) != 1 || !result.Rewards[0].InPresentBox {
					t.Fatal("mailed gacha reward was sent as an inventory delta")
				}
			}
			_, _ = complete()
			if len(s.presents) != 1 {
				t.Fatal("retry duplicated overflow mail")
			}
			encoded, err := json.Marshal(s.presents)
			if err != nil {
				t.Fatal(err)
			}
			reloaded := &Account{onboarding: completedTestOnboarding(), items: map[int]gamestate.Item{10: {ItemID: 10}}, itemDefinitions: s.itemDefinitions}
			if err := json.Unmarshal(encoded, &reloaded.presents); err != nil {
				t.Fatal(err)
			}
			id := reloaded.presents[0].PresentID
			issued := reloaded.presents[0].IssuedAtUnix
			if issued <= 0 || issued > time.Now().Unix() {
				t.Fatal("gift issue time was not saved")
			}
			if _, err := reloaded.ReceivePresent(id); err != nil || reloaded.items[10].Num != 1 || len(reloaded.presents) != 1 || reloaded.presents[0].State != 1 {
				t.Fatalf("saved overflow reward is not claimable after freeing space: %v", err)
			}
			_, _ = reloaded.ReceivePresent(id)
			if _, err := reloaded.DeletePresents(id); err != nil {
				t.Fatal(err)
			}
			// A cached row may submit deletion again after it has been archived.
			if _, err := reloaded.DeletePresents(id); err != nil {
				t.Fatal(err)
			}
			if reloaded.items[10].Num != 1 || len(reloaded.presentHistories) != 1 {
				t.Fatal("retry duplicated saved reward")
			}
		})
	}
}
