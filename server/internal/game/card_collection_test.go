package game

import (
	"testing"
)

// Regression for the stock client's fixed ten-slot reader, discovery filter
// and LOVE_MAX bit, including material-card history after inventory empties.
func TestCardCollectionPagesAndHistory(t *testing.T) {
	s := &Account{onboarding: completedTestOnboarding(), cardCollectionPages: [][10]int{{101, 102, 103, 104, 105, 106, 107, 108, 109, 110}, {201, 202}}}
	s.recordCollectedCardLocked(CardInfo{CardID: 101, Love: 100, LoveMax: 100})
	s.recordCollectedCardLocked(CardInfo{CardID: 201})
	s.recordCollectedCardLocked(CardInfo{CardID: 999}) // not part of the official directory
	pages, count, total := s.CardCollection()
	if len(pages) != 2 || total != 12 || count != 2 || pages[0].Cards[0].State != 3 || pages[1].Cards[0].State != 1 || pages[0].Cards[1].State != 0 || pages[1].Cards[2].CardID != 0 {
		t.Fatalf("collection pages=%+v count=%d total=%d", pages, count, total)
	}
}
