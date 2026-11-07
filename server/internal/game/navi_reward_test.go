package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestNavigatorRewardValidatesIdentityBeforeNarrowingAndUsesUnlockQuantity(t *testing.T) {
	s := &Account{naviCatalogIDs: map[int8]struct{}{0: {}, 1: {}, 70: {}, 127: {}}, selectableNaviIDs: map[int8]struct{}{0: {}}}
	for _, reward := range []gamestate.Reward{
		{Type: 11, RewardTypeID: -1, Num: 0},
		{Type: 11, RewardTypeID: 128, Num: 1},
		{Type: 11, RewardTypeID: 256, Num: 0},
		{Type: 11, RewardTypeID: 2, Num: 0},
		{Type: 11, RewardTypeID: 1, Num: 2},
		{Type: 11, RewardTypeID: 1, Num: -1},
		{Type: 4, Num: 0},
	} {
		if s.validateRewardLocked(reward) == nil {
			t.Fatalf("invalid reward accepted: %+v", reward)
		}
	}
	for _, reward := range []gamestate.Reward{{Type: 11, RewardTypeID: 1, Num: 0}, {Type: 11, RewardTypeID: 70, Num: 1}} {
		for repeat := 0; repeat < 2; repeat++ {
			result := PresentReceiveResult{}
			if err := s.validateRewardBatchCapacityLocked([]gamestate.Reward{reward}); err != nil {
				t.Fatal(err)
			}
			if err := s.applyRewardLocked(reward, &result); err != nil || len(result.Rewards) != 1 || result.Rewards[0].Reward.Num != reward.Num || len(result.Rewards[0].UniqueID) != 0 {
				t.Fatal("unlock changed the reward quantity or allocated inventory instances", result, err)
			}
		}
	}
	mask, ids := s.NaviUnlockState()
	if mask != 2 || len(ids) != 3 || !s.SelectNavi(70) || s.SelectNavi(6) {
		t.Fatal("unlock duplicated ownership or wrapped the legacy mask", mask, ids)
	}
}
