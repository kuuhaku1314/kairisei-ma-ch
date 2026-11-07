package masterdata

import (
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

func validateDeckRankPolicy(policy gamestate.DeckRankPolicy, cards []gamestate.Card) error {
	if policy.ConfigVersion == 0 && len(policy.Cards) == 0 {
		return nil // Isolated profiles without a rank master do not enable rank progression.
	}
	if policy.ConfigVersion != 1 {
		return errors.New("invalid CN deck rank policy version")
	}
	for _, thresholds := range [][]int{policy.ParameterThresholds, policy.SkillThresholds} {
		if len(thresholds) != 18 || thresholds[0] != 0 {
			return errors.New("CN deck rank thresholds must cover F through SSSS")
		}
		for i := 1; i < len(thresholds); i++ {
			if thresholds[i] <= thresholds[i-1] {
				return errors.New("CN deck rank thresholds must increase")
			}
		}
	}
	for id, rule := range policy.Cards {
		if id <= 0 {
			return errors.New("invalid CN rank card identity")
		}
		if rule.ArthurType < 0 || rule.ArthurType > 5 {
			return fmt.Errorf("invalid CN card profession for card %d", id)
		}
		for _, value := range rule.MaximumParameters {
			if value < 0 {
				return fmt.Errorf("negative CN rank parameter for card %d", id)
			}
		}
		for _, value := range rule.SkillPoints {
			if value < -1 || value > 600 {
				return fmt.Errorf("invalid CN rank skill points for card %d", id)
			}
		}
	}
	for _, card := range cards {
		if _, exists := policy.Cards[card.CardID]; !exists {
			return fmt.Errorf("CN deck rank policy is missing card %d", card.CardID)
		}
	}
	return nil
}
