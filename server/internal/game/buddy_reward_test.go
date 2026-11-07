package game

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
)

func TestBuddyGiftLevelExperienceAndRetry(t *testing.T) {
	for _, tc := range []struct {
		level                       int16
		wantLevel, experience, next int
	}{
		{0, 1, 0, 10}, {2, 2, 10, 20}, {3, 3, 30, 0},
	} {
		s := &Account{buddyMax: 10, nextBuddyUniqueID: 1,
			buddyDefinitions: map[int]gamestate.BuddyDefinition{7: {BuddyID: 7, MaxLevel: 3, ExperienceTableID: 2}},
			buddyExperience:  map[int][]int{2: {10, 20}},
			presents:         []gamestate.Present{{PresentID: 1, Reward: gamestate.Reward{Type: 19, RewardTypeID: 7, Num: 2, CardLevel: tc.level}}},
		}
		result, err := s.ReceivePresent(1)
		if err != nil || len(result.Buddies) != 2 {
			t.Fatalf("level %d: %+v (%v)", tc.level, result, err)
		}
		for _, buddy := range result.Buddies {
			if buddy.Level != tc.wantLevel || buddy.Experience != tc.experience || buddy.NowLevelExperience != 0 || buddy.NextLevelExperience != tc.next {
				t.Fatalf("inconsistent progression: %+v", buddy)
			}
		}
		if _, err := s.ReceivePresent(1); err != nil || len(s.buddies) != 2 || s.nextBuddyUniqueID != 3 {
			t.Fatalf("duplicate gift changed inventory: %+v (%v)", s.buddies, err)
		}
		s.presents = append(s.presents, gamestate.Present{PresentID: 2, Reward: gamestate.Reward{Type: 19, RewardTypeID: 7, Num: 1, CardLevel: 4}})
		if _, err := s.ReceivePresent(2); err == nil || len(s.buddies) != 2 || s.nextBuddyUniqueID != 3 || s.presents[1].State != 0 {
			t.Fatal("invalid level consumed or changed the gift")
		}
	}
}
