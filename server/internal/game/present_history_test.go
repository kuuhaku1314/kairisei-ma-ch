package game

import (
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestDeletedPresentHistoryDropsOldestFiftyAfterHundred(t *testing.T) {
	present := func(id int64, state int8) gamestate.Present {
		return gamestate.Present{PresentID: id, State: state, Title: "gift"}
	}
	s := &Account{}
	for id := int64(1); id <= 100; id++ {
		s.presentHistories = append(s.presentHistories, present(id, 1))
	}
	s.presents = []gamestate.Present{present(101, 1), present(102, 0)}
	if _, err := s.DeletePresents(101); err != nil {
		t.Fatal(err)
	}
	if len(s.presentHistories) != 51 || s.presentHistories[0].PresentID != 51 || s.presentHistories[50].PresentID != 101 {
		t.Fatalf("history after 101st deletion: %d entries from %d", len(s.presentHistories), s.presentHistories[0].PresentID)
	}
	// The newest deletion stays, so a retried native Delete still resolves.
	if _, err := s.DeletePresents(101); err != nil || len(s.presents) != 1 {
		t.Fatal("retried deletion of the newest history row failed", err)
	}
	// Histories saved before the bound existed shrink to the bound in one step.
	legacy := make([]gamestate.Present, 300)
	for i := range legacy {
		legacy[i] = present(int64(1000+i), 1)
	}
	trimmed := gamestate.TrimPresentHistories(legacy)
	if len(trimmed) != gamestate.PresentHistoryLimit || trimmed[0].PresentID != 1200 {
		t.Fatalf("legacy history: %d entries from %d", len(trimmed), trimmed[0].PresentID)
	}
}
