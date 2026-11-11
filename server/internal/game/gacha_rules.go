package game

import (
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

// Every display and play resolves the same account-owned step and collection.
func (s *Account) currentGachaLocked(profile gamestate.GachaProfile) gamestate.GachaProfile {
	profile = profile.CurrentStep()
	if !profile.UnownedOnly {
		return profile
	}
	ownedFamilies := make(map[int]bool)
	for id := range s.cardCollectionIDs {
		if definition, ok := s.cardDefinitions[id]; ok {
			ownedFamilies[definition.SameCardID] = true
		}
	}
	ids, weights := []int{}, []int{}
	for i, id := range profile.CardIDs {
		if !ownedFamilies[s.cardDefinitions[id].SameCardID] {
			ids, weights = append(ids, id), append(weights, profile.CardWeights[i])
		}
	}
	profile.CardIDs, profile.CardWeights = ids, weights
	return profile
}

func drawWeightedReward(pool []gamestate.WeightedReward, emptyWeight int) (gamestate.Reward, error) {
	indices, weights := make([]int, len(pool)), make([]int, len(pool))
	for i, entry := range pool {
		indices[i], weights[i] = i, entry.Weight
	}
	if emptyWeight > 0 {
		indices, weights = append(indices, len(pool)), append(weights, emptyWeight)
	}
	index, err := weightedGachaCard(indices, weights)
	if err != nil {
		return gamestate.Reward{}, err
	}
	if index == len(pool) {
		return gamestate.Reward{}, nil
	}
	return cloneReward(pool[index].Reward), nil
}

func GachaPoolRewards(profile gamestate.GachaProfile) []gamestate.Reward {
	if len(profile.RewardPool) > 0 {
		rewards := make([]gamestate.Reward, len(profile.RewardPool))
		for i, entry := range profile.RewardPool {
			rewards[i] = entry.Reward
		}
		return rewards
	}
	rewards := make([]gamestate.Reward, len(profile.CardIDs))
	for i, id := range profile.CardIDs {
		rewards[i] = GachaPoolCardReward(&profile, id)
	}
	return rewards
}

// Original GachaDefs maps sphere expectancy 8/9/10 to presentation grades
// 0/3/5; taking the numeric maximum would put a common sphere above a LEGEND
// buddy. The local policy selects the highest actual presentation grade.
func (s *Account) gachaMixedResultExpectancy(rewards []gamestate.Reward) int {
	expectancy := gachaResultExpectancy(rewards, s.cardDefinitions)
	grade := expectancy
	if grade == 6 { // Original EXRARE uses the MILLIONRARE opening.
		grade = 5
	}
	for _, reward := range rewards {
		if reward.Num <= 0 {
			continue
		}
		candidate, candidateGrade := 0, 0
		switch reward.Type {
		case 19:
			if _, ok := s.buddyDefinitions[reward.RewardTypeID]; ok {
				candidate, candidateGrade = 7, 7
			}
		case 15:
			definition := s.sphereDefinitions[reward.RewardTypeID]
			if definition.Type != "CHALICE" {
				continue
			}
			switch definition.Rarity {
			case "NORMAL":
				candidate, candidateGrade = 8, 0
			case "RARE":
				candidate, candidateGrade = 9, 3
			case "MILLIONRARE":
				candidate, candidateGrade = 10, 5
			}
		}
		if candidateGrade > grade || (candidateGrade == 0 && grade == 0 && candidate == 8) {
			expectancy, grade = candidate, candidateGrade
		}
	}
	return expectancy
}

func (s *Account) validateGachaRulesLocked(profile gamestate.GachaProfile) error {
	if err := gamestate.ValidateGachaRules(profile); err != nil {
		return err
	}
	pools := [][]gamestate.WeightedReward{profile.RewardPool}
	for _, step := range profile.Steps {
		pools = append(pools, step.RewardPool)
	}
	for _, gift := range profile.RandomGifts {
		pools = append(pools, gift.Pool)
	}
	for _, pool := range pools {
		for _, entry := range pool {
			if err := s.validateRewardLocked(entry.Reward); err != nil {
				return fmt.Errorf("gacha %d: %w", profile.GachaID, err)
			}
		}
	}
	for _, gift := range profile.Gifts {
		for _, reward := range gift.Rewards {
			if err := s.validateRewardLocked(reward); err != nil {
				return err
			}
		}
	}
	return nil
}
