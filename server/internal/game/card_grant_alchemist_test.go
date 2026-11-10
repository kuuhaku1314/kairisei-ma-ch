package game

import (
	"strings"
	"testing"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

func TestAlchemistAcquisitionUsesSummonDefinitionAndKeepsOrdinaryLevels(t *testing.T) {
	const skillID, summonID, ordinaryID = 101, 102, 103
	store := &Account{
		cardDefinitions: map[int]gamestate.Card{},
		cardTemplates:   map[int]CardInfo{},
		cardExperience:  map[int][]int{1: {5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5, 5}},
		cardProgression: gamestate.CardProgressionPolicy{FusionGoldPerMaterialPerBaseLevel: 1},
		combatDefinitions: &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{
			skillID: {ID: skillID}, summonID: {ID: summonID, SummonID: 17},
		}, Summons: map[int]battlemodel.CombatSummonDefinition{17: {ID: 17}}},
	}
	for _, id := range []int{skillID, summonID, ordinaryID} {
		rarityPlus := 0
		if id != ordinaryID {
			rarityPlus = 4
		}
		store.cardDefinitions[id] = gamestate.Card{CardID: id, RarityPlus: rarityPlus, LevelMax: 15, ExperienceTableID: 1, FameMax: 100, AddExperience: 10}
		store.cardTemplates[id] = CardInfo{CardID: id, LevelMax: 15, Fame: 1, AddExperience: 10, SkillLevels: []int16{1}}
	}
	grant := func(id int, level int16) CardInfo {
		t.Helper()
		cards, err := store.prepareRewardCardsLocked(gamestate.Reward{Type: 6, Num: 1, RewardTypeID: id, CardLevel: level, CardFame: 1}, 1, 123)
		if err != nil || len(cards) != 1 {
			t.Fatalf("grant card %d: %+v (%v)", id, cards, err)
		}
		return cards[0]
	}
	if card := grant(skillID, 1); card.Level != 15 || card.Experience != 70 || card.AddExperience != 10 {
		t.Fatalf("skill acquisition = %+v", card)
	}
	if card := grant(summonID, 0); card.Level != 1 || card.Experience != 0 {
		t.Fatalf("summon acquisition = %+v", card)
	}
	if card := grant(summonID, 3); card.Level != 3 || card.Experience != 10 {
		t.Fatalf("explicit summon level = %+v", card)
	}
	if card := grant(ordinaryID, 3); card.Level != 3 || card.Experience != 10 {
		t.Fatalf("ordinary acquisition = %+v", card)
	}
	delete(store.combatDefinitions.Summons, 17)
	if _, err := store.prepareRewardCardsLocked(gamestate.Reward{Type: 6, Num: 1, RewardTypeID: summonID}, 1, 123); err == nil || !strings.Contains(err.Error(), "summon definition") {
		t.Fatalf("missing summon source = %v", err)
	}
	delete(store.combatDefinitions.Cards, skillID)
	_, err := store.prepareRewardCardsLocked(gamestate.Reward{Type: 6, Num: 1, RewardTypeID: skillID}, 1, 123)
	if err == nil || !strings.Contains(err.Error(), "combat definition") {
		t.Fatalf("missing alchemist source = %v", err)
	}
}
