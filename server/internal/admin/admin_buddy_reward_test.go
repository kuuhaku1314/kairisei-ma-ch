package admin

import "testing"

func TestAdminBuddyRewardLevel(t *testing.T) {
	entry := AdminCatalogEntry{Kind: "buddy", RewardType: 19, RewardTypeID: 7, LevelMax: 60}
	for _, tc := range []struct {
		name              string
		level, fame, love int
		valid             bool
	}{
		{"default", 0, 0, 0, true}, {"level one", 1, 0, 0, true}, {"maximum", 60, 0, 0, true},
		{"above form maximum", 61, 0, 0, false}, {"negative", -1, 0, 0, false},
		{"no fame", 60, 1, 0, false}, {"no love", 60, 0, 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			reward, err := adminCatalogReward(AdminMailRequest{RewardType: 19, RewardTypeID: 7, Quantity: 2, CardLevel: tc.level, CardFame: tc.fame, CardLove: tc.love}, entry)
			if (err == nil) != tc.valid {
				t.Fatalf("reward=%+v error=%v", reward, err)
			}
			if tc.valid && (int(reward.CardLevel) != tc.level || reward.CardFame != 0 || reward.CardLove != 0 || len(reward.CardSkillLevels) != 0) {
				t.Fatalf("unexpected progression: %+v", reward)
			}
		})
	}
}
