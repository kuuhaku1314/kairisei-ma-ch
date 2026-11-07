package game

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func cloneStoryMainParts(parts []gamestate.StoryMainPart) []gamestate.StoryMainPart {
	cloned := make([]gamestate.StoryMainPart, len(parts))
	for partIndex, part := range parts {
		cloned[partIndex] = part
		cloned[partIndex].Sections = make([]gamestate.StoryMainSection, len(part.Sections))
		for sectionIndex, section := range part.Sections {
			cloned[partIndex].Sections[sectionIndex] = section
			cloned[partIndex].Sections[sectionIndex].Stories = append(
				[]gamestate.StoryMain(nil),
				section.Stories...,
			)
		}
	}
	return cloned
}

func cloneStorySubCharacters(characters []gamestate.StorySubCharacter) []gamestate.StorySubCharacter {
	cloned := make([]gamestate.StorySubCharacter, len(characters))
	for characterIndex, character := range characters {
		cloned[characterIndex] = character
		cloned[characterIndex].Sections = make([]gamestate.StorySubSection, len(character.Sections))
		for sectionIndex, section := range character.Sections {
			cloned[characterIndex].Sections[sectionIndex] = section
			cloned[characterIndex].Sections[sectionIndex].Stories = make([]gamestate.StorySub, len(section.Stories))
			for storyIndex, story := range section.Stories {
				cloned[characterIndex].Sections[sectionIndex].Stories[storyIndex] = story
				cloned[characterIndex].Sections[sectionIndex].Stories[storyIndex].FeatureReward.CardSkillLevels = append(
					[]int16{},
					story.FeatureReward.CardSkillLevels...,
				)
			}
		}
	}
	return cloned
}

func cloneStoryEvents(events []gamestate.StoryEvent) []gamestate.StoryEvent {
	cloned := make([]gamestate.StoryEvent, len(events))
	for eventIndex, event := range events {
		cloned[eventIndex] = event
		cloned[eventIndex].Stories = make([]gamestate.StorySubEvent, len(event.Stories))
		for storyIndex, wrapper := range event.Stories {
			cloned[eventIndex].Stories[storyIndex] = wrapper
			cloned[eventIndex].Stories[storyIndex].Sub.FeatureReward.CardSkillLevels = append(
				[]int16{},
				wrapper.Sub.FeatureReward.CardSkillLevels...,
			)
			cloned[eventIndex].Stories[storyIndex].Materials = append(
				[]gamestate.StoryUnlockMaterial{},
				wrapper.Materials...,
			)
		}
	}
	return cloned
}

func (s *Account) StoryMainState(cnStory bool) []gamestate.StoryMainPart {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !cnStory {
		return cloneStoryMainParts(s.storyMainParts)
	}
	// The theater uses the last returned episode as the current story and
	// removes it from recollection until it is cleared.
	parts := cloneStoryMainParts(s.cnStoryMainParts)
	for i := range parts {
		sections := parts[i].Sections
		for j := range sections {
			sections[j].Stories = slices.DeleteFunc(sections[j].Stories, func(story gamestate.StoryMain) bool {
				return story.StateFlag&4 != 0
			})
		}
		parts[i].Sections = slices.DeleteFunc(sections, func(section gamestate.StoryMainSection) bool {
			return len(section.Stories) == 0
		})
	}
	return slices.DeleteFunc(parts, func(part gamestate.StoryMainPart) bool {
		return len(part.Sections) == 0
	})
}

func (s *Account) CNMainStoryAvailable() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, part := range s.cnStoryMainParts {
		for _, section := range part.Sections {
			if len(section.Stories) > 0 {
				return true
			}
		}
	}
	return false
}

func (s *Account) BeginMainStory(storyMainID int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	for cnStory, parts := range map[bool][]gamestate.StoryMainPart{false: s.storyMainParts, true: s.cnStoryMainParts} {
		for _, part := range parts {
			for _, section := range part.Sections {
				for _, story := range section.Stories {
					if story.StoryMainID == storyMainID {
						if story.StateFlag&(1<<2) != 0 {
							return false
						}
						s.activeMainStoryID = storyMainID
						s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
						s.activeMainStoryCN = cnStory
						s.clearSubStoryContextLocked()
						return true
					}
				}
			}
		}
	}
	return false
}

func (s *Account) applyStoryFirstClearRewardLocked(
	reward gamestate.Reward,
) (PresentReceiveResult, error) {
	result := PresentReceiveResult{}
	if err := s.validateSettlementRewardsLocked([]gamestate.Reward{reward}); err != nil {
		return PresentReceiveResult{}, err
	}
	if err := s.applySettlementRewardLocked(reward, &result); err != nil {
		return PresentReceiveResult{}, err
	}
	return result, nil
}

