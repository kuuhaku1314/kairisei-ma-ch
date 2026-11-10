package game

import (
	"testing"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func TestFameTrainingFinishesAtDeadlineWithoutRead(t *testing.T) {
	s := &Account{stive: 10, coinFree: 100,
		cards:                 []CardInfo{{UniqueID: 1, CardID: 1, Level: 1, LevelMax: 1, Fame: 1}},
		cardDefinitions:       map[int]gamestate.Card{1: {CardID: 1, LevelMax: 1, FameMax: 100}},
		cardDevelopmentRules:  map[int]cardDevelopmentRule{1: {DevelopmentType: 2, DevelopRadix: 1, FameMax: 100}},
		cardDevelopmentPolicy: gamestate.CardDevelopmentPolicy{TimeEveryFameSeconds: 60, CoinEveryHour: 1},
		cardProgression:       gamestate.CardProgressionPolicy{FusionGoldPerMaterialPerBaseLevel: 1},
	}
	start := time.Date(2026, 10, 23, 0, 0, 0, 0, time.UTC)
	if _, err := s.StartCardFameTraining(1, 1, start); err != nil {
		t.Fatal(err)
	}
	due := start.Add(time.Minute)
	if _, err := s.FinishCardFameTraining(1, due); err != nil {
		t.Fatal(err)
	}
	if s.cards[0].Fame != 2 || s.cards[0].IsLock != 0 || s.coinFree != 100 {
		t.Fatal("natural completion charged or lost fame")
	}
	if _, changed := s.CardFameInfo(due); changed {
		t.Fatal("query completed training twice")
	}
	if _, err := s.StartCardFameTraining(1, 1, due); err != nil {
		t.Fatal(err)
	}
	if _, err := s.StartCardFameTraining(1, 1, due.Add(time.Minute)); err != nil {
		t.Fatal("next training depends on prior page read", err)
	}
	if s.cards[0].Fame != 3 {
		t.Fatal("next training lost expired award")
	}
}
