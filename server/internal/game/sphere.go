package game

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

const (
	sphereFusionInputSphere     = 0
	sphereFusionInputCardStack  = 1
	sphereFusionMaterialType    = 7
	sphereEvolutionMaterialType = 8
)

type SphereFusionInput struct {
	InputType int8  `json:"input_type"`
	ID        int64 `json:"id"`
	Num       int   `json:"num"`
}

func cloneSphereExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneSphereEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func (s *Account) SphereCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.spheres)
}

func (s *Account) SphereState() []gamestate.Sphere {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]gamestate.Sphere(nil), s.spheres...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].UniqueID < result[right].UniqueID
	})
	return result
}

func (s *Account) sphereIndexByUniqueIDLocked(uniqueID int64) int {
	for index, sphere := range s.spheres {
		if sphere.UniqueID == uniqueID {
			return index
		}
	}
	return -1
}

func (s *Account) normalizeSphereLocked(sphere gamestate.Sphere, definition gamestate.SphereDefinition) (gamestate.Sphere, error) {
	values, exists := s.sphereExperience[definition.ExperienceTableID]
	if !exists && definition.MaxLevel > 1 {
		return gamestate.Sphere{}, errors.New("sphere experience table is unavailable")
	}
	progression, err := gamestate.NormalizeSphereExperience(definition.MaxLevel, sphere.Experience, values)
	if err != nil || progression.Level != sphere.Level {
		return gamestate.Sphere{}, errors.New("sphere level or experience is invalid")
	}
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := gamestate.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		s.sphereProgression.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return gamestate.Sphere{}, err
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func (s *Account) addSphereExperienceLocked(sphere gamestate.Sphere, definition gamestate.SphereDefinition, addition int) (gamestate.Sphere, error) {
	if addition <= 0 || sphere.Level >= definition.MaxLevel {
		return gamestate.Sphere{}, errors.New("sphere cannot gain experience")
	}
	if sphere.Experience > math.MaxInt-addition {
		return gamestate.Sphere{}, errors.New("sphere experience overflow")
	}
	progression, err := gamestate.NormalizeSphereExperience(
		definition.MaxLevel,
		sphere.Experience+addition,
		s.sphereExperience[definition.ExperienceTableID],
	)
	if err != nil {
		return gamestate.Sphere{}, err
	}
	sphere.Level = progression.Level
	sphere.Experience = progression.Experience
	sphere.NowLevelExperience = progression.NowLevelExperience
	sphere.NextLevelExperience = progression.NextLevelExperience
	addExperience, err := gamestate.SphereMaterialExperience(
		definition.MaterialAddExperience,
		sphere.Level,
		s.sphereProgression.MaterialLevelBonusPermillePerLevel,
	)
	if err != nil {
		return gamestate.Sphere{}, err
	}
	sphere.AddExperience = addExperience
	sphere.BaseAddPrice = definition.FusionBaseAddPrice
	return sphere, nil
}

func (s *Account) checkSphereConsumptionLocked(material gamestate.Sphere) error {
	if material.IsLock != 0 {
		return errSphereLocked
	}
	for _, deck := range s.decks {
		if slices.Contains(deck.SphereUniqueIDs, material.UniqueID) {
			return errSphereInDeck
		}
	}
	return nil
}

func (s *Account) FuseSphere(baseUniqueID int64, inputs []SphereFusionInput) (gamestate.Sphere, int, int, int, []DeckInfo, error) {
	if baseUniqueID <= 0 || len(inputs) == 0 {
		return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.sphereIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("unknown sphere fusion base")
	}
	base := s.spheres[baseIndex]
	definition := s.sphereDefinitions[base.SphereID]
	if base.Level >= definition.MaxLevel {
		return gamestate.Sphere{}, 0, 0, 0, nil, &BusinessError{-1, "圣剑已达到最高等级。"}
	}
	remove := make(map[int64]struct{}, len(inputs))
	stackUses := make([]gamestate.CardStackUse, 0, len(inputs))
	seenStackCards := make(map[int]struct{}, len(inputs))
	materialCount := 0
	addExperience := 0
	for _, input := range inputs {
		if input.ID <= 0 || input.Num <= 0 || materialCount > math.MaxInt-input.Num {
			return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion input")
		}
		materialCount += input.Num
		switch input.InputType {
		case sphereFusionInputSphere:
			if input.Num != 1 || input.ID == baseUniqueID {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion material")
			}
			index := s.sphereIndexByUniqueIDLocked(input.ID)
			if index < 0 {
				return gamestate.Sphere{}, 0, 0, 0, nil, errSphereUnavailable
			}
			if err := s.checkSphereConsumptionLocked(s.spheres[index]); err != nil {
				return gamestate.Sphere{}, 0, 0, 0, nil, err
			}
			if _, duplicate := remove[input.ID]; duplicate {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("duplicate sphere fusion material")
			}
			remove[input.ID] = struct{}{}
			if addExperience > math.MaxInt-s.spheres[index].AddExperience {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion experience overflow")
			}
			addExperience += s.spheres[index].AddExperience
		case sphereFusionInputCardStack:
			if input.ID > math.MaxInt {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion stack-card ID is invalid")
			}
			cardID := int(input.ID)
			if _, duplicate := seenStackCards[cardID]; duplicate {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("duplicate sphere fusion stack-card input")
			}
			seenStackCards[cardID] = struct{}{}
			owned := -1
			for index, stack := range s.stackCards {
				if stack.CardID == cardID {
					owned = index
					break
				}
			}
			if owned < 0 || s.stackCards[owned].Num < input.Num {
				return gamestate.Sphere{}, 0, 0, 0, nil, ErrInsufficientMaterials
			}
			if s.stackCards[owned].MaterialType != sphereFusionMaterialType || s.stackCards[owned].AddExperience <= 0 {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion stack-card material is unavailable")
			}
			addition := int64(s.stackCards[owned].AddExperience) * int64(input.Num)
			if addition > math.MaxInt || int64(addExperience)+addition > math.MaxInt {
				return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("sphere fusion experience overflow")
			}
			addExperience += int(addition)
			stackUses = append(stackUses, gamestate.CardStackUse{CardID: cardID, Num: input.Num})
		default:
			return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("unknown sphere fusion input type")
		}
	}
	cost := int64(base.BaseAddPrice) * int64(materialCount)
	if cost <= 0 || cost > math.MaxInt {
		return gamestate.Sphere{}, 0, 0, 0, nil, errors.New("invalid sphere fusion cost")
	}
	if cost > int64(s.gold) {
		return gamestate.Sphere{}, 0, 0, 0, nil, ErrInsufficientGold
	}
	success, err := rollCardFusionSuccess(s.cardProgression)
	if err != nil {
		return gamestate.Sphere{}, 0, 0, 0, nil, err
	}
	boostedExperience, err := boostedFusionExperience(addExperience, success)
	if err != nil {
		return gamestate.Sphere{}, 0, 0, 0, nil, err
	}
	updated, err := s.addSphereExperienceLocked(base, definition, boostedExperience)
	if err != nil {
		return gamestate.Sphere{}, 0, 0, 0, nil, err
	}
	s.spheres[baseIndex] = updated
	kept := make([]gamestate.Sphere, 0, len(s.spheres)-len(remove))
	for _, sphere := range s.spheres {
		if _, consumed := remove[sphere.UniqueID]; !consumed {
			kept = append(kept, sphere)
		}
	}
	s.spheres = kept
	s.consumeStackUsesLocked(stackUses)
	s.gold -= int(cost)
	return updated, success.SuccessType, s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *Account) EvolveSphere(baseUniqueID int64, materialUniqueID int64, materialCardID int) (gamestate.Sphere, int, int, []DeckInfo, error) {
	if baseUniqueID <= 0 || (materialUniqueID == 0) == (materialCardID == 0) {
		return gamestate.Sphere{}, 0, 0, nil, errors.New("sphere evolution requires exactly one material")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.sphereIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return gamestate.Sphere{}, 0, 0, nil, errors.New("unknown sphere evolution base")
	}
	base := s.spheres[baseIndex]
	definition := s.sphereDefinitions[base.SphereID]
	target, exists := s.sphereDefinitions[definition.EvolutionID]
	if !exists || definition.EvolutionID == 0 ||
		target.SameSphereID != definition.SameSphereID ||
		target.EvolutionCount != definition.EvolutionCount+1 ||
		target.ExperienceTableID != definition.ExperienceTableID ||
		target.MaxLevel < definition.MaxLevel {
		return gamestate.Sphere{}, 0, 0, nil, errors.New("sphere is not ready for evolution")
	}
	prices := s.sphereEvoPrices[target.Rarity]
	priceIndex := target.EvolutionCount - 1
	if priceIndex < 0 || priceIndex >= len(prices) {
		return gamestate.Sphere{}, 0, 0, nil, errors.New("invalid sphere evolution price index")
	}
	if prices[priceIndex] > s.gold {
		return gamestate.Sphere{}, 0, 0, nil, ErrInsufficientGold
	}
	remove := make(map[int64]struct{}, 1)
	stackIndex := -1
	if materialUniqueID != 0 {
		if materialUniqueID == baseUniqueID {
			return gamestate.Sphere{}, 0, 0, nil, errors.New("sphere evolution material equals the base")
		}
		materialIndex := s.sphereIndexByUniqueIDLocked(materialUniqueID)
		if materialIndex < 0 {
			return gamestate.Sphere{}, 0, 0, nil, errSphereUnavailable
		}
		if err := s.checkSphereConsumptionLocked(s.spheres[materialIndex]); err != nil {
			return gamestate.Sphere{}, 0, 0, nil, err
		}
		materialDefinition := s.sphereDefinitions[s.spheres[materialIndex].SphereID]
		if materialDefinition.SameSphereID != definition.SameSphereID {
			return gamestate.Sphere{}, 0, 0, nil, errors.New("sphere evolution material belongs to another family")
		}
		remove[materialUniqueID] = struct{}{}
	} else {
		for index, stack := range s.stackCards {
			if stack.CardID == materialCardID {
				stackIndex = index
				break
			}
		}
		if stackIndex < 0 || s.stackCards[stackIndex].Num < 1 {
			return gamestate.Sphere{}, 0, 0, nil, ErrInsufficientMaterials
		}
		if s.stackCards[stackIndex].MaterialType != sphereEvolutionMaterialType {
			return gamestate.Sphere{}, 0, 0, nil, errors.New("sphere evolution stack-card material is unavailable")
		}
	}
	result := base
	result.SphereID = target.SphereID
	result, err := s.normalizeSphereLocked(result, target)
	if err != nil {
		return gamestate.Sphere{}, 0, 0, nil, err
	}
	s.spheres[baseIndex] = result
	if stackIndex >= 0 {
		s.consumeStackCardLocked(stackIndex, 1)
	}
	if len(remove) != 0 {
		kept := make([]gamestate.Sphere, 0, len(s.spheres)-1)
		for _, sphere := range s.spheres {
			if _, consumed := remove[sphere.UniqueID]; !consumed {
				kept = append(kept, sphere)
			}
		}
		s.spheres = kept
	}
	s.gold -= prices[priceIndex]
	return result, s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *Account) SellSpheres(uniqueIDs []int64) (int, int, int, []DeckInfo, error) {
	if len(uniqueIDs) == 0 {
		return 0, 0, 0, nil, errors.New("sphere sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := make(map[int64]struct{}, len(uniqueIDs))
	getGold := int64(0)
	for _, uniqueID := range uniqueIDs {
		index := s.sphereIndexByUniqueIDLocked(uniqueID)
		if index < 0 {
			return 0, 0, 0, nil, errSphereUnavailable
		}
		if err := s.checkSphereConsumptionLocked(s.spheres[index]); err != nil {
			return 0, 0, 0, nil, err
		}
		if _, duplicate := remove[uniqueID]; duplicate {
			return 0, 0, 0, nil, errors.New("duplicate sphere sell selection")
		}
		remove[uniqueID] = struct{}{}
		definition := s.sphereDefinitions[s.spheres[index].SphereID]
		getGold += int64(definition.SellGold)
		if getGold > math.MaxInt || int64(s.gold)+getGold > math.MaxInt {
			return 0, 0, 0, nil, errors.New("sphere sell gold overflow")
		}
	}
	kept := make([]gamestate.Sphere, 0, len(s.spheres)-len(remove))
	for _, sphere := range s.spheres {
		if _, sold := remove[sphere.UniqueID]; !sold {
			kept = append(kept, sphere)
		}
	}
	s.spheres = kept
	s.gold += int(getGold)
	return int(getGold), s.gold, len(s.spheres), s.rankedDecksLocked(s.decks), nil
}

func (s *Account) SetSphereLock(uniqueID int64, locked bool) error {
	if uniqueID <= 0 {
		return errors.New("invalid sphere unique ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.sphereIndexByUniqueIDLocked(uniqueID)
	if index < 0 {
		return errors.New("unknown sphere unique ID")
	}
	if locked {
		s.spheres[index].IsLock = 1
	} else {
		s.spheres[index].IsLock = 0
	}
	return nil
}

func (s *Account) validateDeckSpheres(deck DeckInfo) error {
	if len(deck.SphereUniqueIDs) != DeckSphereSlots {
		return fmt.Errorf("sphr_uniqid must contain %d entries", DeckSphereSlots)
	}
	families := make(map[int]struct{}, DeckSphereSlots)
	for _, uniqueID := range deck.SphereUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		index := s.sphereIndexByUniqueIDLocked(uniqueID)
		if index < 0 {
			return fmt.Errorf("unknown sphere unique ID %d", uniqueID)
		}
		definition := s.sphereDefinitions[s.spheres[index].SphereID]
		if deck.ArthurType < 1 || deck.ArthurType > 4 || !definition.EquipAllowed[deck.ArthurType-1] {
			return fmt.Errorf("sphere %d cannot equip Arthur %d", uniqueID, deck.ArthurType)
		}
		if _, duplicate := families[definition.SameSphereID]; duplicate {
			return fmt.Errorf("sphere family %d is repeated in the deck", definition.SameSphereID)
		}
		families[definition.SameSphereID] = struct{}{}
	}
	return nil
}
