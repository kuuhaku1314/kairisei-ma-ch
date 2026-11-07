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
	buddyFusionInputCardStack  = 1
	buddyFusionInputBuddy      = 2
	buddyFusionMaterialType    = 9
	buddyEvolutionMaterialType = 10
)

type BuddyFusionInput struct {
	InputType int8  `json:"input_type"`
	ID        int64 `json:"id"`
	Num       int   `json:"num"`
}

type buddyFusionResult struct {
	Buddy          gamestate.Buddy
	SuccessType    int
	Gold           int
	BuddyNum       int
	Decks          []DeckInfo
	BackUniqueIDs  []int64
	BackStackCards []gamestate.CardStack
}

type buddyEvolutionResult struct {
	Buddy    gamestate.Buddy
	Gold     int
	BuddyNum int
	Decks    []DeckInfo
	Reward   any
}

type buddyEvolutionMaterial struct {
	removeBuddies map[int64]struct{}
	stackIndex    int
}

func cloneBuddyExperienceTables(source map[int][]int) map[int][]int {
	result := make(map[int][]int, len(source))
	for tableID, values := range source {
		result[tableID] = append([]int(nil), values...)
	}
	return result
}

func cloneBuddyEvolutionPrices(source map[string][]int) map[string][]int {
	result := make(map[string][]int, len(source))
	for rarity, values := range source {
		result[rarity] = append([]int(nil), values...)
	}
	return result
}

func (s *Account) BuddyCount() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.buddies)
}

func (s *Account) BuddyState() []gamestate.Buddy {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := append([]gamestate.Buddy(nil), s.buddies...)
	sort.Slice(result, func(left, right int) bool {
		return result[left].UniqueID < result[right].UniqueID
	})
	return result
}

func (s *Account) buddyIndexByUniqueIDLocked(uniqueID int64) int {
	for index, buddy := range s.buddies {
		if buddy.UniqueID == uniqueID {
			return index
		}
	}
	return -1
}

func (s *Account) normalizeBuddyLocked(buddy gamestate.Buddy, definition gamestate.BuddyDefinition) (gamestate.Buddy, error) {
	if buddy.Level < 1 || buddy.Level > definition.MaxLevel || buddy.Experience < 0 {
		return gamestate.Buddy{}, errors.New("buddy level or experience is invalid")
	}
	values, exists := s.buddyExperience[definition.ExperienceTableID]
	if !exists || len(values) < definition.MaxLevel-1 {
		return gamestate.Buddy{}, errors.New("buddy experience table is unavailable")
	}
	spent := 0
	for level := 1; level < buddy.Level; level++ {
		if spent > math.MaxInt-values[level-1] {
			return gamestate.Buddy{}, errors.New("buddy experience table overflows")
		}
		spent += values[level-1]
	}
	nowExperience, nextExperience := 0, 0
	if buddy.Level < definition.MaxLevel {
		required := values[buddy.Level-1]
		nowExperience = buddy.Experience - spent
		if nowExperience < 0 || nowExperience >= required {
			return gamestate.Buddy{}, errors.New("buddy experience does not match its level")
		}
		nextExperience = required - nowExperience
	} else if buddy.Experience != spent {
		return gamestate.Buddy{}, errors.New("maximum-level buddy experience is invalid")
	}
	buddy.NowLevelExperience = nowExperience
	buddy.NextLevelExperience = nextExperience
	buddy.AddExperience = definition.MaterialAddExperience
	buddy.BaseAddPrice = definition.FusionBaseAddPrice
	return buddy, nil
}

func (s *Account) buddyMaximumExperienceLocked(definition gamestate.BuddyDefinition) (int, error) {
	return s.buddyExperienceAtLevelLocked(definition, definition.MaxLevel)
}

func (s *Account) buddyExperienceAtLevelLocked(definition gamestate.BuddyDefinition, level int) (int, error) {
	if level < 1 || level > definition.MaxLevel {
		return 0, errors.New("buddy level is invalid")
	}
	values, exists := s.buddyExperience[definition.ExperienceTableID]
	if !exists || len(values) < definition.MaxLevel-1 {
		return 0, errors.New("buddy experience table is unavailable")
	}
	total := 0
	for _, value := range values[:level-1] {
		if value <= 0 || total > math.MaxInt-value {
			return 0, errors.New("buddy experience table is invalid")
		}
		total += value
	}
	return total, nil
}