func (s *Account) EndMainStory(isClear bool) (PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeMainStoryID == 0 {
		return PresentReceiveResult{}, errors.New("no active main story")
	}
	activeID := s.activeMainStoryID
	activeCN := s.activeMainStoryCN
	parts := &s.storyMainParts
	if activeCN {
		parts = &s.cnStoryMainParts
	}
	result := PresentReceiveResult{}
	found := false
	activeChain := 0
	for partIndex := range *parts {
		chain := MainStoryProgressionChain((*parts)[partIndex].StoryMainPartID)
		if found && chain != activeChain {
			continue
		}
		for sectionIndex := range (*parts)[partIndex].Sections {
			stories := (*parts)[partIndex].Sections[sectionIndex].Stories
			for storyIndex := range stories {
				if !found && stories[storyIndex].StoryMainID != activeID {
					continue
				}
				if !found {
					activeChain = chain
					if isClear {
						if stories[storyIndex].StateFlag&2 == 0 {
							var err error
							result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.MainFirstClear)
							if err != nil {
								return PresentReceiveResult{}, fmt.Errorf("apply main story first-clear reward: %w", err)
							}
						}
						stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 1<<1
						s.markMissionMetricLocked(gamestate.MissionStoryCleared)
						stories[storyIndex].UnlockText = ""
						if err := s.advanceOnboardingLocked(onboardingEvent{kind: "story"}); err != nil {
							return PresentReceiveResult{}, err
						}
					}
					found = true
					if !isClear {
						s.activeMainStoryID = 0
						s.activeMainStoryCN = false
						return result, nil
					}
					continue
				}
				if stories[storyIndex].StateFlag&2 == 0 {
					stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 1
					stories[storyIndex].UnlockText = ""
				}
				s.activeMainStoryID = 0
				s.activeMainStoryCN = false
				return result, nil
			}
		}
	}
	if !found {
		return PresentReceiveResult{}, errors.New("active main story is absent from the catalog")
	}
	s.activeMainStoryID = 0
	s.activeMainStoryCN = false
	return result, nil
}

func (s *Account) StorySubState() []gamestate.StorySubCharacter {
	s.mu.Lock()
	defer s.mu.Unlock()
	gamestate.UnlockCollectedCharacterStories(s.storySubCharacters, s.cardCollectionIDs)
	characters := cloneStorySubCharacters(s.storySubCharacters)
	visible := characters[:0]
	for i := range characters {
		sections := characters[i].Sections[:0]
		for _, section := range characters[i].Sections {
			if len(section.Stories) == 0 {
				continue
			}
			for j := range section.Stories {
				story := &section.Stories[j]
				if quest, step, learning := gamestate.FindBurstStory(story.StorySubID); learning {
					story.StateFlag &= 8
					switch {
					case step < int(s.burstProgress[quest.ArthurType-1]):
						story.StateFlag |= 2
						story.UnlockText = ""
					case !s.burstQuestAvailableLocked(quest):
						story.StateFlag |= 4
						story.UnlockText = "完成训练并开放对应职业后解锁。"
					case step > int(s.burstProgress[quest.ArthurType-1]):
						story.StateFlag |= 4
						story.UnlockText = "完成前一段圣剑解放剧情后解锁。"
					default:
						story.StateFlag |= 1
						story.UnlockText = ""
					}
				}
			}
			sections = append(sections, section)
		}
		if len(sections) == 0 {
			continue
		}
		characters[i].Sections = sections
		visible = append(visible, characters[i])
	}
	return visible
}

func (s *Account) StoryEventState() []gamestate.StoryEvent {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneStoryEvents(s.storyEvents)
}

