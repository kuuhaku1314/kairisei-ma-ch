package game

import (
	"errors"
	"fmt"
	"math"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) MoveCards(toSlot int8, uniqueIDs []int64) ([]CardInfo, []DeckInfo, error) {
	if toSlot != 0 && toSlot != 1 {
		return nil, nil, errors.New("card destination slot must be inventory or container")
	}
	if len(uniqueIDs) == 0 {
		return nil, nil, errors.New("card move selection is empty")
	}
	requested := make(map[int64]struct{}, len(uniqueIDs))
	for _, uniqueID := range uniqueIDs {
		if uniqueID <= 0 {
			return nil, nil, errors.New("invalid card unique ID")
		}
		if _, duplicate := requested[uniqueID]; duplicate {
			return nil, nil, errors.New("duplicate card move selection")
		}
		requested[uniqueID] = struct{}{}
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	source := &s.containerCards
	destination := &s.cards
	destinationMax := s.cardMax
	if toSlot == 1 {
		source = &s.cards
		destination = &s.containerCards
		destinationMax = s.cardContainerMax
	}
	if len(*destination)+len(uniqueIDs) > destinationMax {
		if toSlot == 1 {
			return nil, nil, &BusinessError{-2901, "仓库卡牌容量不足，请整理后再试。"}
		}
		return nil, nil, ErrCardCapacity
	}

	selected := make(map[int]struct{}, len(uniqueIDs))
	moved := make([]CardInfo, len(uniqueIDs))
	movePositions := indexCardSelection(*source, uniqueIDs)
	for moveIndex, uniqueID := range uniqueIDs {
		index := movePositions[uniqueID]
		if index < 0 {
			return nil, nil, errors.New("card move source does not contain unique ID")
		}
		selected[index] = struct{}{}
		if s.cardFameTraining != nil && s.cardFameTraining.UniqueID == uniqueID {
			return nil, nil, errors.New("card is locked by active fame training")
		}
		moved[moveIndex] = cloneCard((*source)[index])
		moved[moveIndex].Slot = int(toSlot)
	}
	kept := make([]CardInfo, 0, len(*source)-len(selected))
	for index, card := range *source {
		if _, remove := selected[index]; !remove {
			kept = append(kept, card)
		}
	}
	proposedDestination := append(CloneCards(*destination), moved...)
	updatedDecks := make([]DeckInfo, 0)
	proposedDecks := s.rankedDecksLocked(s.decks)
	if toSlot == 1 {
		for deckIndex := range proposedDecks {
			changed := false
			vacatedMainSlots := make([]int, 0)
			vacatedSupportSlots := make([]int, 0)
			for cardIndex, uniqueID := range proposedDecks[deckIndex].CardUniqueIDs {
				if _, remove := requested[uniqueID]; remove {
					proposedDecks[deckIndex].CardUniqueIDs[cardIndex] = 0
					vacatedMainSlots = append(vacatedMainSlots, cardIndex)
					changed = true
				}
			}
			for cardIndex, uniqueID := range proposedDecks[deckIndex].SupportCardUniqueIDs {
				if _, remove := requested[uniqueID]; remove {
					proposedDecks[deckIndex].SupportCardUniqueIDs[cardIndex] = 0
					vacatedSupportSlots = append(vacatedSupportSlots, cardIndex)
					changed = true
				}
			}
			if !changed {
				continue
			}
			if err := s.fillVacatedDeckSlots(
				&proposedDecks[deckIndex], kept, vacatedMainSlots, false,
			); err != nil {
				return nil, nil, err
			}
			if err := s.fillVacatedDeckSlots(
				&proposedDecks[deckIndex], kept, vacatedSupportSlots, true,
			); err != nil {
				return nil, nil, err
			}
			updatedDecks = append(updatedDecks, cloneDeck(proposedDecks[deckIndex]))
		}
	}
	*source = kept
	*destination = proposedDestination
	s.decks = proposedDecks
	return CloneCards(moved), s.rankedDecksLocked(updatedDecks), nil
}

func (s *Account) fillVacatedDeckSlots(
	deck *DeckInfo,
	candidates []CardInfo,
	vacatedSlots []int,
	support bool,
) error {
	if len(vacatedSlots) == 0 {
		return nil
	}
	owned := ownedCardIDs(s.cards)
	for _, slot := range vacatedSlots {
		filled := false
		for _, candidate := range candidates {
			if deckContainsCardUniqueID(*deck, candidate.UniqueID) {
				continue
			}
			if support {
				deck.SupportCardUniqueIDs[slot] = candidate.UniqueID
			} else {
				deck.CardUniqueIDs[slot] = candidate.UniqueID
			}
			// System-generated main slots must obey profession rules even in an
			// inactive draft. Manual drafts and ordinary-profession EX stay editable.
			if err := s.validateDeckCardFamilies(*deck, owned, 0, 0, !support); err == nil {
				filled = true
				break
			}
			if support {
				deck.SupportCardUniqueIDs[slot] = 0
			} else {
				deck.CardUniqueIDs[slot] = 0
			}
		}
		if !filled {
			kind := "main"
			if support {
				kind = "support"
			}
			return fmt.Errorf(
				"cannot auto-fill %s deck slot %d for Arthur %d deck %d",
				kind, slot+1, deck.ArthurType, deck.Index,
			)
		}
	}
	return nil
}

// repairIncompleteMainDecks restores vacated slots in equipped main decks.
// Inactive decks are editable drafts and may legitimately contain empty slots.
func (s *Account) repairIncompleteMainDecks() (bool, error) {
	proposed := s.rankedDecksLocked(s.decks)
	mainInventory := make(map[int64]struct{}, len(s.cards))
	for _, card := range s.cards {
		mainInventory[card.UniqueID] = struct{}{}
	}
	containerInventory := make(map[int64]struct{}, len(s.containerCards))
	for _, card := range s.containerCards {
		containerInventory[card.UniqueID] = struct{}{}
	}

	changed := false
	for deckIndex := range proposed {
		if proposed[deckIndex].IsActive == 0 {
			continue
		}
		vacated := make([]int, 0)
		for slot, uniqueID := range proposed[deckIndex].CardUniqueIDs {
			if uniqueID == 0 {
				vacated = append(vacated, slot)
				continue
			}
			if _, exists := mainInventory[uniqueID]; exists {
				continue
			}
			if _, stored := containerInventory[uniqueID]; !stored {
				return false, fmt.Errorf(
					"Arthur %d deck %d references unknown card unique ID %d",
					proposed[deckIndex].ArthurType,
					proposed[deckIndex].Index,
					uniqueID,
				)
			}
			proposed[deckIndex].CardUniqueIDs[slot] = 0
			vacated = append(vacated, slot)
		}
		if len(vacated) == 0 {
			continue
		}
		if err := s.fillVacatedDeckSlots(
			&proposed[deckIndex], s.cards, vacated, false,
		); err != nil {
			return false, err
		}
		changed = true
	}
	if changed {
		s.decks = proposed
	}
	return changed, nil
}

func (s *Account) SetCardLock(uniqueID int64, slot int, locked bool) error {
	if uniqueID <= 0 {
		return errors.New("invalid card unique ID")
	}
	if slot != 0 && slot != 1 {
		return errors.New("invalid card slot")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	inventory := &s.cards
	if slot == 1 {
		inventory = &s.containerCards
	}
	index := cardIndexByUniqueID(*inventory, uniqueID)
	if index < 0 {
		return errors.New("unknown card unique ID in requested slot")
	}
	if !locked && s.cardFameTraining != nil && s.cardFameTraining.UniqueID == uniqueID {
		return errors.New("card is locked by active fame training")
	}
	if locked {
		(*inventory)[index].IsLock = 1
	} else {
		(*inventory)[index].IsLock = 0
	}
	return nil
}

type LoveUpItemUse struct {
	ItemID int `json:"itemid"`
	Num    int `json:"num"`
}

type cardLoveUpResult struct {
	Card    CardInfo
	Items   []gamestate.Item
	Gold    int
	OldLove int
	NewLove int
	IsMax   int8
}

func (s *Account) LoveUpCard(baseUniqueID int64, uses []LoveUpItemUse) (cardLoveUpResult, error) {
	if baseUniqueID <= 0 || len(uses) == 0 || len(uses) > maxLoveUpItemKinds {
		return cardLoveUpResult{}, errors.New("invalid card love-up selection")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())

	cardIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if cardIndex < 0 {
		return cardLoveUpResult{}, errors.New("love-up card is not in the inventory")
	}
	card := s.cards[cardIndex]
	rule, exists := s.cardLoveRules[card.CardID]
	if !exists || rule.Max <= 0 || card.Love < 0 || card.Love >= rule.Max {
		return cardLoveUpResult{}, errors.New("card cannot increase love")
	}

	seen := make(map[int]struct{}, len(uses))
	updatedItems := make([]gamestate.Item, len(uses))
	var addLove int64
	var totalPrice int64
	for index, use := range uses {
		if use.ItemID <= 0 || use.Num <= 0 {
			return cardLoveUpResult{}, errors.New("invalid love-up item selection")
		}
		if _, duplicate := seen[use.ItemID]; duplicate {
			return cardLoveUpResult{}, errors.New("duplicate love-up item selection")
		}
		seen[use.ItemID] = struct{}{}
		definition, known := s.itemDefinitions[use.ItemID]
		item, owned := s.items[use.ItemID]
		if !owned || use.Num > item.Num {
			return cardLoveUpResult{}, ErrInsufficientMaterials
		}
		if !known || definition.ItemType != "LOVE_UP" ||
			(definition.Function != "LOVEUP_NORMAL" && definition.Function != "LOVEUP_ALL") ||
			definition.FunctionValue <= 0 || definition.LoveUpPrice <= 0 {
			return cardLoveUpResult{}, errors.New("love-up item is unavailable")
		}
		if rule.Premium && definition.Function != "LOVEUP_ALL" {
			return cardLoveUpResult{}, errors.New("love-up item does not support premium rarity")
		}
		addLove += int64(definition.FunctionValue) * int64(use.Num)
		totalPrice += int64(definition.LoveUpPrice) * int64(use.Num)
		item.Num -= use.Num
		updatedItems[index] = item
	}
	if addLove <= 0 || totalPrice <= 0 || totalPrice > int64(s.gold) {
		return cardLoveUpResult{}, ErrInsufficientGold
	}

	oldLove := card.Love
	newLove := int64(oldLove) + addLove
	if newLove > int64(rule.Max) {
		newLove = int64(rule.Max)
	}
	card.Love = int(newLove)
	card, err := s.normalizeCardLocked(card)
	if err != nil {
		return cardLoveUpResult{}, err
	}
	for _, item := range updatedItems {
		s.items[item.ItemID] = item
	}
	s.gold -= int(totalPrice)
	s.cards[cardIndex] = card
	isMax := int8(0)
	s.recordCollectedCardLocked(card)
	if card.Love == rule.Max {
		isMax = 1
	}
	return cardLoveUpResult{
		Card: card, Items: updatedItems, Gold: s.gold,
		OldLove: oldLove, NewLove: card.Love, IsMax: isMax,
	}, nil
}

func (s *Account) SellContainerCards(uniqueIDs []int64) (SellCardsResult, error) {
	if len(uniqueIDs) == 0 {
		return SellCardsResult{}, errors.New("container sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	remove := make(map[int]struct{}, len(uniqueIDs))
	getGold := 0
	extractNum := 0
	positions := indexCardSelection(s.containerCards, uniqueIDs)
	for _, uniqueID := range uniqueIDs {
		index := positions[uniqueID]
		if index < 0 {
			return SellCardsResult{}, ErrCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return SellCardsResult{}, err
		}
		definition, exists := s.cardDefinitions[s.containerCards[index].CardID]
		if !exists || definition.SellGold < 0 {
			return SellCardsResult{}, errors.New("container sell price is unavailable")
		}
		remove[index] = struct{}{}
		var err error
		getGold, err = checkedCardSaleGold(s.gold, getGold, definition.SellGold, 1)
		if err != nil {
			return SellCardsResult{}, err
		}
		count := s.cardSellExtractCount(definition.CardID)
		if count > math.MaxInt-extractNum {
			return SellCardsResult{}, errors.New("card sell extract quantity overflows")
		}
		extractNum += count
	}
	if len(remove) != len(uniqueIDs) {
		return SellCardsResult{}, errors.New("duplicate container sell card")
	}
	extract, err := s.grantCardSellExtractLocked(extractNum)
	if err != nil {
		return SellCardsResult{}, err
	}
	kept := make([]CardInfo, 0, len(s.containerCards)-len(remove))
	for index, card := range s.containerCards {
		if _, exists := remove[index]; !exists {
			kept = append(kept, card)
		}
	}
	s.containerCards = kept
	s.gold += getGold
	s.markMissionMetricLocked(gamestate.MissionCardOwned)
	s.markMissionMetricLocked(gamestate.MissionCardFame)
	return SellCardsResult{GetGold: getGold, Gold: s.gold, Decks: s.rankedDecksLocked(s.decks), Extract: extract}, nil
}

func (s *Account) FuseCard(
	baseUniqueID int64,
	materialUniqueIDs []int64,
	containerMaterialUniqueIDs []int64,
	stackUses []gamestate.CardStackUse,
) (CardInfo, CardInfo, int, []DeckInfo, error) {
	if baseUniqueID <= 0 ||
		(len(materialUniqueIDs) == 0 && len(containerMaterialUniqueIDs) == 0 && len(stackUses) == 0) {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("fusion selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	baseIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if baseIndex < 0 {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("unknown fusion base card")
	}
	oldCard := s.cards[baseIndex]
	if s.cardFameTraining != nil && s.cardFameTraining.UniqueID == baseUniqueID {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("fusion base is in active fame training")
	}
	baseDefinition, exists := s.cardDefinitions[oldCard.CardID]
	if !exists {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("fusion base card definition is unavailable")
	}
	materialIndexes := make(map[int]struct{}, len(materialUniqueIDs))
	materialPositions := indexCardSelection(s.cards, materialUniqueIDs)
	for _, uniqueID := range materialUniqueIDs {
		index := materialPositions[uniqueID]
		if index < 0 {
			return CardInfo{}, CardInfo{}, 0, nil, ErrCardUnavailable
		}
		if index == baseIndex {
			return CardInfo{}, CardInfo{}, 0, nil, errors.New("invalid fusion material card")
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return CardInfo{}, CardInfo{}, 0, nil, err
		}
		materialIndexes[index] = struct{}{}
	}
	if len(materialIndexes) != len(materialUniqueIDs) {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("duplicate fusion material card")
	}
	containerMaterialIndexes := make(map[int]struct{}, len(containerMaterialUniqueIDs))
	containerPositions := indexCardSelection(s.containerCards, containerMaterialUniqueIDs)
	for _, uniqueID := range containerMaterialUniqueIDs {
		index := containerPositions[uniqueID]
		if index < 0 {
			return CardInfo{}, CardInfo{}, 0, nil, ErrCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return CardInfo{}, CardInfo{}, 0, nil, err
		}
		containerMaterialIndexes[index] = struct{}{}
	}
	if len(containerMaterialIndexes) != len(containerMaterialUniqueIDs) {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("duplicate container fusion material card")
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return CardInfo{}, CardInfo{}, 0, nil, err
	}
	materialCount := len(materialUniqueIDs) + len(containerMaterialUniqueIDs)
	if len(materialUniqueIDs) > gamestate.CardFusionMaterialLimit || len(containerMaterialUniqueIDs) > gamestate.CardFusionMaterialLimit || len(stackUses) > gamestate.CardFusionMaterialLimit {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("too many card fusion material instances or types")
	}
	for _, use := range stackUses {
		if use.Num > cardFusionTotalMaterialLimit-materialCount {
			return CardInfo{}, CardInfo{}, 0, nil, errors.New("too many card fusion materials")
		}
		materialCount += use.Num
	}
	if materialCount <= 0 || materialCount > cardFusionTotalMaterialLimit {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("too many card fusion materials")
	}
	cost64 := int64(oldCard.BaseAddPrice) * int64(materialCount)
	if cost64 <= 0 {
		return CardInfo{}, CardInfo{}, 0, nil, errors.New("invalid card fusion cost")
	}
	if cost64 > int64(s.gold) {
		return CardInfo{}, CardInfo{}, 0, nil, ErrInsufficientGold
	}
	cost := int(cost64)
	expUp := 0
	fameUp := 0
	addMaterial := func(material CardInfo) error {
		if material.AddExperience <= 0 || expUp > math.MaxInt-material.AddExperience {
			return errors.New("card fusion experience overflows")
		}
		expUp += material.AddExperience
		definition, available := s.cardDefinitions[material.CardID]
		if !available {
			return errors.New("card fusion material definition is unavailable")
		}
		// CalcAddFame uses family identity; level/rarity restrictions belong to
		// the client's convenient same-card selector, not manual fusion results.
		if definition.SameCardID > 0 && definition.SameCardID == baseDefinition.SameCardID {
			if material.Fame <= 0 || fameUp > math.MaxInt-material.Fame {
				return errors.New("card fusion material fame is invalid")
			}
			fameUp += material.Fame
		}
		return nil
	}
	for index := range materialIndexes {
		if err := addMaterial(s.cards[index]); err != nil {
			return CardInfo{}, CardInfo{}, 0, nil, err
		}
	}
	for index := range containerMaterialIndexes {
		if err := addMaterial(s.containerCards[index]); err != nil {
			return CardInfo{}, CardInfo{}, 0, nil, err
		}
	}
	for _, use := range stackUses {
		bonus := 0
		for attribute, value := range s.cardProgression.FameMaterials[use.CardID] {
			if baseDefinition.FusionAttributes&(1<<attribute) != 0 {
				bonus = max(bonus, value) // Dual attributes take the maximum, not the sum.
			}
		}
		if bonus > 0 {
			addition := int64(bonus) * int64(use.Num)
			if addition > int64(math.MaxInt-fameUp) {
				return CardInfo{}, CardInfo{}, 0, nil, errors.New("card fusion fame overflows")
			}
			fameUp += int(addition)
		}
		for _, stack := range s.stackCards {
			if stack.CardID == use.CardID {
				addition := int64(stack.AddExperience) * int64(use.Num)
				if stack.AddExperience <= 0 || addition > math.MaxInt || int64(expUp)+addition > math.MaxInt {
					return CardInfo{}, CardInfo{}, 0, nil, errors.New("card fusion experience overflows")
				}
				expUp += int(addition)
				break
			}
		}
	}
	updated := oldCard
	success := s.cardProgression.FusionSuccessTypes[0]
	if oldCard.Level < oldCard.LevelMax {
		rolled, rollErr := rollCardFusionSuccess(s.cardProgression)
		if rollErr != nil {
			return CardInfo{}, CardInfo{}, 0, nil, rollErr
		}
		success = rolled
		boosted, boostErr := boostedFusionExperience(expUp, success)
		if boostErr != nil || updated.Experience > math.MaxInt-boosted {
			return CardInfo{}, CardInfo{}, 0, nil, errors.New("card fusion experience overflows")
		}
		updated.Experience += boosted
	}
	if fameUp > 0 && updated.Fame < baseDefinition.FameMax {
		remaining := baseDefinition.FameMax - updated.Fame
		if fameUp > remaining {
			fameUp = remaining
		}
		updated.Fame += fameUp
	} else {
		fameUp = 0
	}
	if oldCard.Level >= oldCard.LevelMax && fameUp == 0 {
		return CardInfo{}, CardInfo{}, 0, nil, &BusinessError{-1, "这张卡牌的等级和名声已无法通过所选素材提升。"}
	}
	updated, err := s.normalizeCardLocked(updated)
	if err != nil {
		return CardInfo{}, CardInfo{}, 0, nil, err
	}
	s.cards[baseIndex] = updated
	kept := make([]CardInfo, 0, len(s.cards)-len(materialIndexes))
	for index, card := range s.cards {
		if _, remove := materialIndexes[index]; !remove {
			kept = append(kept, card)
		} else {
			s.clearCardFromDecksLocked(card.UniqueID)
		}
	}
	s.cards = kept
	if len(containerMaterialIndexes) != 0 {
		keptContainer := make([]CardInfo, 0, len(s.containerCards)-len(containerMaterialIndexes))
		for index, card := range s.containerCards {
			if _, remove := containerMaterialIndexes[index]; !remove {
				keptContainer = append(keptContainer, card)
			}
		}
		s.containerCards = keptContainer
	}
	s.consumeStackUsesLocked(stackUses)
	s.gold -= cost
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "fusion"}); err != nil {
		return CardInfo{}, CardInfo{}, 0, nil, err
	}
	s.markMissionMetricLocked(gamestate.MissionCardOwned)
	s.markMissionMetricLocked(gamestate.MissionCardFame)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardFusion}, time.Now())
	return oldCard, updated, success.SuccessType, s.rankedDecksLocked(s.decks), nil
}

func (s *Account) EvolveCard(
	baseUniqueID int64,
	toCardID int,
	materialUniqueIDs []int64,
	containerMaterialUniqueIDs []int64,
	materialCardIDs []int,
	materialItemIDs []int,
) (CardInfo, int, []DeckInfo, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	baseIndex := cardIndexByUniqueID(s.cards, baseUniqueID)
	if baseIndex < 0 {
		return CardInfo{}, 0, nil, errors.New("unknown evolution base card")
	}
	base := s.cards[baseIndex]
	if s.cardFameTraining != nil && s.cardFameTraining.UniqueID == baseUniqueID {
		return CardInfo{}, 0, nil, errors.New("evolution base is in active fame training")
	}
	var transition *gamestate.EvolutionTransition
	for index := range s.cardActions.EvolutionTransitions {
		candidate := &s.cardActions.EvolutionTransitions[index]
		if candidate.FromCardID == base.CardID && candidate.ToCardID == toCardID {
			transition = candidate
			break
		}
	}
	if transition == nil || base.Level != base.LevelMax && s.cardDefinitions[base.CardID].RarityPlus != 4 {
		return CardInfo{}, 0, nil, errors.New("card is not ready for evolution")
	}
	itemEvolution := transition.Type == 4
	if itemEvolution && (len(materialUniqueIDs) != 0 || len(containerMaterialUniqueIDs) != 0 || len(materialCardIDs) != 0) ||
		!itemEvolution && len(materialItemIDs) != 0 {
		return CardInfo{}, 0, nil, errors.New("evolution material inventory differs from the recipe")
	}
	if s.evolutionRestrictions != nil {
		if _, blocked := s.evolutionRestrictions.blocked[EvolutionPath{FromCardID: base.CardID, ToCardID: toCardID}]; blocked {
			return CardInfo{}, 0, nil, ErrEvolutionClosed
		}
	}
	materialIndexes := make(map[int]struct{}, len(materialUniqueIDs))
	selectedMaterials := make(map[int]int, len(materialUniqueIDs)+len(containerMaterialUniqueIDs)+len(materialCardIDs))
	materialPositions := indexCardSelection(s.cards, materialUniqueIDs)
	for _, uniqueID := range materialUniqueIDs {
		index := materialPositions[uniqueID]
		if index < 0 {
			return CardInfo{}, 0, nil, ErrCardUnavailable
		}
		if index == baseIndex {
			return CardInfo{}, 0, nil, errors.New("invalid evolution material card")
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return CardInfo{}, 0, nil, err
		}
		materialIndexes[index] = struct{}{}
		selectedMaterials[s.cards[index].CardID]++
	}
	if len(materialIndexes) != len(materialUniqueIDs) {
		return CardInfo{}, 0, nil, errors.New("duplicate evolution material card")
	}
	containerMaterialIndexes := make(map[int]struct{}, len(containerMaterialUniqueIDs))
	containerPositions := indexCardSelection(s.containerCards, containerMaterialUniqueIDs)
	for _, uniqueID := range containerMaterialUniqueIDs {
		index := containerPositions[uniqueID]
		if index < 0 {
			return CardInfo{}, 0, nil, ErrCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.containerCards[index]); err != nil {
			return CardInfo{}, 0, nil, err
		}
		containerMaterialIndexes[index] = struct{}{}
		selectedMaterials[s.containerCards[index].CardID]++
	}
	if len(containerMaterialIndexes) != len(containerMaterialUniqueIDs) {
		return CardInfo{}, 0, nil, errors.New("duplicate container evolution material card")
	}
	materialIDs := materialCardIDs
	if itemEvolution {
		materialIDs = materialItemIDs
	}
	uses, err := evolutionMaterialUses(materialIDs, selectedMaterials, transition.Materials)
	if err != nil {
		return CardInfo{}, 0, nil, err
	}
	stackUses := make([]gamestate.CardStackUse, 0, len(uses))
	for _, use := range uses {
		if itemEvolution {
			if s.items[use.id].Num < use.num {
				return CardInfo{}, 0, nil, ErrInsufficientMaterials
			}
		} else {
			stackUses = append(stackUses, gamestate.CardStackUse{CardID: use.id, Num: use.num})
		}
		selectedMaterials[use.id] += use.num
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return CardInfo{}, 0, nil, err
	}
	if !sameEvolutionMaterials(selectedMaterials, transition.Materials) {
		return CardInfo{}, 0, nil, errors.New("evolution materials differ")
	}
	// GOD evolution consumes the required fame from each selected instance.
	// Fame from several cards cannot be combined to satisfy one material slot.
	var materialFameCosts map[int]int
	if transition.Type == 1 {
		if len(stackUses) != 0 {
			return CardInfo{}, 0, nil, errors.New("GOD evolution requires material card instances")
		}
		materialFameCosts = make(map[int]int, len(transition.Materials))
		for _, required := range transition.Materials {
			materialFameCosts[required.CardID] = required.Fame
			for index := range materialIndexes {
				card := s.cards[index]
				if card.CardID == required.CardID && card.Fame < required.Fame {
					return CardInfo{}, 0, nil, &BusinessError{-1, "进化素材名声不足。"}
				}
			}
			for index := range containerMaterialIndexes {
				card := s.containerCards[index]
				if card.CardID == required.CardID && card.Fame < required.Fame {
					return CardInfo{}, 0, nil, &BusinessError{-1, "进化素材名声不足。"}
				}
			}
		}
	}
	if s.gold < transition.Gold {
		return CardInfo{}, 0, nil, ErrInsufficientGold
	}
	template, exists := s.cardTemplates[toCardID]
	if !exists {
		return CardInfo{}, 0, nil, errors.New("evolution target template is missing")
	}
	result := cloneCard(template)
	result.UniqueID = base.UniqueID
	result.CreateTime = base.CreateTime
	result.Love = base.Love
	result.Fame = base.Fame
	result.IsLock = base.IsLock
	if transition.KeepLevel {
		definition, exists := s.cardDefinitions[toCardID]
		if !exists {
			return CardInfo{}, 0, nil, errors.New("evolution target definition is missing")
		}
		experience, err := gamestate.CardExperienceAtLevelStart(min(base.Level, definition.LevelMax), definition.LevelMax, s.cardExperience[definition.ExperienceTableID])
		if err != nil {
			return CardInfo{}, 0, nil, err
		}
		result.Experience = experience
	}
	result, err = s.normalizeCardLocked(result)
	if err != nil {
		return CardInfo{}, 0, nil, err
	}
	for _, deck := range s.decks {
		if !deckContainsCardUniqueID(deck, base.UniqueID) {
			continue
		}
		if err := s.validateDeckCardFamilies(deck, ownedCardIDs(s.cards), base.UniqueID, toCardID, false); err != nil {
			return CardInfo{}, 0, nil, fmt.Errorf("evolution target invalidates deck: %w", err)
		}
	}
	s.cards[baseIndex] = result
	s.recordCollectedCardLocked(result)
	if len(materialIndexes) != 0 {
		kept := make([]CardInfo, 0, len(s.cards)-len(materialIndexes))
		for index, card := range s.cards {
			if _, remove := materialIndexes[index]; !remove {
				kept = append(kept, card)
			} else if fame, consumesFame := materialFameCosts[card.CardID]; consumesFame && card.Fame > fame {
				card.Fame -= fame
				kept = append(kept, card)
			} else {
				s.clearCardFromDecksLocked(card.UniqueID)
			}
		}
		s.cards = kept
	}
	if len(containerMaterialIndexes) != 0 {
		keptContainer := make([]CardInfo, 0, len(s.containerCards)-len(containerMaterialIndexes))
		for index, card := range s.containerCards {
			if _, remove := containerMaterialIndexes[index]; !remove {
				keptContainer = append(keptContainer, card)
			} else if fame, consumesFame := materialFameCosts[card.CardID]; consumesFame && card.Fame > fame {
				card.Fame -= fame
				keptContainer = append(keptContainer, card)
			}
		}
		s.containerCards = keptContainer
	}
	s.consumeStackUsesLocked(stackUses)
	if itemEvolution {
		for _, use := range uses {
			item := s.items[use.id]
			item.Num -= use.num
			s.items[use.id] = item
		}
	}
	s.gold -= transition.Gold
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionCardEvolution, subjectID: result.CardID}, time.Now())
	return result, s.gold, s.rankedDecksLocked(s.decks), nil
}

