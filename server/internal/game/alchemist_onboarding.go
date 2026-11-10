package game

import (
	"fmt"
	"time"

	"kairisei.local/server/internal/gamestate"
)

const alchemistFeatureID uint = 37

// Home is the common post-login/post-tutorial entry. Feature 37 commits the
// starter cards and deck once; later deletion of cards never regrants them.
func (s *Account) InitializeAlchemist() (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.alchemistStarter == nil || s.onboarding.Step != cnOnboardingStepCount {
		return false, nil
	}
	if _, done := s.unlockedFeatureIDs[alchemistFeatureID]; done {
		return false, nil
	}
	ids := append(s.alchemistStarter.SummonCardIDs[:], s.alchemistStarter.SkillCardIDs[:]...)
	rewards := make([]gamestate.Reward, len(ids))
	for index, id := range ids {
		rewards[index] = gamestate.Reward{Type: 6, Num: 1, RewardTypeID: id, CardLevel: 1, CardFame: 1, CardSkillLevels: []int16{1}}
	}
	cards := make([]CardInfo, 0, len(ids))
	createdAt := int(time.Now().Unix())
	for _, reward := range rewards {
		prepared, err := s.prepareRewardCardsLocked(reward, s.nextUniqueID+int64(len(cards)), createdAt)
		if err != nil {
			return false, fmt.Errorf("prepare alchemist starter: %w", err)
		}
		cards = append(cards, prepared...)
	}
	initial := DeckInfo{ArthurType: 5, JobType: 5, Index: 0, IsActive: 1, Name: "炼金初始卡组", CardUniqueIDs: make([]int64, 10), SupportCardUniqueIDs: make([]int64, s.supportSlotCapacity), SphereUniqueIDs: make([]int64, DeckSphereSlots), BuddyUniqueIDs: make([]int64, s.buddySlots)}
	for index, card := range cards {
		initial.CardUniqueIDs[index] = card.UniqueID
	}
	existing := -1
	for index, deck := range s.decks {
		if deck.ArthurType == 5 && deck.IsActive != 0 {
			initial.IsActive = 0
		}
		if deck.ArthurType == 5 && deck.Index == 0 {
			existing = index
		}
	}
	if existing < 0 {
		s.decks = append(s.decks, initial)
	} else {
		empty := true
		for _, id := range s.decks[existing].CardUniqueIDs {
			empty = empty && id == 0
		}
		if empty {
			initial.IsActive = s.decks[existing].IsActive
			s.decks[existing] = initial
		}
	}
	s.cards = append(s.cards, cards...)
	s.nextUniqueID += int64(len(cards))
	for _, card := range cards {
		s.recordCollectedCardLocked(card)
	}
	s.unlockedFeatureIDs[alchemistFeatureID] = struct{}{}
	s.unlockedFeatureIDs[32] = struct{}{} // The tower entry is separate from publication of any floor.
	return true, nil
}
