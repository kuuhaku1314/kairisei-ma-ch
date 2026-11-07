package game

import (
	"kairisei.local/server/internal/gamestate"
)

func (s *Account) QuestStoryID() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.storyTeamBattleSession.StoryID
}

func (s *Account) SetQuestStoryResponse(response []byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storyTeamBattleSession.Response = append([]byte(nil), response...)
}

func (s *Account) CardFameMaximumReached(cardID, fame int) bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	definition, exists := s.cardDefinitions[cardID]
	return exists && fame >= definition.FameMax
}

func (s *Account) FameTrainingPolicy() gamestate.CardDevelopmentPolicy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.cardDevelopmentPolicy
}

func (s *Account) ResolveCardAttributes(cards map[int64]CardInfo, attributes map[int]uint8) map[int]uint8 {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, card := range cards {
		if definition, ok := s.cardDefinitions[card.CardID]; ok {
			attributes[card.CardID] = definition.FusionAttributes
		}
	}
	return attributes
}

func (s *Account) RequiresInitialSave() bool { return s.initialStateRepair }