type SellCardsResult struct {
	GetGold int
	Gold    int
	Decks   []DeckInfo
	Extract PresentReceiveResult
}

func (s *Account) cardSellExtractCount(cardID int) int {
	if s.cardSellExtract == nil {
		return 0
	}
	baseID, found := s.cardSellExtract.BaseCardIDs[cardID]
	if !found {
		return 0
	}
	rarity, found := s.cardSellExtract.BaseRarities[baseID]
	if !found {
		return 0
	}
	return s.cardSellExtract.CountByBaseRarity[rarity]
}

func (s *Account) grantCardSellExtractLocked(count int) (PresentReceiveResult, error) {
	result := PresentReceiveResult{}
	if count == 0 {
		return result, nil
	}
	// Grant under the same account lock before the infallible sale mutations.
	// The HTTP layer persists both changes in one account snapshot.
	reward := gamestate.Reward{Type: 8, RewardTypeID: s.cardSellExtract.ItemID, Num: count, CardSkillLevels: []int16{}}
	if err := s.validateRewardLocked(reward); err != nil {
		return PresentReceiveResult{}, err
	}
	if err := s.applyRewardOrPresentLocked(reward, &result, "卡牌出售提取奖励"); err != nil {
		return PresentReceiveResult{}, err
	}
	return result, nil
}

