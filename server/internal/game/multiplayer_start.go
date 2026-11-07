package game

import (
	"errors"
	"time"

	"kairisei.local/server/internal/gamestate"
)

type MultiplayerAdmission struct {
	RoomID    int64
	BossID    int
	BPUse     int
	CheckOnly bool
}

var ErrMultiplayerBattlePoints = errors.New("房主体力不足，无法开始战斗。")
var ErrMultiplayerMedals = errors.New("骑士徽章不足，每次参与他人的房间需要1枚。")

func (s *Account) ChargeMultiplayerStart(start MultiplayerAdmission, base gamestate.State, persist StatePersister) error {
	return s.chargeMultiplayerStart(start, start.BPUse, 0, 0, base, persist)
}

func (s *Account) ChargeMultiplayerEntry(start MultiplayerAdmission, base gamestate.State, persist StatePersister) error {
	if base.TeamBattleMedalItemID <= 0 {
		return errors.New("multiplayer entry medal is not configured")
	}
	// LOCAL_POLICY: a real guest pays one knight medal per accepted room start.
	return s.chargeMultiplayerStart(start, 0, base.TeamBattleMedalItemID, 1, base, persist)
}

func (s *Account) chargeMultiplayerStart(start MultiplayerAdmission, bpUse, medalItemID, medalUse int, base gamestate.State, persist StatePersister) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, receipt := range s.teamBattleStartReceipts {
		if receipt.RoomID != start.RoomID {
			continue
		}
		if receipt.BossID != start.BossID || receipt.BPUse != bpUse || receipt.MedalItemID != medalItemID || receipt.MedalUse != medalUse {
			return errors.New("multiplayer start receipt conflicts with room")
		}
		return nil
	}
	now := time.Now()
	s.refreshBattlePointsLocked(now)
	if bpUse < 0 || s.bp < bpUse {
		return ErrMultiplayerBattlePoints
	}
	medal, hadMedal := s.items[medalItemID]
	if medalUse > 0 && (!hadMedal || medal.Num < medalUse) {
		return ErrMultiplayerMedals
	}
	if start.CheckOnly {
		return nil
	}
	previousBP, previousRecovery, previousReceipts := s.bp, s.bpNextRecovery, s.teamBattleStartReceipts
	s.bp -= bpUse
	if medalUse > 0 {
		next := medal
		next.Num -= medalUse
		s.items[medalItemID] = next
	}
	if s.bp < s.bpMax && s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
	}
	// Active rooms never survive a process restart and completed rooms expire
	// after 30 minutes. Keep a bounded recent ledger for retry acknowledgement;
	// only the live Hub can request a start, never a client-provided room ID.
	const retainedStarts = 512
	first := max(0, len(previousReceipts)-(retainedStarts-1))
	s.teamBattleStartReceipts = append(append([]gamestate.TeamBattleStartReceipt(nil), previousReceipts[first:]...), gamestate.TeamBattleStartReceipt{
		RoomID: start.RoomID, BossID: start.BossID, BPUse: bpUse, MedalItemID: medalItemID, MedalUse: medalUse,
	})
	if persist != nil {
		if err := persist(s.snapshotLocked(base)); err != nil {
			s.bp, s.bpNextRecovery, s.teamBattleStartReceipts = previousBP, previousRecovery, previousReceipts
			if medalUse > 0 {
				s.items[medalItemID] = medal
			}
			return err
		}
	}
	return nil
}

func (s *Account) prepaidTeamBattleCostLocked(roomID int64, bossID int) (int, bool) {
	for _, receipt := range s.teamBattleStartReceipts {
		if receipt.RoomID == roomID && receipt.BossID == bossID {
			return receipt.BPUse, true
		}
	}
	return 0, false
}
