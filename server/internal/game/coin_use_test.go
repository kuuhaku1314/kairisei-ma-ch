package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestCardExpansionChargesOnlyAcceptedCapacity(t *testing.T) {
	for _, container := range []bool{false, true} {
		base := CardCapacityLimit - 80
		s := &Account{cardMax: base, cardContainerMax: base, coinFree: 640, highestDeckRank: 1}
		extend, capacity := s.ExtendCardCapacity, s.CardCapacity
		if container {
			extend, capacity = s.ExtendCardContainerCapacity, s.CardContainerCapacity
		}
		for option, want := range []int{base + 5, base + 30, CardCapacityLimit} {
			if err := extend(option); err != nil || capacity() != want {
				t.Fatalf("container=%v expansion=%d capacity=%d err=%v", container, option, capacity(), err)
			}
		}
		snapshot := s.Snapshot(gamestate.State{})
		stored := snapshot.User.CardMax
		if container {
			stored = snapshot.User.CardContainerMax
		}
		if s.coinFree != 0 || stored != CardCapacityLimit {
			t.Fatal("capacity or charge missing from snapshot")
		}
		s.coinFree = 400
		for _, option := range []int{-1, 0, 1, 2, 3} {
			if extend(option) == nil || s.coinFree != 400 || capacity() != CardCapacityLimit {
				t.Fatal("rejected expansion changed capacity or crystals")
			}
		}
		s.cardMax, s.cardContainerMax, s.coinFree = 5900, 5900, 39
		if extend(0) == nil || capacity() != 5900 || s.coinFree != 39 {
			t.Fatal("unaffordable expansion changed state")
		}
	}
}
