package game

import (
	"encoding/json"
	"errors"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) ExploreIsActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exploreActive
}

func (s *Account) ExploreResultReceipt() (json.RawMessage, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastExploreResultReceipt == nil {
		return nil, false
	}
	return append(json.RawMessage(nil), s.lastExploreResultReceipt.Response...), true
}

func (s *Account) RecordExploreResultReceipt(
	startedAtUnix int64,
	response json.RawMessage,
	claimedAt time.Time,
) error {
	if startedAtUnix <= 0 || claimedAt.IsZero() || claimedAt.Unix() <= 0 ||
		len(response) == 0 || len(response) > MaxRequestBytes || !json.Valid(response) {
		return errors.New("Explore result receipt is invalid")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastExploreResultReceipt = &gamestate.ExploreResultReceipt{
		StartedAtUnix: startedAtUnix,
		ClaimedAtUnix: claimedAt.Unix(),
		Response:      append(json.RawMessage(nil), response...),
	}
	return nil
}

type apStatus struct {
	Current         int
	Max             int
	NextSeconds     int
	IntervalSeconds int
}

func (s *Account) refreshAPLocked(now time.Time) {
	if s.ap >= s.apMax {
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
		return
	}
	if s.apNextRecovery.IsZero() {
		s.apNextRecovery = now.Add(s.apRecoveryInterval)
		return
	}
	if now.Before(s.apNextRecovery) {
		return
	}
	elapsed := now.Sub(s.apNextRecovery)
	recovered := 1 + int(elapsed/s.apRecoveryInterval)
	s.ap += recovered
	if s.ap >= s.apMax {
		s.ap = s.apMax
		s.apNextRecovery = time.Time{}
		return
	}
	s.apNextRecovery = s.apNextRecovery.Add(
		time.Duration(recovered) * s.apRecoveryInterval,
	)
}

func (s *Account) apStatusLocked(now time.Time) apStatus {
	s.refreshAPLocked(now)
	// APTimer shares the same PointTimer interval contract as BPTimer.
	status := apStatus{Current: s.ap, Max: s.apMax,
		IntervalSeconds: int(s.apRecoveryInterval / time.Second)}
	if s.ap >= s.apMax {
		return status
	}
	remaining := s.apNextRecovery.Sub(now)
	status.NextSeconds = int((remaining + time.Second - 1) / time.Second)
	return status
}

func (s *Account) ApState() apStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apStatusLocked(time.Now())
}

func (s *Account) BeginExplore(arthurType, deckIndex int8, acceptedRewards ...[]gamestate.Reward) (apStatus, int, gamestate.Avatar, gamestate.ExploreStage, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.refreshAPLocked(now)
	leaderCardID, avatar, _, selectionOK := s.exploreSelectionLocked(arthurType, deckIndex)
	if s.exploreActive || s.ap <= 0 {
		return s.apStatusLocked(now), 0, gamestate.Avatar{}, gamestate.ExploreStage{}, false
	}
	if !selectionOK || len(s.exploreStages) == 0 || s.exploreStageCursor < 0 || s.exploreStageCursor >= len(s.exploreStages) {
		return s.apStatusLocked(now), 0, gamestate.Avatar{}, gamestate.ExploreStage{}, false
	}
	stage := cloneExploreStages(s.exploreStages[s.exploreStageCursor : s.exploreStageCursor+1])[0]
	var frozen *[]gamestate.Reward
	if len(acceptedRewards) > 0 {
		if s.validateSettlementRewardsLocked(acceptedRewards[0]) != nil {
			return s.apStatusLocked(now), 0, gamestate.Avatar{}, gamestate.ExploreStage{}, false
		}
		copy := cloneRewards(acceptedRewards[0])
		frozen = &copy
	}
	s.exploreActiveRewards = frozen
	s.exploreStageCursor = (s.exploreStageCursor + 1) % len(s.exploreStages)
	s.exploreActiveStage = stage.ExploreStageID
	s.ap--
	if s.apNextRecovery.IsZero() {
		s.apNextRecovery = now.Add(s.apRecoveryInterval)
	}
	s.exploreActive = true
	s.exploreArthurType = arthurType
	s.exploreDeckIndex = deckIndex
	s.exploreStartedAt = now
	return s.apStatusLocked(now), leaderCardID, avatar, stage, true
}

func (s *Account) hasExploreStageLocked(stageID int) bool {
	for _, stage := range s.exploreStages {
		if stage.ExploreStageID == stageID {
			return true
		}
	}
	return false
}

func (s *Account) exploreSelectionLocked(arthurType, deckIndex int8) (int, gamestate.Avatar, []int64, bool) {
	if arthurType < 1 || arthurType > 4 || int(arthurType) > len(s.avatars) {
		return 0, gamestate.Avatar{}, nil, false
	}
	for _, deck := range s.decks {
		if deck.ArthurType != arthurType || deck.Index != deckIndex ||
			deck.LeaderCardIndex < 0 || int(deck.LeaderCardIndex) >= len(deck.CardUniqueIDs) {
			continue
		}
		leaderUniqueID := deck.CardUniqueIDs[deck.LeaderCardIndex]
		cardIndex := cardIndexByUniqueID(s.cards, leaderUniqueID)
		if cardIndex < 0 {
			return 0, gamestate.Avatar{}, nil, false
		}
		avatar := s.avatars[int(arthurType)-1]
		avatar.AvatarPartIDs = append([]int(nil), avatar.AvatarPartIDs...)
		return s.cards[cardIndex].CardID, avatar, append([]int64(nil), deck.CardUniqueIDs...), true
	}
	return 0, gamestate.Avatar{}, nil, false
}

func (s *Account) EndExplore(rewards []gamestate.Reward) (PresentReceiveResult, []CardInfo, bool, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	completed := s.exploreActive
	if !completed {
		return PresentReceiveResult{}, nil, false, 0, nil
	}
	if s.exploreActiveRewards != nil {
		rewards = *s.exploreActiveRewards
	}
	if err := s.validateSettlementRewardsLocked(rewards); err != nil {
		return PresentReceiveResult{}, nil, false, 0, err
	}
	startedAtUnix := s.exploreStartedAt.Unix()
	result := PresentReceiveResult{}
	for _, reward := range rewards {
		if err := s.applySettlementRewardLocked(reward, &result); err != nil {
			return PresentReceiveResult{}, nil, false, 0, err
		}
	}
	s.exploreActive = false
	s.exploreActiveRewards = nil
	arthurType := s.exploreArthurType
	deckIndex := s.exploreDeckIndex
	s.exploreArthurType = 0
	s.exploreDeckIndex = 0
	s.exploreStartedAt = time.Time{}
	s.exploreActiveStage = 0
	if err := s.advanceOnboardingLocked(onboardingEvent{kind: "explore"}); err != nil {
		return PresentReceiveResult{}, nil, false, 0, err
	}
	if arthurType == 0 {
		arthurType = 1
	}
	var cardIDs []int64
	for _, deck := range s.decks {
		if deck.ArthurType == arthurType && deck.Index == deckIndex {
			cardIDs = deck.CardUniqueIDs
			break
		}
	}
	if cardIDs == nil {
		cardIDs = s.decks[0].CardUniqueIDs
	}
	cards := make([]CardInfo, 0, len(cardIDs))
	for _, uniqueID := range cardIDs {
		index := cardIndexByUniqueID(s.cards, uniqueID)
		if index >= 0 {
			cards = append(cards, cloneCard(s.cards[index]))
		}
	}
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionExploreClear}, time.Now())
	return result, cards, true, startedAtUnix, nil
}
