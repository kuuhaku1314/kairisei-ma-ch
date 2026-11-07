package game

import (
	"testing"
	"time"
)

func TestPopupAnnouncementWindowBoundaries(t *testing.T) {
	s := &Account{popupReadIDs: make(map[int]struct{})}
	p := PopupAnnouncement{ID: 610000001, Enabled: true, PublishedAt: 1, StartUnix: 100, EndUnix: 200}
	for _, tc := range []struct {
		now  int64
		want int
	}{{99, 0}, {100, 1}, {199, 1}, {200, 0}} {
		if got := s.UnreadPopupAnnouncements([]PopupAnnouncement{p}, time.Unix(tc.now, 0)); len(got) != tc.want {
			t.Fatal(tc, got)
		}
	}
}
