package game

import (
	"errors"
	"reflect"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCollectionRewardExchangeAndDuplicateGift(t *testing.T) {
	for _, kind := range []int{14, 16, 18} {
		s := &Account{costumeIDs: map[int]struct{}{}, stampIDs: map[int]struct{}{}, honorIDs: map[int]struct{}{},
			collectionRewardIDs: map[[2]int]struct{}{{kind, 2}: {}}, items: map[int]gamestate.Item{4000: {ItemID: 4000, Num: 100}},
			tradeShopPurchases: map[int]int{}, tradeShopProfiles: map[int]gamestate.TradeShopProfile{1: {TradeShopID: 1, Lineups: []gamestate.TradeShopLineupProfile{{LineupID: 2, LineupName: "解锁", StockNum: 1, Prices: []gamestate.TradeShopPointProfile{{Type: 4, ID: 4000, Num: 25}}, Rewards: []gamestate.Reward{{Type: kind, RewardTypeID: 2, Num: 1}}}}}}}
		if _, err := s.BuyTradeShop(2, 1, nil); err != nil {
			t.Fatal(err)
		}
		reward := gamestate.Reward{Type: kind, RewardTypeID: 2, Num: 1}
		if !s.ownsCollectionRewardLocked(reward) || s.items[4000].Num != 75 || s.TradeShopState()[0].Lineups[0].StockRemain != 0 {
			t.Fatal("collection exchange not applied")
		}
		if _, err := s.BuyTradeShop(2, 1, nil); err == nil {
			t.Fatal("duplicate exchange accepted")
		} else {
			var business *BusinessError
			if !errors.As(err, &business) {
				t.Fatal(err)
			}
		}
		gift := PresentReceiveResult{}
		if err := s.applyRewardLocked(reward, &gift); err != nil || s.items[4000].Num != 75 || len(gift.StampIDs) != 0 {
			t.Fatal("duplicate gift charged or duplicated unlock")
		}
		reward.Num = 2
		if s.validateRewardLocked(reward) == nil {
			t.Fatal("unlock quantity must be one")
		}
	}
}

func TestConfiguredFamePoolKeepsTriggerAndFrozenRewards(t *testing.T) {
	policy := gamestate.TeamBattleFameBonusPolicy{ChanceMaximum: 100, FullFameThreshold: 100, FullFameRewardCount: 2, EligibleRewardTypes: []int{6, 13, 8}}
	s := &Account{teamBattleFameBonus: policy}
	profile := gamestate.TeamBattleRewardProfile{ResultRewards: []gamestate.Reward{{Type: 13, RewardTypeID: 1, Num: 1}}, FameRewards: []gamestate.Reward{{Type: 8, RewardTypeID: 4000, Num: 600}}}
	context := TeamBattleContext{FameSeed: "multi:room=123", FameSources: []TeamBattleFameSource{{MemberType: 4, LeaderFame: 100}, {MemberType: 2, LeaderFame: 0}}, FameRewardsSet: true, FameRewards: TeamBattleFamePool(profile, policy)}
	before, err := s.planTeamBattleFameAwardsLocked(profile, context)
	if err != nil || len(before) != 2 || before[0].MemberType != 4 || before[1].MemberType != 4 || before[0].Reward.Num != 600 || before[1].RewardKind != 1 {
		t.Fatalf("fame: %+v %v", before, err)
	}
	profile.FameRewards = []gamestate.Reward{}
	after, err := s.planTeamBattleFameAwardsLocked(profile, context)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("live edit changed a frozen fame pool")
	}
	context.FameRewards = TeamBattleFamePool(profile, policy)
	if disabled, err := s.planTeamBattleFameAwardsLocked(profile, context); err != nil || len(disabled) != 0 {
		t.Fatal("explicit empty pool did not disable rewards")
	}
	profile.FameRewards = nil
	if pool := TeamBattleFamePool(profile, policy); len(pool) != 1 || pool[0].RewardTypeID != 1 {
		t.Fatal("default fame reward changed")
	}
}

func (s *Account) planTeamBattleFameAwardsLocked(
	profile gamestate.TeamBattleRewardProfile,
	context TeamBattleContext,
) ([]teamBattleFameAwardPlan, error) {
	return planTeamBattleFameAwards(profile, context, s.teamBattleFameBonus)
}
