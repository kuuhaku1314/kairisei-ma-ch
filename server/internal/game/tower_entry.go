package game

import (
	"errors"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func towerFloorAccessible(progress gamestate.TowerQuestProgress, floor int) bool {
	return progress.Floor == floor || slices.Contains(progress.ClearedFloors, floor)
}

// Check every balance before applying either debit. Story and battle floors
// consume the same floor-owned price inside their account transaction.
func (s *Account) spendTowerFloorEntryLocked(floor gamestate.TowerQuestFloorProfile, now time.Time) (bool, error) {
	item := s.items[floor.ItemID]
	if floor.ItemUse > 0 && (item.Num < floor.ItemUse || item.LimitTime > 0 && now.Unix() >= int64(item.LimitTime)) {
		return false, errors.New("tower entry item is insufficient")
	}
	if s.bp < floor.BPUse {
		return false, nil
	}
	if floor.ItemUse > 0 {
		item.Num -= floor.ItemUse
		s.items[floor.ItemID] = item
	}
	s.bp -= floor.BPUse
	if floor.BPUse > 0 && s.bp < s.bpMax && s.bpNextRecovery.IsZero() {
		s.bpNextRecovery = now.Add(s.bpRecoveryInterval)
	}
	return true, nil
}
