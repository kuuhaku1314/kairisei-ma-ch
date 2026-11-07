package game

import (
	"encoding/json"
	"errors"
	"maps"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) TowerQuestShowState(
	towerID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestProgress, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{}, false,
			errors.New("tower quest is unavailable")
	}
	if !s.towerOpenLocked(towerID) {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{}, false,
			&BusinessError{Code: -1, Message: "炼金副本暂未开放。"}
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{}, false,
			errors.New("tower quest progress is unavailable")
	}
	responseProgress := cloneTowerQuestProgress(progress)
	consumed := progress.LastResult != ""
	if consumed {
		progress.LastResult = ""
		progress.LastRankUp = false
		progress.LastResultLoseCount = 0
		s.towerQuestProgress[towerID] = progress
	}
	return cloneTowerQuestProfile(profile), responseProgress, consumed, nil
}

func (s *Account) TowerQuestRankingState(
	towerID int,
) (gamestate.TowerQuestProfile, gamestate.TowerQuestProgress, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	profile, exists := s.towerQuestProfiles[towerID]
	if !exists {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{},
			errors.New("tower quest is unavailable")
	}
	if !s.towerOpenLocked(towerID) {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{},
			&BusinessError{Code: -1, Message: "炼金副本暂未开放。"}
	}
	progress, exists := s.towerQuestProgress[towerID]
	if !exists {
		return gamestate.TowerQuestProfile{}, gamestate.TowerQuestProgress{},
			errors.New("tower quest progress is unavailable")
	}
	return cloneTowerQuestProfile(profile), cloneTowerQuestProgress(progress), nil
}

func (s *Account) TowerQuestIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	result := make([]int, 0, len(s.towerQuestProfiles))
	for towerID := range s.towerQuestProfiles {
		if !s.towerOpenLocked(towerID) {
			continue
		}
		result = append(result, towerID)
	}
	sort.Ints(result)
	return result
}

func (s *Account) ShowCards() []CardInfo {
	cards, _ := s.Show()
	return cards
}

func cloneTowerQuestProfile(source gamestate.TowerQuestProfile) gamestate.TowerQuestProfile {
	result := source
	result.Ranks = append([]gamestate.TowerQuestRankProfile(nil), source.Ranks...)
	result.Floors = make([]gamestate.TowerQuestFloorProfile, len(source.Floors))
	for index, floor := range source.Floors {
		floor.Boss = append(json.RawMessage(nil), floor.Boss...)
		floor.ClearRewards = cloneRewards(floor.ClearRewards)
		result.Floors[index] = floor
	}
	return result
}

func cloneTowerQuestProgress(source gamestate.TowerQuestProgress) gamestate.TowerQuestProgress {
	result := source
	result.ClearedFloors = append([]int(nil), source.ClearedFloors...)
	result.ClearRatings = maps.Clone(source.ClearRatings)
	result.BestClearTurns = maps.Clone(source.BestClearTurns)
	return result
}
