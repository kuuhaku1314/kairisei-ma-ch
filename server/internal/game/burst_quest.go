package game

import (
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

var ErrNoActiveBurstStory = errors.New("no active sword-release story")

// LOCAL_POLICY: finishing all nine training steps opens all four swords.
// Learning stories remain optional. The feature bits also serve as the
// one-time receipt for each initial Buddy, including after selling it.
func (s *Account) completeTrainingBurstLocked() error {
	if s.onboarding.Step != gamestate.OnboardingStepCount {
		return nil
	}
	pending := []gamestate.BurstQuest{}
	rewards := []gamestate.Reward{}
	for _, quest := range gamestate.BurstQuests() {
		if _, done := s.unlockedFeatureIDs[uint(32+quest.ArthurType)]; done {
			continue
		}
		pending = append(pending, quest)
		owned := false
		for _, buddy := range s.buddies {
			if buddy.BuddyID == quest.BuddyID {
				owned = true
				break
			}
		}
		// Non-CN/minimal catalogs need not contain the four CN learning Buddies.
		if _, present := s.buddyDefinitions[quest.BuddyID]; present && !owned {
			rewards = append(rewards, gamestate.Reward{Type: 19, Num: 1, RewardTypeID: quest.BuddyID, CardSkillLevels: []int16{}})
		}
	}
	for _, reward := range rewards {
		if err := s.validateRewardLocked(reward); err != nil {
			return err
		}
	}
	for _, reward := range rewards {
		if err := s.applyRewardOrPresentLocked(reward, &PresentReceiveResult{}, "圣剑解放奖励"); err != nil {
			return err
		}
	}
	for _, quest := range pending {
		s.unlockedFeatureIDs[uint(32+quest.ArthurType)] = struct{}{}
		var uniqueID int64
		for _, buddy := range s.buddies {
			if buddy.BuddyID == quest.BuddyID {
				uniqueID = buddy.UniqueID
				break
			}
		}
		if uniqueID == 0 {
			continue
		}
		for i := range s.decks {
			deck := &s.decks[i]
			if deck.ArthurType == quest.ArthurType && len(deck.BuddyUniqueIDs) > 0 && deck.BuddyUniqueIDs[0] == 0 {
				deck.BuddyUniqueIDs[0] = uniqueID
			}
		}
	}
	return nil
}

func (s *Account) ArthurBurstUnlocked(arthur int8) int8 {
	if arthur < 1 || arthur > 4 {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, unlocked := s.unlockedFeatureIDs[uint(32+arthur)]
	return int8(BoolInt(unlocked))
}

func (s *Account) burstQuestAvailableLocked(quest gamestate.BurstQuest) bool {
	_, profession := s.unlockedFeatureIDs[uint(quest.ArthurType-1)]
	_, buddy := s.buddyDefinitions[quest.BuddyID]
	_, script := s.storyBattleIDsByStory[fmt.Sprintf("sub:%d", quest.StoryIDs[2])]
	return profession && buddy && script && s.onboarding.Step >= gamestate.OnboardingStepCount
}

func (s *Account) BeginBurstStory(storyID int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	quest, step, exists := gamestate.FindBurstStory(storyID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return errors.New("unknown or locked sword-release story")
	}
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{StoryID: storyID}
	s.activeMainStoryID, s.activeSubStoryID = 0, 0
	s.activeMainStoryCN = false
	return nil
}

// Learning is optional; the training flow owns all initial sword grants.
func (s *Account) endBurstStoryLocked() (PresentReceiveResult, error) {
	quest, step, exists := gamestate.FindBurstStory(s.storyTeamBattleSession.StoryID)
	if !exists || !s.burstQuestAvailableLocked(quest) || step > int(s.burstProgress[quest.ArthurType-1]) {
		return PresentReceiveResult{}, ErrNoActiveBurstStory
	}
	if step < int(s.burstProgress[quest.ArthurType-1]) {
		return PresentReceiveResult{}, nil
	}
	result, err := s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.SubFirstClear)
	if err != nil {
		return PresentReceiveResult{}, err
	}
	s.burstProgress[quest.ArthurType-1] = uint8(step + 1)
	s.markMissionMetricLocked(gamestate.MissionStoryCleared)
	return result, nil
}