func (s *Account) SellCards(uniqueIDs []int64, stackUses []gamestate.CardStackUse) (SellCardsResult, error) {
	if len(uniqueIDs) == 0 && len(stackUses) == 0 {
		return SellCardsResult{}, errors.New("sell selection is empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	remove := make(map[int]struct{}, len(uniqueIDs))
	getGold := 0
	extractNum := 0
	positions := indexCardSelection(s.cards, uniqueIDs)
	for _, uniqueID := range uniqueIDs {
		index := positions[uniqueID]
		if index < 0 {
			return SellCardsResult{}, ErrCardUnavailable
		}
		if err := s.checkCardConsumptionLocked(s.cards[index]); err != nil {
			return SellCardsResult{}, err
		}
		definition, exists := s.cardDefinitions[s.cards[index].CardID]
		if !exists || definition.SellGold < 0 {
			return SellCardsResult{}, errors.New("card sell price is unavailable")
		}
		remove[index] = struct{}{}
		var err error
		getGold, err = checkedCardSaleGold(s.gold, getGold, definition.SellGold, 1)
		if err != nil {
			return SellCardsResult{}, err
		}
		count := s.cardSellExtractCount(definition.CardID)
		if count > math.MaxInt-extractNum {
			return SellCardsResult{}, errors.New("card sell extract quantity overflows")
		}
		extractNum += count
	}
	if len(remove) != len(uniqueIDs) {
		return SellCardsResult{}, errors.New("duplicate sell card")
	}
	if err := validateStackUses(s.stackCards, stackUses); err != nil {
		return SellCardsResult{}, err
	}
	for _, use := range stackUses {
		for _, stack := range s.stackCards {
			if stack.CardID == use.CardID {
				var err error
				getGold, err = checkedCardSaleGold(s.gold, getGold, stack.BaseAddPrice, use.Num)
				if err != nil {
					return SellCardsResult{}, err
				}
				count := s.cardSellExtractCount(use.CardID)
				if count > 0 && use.Num > (math.MaxInt-extractNum)/count {
					return SellCardsResult{}, errors.New("card sell extract quantity overflows")
				}
				extractNum += count * use.Num
				break
			}
		}
	}
	extract, err := s.grantCardSellExtractLocked(extractNum)
	if err != nil {
		return SellCardsResult{}, err
	}
	result := SellCardsResult{Extract: extract}
	kept := make([]CardInfo, 0, len(s.cards)-len(remove))
	for index, card := range s.cards {
		if _, exists := remove[index]; !exists {
			kept = append(kept, card)
		} else {
			s.clearCardFromDecksLocked(card.UniqueID)
		}
	}
	s.cards = kept
	s.consumeStackUsesLocked(stackUses)
	s.gold += getGold
	s.markMissionMetricLocked(gamestate.MissionCardOwned)
	s.markMissionMetricLocked(gamestate.MissionCardFame)
	result.GetGold, result.Gold, result.Decks = getGold, s.gold, s.rankedDecksLocked(s.decks)
	return result, nil
}

// Selling and using a card as material share these ordinary availability
// rules. They do not apply to the base card being strengthened or evolved.
func (s *Account) checkCardConsumptionLocked(card CardInfo) error {
	if s.uniqueIDInDeck(card.UniqueID) {
		return ErrCardInDeck
	}
	if card.IsLock != 0 {
		return ErrCardLocked
	}
	return nil
}

func (s *Account) uniqueIDInDeck(uniqueID int64) bool {
	// The Alchemist deck opens after training, independently of the A-rank
	// gate on ordinary professions. Protect its main and EX slots at any rank.
	allDecks := len(s.deckRankPolicy.ParameterThresholds) == 0 || s.highestDeckRank >= 10
	if !allDecks {
		current, found := SelectPartnerDeck(s.decks, s.currentActiveArthur)
		if found && deckContainsCardUniqueID(current, uniqueID) {
			return true
		}
	}
	for _, deck := range s.decks {
		if (allDecks || deck.ArthurType == 5) && deckContainsCardUniqueID(deck, uniqueID) {
			return true
		}
	}
	return false
}

func cardIndexByUniqueID(cards []CardInfo, uniqueID int64) int {
	for index, card := range cards {
		if card.UniqueID == uniqueID {
			return index
		}
	}
	return -1
}

func validateStackUses(cards []gamestate.CardStack, uses []gamestate.CardStackUse) error {
	available := make(map[int]int, len(cards))
	for _, card := range cards {
		available[card.CardID] = card.Num
	}
	seen := make(map[int]struct{}, len(uses))
	for _, use := range uses {
		if use.CardID <= 0 || use.Num <= 0 {
			return errors.New("invalid stack-card selection")
		}
		if _, exists := seen[use.CardID]; exists {
			return errors.New("duplicate stack-card selection")
		}
		seen[use.CardID] = struct{}{}
		if available[use.CardID] < use.Num {
			return ErrInsufficientMaterials
		}
	}
	return nil
}

func (s *Account) consumeStackUsesLocked(uses []gamestate.CardStackUse) {
	for _, use := range uses {
		for index := range s.stackCards {
			if s.stackCards[index].CardID == use.CardID {
				s.consumeStackCardLocked(index, use.Num)
				break
			}
		}
	}
}

func (s *Account) consumeStackCardLocked(index, count int) {
	card := &s.stackCards[index]
	card.Num -= count
	if card.Num == 0 {
		s.markMissionMetricLocked(gamestate.MissionCardOwned)
	}
}

func sameEvolutionMaterials(selected map[int]int, expected []gamestate.EvolutionMaterial) bool {
	if len(selected) != len(expected) {
		return false
	}
	for _, material := range expected {
		if selected[material.CardID] != material.Num {
			return false
		}
	}
	return true
}

type evolutionMaterialUse struct {
	id  int
	num int
}

// EvoFusionUI.SendFusionInfo sends one card/item ID per selected material slot,
// even when that slot requires 25 or 800 copies. The authoritative recipe
// supplies quantities; unlike exp fusion, repeated IDs are not unit counts.
func evolutionMaterialUses(materialIDs []int, instanceCounts map[int]int, materials []gamestate.EvolutionMaterial) ([]evolutionMaterialUse, error) {
	selectedSlots := make(map[int]int, len(materialIDs))
	for _, id := range materialIDs {
		if id <= 0 {
			return nil, errors.New("invalid evolution material ID")
		}
		selectedSlots[id]++
	}
	uses := make([]evolutionMaterialUse, 0, len(selectedSlots))
	for _, material := range materials {
		slots := selectedSlots[material.CardID]
		if slots == 0 {
			continue
		}
		count := material.Num - instanceCounts[material.CardID]
		if count <= 0 || slots > count {
			return nil, errors.New("invalid evolution material slots")
		}
		uses = append(uses, evolutionMaterialUse{id: material.CardID, num: count})
		delete(selectedSlots, material.CardID)
	}
	if len(selectedSlots) != 0 {
		return nil, errors.New("evolution material is not in the recipe")
	}
	return uses, nil
}

func (s *Account) SetDecks(incoming []DeckInfo) ([]CardInfo, []DeckInfo, error) {
	if len(incoming) == 0 {
		return nil, nil, errors.New("decks must not be empty")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.refreshCardFameTrainingLocked(time.Now())
	unlockedRank := s.highestDeckRank
	inventory := s.rankInventoryLocked()
	owned := ownedCardIDs(s.cards)
	for _, deck := range incoming {
		if err := s.validateDeck(deck, owned); err != nil {
			return nil, nil, err
		}
		// The client batches the edited deck and any newly opened empty slot.
		// Compute its earned rank before checking the other slots, without
		// committing rank or decks until the whole request has been validated.
		if len(s.deckRankPolicy.ParameterThresholds) > 0 && deck.ArthurType <= 4 && s.highestDeckRank >= requiredDeckRank(deck.Index) {
			unlockedRank = max(unlockedRank, int(s.deckRankLocked(deck, inventory)))
		}
	}
	for _, deck := range incoming {
		if len(s.deckRankPolicy.ParameterThresholds) > 0 && deck.ArthurType <= 4 && unlockedRank < requiredDeckRank(deck.Index) {
			return nil, nil, errors.New("deck slot is locked by highest deck rank")
		}
	}
	for _, deck := range incoming {
		replaced := false
		for index := range s.decks {
			if s.decks[index].ArthurType == deck.ArthurType &&
				s.decks[index].Index == deck.Index {
				s.decks[index] = cloneDeck(deck)
				replaced = true
				break
			}
		}
		if !replaced {
			s.decks = append(s.decks, cloneDeck(deck))
		}
	}
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "deck"}); err != nil {
		return nil, nil, err
	}
	s.refreshDeckRanksLocked()
	return CloneCards(s.cards), s.rankedDecksLocked(s.decks), nil
}

func (s *Account) validateDeck(deck DeckInfo, owned map[int64]int) error {
	if err := s.validateStoredDeck(deck, owned); err != nil {
		return err
	}
	return s.validateDeckComposition(deck, owned, 0, 0, deck.IsActive != 0)
}

// Loading checks shape, ownership and card families. Current composition rules
// apply when saving, generating replacements or admitting a battle, so a saved
// deck can still be opened and corrected without preventing account login.
func (s *Account) validateStoredDeck(deck DeckInfo, owned map[int64]int) error {
	if deck.ArthurType < 1 || deck.ArthurType > 5 {
		return errors.New("arthur_type must be 1 through 5")
	}
	if deck.Index < 0 {
		return errors.New("idx must be non-negative")
	}
	if deck.LeaderCardIndex < 0 ||
		int(deck.LeaderCardIndex) >= s.deckSlots {
		return errors.New("leader_card_idx is outside the deck")
	}
	if len(deck.CardUniqueIDs) != s.deckSlots {
		return fmt.Errorf("card_uniqid must contain %d entries", s.deckSlots)
	}
	if len(deck.SupportCardUniqueIDs) != s.supportSlotCapacity {
		return fmt.Errorf(
			"support_card_uniqid must contain %d entries",
			s.supportSlotCapacity,
		)
	}
	if deck.ArthurType >= 1 && deck.ArthurType <= 4 {
		unlocked := int(s.supportUnlockedSlots[deck.ArthurType-1])
		for index := unlocked; index < len(deck.SupportCardUniqueIDs); index++ {
			if deck.SupportCardUniqueIDs[index] != 0 {
				return fmt.Errorf("support card slot %d is locked for Arthur %d", index+1, deck.ArthurType)
			}
		}
	}
	if len(deck.BuddyUniqueIDs) != s.buddySlots {
		return fmt.Errorf("buddy_uniqid must contain %d entries", s.buddySlots)
	}
	if deck.BuddyUniqueIDs[0] == 0 {
		for _, uniqueID := range deck.BuddyUniqueIDs[1:] {
			if uniqueID != 0 {
				return errors.New("non-zero buddy entries require a non-zero leader")
			}
		}
	}
	if err := s.validateDeckSpheres(deck); err != nil {
		return err
	}
	if err := s.validateDeckBuddies(deck); err != nil {
		return err
	}
	for _, uniqueID := range deck.CardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		if _, exists := owned[uniqueID]; !exists {
			return fmt.Errorf("unknown card unique ID %d", uniqueID)
		}
	}
	for _, uniqueID := range deck.SupportCardUniqueIDs {
		if uniqueID == 0 {
			continue
		}
		if _, exists := owned[uniqueID]; !exists {
			return fmt.Errorf("unknown support card unique ID %d", uniqueID)
		}
	}
	return s.validateDeckFamilies(deck, owned, 0, 0)
}

