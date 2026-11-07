package game

import (
	"errors"
	"kairisei.local/server/internal/gamestate"
	"time"
)

// SoloBattleCommit is one accepted outcome: inventory, receipt, optional clear
// statistics and helper rental credits share the same persistence boundary.
type SoloBattleCommit struct {
	Score          *gamestate.BossScoreResult
	BossID         int
	ReceiptSHA     string
	Clear          *gamestate.BattleClearRecord
	RentalEventKey string
	RentalCredits  []FriendPointRentalCredit
}

type BattleHistoryRepository interface {
	PersistSoloBattle(gamestate.State, SoloBattleCommit) error
	RecentBattleClears(int, time.Time) ([]gamestate.BattleClearRecord, error)
	YesterdayBattleRanks(int, int, time.Time) ([]gamestate.BattleClearRecord, error)
}

func (s *Account) SetTeamBattleClearDecks(bossID int, decks []gamestate.BattleClearDeck) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.activeBattle == nil || s.activeBattle.BossID != bossID || len(decks) != 1+len(s.activeBattle.SelectedPartners) {
		return errors.New("clear-deck snapshot does not match active battle")
	}
	if len(s.activeBattle.ClearDecks) == 0 {
		s.activeBattle.ClearDecks = gamestate.CloneBattleClearDecks(decks)
	}
	return nil
}
