package game

import (
	"encoding/json"
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func skipTestAccount(itemPrice bool) (*Account, []gamestate.TeamBattleRewardProfile) {
	floor := gamestate.TowerQuestFloorProfile{Floor: 1, Rank: 1, BPUse: 60,
		Boss: json.RawMessage(`{"0":123,"skip_itemid":10}`)}
	if itemPrice {
		floor.ItemID, floor.ItemUse = 11, 3
	}
	s := &Account{onboarding: completedTestOnboarding(), bp: 1000, bpMax: 1000, bpRecoveryInterval: 3 * time.Minute,
		towerAdmission:     TowerAdmission{TowerIDs: map[int]struct{}{7: {}}},
		items:              map[int]gamestate.Item{10: {ItemID: 10, Num: 10}, 11: {ItemID: 11, Num: 10}},
		itemDefinitions:    map[int]gamestate.ItemDefinition{20: {ItemID: 20, MaxOwned: 3}},
		towerQuestProfiles: map[int]gamestate.TowerQuestProfile{7: {TowerID: 7, Floors: []gamestate.TowerQuestFloorProfile{floor}}},
		towerQuestProgress: map[int]gamestate.TowerQuestProgress{7: {TowerID: 7, Floor: 0, ClearedFloors: []int{1}, ClearRatings: map[int]int{1: 7}, BestClearTurns: map[int]int{1: 8}}},
	}
	return s, []gamestate.TeamBattleRewardProfile{{BossID: 123, TowerID: 7, TowerFloor: 1,
		ResultRewards:     []gamestate.Reward{{Type: 4, Num: 5}, {Type: 8, RewardTypeID: 20, Num: 2}},
		FirstClearRewards: []gamestate.Reward{{Type: 10, Num: 50}}}}
}

func TestTeamBattleSkipChargesOneBatchAndPreservesCompletedProgress(t *testing.T) {
	for _, itemPrice := range []bool{false, true} {
		s, profiles := skipTestAccount(itemPrice)
		before, _ := json.Marshal(s.towerQuestProgress[7])
		results, err := s.SkipTeamBattle(123, 2, profiles, []int8{0})
		if err != nil {
			t.Fatal(err)
		}
		if s.items[10].Num != 8 || s.gold != 10 || s.coinFree != 0 || len(results) != 2 || len(results[0].Rewards) != 2 || len(results[1].Rewards) != 2 {
			t.Fatal("skip did not charge tickets and grant exactly two normal clears", results)
		}
		if itemPrice {
			if s.items[11].Num != 4 || s.bp != 1000 {
				t.Fatal("item entry also charged BP")
			}
		} else if s.items[11].Num != 10 || s.bp != 880 || s.bpNextRecovery.IsZero() {
			t.Fatal("BP price or recovery clock is wrong")
		}
		if s.items[20].Num != 2 || len(s.presents) != 1 || results[0].InPresentBox || !results[1].InPresentBox {
			t.Fatal("batch overflow was lost or not marked as mailed")
		}
		after, _ := json.Marshal(s.towerQuestProgress[7])
		if string(before) != string(after) || s.activeBattle != nil {
			t.Fatal("skip invented a new clear record, score or battle session")
		}
	}
}

func TestTeamBattleSkipRejectsBeforeAnyCostOrGrant(t *testing.T) {
	for _, reason := range []string{"zero", "over-limit", "not-three-stars", "operator-closed", "active-battle", "insufficient-bp", "insufficient-item", "expired-ticket", "same-ticket-and-price", "invalid-reward"} {
		t.Run(reason, func(t *testing.T) {
			s, profiles := skipTestAccount(true)
			count := 2
			switch reason {
			case "zero":
				count = 0
			case "over-limit":
				count = 100
			case "not-three-stars":
				s.towerQuestProgress[7].ClearRatings[1] = 3
			case "operator-closed":
				s.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{}})
			case "active-battle":
				s.activeBattle = &TeamBattleContext{BossID: 456}
			case "insufficient-bp":
				s.towerQuestProfiles[7].Floors[0].ItemID = 0
				s.bp, s.bpMax = 100, 100
			case "insufficient-item":
				s.items[11] = gamestate.Item{ItemID: 11, Num: 5}
			case "expired-ticket":
				s.items[10] = gamestate.Item{ItemID: 10, Num: 10, LimitTime: int(time.Now().Unix() - 1)}
			case "same-ticket-and-price":
				s.towerQuestProfiles[7].Floors[0].ItemID = 10
				s.items[10] = gamestate.Item{ItemID: 10, Num: 7} // Needs (1+3)*2, not max(2,6).
			case "invalid-reward":
				profiles[0].ResultRewards[1].RewardTypeID = 999
			}
			before, _ := json.Marshal(s.Snapshot(gamestate.State{}))
			if _, err := s.SkipTeamBattle(123, count, profiles, []int8{0}); err == nil {
				t.Fatal("invalid skip request accepted")
			}
			after, _ := json.Marshal(s.Snapshot(gamestate.State{}))
			if string(before) != string(after) {
				t.Fatal("rejected batch partially changed the account")
			}
		})
	}
}
