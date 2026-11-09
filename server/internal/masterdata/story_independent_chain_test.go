package masterdata

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestAlchemyStoryStartsIndependentlyWithoutOpeningPartTwo(t *testing.T) {
	parts := []gamestate.StoryMainPart{
		{StoryMainPartID: 1, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 10001, StateFlag: 8}}}}},
		{StoryMainPartID: 2, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 20101, StateFlag: 4}}}}},
		{StoryMainPartID: 3, Sections: []gamestate.StoryMainSection{{Stories: []gamestate.StoryMain{{StoryMainID: 30101, StateFlag: 4 | 8}, {StoryMainID: 30102, StateFlag: 4}}}}},
	}
	NormalizeStoryProgress(parts)
	if parts[0].Sections[0].Stories[0].StateFlag != 9 || parts[1].Sections[0].Stories[0].StateFlag != 4 || parts[2].Sections[0].Stories[0].StateFlag != 9 || parts[2].Sections[0].Stories[1].StateFlag != 4 {
		t.Fatal("independent entry or subsequent progression wrong", parts)
	}
	parts[0].Sections[0].Stories[0].StateFlag = 10
	parts[2].Sections[0].Stories[0].StateFlag = 10
	NormalizeStoryProgress(parts)
	if parts[1].Sections[0].Stories[0].StateFlag != 1 || parts[2].Sections[0].Stories[0].StateFlag != 10 || parts[2].Sections[0].Stories[1].StateFlag != 1 {
		t.Fatal("saved clears or original part1/2 sequence lost", parts)
	}
}
