package game

import (
	"crypto/rand"
	"errors"
	"math"
	"math/big"
	"reflect"

	"kairisei.local/server/internal/gamestate"
)

// JP 8.0.7 ExpFusion's total selection limit. Inventory instances, warehouse
// instances and stack types each keep the CN CardFusionMaterialLimit.
const cardFusionTotalMaterialLimit = 9999

func validateCardFusionSuccessPolicy(policy gamestate.CardProgressionPolicy) error {
	if len(policy.FusionSuccessTypes) != 3 {
		return errors.New("card fusion success policy is incomplete")
	}
	totalWeight := 0
	for index, result := range policy.FusionSuccessTypes {
		if result.SuccessType != index || result.Weight <= 0 || result.ExperiencePermille < 1000 ||
			(index == 0 && result.ExperiencePermille != 1000) || totalWeight > math.MaxInt-result.Weight {
			return errors.New("card fusion success policy is invalid")
		}
		totalWeight += result.Weight
	}
	return nil
}

func rollCardFusionSuccess(policy gamestate.CardProgressionPolicy) (gamestate.CardFusionSuccessPolicy, error) {
	if err := validateCardFusionSuccessPolicy(policy); err != nil {
		return gamestate.CardFusionSuccessPolicy{}, err
	}
	totalWeight := 0
	for _, result := range policy.FusionSuccessTypes {
		totalWeight += result.Weight
	}
	value, err := rand.Int(rand.Reader, big.NewInt(int64(totalWeight)))
	if err != nil {
		return gamestate.CardFusionSuccessPolicy{}, errors.New("generate card fusion success result")
	}
	draw := int(value.Int64())
	for _, result := range policy.FusionSuccessTypes {
		if draw < result.Weight {
			return result, nil
		}
		draw -= result.Weight
	}
	return gamestate.CardFusionSuccessPolicy{}, errors.New("card fusion success result is unavailable")
}

func boostedFusionExperience(experience int, result gamestate.CardFusionSuccessPolicy) (int, error) {
	if experience <= 0 || result.ExperiencePermille < 1000 ||
		int64(experience) > math.MaxInt64/int64(result.ExperiencePermille) {
		return 0, errors.New("card fusion experience inputs are invalid")
	}
	boosted := int64(experience) * int64(result.ExperiencePermille) / 1000
	if boosted <= 0 || boosted > math.MaxInt {
		return 0, errors.New("card fusion experience overflows")
	}
	return int(boosted), nil
}

func EqualCardInfo(left CardInfo, right CardInfo) bool {
	return reflect.DeepEqual(left, right)
}

// Check multiplication, aggregate and final balance BEFORE consuming cards.
func checkedCardSaleGold(balance, total, price, count int) (int, error) {
	if balance < 0 || total < 0 || price < 0 || count <= 0 || total > math.MaxInt-balance || price > (math.MaxInt-balance-total)/count {
		return 0, errors.New("card sale gold overflows")
	}
	return total + price*count, nil
}

func cloneCardExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func (s *Account) normalizeCardLocked(card CardInfo) (CardInfo, error) {
	definition, exists := s.cardDefinitions[card.CardID]
	if !exists {
		return CardInfo{}, errors.New("card definition is unavailable")
	}
	values := s.cardExperience[definition.ExperienceTableID]
	experienceState, err := gamestate.NormalizeCardExperience(definition.LevelMax, card.Experience, values)
	if err != nil {
		return CardInfo{}, err
	}
	parameters, err := s.cardProgression.ParametersAt(
		definition, experienceState.Level, card.Love, card.Fame,
	)
	if err != nil {
		return CardInfo{}, err
	}
	card.Level = experienceState.Level
	card.LevelMax = definition.LevelMax
	card.LoveMax = definition.LoveMax
	card.Experience = experienceState.Experience
	card.NowLevelEXP = experienceState.NowLevelExperience
	card.NextLevelEXP = experienceState.NextLevelExperience
	card.HP = parameters.HP
	card.Attack = parameters.Attack
	card.Magic = parameters.Magic
	card.Mind = parameters.Mind
	card.AddExperience = definition.AddExperience
	fusionGold, err := s.cardProgression.FusionGoldPerMaterial(experienceState.Level)
	if err != nil {
		return CardInfo{}, err
	}
	card.BaseAddPrice = fusionGold
	return card, nil
}
