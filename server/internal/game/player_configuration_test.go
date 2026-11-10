package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestPlayerPolicyKeepsClaimsAndMatchesNaviPrice(t *testing.T) {
	reward := func(n int) gamestate.Reward { return gamestate.Reward{Type: 10, Num: n, CardSkillLevels: []int16{}} }
	day := func(n int) []gamestate.LoginBonusDay {
		return []gamestate.LoginBonusDay{{Day: 1, Comment: "签到", Reward: reward(n)}}
	}
	config := PlayerConfiguration{Revision: 1, StoryCrystals: 75, LoginBonus: gamestate.LoginBonusPolicy{Cycle: day(10), Beginner: day(20), TotalMilestones: day(30)}, Navigators: []NaviSetting{{NaviID: 1, Enabled: true, Price: 77}}}
	s := &Account{onboarding: completedTestOnboarding(), currentName: "亚瑟", coinFree: 100, loginBonusState: gamestate.LoginBonusState{},
		naviCatalogIDs: map[int8]struct{}{0: {}, 1: {}}, selectableNaviIDs: map[int8]struct{}{0: {}}, naviPurchasePrice: 500,
		storyRewardPolicy: gamestate.StoryRewardPolicy{MainFirstClear: reward(50), SubFirstClear: reward(50), EventFirstClear: reward(50)},
		storyMainParts:    []gamestate.StoryMainPart{{Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 1, StateFlag: 1}}}}}},
	}
	s.ApplyPlayerConfiguration(config)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	if claims, err := s.ClaimLoginBonuses(now); err != nil || claims.Daily == nil || s.coinFree != 160 {
		t.Fatalf("configured login rewards: %v %d", err, s.coinFree)
	}
	config.Revision++
	config.LoginBonus.Cycle = day(40)
	s.ApplyPlayerConfiguration(config)
	if claims, err := s.ClaimLoginBonuses(now); err != nil || claims.Daily != nil || s.coinFree != 160 {
		t.Fatal("configuration reset today's claim")
	}
	if _, err := s.ClaimLoginBonuses(now.Add(24 * time.Hour)); err != nil || s.coinFree != 200 {
		t.Fatalf("next-day reward: %v %d", err, s.coinFree)
	}
	if s.NaviPrices()[1].Price != 77 {
		t.Fatal("navigator display price differs")
	}
	if err := s.PurchaseNavi(1); err != nil || s.coinFree != 123 {
		t.Fatalf("navi charge: %v %d", err, s.coinFree)
	}
	config.Revision++
	config.Navigators = []NaviSetting{{NaviID: 1, Enabled: false, Price: 77}}
	s.ApplyPlayerConfiguration(config)
	if !s.SelectNavi(1) {
		t.Fatal("disabling sale removed ownership")
	}
	delete(s.selectableNaviIDs, 1)
	if err := s.PurchaseNavi(1); err == nil || s.coinFree != 123 {
		t.Fatal("disabled navi was purchased")
	}
	config.Revision++
	config.Navigators[0].Enabled = true
	config.Navigators[0].Price = 500
	s.ApplyPlayerConfiguration(config)
	if err := s.PurchaseNavi(1); err != ErrInsufficientCrystals || s.coinFree != 123 {
		t.Fatal("insufficient balance changed")
	}
	for i := 0; i < 2; i++ {
		if !s.BeginMainStory(1) {
			t.Fatal("story unavailable")
		}
		if _, err := s.EndMainStory(true); err != nil {
			t.Fatal(err)
		}
	}
	if s.coinFree != 198 {
		t.Fatalf("story reward duplicated or wrong: %d", s.coinFree)
	}
}
