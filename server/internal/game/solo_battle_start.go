package game

import (
	"errors"
	"fmt"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// SoloBattleEntry describes a validated party. Its request key identifies a
// repeated start while that run remains active; settlement or Home ends it.
type SoloBattleEntry struct {
	ScoreBossID              int
	DeckArthurType           int8
	UserID                   int
	BossID                   int
	EnemyTypes               []int8
	Profiles                 []gamestate.TeamBattleRewardProfile
	RequestKey               string
	FameSources              []TeamBattleFameSource
	FriendPointPartners      int
	FriendPointReward        int
	FriendPointRentalCredits []FriendPointRentalCredit
	SelectedPartners         []TeamBattleResultPartner
}

func (s *Account) BeginSoloBattle(entry SoloBattleEntry) (TeamBattleContext, BattlePointStatus, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry.RequestKey == "" {
		return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("solo start request identity is unavailable")
	}
	now := time.Now()
	if s.activeBattle != nil && s.activeBattle.BossID == entry.BossID && s.activeBattle.SoloRequestKey == entry.RequestKey {
		s.refreshBattlePointsLocked(now)
		return *teamBattleContextFromRelease(snapshotTeamBattleContext(s.activeBattle)), s.battlePointStatusLocked(now), true, nil
	}
	catalog := s.teamBattleSolo
	rules, found := s.teamBattleEntryRulesForBossLocked(entry.BossID, catalog)
	if !found || len(entry.FameSources) != 1 || entry.FameSources[0].MemberType != 1 {
		return TeamBattleContext{}, BattlePointStatus{}, false, errors.New("solo party entry is unavailable")
	}
	if err := rules.ValidateSoloParty(entry.DeckArthurType, entry.SelectedPartners); err != nil {
		return TeamBattleContext{}, BattlePointStatus{}, false, err
	}
	identity := now.UnixNano()
	fameSeed := fmt.Sprintf("solo:user=%d:boss=%d:start=%d", entry.UserID, entry.BossID, identity)
	rentalKey := ""
	if len(entry.FriendPointRentalCredits) != 0 {
		rentalKey = fmt.Sprintf("solo-rental:user=%d:boss=%d:start=%d", entry.UserID, entry.BossID, identity)
	}
	context, points, started, err := s.beginTeamBattleLocked(TeamBattleEntry{
		ScoreBossID: entry.ScoreBossID,
		BossID:      entry.BossID, StageType: rules.StageType, EntryRole: gamestate.BattleEntrySolo,
		EnemyTypes: entry.EnemyTypes, Profiles: entry.Profiles,
		FameSeed: fameSeed, FameSources: entry.FameSources,
		FriendPointPartners: entry.FriendPointPartners, FriendPointReward: entry.FriendPointReward,
		FriendPointRentalCredits: entry.FriendPointRentalCredits, FriendPointRentalEventKey: rentalKey,
		SelectedPartners: entry.SelectedPartners,
	}, catalog)
	if err != nil || !started {
		return context, points, started, err
	}
	s.activeBattle.StartTime = int(now.Unix())
	s.activeBattle.SoloRequestKey = entry.RequestKey
	context.StartTime = s.activeBattle.StartTime
	context.SoloRequestKey = entry.RequestKey
	return context, points, started, nil
}
