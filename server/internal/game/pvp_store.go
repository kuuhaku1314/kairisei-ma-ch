package game

import (
	"errors"
	"fmt"
	"sort"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// Original str_table 240/20/1 specifies a daily midnight refill. The local
// CN service uses UTC+8, independently of the server machine's timezone.
func pvpChallengeDay(now time.Time) int64 { return (now.Unix() + 8*3600) / 86400 }

func (s *Account) RefreshPVPChallenges(config PVPConfig, now time.Time, base gamestate.State, persist StatePersister) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	day := pvpChallengeDay(now)
	if day <= s.pvp.ChallengeDay {
		return nil
	}
	previousCount, previousDay := s.pvp.Challenge, s.pvp.ChallengeDay
	if s.pvp.ChallengeDay == 0 {
		// Anchor an unstamped allowance to its latest ranked debit when one
		// exists. A fresh account keeps its initialized allowance for today.
		s.pvp.ChallengeDay = day
		for index := len(s.pvp.History) - 1; index >= 0; index-- {
			match := s.pvp.History[index]
			if match.BattleType == 1 && match.StartedAtUnix > 0 {
				s.pvp.ChallengeDay = pvpChallengeDay(time.Unix(match.StartedAtUnix, 0))
				break
			}
		}
	}
	if day > s.pvp.ChallengeDay {
		s.pvp.Challenge, s.pvp.ChallengeDay = config.ChallengeMax, day
	}
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.pvp.Challenge, s.pvp.ChallengeDay = previousCount, previousDay
			return err
		}
	}
	return nil
}

func (s *Account) PvpStatus() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pvpPoint, s.pvp.Challenge
}

func (s *Account) PvpStatusWithBattleID() (int, int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pvpPoint, s.pvp.Challenge, s.pvp.NextBattleID
}

var ErrInvalidPVPDeck = errors.New("invalid PVP deck selection")

func (s *Account) validatePVPDefenseLocked(decks []gamestate.PVPDeckSelection) ([]gamestate.PVPDeckSelection, error) {
	if len(decks) != 4 {
		return nil, errors.New("PVP requires one deck for each of the four Arthur types")
	}
	knownCards := ownedCardIDs(s.cards)
	seenArthur := make(map[int8]struct{}, 4)
	canonical := ClonePVPDeckSelections(decks)
	for _, deck := range canonical {
		if deck.ArthurType < 1 || deck.ArthurType > 4 || deck.JobType < 1 || deck.JobType > 4 {
			return nil, errors.New("PVP defense contains an invalid Arthur or job type")
		}
		if _, exists := seenArthur[deck.ArthurType]; exists {
			return nil, errors.New("PVP defense contains a duplicate Arthur type")
		}
		seenArthur[deck.ArthurType] = struct{}{}
		if len(deck.CardUniqueIDs) != s.deckSlots || len(deck.SupportCardUniqueIDs) != s.supportSlotCapacity {
			return nil, errors.New("PVP defense deck shape differs from the local card store")
		}
		unlocked := int(s.supportUnlockedSlots[deck.ArthurType-1])
		for index := unlocked; index < len(deck.SupportCardUniqueIDs); index++ {
			if deck.SupportCardUniqueIDs[index] != 0 {
				return nil, fmt.Errorf("PVP defense support slot %d is locked for Arthur %d", index+1, deck.ArthurType)
			}
		}
		for _, uniqueID := range append(append([]int64(nil), deck.CardUniqueIDs...), deck.SupportCardUniqueIDs...) {
			if uniqueID == 0 {
				continue
			}
			if _, exists := knownCards[uniqueID]; !exists {
				return nil, fmt.Errorf("PVP defense references unknown card %d", uniqueID)
			}
		}
		if err := s.validateDeckComposition(DeckInfo{ArthurType: deck.ArthurType, CardUniqueIDs: deck.CardUniqueIDs}, knownCards, 0, 0, true); err != nil {
			return nil, err
		}
	}
	sort.Slice(canonical, func(left, right int) bool { return canonical[left].ArthurType < canonical[right].ArthurType })
	return canonical, nil
}

func (s *Account) BeginPVP(battleType int, opponentUserID int, decks []gamestate.PVPDeckSelection, config PVPConfig, now time.Time) (gamestate.PVPMatch, int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	canonical, err := s.validatePVPDefenseLocked(decks)
	if err != nil {
		return gamestate.PVPMatch{}, s.pvp.Challenge, fmt.Errorf("%w: %v", ErrInvalidPVPDeck, err)
	}
	if opponentUserID <= 0 {
		return gamestate.PVPMatch{}, s.pvp.Challenge, errors.New("PVP opponent is invalid")
	}
	if battleType == 1 {
		if s.pvp.Challenge <= 0 {
			return gamestate.PVPMatch{}, 0, errors.New("排位战挑战次数已用完，可选择自由战。")
		}
		s.pvp.Challenge--
	}
	// Defense selection and the accepted challenge form one state change.
	// Failed matchmaking or an exhausted allowance must not replace the defense.
	s.pvp.DefenseDecks = canonical
	s.pvp.DefenseUpdatedAtUnix = now.Unix()
	pointAfter := s.pvpPoint
	expectedWin := config.ReplayResult > 0
	if battleType == 1 {
		if expectedWin {
			pointAfter += config.RankWinPoint
		} else {
			pointAfter += config.RankLosePoint
			if pointAfter < 0 {
				pointAfter = 0
			}
		}
	}
	match := gamestate.PVPMatch{
		BattleID: s.pvp.NextBattleID, BattleType: battleType,
		OpponentUserID: opponentUserID, ExpectedWin: expectedWin,
		PointBefore: s.pvpPoint, PointAfter: pointAfter, StartedAtUnix: now.Unix(),
		CompletedAtUnix: now.Unix(),
	}
	s.pvp.NextBattleID++
	// Start commits the result consumed by PvPMgr.PvPEnd, including skip and
	// replay. PvpEnd only acknowledges that result and cannot grant it again.
	s.pvpPoint = match.PointAfter
	s.markMissionMetricLocked(gamestate.MissionPVPRank)
	s.recordMissionEventLocked(missionEvent{kind: gamestate.MissionPVPPlayed}, now)
	s.pvp.History = append(s.pvp.History, match)
	if len(s.pvp.History) > pvpHistoryLimit {
		s.pvp.History = append([]gamestate.PVPMatch(nil), s.pvp.History[len(s.pvp.History)-pvpHistoryLimit:]...)
	}
	return match, s.pvp.Challenge, nil
}

func (s *Account) FinishPVP(battleID int, isWin bool, isRetire bool) (gamestate.PVPMatch, int, int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for index := len(s.pvp.History) - 1; index >= 0; index-- {
		match := s.pvp.History[index]
		if match.BattleID == battleID {
			if match.ExpectedWin != (isWin && !isRetire) || match.Retired != isRetire {
				return gamestate.PVPMatch{}, s.pvp.Challenge, 0, errors.New("PVP report conflicts with the committed result")
			}
			return match, s.pvp.Challenge, BoolInt(match.ExpectedWin), nil
		}
	}
	return gamestate.PVPMatch{}, s.pvp.Challenge, 0, errors.New("PVP report does not match a committed battle")
}