func deckContainsCardUniqueID(deck DeckInfo, uniqueID int64) bool {
	for _, candidate := range deck.CardUniqueIDs {
		if candidate == uniqueID {
			return true
		}
	}
	for _, candidate := range deck.SupportCardUniqueIDs {
		if candidate == uniqueID {
			return true
		}
	}
	return false
}

// validateDeckFamilies mirrors the client's DeckUtility.IsValidDeck
// family checks. Main slots compare same_cardid, support slots compare
// same_supportcardid, and a support/main pair conflicts only when both family
// IDs match. replacementUniqueID/replacementCardID lets evolution validate
// the target definition before any inventory, gold, or material mutation.
func (s *Account) validateDeckFamilies(
	deck DeckInfo,
	cardIDsByUniqueID map[int64]int,
	replacementUniqueID int64,
	replacementCardID int,
) error {
	definitionFor := func(uniqueID int64) (gamestate.Card, error) {
		cardID, exists := cardIDsByUniqueID[uniqueID]
		if !exists {
			return gamestate.Card{}, fmt.Errorf("unknown card unique ID %d", uniqueID)
		}
		if uniqueID == replacementUniqueID {
			cardID = replacementCardID
		}
		definition, exists := s.cardDefinitions[cardID]
		if !exists {
			return gamestate.Card{}, fmt.Errorf("card definition %d is unavailable", cardID)
		}
		return definition, nil
	}
	collect := func(uniqueIDs []int64) ([]gamestate.Card, error) {
		definitions := make([]gamestate.Card, 0, len(uniqueIDs))
		for _, uniqueID := range uniqueIDs {
			if uniqueID == 0 {
				continue
			}
			definition, err := definitionFor(uniqueID)
			if err != nil {
				return nil, err
			}
			definitions = append(definitions, definition)
		}
		return definitions, nil
	}
	mainCards, err := collect(deck.CardUniqueIDs)
	if err != nil {
		return err
	}
	supportCards, err := collect(deck.SupportCardUniqueIDs)
	if err != nil {
		return err
	}
	for left := range mainCards {
		for right := left + 1; right < len(mainCards); right++ {
			if mainCards[left].SameCardID == mainCards[right].SameCardID {
				return fmt.Errorf("main cards %d and %d share family %d",
					mainCards[left].CardID, mainCards[right].CardID, mainCards[left].SameCardID)
			}
		}
	}
	for left := range supportCards {
		for right := left + 1; right < len(supportCards); right++ {
			if supportCards[left].SameSupportCardID == supportCards[right].SameSupportCardID {
				return fmt.Errorf("support cards %d and %d share family %d",
					supportCards[left].CardID, supportCards[right].CardID,
					supportCards[left].SameSupportCardID)
			}
		}
	}
	for _, support := range supportCards {
		for _, main := range mainCards {
			if support.SameCardID == main.SameCardID &&
				support.SameSupportCardID == main.SameSupportCardID {
				return fmt.Errorf("support card %d conflicts with main card %d in family %d/%d",
					support.CardID, main.CardID, support.SameCardID, support.SameSupportCardID)
			}
		}
	}
	return nil
}