func (s *Account) addBuddyExperienceLocked(buddy gamestate.Buddy, definition gamestate.BuddyDefinition, addition int) (gamestate.Buddy, error) {
	if addition <= 0 || buddy.Level >= definition.MaxLevel || buddy.Experience > math.MaxInt-addition {
		return gamestate.Buddy{}, errors.New("buddy cannot gain experience")
	}
	maximum, err := s.buddyMaximumExperienceLocked(definition)
	if err != nil {
		return gamestate.Buddy{}, err
	}
	buddy.Experience += addition
	if buddy.Experience > maximum {
		buddy.Experience = maximum
	}
	values := s.buddyExperience[definition.ExperienceTableID]
	spent := 0
	buddy.Level = 1
	for buddy.Level < definition.MaxLevel && buddy.Experience >= spent+values[buddy.Level-1] {
		spent += values[buddy.Level-1]
		buddy.Level++
	}
	return s.normalizeBuddyLocked(buddy, definition)
}

func (s *Account) checkBuddyConsumptionLocked(material gamestate.Buddy) error {
	if material.IsLock != 0 {
		return ErrBuddyLocked
	}
	for _, deck := range s.decks {
		if slices.Contains(deck.BuddyUniqueIDs, material.UniqueID) {
			return errBuddyInDeck
		}
	}
	return nil
}

