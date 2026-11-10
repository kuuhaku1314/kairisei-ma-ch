package game

import (
	"kairisei.local/server/internal/gamestate"
)

func (s *Account) deckRankLocked(deck DeckInfo, inventory map[int64]int) int8 {
	policy := s.deckRankPolicy
	if len(policy.ParameterThresholds) == 0 {
		return deck.DeckRank
	}
	var job gamestate.JobParameter
	if int(deck.JobType) >= 0 && int(deck.JobType) < len(s.currentJobs) {
		job = s.currentJobs[deck.JobType]
	}
	parameters := [4]int{job.HP, job.Attack, job.Magic, job.Mind}
	skillPoints := 0
	for _, id := range deck.CardUniqueIDs {
		index, present := inventory[id]
		if !present {
			continue
		}
		card := s.cards[index]
		rule, found := policy.Cards[card.CardID]
		if !found {
			continue // Catalog completeness is checked before enabling this policy.
		}
		for i, value := range rule.MaximumParameters {
			parameters[i] += value
		}
		arthur := int(deck.ArthurType)
		if arthur >= 0 && arthur < len(rule.SkillPoints) && rule.SkillPoints[arthur] >= 0 {
			skillPoints += rule.SkillPoints[arthur] + card.Level
		}
	}
	parameterScore := parameters[1] + parameters[2] + parameters[3] + parameters[0]/6
	return min(rankAtThreshold(parameterScore, policy.ParameterThresholds), rankAtThreshold(skillPoints, policy.SkillThresholds))
}

func rankAtThreshold(points int, thresholds []int) int8 {
	rank := int8(0)
	for i, threshold := range thresholds {
		if points < threshold {
			break
		}
		rank = int8(i)
	}
	return rank
}

func (s *Account) rankedDecksLocked(decks []DeckInfo) []DeckInfo {
	result := CloneDecks(decks)
	inventory := s.rankInventoryLocked()
	for i := range result {
		result[i].DeckRank = s.deckRankLocked(result[i], inventory)
	}
	return result
}

func (s *Account) rankInventoryLocked() map[int64]int {
	inventory := make(map[int64]int, len(s.cards))
	for i, card := range s.cards {
		inventory[card.UniqueID] = i
	}
	return inventory
}

func (s *Account) refreshDeckRanksLocked() {
	if len(s.deckRankPolicy.ParameterThresholds) == 0 {
		return
	}
	inventory := s.rankInventoryLocked()
	for i := range s.decks {
		s.decks[i].DeckRank = s.deckRankLocked(s.decks[i], inventory)
		s.highestDeckRank = max(s.highestDeckRank, int(s.decks[i].DeckRank))
	}
	if s.highestDeckRank >= 10 {
		for feature := uint(0); feature < 4; feature++ {
			s.unlockedFeatureIDs[feature] = struct{}{}
		}
	}
}

func (s *Account) MaximumDeckRank() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.highestDeckRank
}

// GameMgr.setMaxRank decodes current rank from the low 16 bits and the last
// Home rank from the high 16 bits, then shows the original unlock notification.
func (s *Account) HomeDeckRank() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshDeckRanksLocked()
	if len(s.deckRankPolicy.ParameterThresholds) == 0 {
		return s.highestDeckRank
	}
	value := s.highestDeckRank | (s.lastHomeDeckRank << 16)
	s.lastHomeDeckRank = s.highestDeckRank
	return value
}

// DeckSl.getNeedRank takes the one-based display slot. Runtime Index is zero-based.
func requiredDeckRank(index int8) int {
	thresholds := [...]int{0, 8, 10, 11, 12, 13, 13, 14, 15, 16, 16, 16, 16, 16, 16}
	if index < 0 || int(index) >= len(thresholds) {
		return 18
	}
	return thresholds[index]
}