func CloneCards(cards []CardInfo) []CardInfo {
	result := append([]CardInfo(nil), cards...)
	for index := range result {
		result[index] = cloneCard(result[index])
	}
	return result
}

func cloneCard(card CardInfo) CardInfo {
	card.SkillLevels = append([]int16(nil), card.SkillLevels...)
	return card
}

func CloneDecks(decks []DeckInfo) []DeckInfo {
	result := make([]DeckInfo, len(decks))
	for index, deck := range decks {
		result[index] = cloneDeck(deck)
	}
	return result
}

func cloneDeck(deck DeckInfo) DeckInfo {
	deck.CardUniqueIDs = append([]int64(nil), deck.CardUniqueIDs...)
	deck.SupportCardUniqueIDs = append(
		[]int64(nil),
		deck.SupportCardUniqueIDs...,
	)
	deck.SphereUniqueIDs = fixedInt64Slots(deck.SphereUniqueIDs, DeckSphereSlots)
	deck.BuddyUniqueIDs = fixedInt64Slots(deck.BuddyUniqueIDs, deckBuddySlots)
	return deck
}

func fixedInt64Slots(values []int64, size int) []int64 {
	result := make([]int64, size)
	copy(result, values)
	return result
}

// Selection-local lookup avoids rescanning a large inventory for every material.
func indexCardSelection(cards []CardInfo, ids []int64) map[int64]int {
	positions := make(map[int64]int, len(ids))
	for _, id := range ids {
		positions[id] = -1
	}
	remaining := len(positions)
	for index, card := range cards {
		if remaining == 0 {
			break
		}
		if _, selected := positions[card.UniqueID]; selected {
			positions[card.UniqueID] = index
			remaining--
		}
	}
	return positions
}