func (s *Account) FuseBuddy(baseUniqueID int64, inputs []BuddyFusionInput) (buddyFusionResult, error) {
	if baseUniqueID <= 0 || len(inputs) == 0 {
		return buddyFusionResult{}, errors.New("buddy fusion selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.buddyIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return buddyFusionResult{}, errors.New("unknown buddy fusion base")
	}
	base := s.buddies[baseIndex]
	definition := s.buddyDefinitions[base.BuddyID]
	maximum, err := s.buddyMaximumExperienceLocked(definition)
	if err != nil || base.Level >= definition.MaxLevel {
		return buddyFusionResult{}, &BusinessError{-1, "传承卡已达到最高等级。"}
	}
	type preparedInput struct {
		input         BuddyFusionInput
		addExperience int
		stackIndex    int
	}
	prepared := make([]preparedInput, 0, len(inputs))
	seenBuddies := make(map[int64]struct{}, len(inputs))
	seenStacks := make(map[int]struct{}, len(inputs))
	materialCount := 0
	for _, input := range inputs {
		if input.ID <= 0 || input.Num <= 0 || materialCount > s.buddyProgression.MaximumMaterialCount-input.Num {
			return buddyFusionResult{}, errors.New("invalid buddy fusion input")
		}
		materialCount += input.Num
		switch input.InputType {
		case buddyFusionInputBuddy:
			if input.Num != 1 || input.ID == baseUniqueID {
				return buddyFusionResult{}, errors.New("invalid buddy fusion material")
			}
			index := s.buddyIndexByUniqueIDLocked(input.ID)
			if index < 0 {
				return buddyFusionResult{}, ErrBuddyUnavailable
			}
			if err := s.checkBuddyConsumptionLocked(s.buddies[index]); err != nil {
				return buddyFusionResult{}, err
			}
			if _, duplicate := seenBuddies[input.ID]; duplicate {
				return buddyFusionResult{}, errors.New("duplicate buddy fusion material")
			}
			seenBuddies[input.ID] = struct{}{}
			prepared = append(prepared, preparedInput{input: input, addExperience: s.buddies[index].AddExperience, stackIndex: -1})
		case buddyFusionInputCardStack:
			if input.ID > math.MaxInt {
				return buddyFusionResult{}, errors.New("buddy fusion stack-card ID is invalid")
			}
			cardID := int(input.ID)
			if _, duplicate := seenStacks[cardID]; duplicate {
				return buddyFusionResult{}, errors.New("duplicate buddy fusion stack-card input")
			}
			seenStacks[cardID] = struct{}{}
			stackIndex := -1
			for index, stack := range s.stackCards {
				if stack.CardID == cardID {
					stackIndex = index
					break
				}
			}
			if stackIndex < 0 || s.stackCards[stackIndex].Num < input.Num {
				return buddyFusionResult{}, ErrInsufficientMaterials
			}
			if s.stackCards[stackIndex].MaterialType != buddyFusionMaterialType || s.stackCards[stackIndex].AddExperience <= 0 {
				return buddyFusionResult{}, errors.New("buddy fusion stack-card material is unavailable")
			}
			prepared = append(prepared, preparedInput{input: input, addExperience: s.stackCards[stackIndex].AddExperience, stackIndex: stackIndex})
		default:
			return buddyFusionResult{}, errors.New("unknown buddy fusion input type")
		}
	}
	success, err := rollCardFusionSuccess(s.cardProgression)
	if err != nil {
		return buddyFusionResult{}, err
	}
	remaining := maximum - base.Experience
	consumedBuddies := make(map[int64]struct{}, len(seenBuddies))
	stackUses := make([]gamestate.CardStackUse, 0, len(seenStacks))
	backUniqueIDs := make([]int64, 0)
	backStackCounts := make(map[int]int)
	rawExperience := 0
	consumedCount := 0
	for _, value := range prepared {
		if value.input.InputType == buddyFusionInputBuddy {
			if rawExperience > 0 {
				current, boostErr := boostedFusionExperience(rawExperience, success)
				if boostErr != nil {
					return buddyFusionResult{}, boostErr
				}
				if current >= remaining {
					backUniqueIDs = append(backUniqueIDs, value.input.ID)
					continue
				}
			}
			consumedBuddies[value.input.ID] = struct{}{}
			if value.addExperience <= 0 || rawExperience > math.MaxInt-value.addExperience {
				return buddyFusionResult{}, errors.New("buddy fusion experience overflow")
			}
			rawExperience += value.addExperience
			consumedCount++
			continue
		}
		consume := 0
		for consume < value.input.Num {
			if rawExperience > 0 {
				current, boostErr := boostedFusionExperience(rawExperience, success)
				if boostErr != nil {
					return buddyFusionResult{}, boostErr
				}
				if current >= remaining {
					break
				}
			}
			if value.addExperience <= 0 || rawExperience > math.MaxInt-value.addExperience {
				return buddyFusionResult{}, errors.New("buddy fusion experience overflow")
			}
			rawExperience += value.addExperience
			consume++
		}
		if consume > 0 {
			stackUses = append(stackUses, gamestate.CardStackUse{CardID: int(value.input.ID), Num: consume})
			consumedCount += consume
		}
		backStackCounts[int(value.input.ID)] += value.input.Num - consume
	}
	if consumedCount == 0 || rawExperience <= 0 {
		return buddyFusionResult{}, errors.New("buddy fusion has no consumable material")
	}
	cost := int64(base.BaseAddPrice) * int64(consumedCount)
	if cost <= 0 || cost > math.MaxInt {
		return buddyFusionResult{}, errors.New("invalid buddy fusion cost")
	}
	if cost > int64(s.gold) {
		return buddyFusionResult{}, ErrInsufficientGold
	}
	addExperience, err := boostedFusionExperience(rawExperience, success)
	if err != nil {
		return buddyFusionResult{}, err
	}
	updated, err := s.addBuddyExperienceLocked(base, definition, addExperience)
	if err != nil {
		return buddyFusionResult{}, err
	}
	s.buddies[baseIndex] = updated
	kept := make([]gamestate.Buddy, 0, len(s.buddies)-len(consumedBuddies))
	for _, buddy := range s.buddies {
		if _, consumed := consumedBuddies[buddy.UniqueID]; !consumed {
			kept = append(kept, buddy)
		}
	}
	s.buddies = kept
	s.consumeStackUsesLocked(stackUses)
	s.gold -= int(cost)
	backStackCards := make([]gamestate.CardStack, 0)
	for cardID, count := range backStackCounts {
		if count == 0 {
			continue
		}
		var current gamestate.CardStack
		for _, stack := range s.stackCards {
			if stack.CardID == cardID {
				current = stack
				break
			}
		}
		for index := 0; index < count; index++ {
			backStackCards = append(backStackCards, current)
		}
	}
	return buddyFusionResult{
		Buddy: updated, SuccessType: success.SuccessType, Gold: s.gold, BuddyNum: len(s.buddies), Decks: s.rankedDecksLocked(s.decks),
		BackUniqueIDs: backUniqueIDs, BackStackCards: backStackCards,
	}, nil
}

func (s *Account) prepareBuddyEvolutionMaterialLocked(base gamestate.Buddy, materialUniqueID int64, materialCardID int) (buddyEvolutionMaterial, error) {
	if (materialUniqueID == 0) == (materialCardID == 0) {
		return buddyEvolutionMaterial{}, errors.New("buddy evolution requires exactly one material")
	}
	remove := make(map[int64]struct{}, 1)
	if materialUniqueID != 0 {
		if materialUniqueID == base.UniqueID {
			return buddyEvolutionMaterial{}, errors.New("buddy evolution material equals the base")
		}
		index := s.buddyIndexByUniqueIDLocked(materialUniqueID)
		baseDefinition := s.buddyDefinitions[base.BuddyID]
		materialDefinition := gamestate.BuddyDefinition{}
		if index >= 0 {
			materialDefinition = s.buddyDefinitions[s.buddies[index].BuddyID]
		}
		if index < 0 {
			return buddyEvolutionMaterial{}, ErrBuddyUnavailable
		}
		if err := s.checkBuddyConsumptionLocked(s.buddies[index]); err != nil {
			return buddyEvolutionMaterial{}, err
		}
		if materialDefinition.SameBuddyID != baseDefinition.SameBuddyID {
			return buddyEvolutionMaterial{}, errors.New("buddy evolution material belongs to another family")
		}
		remove[materialUniqueID] = struct{}{}
		return buddyEvolutionMaterial{removeBuddies: remove, stackIndex: -1}, nil
	}
	owned := -1
	for index, stack := range s.stackCards {
		if stack.CardID == materialCardID {
			owned = index
			break
		}
	}
	if owned < 0 || s.stackCards[owned].Num < 1 {
		return buddyEvolutionMaterial{}, ErrInsufficientMaterials
	}
	if s.stackCards[owned].MaterialType != buddyEvolutionMaterialType {
		return buddyEvolutionMaterial{}, errors.New("buddy evolution stack-card material is unavailable")
	}
	return buddyEvolutionMaterial{removeBuddies: remove, stackIndex: owned}, nil
}

func (s *Account) consumePreparedBuddyEvolutionMaterialLocked(material buddyEvolutionMaterial) {
	if material.stackIndex >= 0 {
		s.consumeStackCardLocked(material.stackIndex, 1)
	}
	s.removeBuddiesLocked(material.removeBuddies)
}

func (s *Account) EvolveBuddy(baseUniqueID int64, materialUniqueID int64, materialCardID int) (buddyEvolutionResult, error) {
	if baseUniqueID <= 0 {
		return buddyEvolutionResult{}, errors.New("buddy evolution base is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	baseIndex := s.buddyIndexByUniqueIDLocked(baseUniqueID)
	if baseIndex < 0 {
		return buddyEvolutionResult{}, errors.New("unknown buddy evolution base")
	}
	base := s.buddies[baseIndex]
	definition := s.buddyDefinitions[base.BuddyID]
	if definition.EvolutionID == 0 && definition.OverlimitItemID == 0 {
		return buddyEvolutionResult{}, errors.New("buddy is not ready for evolution")
	}
	priceIndex := definition.EvolutionCount
	if definition.EvolutionID == 0 {
		priceIndex = 4
	}
	prices := s.buddyEvoPrices[definition.Rarity]
	if priceIndex < 0 || priceIndex >= len(prices) {
		return buddyEvolutionResult{}, errors.New("invalid buddy evolution price index")
	}
	if prices[priceIndex] > s.gold {
		return buddyEvolutionResult{}, ErrInsufficientGold
	}
	if definition.EvolutionID == 0 {
		item, exists := s.items[definition.OverlimitItemID]
		itemDefinition, defined := s.itemDefinitions[definition.OverlimitItemID]
		if !defined || definition.OverlimitItemNum <= 0 {
			return buddyEvolutionResult{}, errors.New("buddy overlimit reward inventory is unavailable")
		}
		if item.Num > itemDefinition.MaxOwned-definition.OverlimitItemNum {
			return buddyEvolutionResult{}, errItemCapacity
		}
		if !exists {
			item = gamestate.Item{ItemID: definition.OverlimitItemID}
		}
		material, err := s.prepareBuddyEvolutionMaterialLocked(base, materialUniqueID, materialCardID)
		if err != nil {
			return buddyEvolutionResult{}, err
		}
		s.consumePreparedBuddyEvolutionMaterialLocked(material)
		item.Num += definition.OverlimitItemNum
		s.items[item.ItemID] = item
		s.gold -= prices[priceIndex]
		reward := map[string]any{
			"reward": map[string]any{
				"type": 8, "reward_typeid": definition.OverlimitItemID, "num": definition.OverlimitItemNum,
				"card_lv": 0, "card_fame": 0, "card_love": 0, "card_skill_lv": []any{},
			},
			"uniqid": []any{}, "is_new": 0, "auto_fusion_used": 0,
			"auto_loveup_used": 0, "add": []any{},
		}
		return buddyEvolutionResult{
			Buddy: base, Gold: s.gold, BuddyNum: len(s.buddies), Decks: s.rankedDecksLocked(s.decks), Reward: reward,
		}, nil
	}
	target, exists := s.buddyDefinitions[definition.EvolutionID]
	if !exists || target.SameBuddyID != definition.SameBuddyID ||
		target.EvolutionCount != definition.EvolutionCount+1 ||
		target.ExperienceTableID != definition.ExperienceTableID ||
		target.MaxLevel < base.Level {
		return buddyEvolutionResult{}, errors.New("buddy evolution target is unavailable")
	}
	material, err := s.prepareBuddyEvolutionMaterialLocked(base, materialUniqueID, materialCardID)
	if err != nil {
		return buddyEvolutionResult{}, err
	}
	result := base
	result.BuddyID = target.BuddyID
	result, err = s.normalizeBuddyLocked(result, target)
	if err != nil {
		return buddyEvolutionResult{}, err
	}
	s.buddies[baseIndex] = result
	s.consumePreparedBuddyEvolutionMaterialLocked(material)
	s.gold -= prices[priceIndex]
	return buddyEvolutionResult{
		Buddy: result, Gold: s.gold, BuddyNum: len(s.buddies), Decks: s.rankedDecksLocked(s.decks), Reward: nil,
	}, nil
}

func (s *Account) removeBuddiesLocked(remove map[int64]struct{}) {
	if len(remove) == 0 {
		return
	}
	kept := make([]gamestate.Buddy, 0, len(s.buddies)-len(remove))
	for _, buddy := range s.buddies {
		if _, consumed := remove[buddy.UniqueID]; !consumed {
			kept = append(kept, buddy)
		}
	}
	s.buddies = kept
}

func (s *Account) SellBuddies(uniqueIDs []int64) (int, int, int, []DeckInfo, error) {
	if len(uniqueIDs) == 0 {
		return 0, 0, 0, nil, errors.New("buddy sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	remove := make(map[int64]struct{}, len(uniqueIDs))
	getGold := int64(0)
	for _, uniqueID := range uniqueIDs {
		index := s.buddyIndexByUniqueIDLocked(uniqueID)
		if index < 0 {
			return 0, 0, 0, nil, ErrBuddyUnavailable
		}
		if err := s.checkBuddyConsumptionLocked(s.buddies[index]); err != nil {
			return 0, 0, 0, nil, err
		}
		if _, duplicate := remove[uniqueID]; duplicate {
			return 0, 0, 0, nil, errors.New("duplicate buddy sell selection")
		}
		remove[uniqueID] = struct{}{}
		getGold += int64(s.buddyDefinitions[s.buddies[index].BuddyID].SellGold)
		if getGold > math.MaxInt || int64(s.gold)+getGold > math.MaxInt {
			return 0, 0, 0, nil, errors.New("buddy sell gold overflow")
		}
	}
	s.removeBuddiesLocked(remove)
	s.gold += int(getGold)
	return int(getGold), s.gold, len(s.buddies), s.rankedDecksLocked(s.decks), nil
}

func (s *Account) SetBuddyLock(uniqueID int64, locked bool) error {
	if uniqueID <= 0 {
		return errors.New("invalid buddy unique ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	index := s.buddyIndexByUniqueIDLocked(uniqueID)
	if index < 0 {
		return errors.New("unknown buddy unique ID")
	}
	if locked {
		s.buddies[index].IsLock = 1
	} else {
		s.buddies[index].IsLock = 0
	}
	return nil
}

func (s *Account) validateDeckBuddies(deck DeckInfo) error {
	seen := make(map[int64]struct{}, len(deck.BuddyUniqueIDs))
	for _, uniqueID := range deck.BuddyUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		if _, duplicate := seen[uniqueID]; duplicate {
			return fmt.Errorf("buddy unique ID %d is repeated in the deck", uniqueID)
		}
		if s.buddyIndexByUniqueIDLocked(uniqueID) < 0 {
			return fmt.Errorf("unknown buddy unique ID %d", uniqueID)
		}
		seen[uniqueID] = struct{}{}
	}
	return nil
}
