package game

import (
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

// Prepare the same inventory instances for ordinary rewards and starter decks.
// No account field changes until the whole caller-owned batch is valid.
func (s *Account) prepareRewardCardsLocked(reward gamestate.Reward, firstID int64, createdAt int) ([]CardInfo, error) {
	template, exists := s.cardTemplates[reward.RewardTypeID]
	if !exists {
		return nil, fmt.Errorf("card template %d is unavailable", reward.RewardTypeID)
	}
	definition, exists := s.cardDefinitions[reward.RewardTypeID]
	if !exists {
		return nil, fmt.Errorf("card definition %d is unavailable", reward.RewardTypeID)
	}
	level := int(reward.CardLevel)
	if level == 0 {
		level = 1
	}
	if definition.RarityPlus == 4 {
		if s.combatDefinitions == nil {
			return nil, fmt.Errorf("alchemist card %d combat definitions are unavailable", reward.RewardTypeID)
		}
		combat, exists := s.combatDefinitions.Cards[reward.RewardTypeID]
		if !exists {
			return nil, fmt.Errorf("alchemist card %d combat definition is unavailable", reward.RewardTypeID)
		}
		if combat.SummonID == 0 {
			level = template.LevelMax
		} else if _, exists := s.combatDefinitions.Summons[combat.SummonID]; !exists {
			return nil, fmt.Errorf("alchemist card %d summon definition %d is unavailable", reward.RewardTypeID, combat.SummonID)
		}
	}
	experience, err := gamestate.CardExperienceAtLevelStart(level, template.LevelMax, s.cardExperience[definition.ExperienceTableID])
	if err != nil {
		return nil, err
	}
	prepared := make([]CardInfo, 0, reward.Num)
	for count := 0; count < reward.Num; count++ {
		card := cloneCard(template)
		card.UniqueID = firstID + int64(count)
		card.CreateTime = createdAt
		card.Level = level
		card.Experience = experience
		card.Love = reward.CardLove
		card.Fame = int(reward.CardFame)
		if card.Fame == 0 {
			card.Fame = 1
		}
		if len(reward.CardSkillLevels) > 0 {
			card.SkillLevels = append([]int16(nil), reward.CardSkillLevels...)
		}
		card, err = s.normalizeCardLocked(card)
		if err != nil {
			return nil, err
		}
		prepared = append(prepared, card)
	}
	return prepared, nil
}
