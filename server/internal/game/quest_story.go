package game

import (
	"encoding/json"
	"errors"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

var ErrNoActiveQuestStory = errors.New("no active quest reading story")

func (s *Account) questStoryClearedIDsLocked() []int {
	ids := make([]int, 0, len(s.questStoryCleared))
	for id := range s.questStoryCleared {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids
}

func (s *Account) QuestStoryClearedIDs() []int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.questStoryClearedIDsLocked()
}

func (s *Account) findQuestStoryLocked(id int) bool {
	for _, section := range s.questStorySections {
		for _, group := range section.BossGroups {
			for _, story := range group.Stories {
				if story.StoryTeamBattleID == id {
					return true
				}
			}
		}
	}
	return false
}

func (s *Account) BeginQuestStory(id int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.findQuestStoryLocked(id) {
		return errors.New("unknown quest reading story")
	}
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{StoryID: id}
	s.activeMainStoryID, s.activeSubStoryID = 0, 0
	s.activeMainStoryCN = false
	return nil
}

func (s *Account) EndQuestStory() (PresentReceiveResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	id := s.storyTeamBattleSession.StoryID
	if !s.findQuestStoryLocked(id) {
		return PresentReceiveResult{}, ErrNoActiveQuestStory
	}
	if s.questStoryCleared[id] {
		return PresentReceiveResult{}, nil
	}
	result, err := s.applyStoryFirstClearRewardLocked(s.storyRewardPolicy.MainFirstClear)
	if err != nil {
		return PresentReceiveResult{}, err
	}
	s.questStoryCleared[id] = true
	s.markMissionMetricLocked(gamestate.MissionStoryCleared)
	return result, nil
}

func (s *Account) ClearStoryBattleSession() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storyTeamBattleSession = gamestate.StoryTeamBattleSession{}
}

func (s *Account) QuestStoryResponse() json.RawMessage {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return append(json.RawMessage(nil), s.storyTeamBattleSession.Response...)
}
