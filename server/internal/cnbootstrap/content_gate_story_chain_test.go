package cnbootstrap

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

func TestCNMainStoryGateUsesIndependentProgressionChains(t *testing.T) {
	part := func(id int, flags ...int) gamestate.StoryMainPart {
		stories := make([]gamestate.StoryMain, len(flags))
		for i, flag := range flags {
			stories[i] = gamestate.StoryMain{StoryMainID: id*10000 + i + 1, Title: "story", StateFlag: flag}
		}
		return gamestate.StoryMainPart{StoryMainPartID: id, Name: "part", Sections: []gamestate.StoryMainSection{{StoryMainSectionID: id, SectionTitle: "section", Stories: stories}}}
	}
	parts := []gamestate.StoryMainPart{part(1, 8, 4), part(2, 4), part(3, 4|8, 4)}
	masterdata.NormalizeStoryProgress(parts)
	if count, err := validateCNMainStoryParts(parts); err != nil || count != 5 {
		t.Fatalf("independent normalized chains: count=%d err=%v", count, err)
	}
	parts[1].Sections[0].Stories[0].StateFlag = 1
	if _, err := validateCNMainStoryParts(parts); err == nil {
		t.Fatal("part2 must not open a second boundary in the original part1/2 chain")
	}
	parts[1].Sections[0].Stories[0].StateFlag = 4
	parts[2].Sections[0].Stories[1].StateFlag = 1
	if _, err := validateCNMainStoryParts(parts); err == nil {
		t.Fatal("Alchemy must not have two playable boundaries within its own chain")
	}
}
