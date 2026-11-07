package game

import "kairisei.local/server/internal/gamestate"

type collectionCard struct {
	CardID int `json:"cardid"`
	State  int `json:"state_bit"`
}

type collectionPage struct {
	Cards [10]collectionCard `json:"cards"`
}

func (s *Account) cardCollectionCountLocked() int {
	count := 0
	for _, page := range s.cardCollectionPages {
		for _, id := range page {
			if _, found := s.cardCollectionIDs[id]; id != 0 && found {
				count++
			}
		}
	}
	return count
}

func (s *Account) recordCollectedCardLocked(card CardInfo) {
	if s.cardCollectionIDs == nil {
		s.cardCollectionIDs = make(map[int]struct{})
	}
	if _, exists := s.cardCollectionIDs[card.CardID]; !exists {
		s.markMissionMetricLocked(gamestate.MissionCollectionCount)
	}
	s.cardCollectionIDs[card.CardID] = struct{}{}
	s.markMissionMetricLocked(gamestate.MissionCardOwned)
	s.markMissionMetricLocked(gamestate.MissionCardFame)
	if card.LoveMax > 0 && card.Love >= card.LoveMax {
		if s.cardCollectionLoveMaxIDs == nil {
			s.cardCollectionLoveMaxIDs = make(map[int]struct{})
		}
		s.cardCollectionLoveMaxIDs[card.CardID] = struct{}{}
	}
}

func (s *Account) CardCollection() ([]collectionPage, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	pages := make([]collectionPage, len(s.cardCollectionPages))
	count, total := 0, 0
	for i, page := range s.cardCollectionPages {
		for j, id := range page {
			if id == 0 {
				continue
			}
			card := collectionCard{CardID: id}
			total++
			if _, acquired := s.cardCollectionIDs[id]; acquired {
				card.State = 1
				count++
			}
			if _, maxLove := s.cardCollectionLoveMaxIDs[id]; maxLove {
				card.State |= 2
			}
			pages[i].Cards[j] = card
		}
	}
	return pages, count, total
}
