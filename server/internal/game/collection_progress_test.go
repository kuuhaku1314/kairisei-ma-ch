package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardRewardRemembersPreviouslyCollectedCard(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(),
		cardProgression:   gamestate.CardProgressionPolicy{FusionGoldPerMaterialPerBaseLevel: 1},
		cardCollectionIDs: map[int]struct{}{10: {}},
		cardTemplates:     map[int]CardInfo{10: {CardID: 10, LevelMax: 1}},
		cardDefinitions:   map[int]gamestate.Card{10: {CardID: 10, LevelMax: 1, ExperienceTableID: 1, FameMax: 100}},
		cardExperience:    map[int][]int{1: {}}, nextUniqueID: 1,
	}
	result := PresentReceiveResult{}
	_, owned := s.GachaStateWithOwnership()
	if _, ok := owned[10]; !ok {
		t.Fatal("previously collected card is missing from gacha lineup ownership")
	}
	if err := s.applyRewardLocked(GachaCardReward(10), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Rewards) != 1 || result.Rewards[0].IsNew != 0 {
		t.Fatalf("previously collected reward: %+v", result.Rewards)
	}
}
