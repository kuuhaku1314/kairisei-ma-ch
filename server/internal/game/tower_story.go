package game

import (
	"errors"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) clearSubStoryContextLocked() {
	s.activeSubStoryID = 0
	s.activeSubStoryTowerID = 0
	s.activeSubStoryTowerFloor = 0
}

// Tower stories use the same persisted reading session as ordinary sub stories,
// while floor access and first-clear rewards belong to the tower progression.
func (s *Account) BeginTowerStory(storyID, towerID, floorNumber int) (bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, found := s.towerQuestProfiles[towerID]
	progress, hasProgress := s.towerQuestProgress[towerID]
	if !found || !hasProgress ||
		floorNumber < 1 || floorNumber > len(profile.Floors) {
		return false, nil
	}
	floor := profile.Floors[floorNumber-1]
	if storyID <= 0 || floor.Floor != floorNumber || floor.Story.TalkID != storyID ||
		!towerFloorAccessible(progress, floorNumber) {
		return false, nil
	}
	if s.activeSubStoryID == storyID && s.activeSubStoryTowerID == towerID && s.activeSubStoryTowerFloor == floorNumber {
		return true, nil
	}
	if !s.towerOpenLocked(towerID) {
		return false, &BusinessError{Code: -1, Message: "炼金副本暂未开放。"}
	}
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	paid, err := s.spendTowerFloorEntryLocked(floor, now)
	if err != nil {
		return false, err
	}
	if !paid {
		return false, &BusinessError{-1, "体力不足，无法进入剧情。"}
	}
	s.activeSubStoryID, s.activeSubStoryTowerID, s.activeSubStoryTowerFloor = storyID, towerID, floorNumber
	s.activeMainStoryID, s.activeMainStoryCN = 0, false
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
	return true, nil
}

func (s *Account) endTowerStoryLocked(isClear bool) (PresentReceiveResult, error) {
	profile, found := s.towerQuestProfiles[s.activeSubStoryTowerID]
	progress, hasProgress := s.towerQuestProgress[s.activeSubStoryTowerID]
	number := s.activeSubStoryTowerFloor
	if !found || !hasProgress || number < 1 || number > len(profile.Floors) ||
		profile.Floors[number-1].Story.TalkID != s.activeSubStoryID {
		return PresentReceiveResult{}, errors.New("active tower story is unavailable")
	}
	result := PresentReceiveResult{}
	if isClear && !slices.Contains(progress.ClearedFloors, number) {
		if progress.Floor != number {
			return result, errors.New("active tower story is not the current floor")
		}
		rewards := profile.Floors[number-1].ClearRewards
		if err := s.validateSettlementRewardsLocked(rewards); err != nil {
			return result, err
		}
		for _, reward := range rewards {
			if err := s.applySettlementRewardLocked(reward, &result); err != nil {
				return PresentReceiveResult{}, err
			}
		}
		if err := s.settleTowerQuestLocked(TeamBattleContext{TowerID: profile.TowerID, TowerFloor: number}, true); err != nil {
			return PresentReceiveResult{}, err
		}
	}
	s.clearSubStoryContextLocked()
	return result, nil
}
