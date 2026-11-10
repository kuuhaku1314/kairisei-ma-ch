package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestItemExchangePreservesEmptyRewardSkills(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(),
		items: map[int]gamestate.Item{10: {ItemID: 10, Num: 3}},
		itemExchangeProfiles: map[int]gamestate.ItemExchangeProfile{10: {
			ItemID: 10, NeedNum: 1,
			Reward: gamestate.Reward{Type: 4, Num: 5, CardSkillLevels: []int16{}},
		}},
	}
	result, err := s.ExchangeItem(10, 2)
	if err != nil {
		t.Fatalf("valid currency exchange rejected: %v", err)
	}
	if s.items[10].Num != 1 || s.gold != 10 || len(result.Reward.Rewards) != 1 ||
		result.Reward.Rewards[0].Reward.CardSkillLevels == nil {
		t.Fatalf("exchange inventory, balance or reward shape is incorrect: %+v", result)
	}
	if _, err := s.ExchangeItem(10, 2); err == nil || s.items[10].Num != 1 || s.gold != 10 {
		t.Fatal("insufficient inventory changed exchange balances")
	}
}
