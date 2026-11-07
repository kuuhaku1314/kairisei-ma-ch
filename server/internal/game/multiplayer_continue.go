package game

import (
	"errors"

	"kairisei.local/server/internal/gamestate"
)

type MultiplayerContinuation struct {
	RoomID   int64
	BossID   int
	Sequence int
}

type CrystalBalance struct{ Coin, CoinFree int }

// User-approved local policy, shared by solo and multiplayer. The corresponding
// original-client dialog adapter is Patch-CNLocalContinue.ps1.
const continueCrystalCost = 50

func (s *Account) ChargeMultiplayerContinue(request MultiplayerContinuation, base gamestate.State, persist StatePersister) (CrystalBalance, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range s.teamBattleContinueReceipts {
		if receipt.RoomID == request.RoomID && receipt.Sequence == request.Sequence {
			if receipt.BossID != request.BossID {
				return CrystalBalance{}, errors.New("multiplayer continue receipt conflicts with room")
			}
			return CrystalBalance{Coin: s.coin, CoinFree: s.coinFree}, nil
		}
	}
	if s.coin+s.coinFree < continueCrystalCost {
		return CrystalBalance{}, errors.New("insufficient crystals for continuation")
	}
	previousCoin, previousFree, previousReceipts := s.coin, s.coinFree, s.teamBattleContinueReceipts
	freeSpend := min(s.coinFree, continueCrystalCost)
	s.coinFree -= freeSpend
	s.coin -= continueCrystalCost - freeSpend
	first := max(0, len(previousReceipts)-511)
	s.teamBattleContinueReceipts = append(append([]gamestate.TeamBattleContinueReceipt(nil), previousReceipts[first:]...), gamestate.TeamBattleContinueReceipt{
		RoomID: request.RoomID, BossID: request.BossID, Sequence: request.Sequence,
	})
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.coin, s.coinFree, s.teamBattleContinueReceipts = previousCoin, previousFree, previousReceipts
			return CrystalBalance{}, err
		}
	}
	return CrystalBalance{Coin: s.coin, CoinFree: s.coinFree}, nil
}
