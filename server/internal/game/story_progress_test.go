package game

import (
	"encoding/json"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCollectedCardUnlocksCharacterStoryWithoutReload(t *testing.T) {
	for _, listFirst := range []bool{false, true} {
		s := &Account{onboarding: completedTestOnboarding(), cardCollectionIDs: map[int]struct{}{}, storySubCharacters: []gamestate.StorySubCharacter{
			{Sections: []gamestate.StorySubSection{{StorySubSectionID: 101, Stories: []gamestate.StorySub{
				{StorySubID: 1, StateFlag: 4 | 8, UnlockText: "获得卡牌后解锁"},
				{StorySubID: 2, StateFlag: 4},
			}}}},
		}}
		if s.BeginSubStory(1) {
			t.Fatal("uncollected character was unlocked")
		}
		// Card acquisition records this durable discovery even if the player
		// sells or fuses the card before opening the story screen.
		s.cardCollectionIDs[101] = struct{}{}
		if listFirst {
			story := s.StorySubState()[0].Sections[0].Stories[0]
			if story.StateFlag != 1|8 || story.UnlockText != "" {
				t.Fatalf("newly collected character remains locked in list: %+v", story)
			}
		}
		if !s.BeginSubStory(1) || s.BeginSubStory(2) {
			t.Fatal("discovery did not unlock exactly the first unfinished story")
		}
		content, err := json.Marshal(s.StorySubState())
		if err != nil {
			t.Fatal(err)
		}
		reloaded := &Account{onboarding: completedTestOnboarding()}
		if err := json.Unmarshal(content, &reloaded.storySubCharacters); err != nil || !reloaded.BeginSubStory(1) {
			t.Fatalf("saved story unlock was lost after card disposal: %v", err)
		}
	}
}

func TestStoryReplayPreservesLaterClearAndFirstClearReward(t *testing.T) {
	for _, kind := range []string{"main", "cn main", "character", "event"} {
		t.Run(kind, func(t *testing.T) {
			reward := gamestate.Reward{Type: 4, Num: 5}
			s := &Account{onboarding: completedTestOnboarding(), storyRewardPolicy: gamestate.StoryRewardPolicy{
				MainFirstClear: reward, SubFirstClear: reward, EventFirstClear: reward,
			}}
			var begin func(int) bool
			var end func(bool) (PresentReceiveResult, error)
			var secondFlag func() int
			switch kind {
			case "main", "cn main":
				parts := []gamestate.StoryMainPart{{Sections: []gamestate.StoryMainSection{
					{Stories: []gamestate.StoryMain{{StoryMainID: 1, StateFlag: 1}}},
					{Stories: []gamestate.StoryMain{{StoryMainID: 2, StateFlag: 4 | 8}}},
				}}}
				if kind == "cn main" {
					s.cnStoryMainParts = parts
				} else {
					s.storyMainParts = parts
				}
				begin, end = s.BeginMainStory, s.EndMainStory
				secondFlag = func() int { return parts[0].Sections[1].Stories[0].StateFlag }
			case "character":
				s.storySubCharacters = []gamestate.StorySubCharacter{{Sections: []gamestate.StorySubSection{{Stories: []gamestate.StorySub{
					{StorySubID: 1, StateFlag: 1}, {StorySubID: 2, StateFlag: 4 | 8},
				}}}}}
				begin, end = s.BeginSubStory, s.EndSubStory
				secondFlag = func() int { return s.storySubCharacters[0].Sections[0].Stories[1].StateFlag }
			case "event":
				s.storyEvents = []gamestate.StoryEvent{{Stories: []gamestate.StorySubEvent{
					{Sub: gamestate.StorySub{StorySubID: 1, StateFlag: 1}},
					{Sub: gamestate.StorySub{StorySubID: 2, StateFlag: 4 | 8}},
				}}}
				begin, end = s.BeginSubStory, s.EndSubStory
				secondFlag = func() int { return s.storyEvents[0].Stories[1].Sub.StateFlag }
			}
			for _, id := range []int{1, 2, 1, 2} {
				if !begin(id) {
					t.Fatalf("story %d is unexpectedly locked", id)
				}
				if _, err := end(true); err != nil {
					t.Fatal(err)
				}
			}
			if s.gold != 10 || secondFlag() != 2|8 {
				t.Fatalf("replay changed clear progress/repeated reward: gold=%d second flag=%d", s.gold, secondFlag())
			}
		})
	}
}
