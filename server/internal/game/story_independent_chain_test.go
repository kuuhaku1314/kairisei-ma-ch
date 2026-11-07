package game

import (
	"kairisei.local/server/internal/gamestate"
	"testing"
)

func TestMainStoryCompletionStaysInItsProgressionChain(t *testing.T) {
	parts := []gamestate.StoryMainPart{
		{StoryMainPartID: 1, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 10001, StateFlag: 1}}}}},
		{StoryMainPartID: 2, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 20101, StateFlag: 1}}}}},
		{StoryMainPartID: 3, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 30101, StateFlag: 4}, {StoryMainID: 30102, StateFlag: 4}}}}},
	}
	s := &Account{storyMainParts: parts, storyRewardPolicy: gamestate.StoryRewardPolicy{MainFirstClear: gamestate.Reward{Type: 4, Num: 5}}}
	clear := func(id int) {
		if !s.BeginMainStory(id) {
			t.Fatal("story locked", id)
		}
		if _, err := s.EndMainStory(true); err != nil {
			t.Fatal(err)
		}
	}
	clear(20101)
	if parts[2].Sections[0].Stories[0].StateFlag != 4 || parts[2].Sections[0].Stories[1].StateFlag != 4 || parts[0].Sections[0].Stories[0].StateFlag != 1 {
		t.Fatal("ordinary part completion crossed into Alchemy")
	}
	parts[2].Sections[0].Stories[0].StateFlag = 1
	clear(30101)
	clear(30101)
	if parts[2].Sections[0].Stories[1].StateFlag != 1 || s.gold != 10 {
		t.Fatal("Alchemy next story or first-clear idempotency wrong")
	}
	clear(10001)
	if parts[1].Sections[0].Stories[0].StateFlag != 2 {
		t.Fatal("prior completion was reset")
	}
}