func (s *Account) AuthorizeStoryBattle(
	storyBattleID int,
	deckArthurType int8,
	deckArthurTypeIndexes []int8,
) error {
	if storyBattleID <= 0 || deckArthurType < 1 || deckArthurType > 4 || len(deckArthurTypeIndexes) != 4 {
		return errors.New("invalid story fixed-battle request")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	storyKey := ""
	if s.storyTeamBattleSession.StoryID != 0 {
		quest, step, ok := gamestate.FindBurstStory(s.storyTeamBattleSession.StoryID)
		if !ok || step != 2 || storyBattleID != quest.StoryIDs[2] {
			return errors.New("story fixed battle differs from active learning script")
		}
		storyKey = fmt.Sprintf("sub:%d", quest.StoryIDs[2])
	}
	if s.activeMainStoryID != 0 {
		storyKind := "main"
		if s.activeMainStoryCN {
			storyKind = "cn_main"
		}
		storyKey = fmt.Sprintf("%s:%d", storyKind, s.activeMainStoryID)
	} else if s.activeSubStoryID != 0 {
		storyKey = fmt.Sprintf("sub:%d", s.activeSubStoryID)
	}
	allowed, active := s.storyBattleIDsByStory[storyKey]
	if !active {
		return errors.New("story fixed battle has no active script owner")
	}
	if _, exists := allowed[storyBattleID]; !exists {
		return errors.New("story fixed battle differs from active script")
	}
	for index, deckIndex := range deckArthurTypeIndexes {
		if deckIndex < -1 {
			return errors.New("story fixed-battle deck index is invalid")
		}
		if deckIndex < 0 {
			if int8(index+1) == deckArthurType {
				return errors.New("selected Arthur has no active story deck")
			}
			continue
		}
		found := false
		for _, deck := range s.decks {
			if deck.ArthurType == int8(index+1) && deck.Index == deckIndex {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("story fixed-battle deck %d/%d is unknown", index+1, deckIndex)
		}
	}
	return nil
}

func (s *Account) UnlockEventStory(storySubID int) ([]gamestate.Item, []gamestate.StoryEvent, error) {
	if storySubID <= 0 {
		return nil, nil, errors.New("invalid event story ID")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	eventIndex := -1
	storyIndex := -1
	var materials []gamestate.StoryUnlockMaterial
	for candidateEventIndex := range s.storyEvents {
		for candidateStoryIndex := range s.storyEvents[candidateEventIndex].Stories {
			wrapper := s.storyEvents[candidateEventIndex].Stories[candidateStoryIndex]
			if wrapper.Sub.StorySubID != storySubID {
				continue
			}
			if wrapper.Sub.StateFlag&16 == 0 || len(wrapper.Materials) == 0 {
				return nil, nil, errors.New("event story has no published material unlock")
			}
			eventIndex = candidateEventIndex
			storyIndex = candidateStoryIndex
			materials = append([]gamestate.StoryUnlockMaterial(nil), wrapper.Materials...)
			break
		}
	}
	if eventIndex < 0 || storyIndex < 0 {
		return nil, nil, errors.New("unknown event story ID")
	}
	maximum := int(^uint(0) >> 1)
	goldCost := 0
	coinCost := 0
	secureCoinCost := 0
	itemCosts := make(map[int]int)
	addCost := func(current int, added int) (int, error) {
		if added <= 0 || current > maximum-added {
			return 0, errors.New("event story unlock cost is invalid")
		}
		return current + added, nil
	}
	for _, material := range materials {
		var err error
		switch material.PayType {
		case 1:
			goldCost, err = addCost(goldCost, material.Price)
		case 3:
			coinCost, err = addCost(coinCost, material.Price)
		case 4:
			if material.PayTypeID <= 0 {
				return nil, nil, errors.New("event story unlock item is invalid")
			}
			itemCosts[material.PayTypeID], err = addCost(itemCosts[material.PayTypeID], material.Price)
		case 6:
			secureCoinCost, err = addCost(secureCoinCost, material.Price)
		default:
			return nil, nil, fmt.Errorf("unsupported event story pay type %d", material.PayType)
		}
		if err != nil {
			return nil, nil, err
		}
	}
	if s.gold < goldCost {
		return nil, nil, ErrInsufficientGold
	}
	if s.coin < secureCoinCost {
		return nil, nil, errInsufficientPaidCrystals
	}
	if coinCost > s.coinFree && s.coin-secureCoinCost < coinCost-s.coinFree {
		return nil, nil, ErrInsufficientCrystals
	}
	now := time.Now().Unix()
	for itemID, cost := range itemCosts {
		item, exists := s.items[itemID]
		if !exists || item.Num < cost || item.LimitTime > 0 && now >= int64(item.LimitTime) {
			return nil, nil, ErrInsufficientMaterials
		}
	}

	s.gold -= goldCost
	s.coin -= secureCoinCost
	freeUse := coinCost
	if freeUse > s.coinFree {
		freeUse = s.coinFree
	}
	s.coinFree -= freeUse
	s.coin -= coinCost - freeUse
	changedItems := make([]gamestate.Item, 0, len(itemCosts))
	for itemID, cost := range itemCosts {
		item := s.items[itemID]
		item.Num -= cost
		s.items[itemID] = item
		changedItems = append(changedItems, item)
	}
	sort.Slice(changedItems, func(left, right int) bool {
		return changedItems[left].ItemID < changedItems[right].ItemID
	})
	story := &s.storyEvents[eventIndex].Stories[storyIndex].Sub
	story.StateFlag = story.StateFlag&8 | 1
	story.UnlockText = ""
	return changedItems, cloneStoryEvents(s.storyEvents), nil
}

func (s *Account) BeginSubStory(storySubID int) bool {
	if _, _, learning := gamestate.FindBurstStory(storySubID); learning {
		return s.BeginBurstStory(storySubID) == nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	gamestate.UnlockCollectedCharacterStories(s.storySubCharacters, s.cardCollectionIDs)
	for _, character := range s.storySubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				if story.StorySubID != storySubID {
					continue
				}
				if story.StateFlag&4 != 0 {
					return false
				}
				s.activeSubStoryID = storySubID
				s.activeSubStoryTowerID, s.activeSubStoryTowerFloor = 0, 0
				s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
				s.activeMainStoryID = 0
				s.activeMainStoryCN = false
				return true
			}
		}
	}
	for _, event := range s.storyEvents {
		for _, wrapper := range event.Stories {
			if wrapper.Sub.StorySubID != storySubID {
				continue
			}
			if wrapper.Sub.StateFlag&(4|16) != 0 {
				return false
			}
			s.activeSubStoryID = storySubID
			s.activeSubStoryTowerID, s.activeSubStoryTowerFloor = 0, 0
			s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
			s.activeMainStoryID = 0
			s.activeMainStoryCN = false
			return true
		}
	}
	return false
}

func (s *Account) EndSubStory(isClear bool) (PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.storyTeamBattleSession.StoryID != 0 {
		if !isClear {
			s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
			return PresentReceiveResult{}, nil
		}
		return s.endBurstStoryLocked()
	}
	if s.activeSubStoryID == 0 {
		return PresentReceiveResult{}, errors.New("no active sub story")
	}
	if s.activeSubStoryTowerID != 0 {
		return s.endTowerStoryLocked(isClear)
	}
	activeID := s.activeSubStoryID
	for characterIndex := range s.storySubCharacters {
		for sectionIndex := range s.storySubCharacters[characterIndex].Sections {
			stories := s.storySubCharacters[characterIndex].Sections[sectionIndex].Stories
			for storyIndex := range stories {
				if stories[storyIndex].StorySubID != activeID {
					continue
				}
				if !isClear {
					s.activeSubStoryID = 0
					return PresentReceiveResult{}, nil
				}
				result := PresentReceiveResult{}
				if stories[storyIndex].StateFlag&2 == 0 {
					var err error
					result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.SubFirstClear)
					if err != nil {
						return PresentReceiveResult{}, fmt.Errorf("apply character story first-clear reward: %w", err)
					}
				}
				stories[storyIndex].StateFlag = stories[storyIndex].StateFlag&8 | 2
				s.markMissionMetricLocked(gamestate.MissionStoryCleared)
				stories[storyIndex].UnlockText = ""
				if storyIndex+1 < len(stories) && stories[storyIndex+1].StateFlag&2 == 0 {
					stories[storyIndex+1].StateFlag = stories[storyIndex+1].StateFlag&8 | 1
					stories[storyIndex+1].UnlockText = ""
				}
				s.activeSubStoryID = 0
				return result, nil
			}
		}
	}
	for eventIndex := range s.storyEvents {
		stories := s.storyEvents[eventIndex].Stories
		for storyIndex := range stories {
			if stories[storyIndex].Sub.StorySubID != activeID {
				continue
			}
			if !isClear {
				s.activeSubStoryID = 0
				return PresentReceiveResult{}, nil
			}
			result := PresentReceiveResult{}
			if stories[storyIndex].Sub.StateFlag&2 == 0 {
				var err error
				result, err = s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.EventFirstClear)
				if err != nil {
					return PresentReceiveResult{}, fmt.Errorf("apply event story first-clear reward: %w", err)
				}
			}
			stories[storyIndex].Sub.StateFlag = stories[storyIndex].Sub.StateFlag&8 | 2
			s.markMissionMetricLocked(gamestate.MissionStoryCleared)
			stories[storyIndex].Sub.UnlockText = ""
			if storyIndex+1 < len(stories) && stories[storyIndex+1].Sub.StateFlag&2 == 0 {
				nextStatic := stories[storyIndex+1].Sub.StateFlag & (8 | 16)
				if nextStatic&16 != 0 {
					stories[storyIndex+1].Sub.StateFlag = nextStatic
					stories[storyIndex+1].Sub.UnlockText = "需要活动材料解锁"
				} else {
					stories[storyIndex+1].Sub.StateFlag = nextStatic | 1
					stories[storyIndex+1].Sub.UnlockText = ""
				}
			}
			s.activeSubStoryID = 0
			return result, nil
		}
	}
	return PresentReceiveResult{}, errors.New("active sub story is absent from the catalog")
}
