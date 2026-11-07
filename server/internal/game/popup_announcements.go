package game

import (
	"time"

	"kairisei.local/server/internal/gamestate"
)

// Published copy is immutable; acknowledgements remain valid after withdrawal.
type PopupAnnouncement struct {
	ID          int    `json:"id"`
	Title       string `json:"title"`
	Body        string `json:"body"`
	ButtonText  string `json:"button_text"`
	StartUnix   int64  `json:"start_unix"`
	EndUnix     int64  `json:"end_unix"`
	PublishedAt int64  `json:"published_at"`
	Enabled     bool   `json:"enabled"`
}

type PopupAnnouncementConfigurator interface {
	ApplyPopupAnnouncements([]PopupAnnouncement)
}

func (p PopupAnnouncement) Profile() gamestate.PopupProfile {
	return gamestate.PopupProfile{PopupID: p.ID, PopupType: 7, Priority: 100,
		TitleText: p.Title, BodyText: p.Body, ButtonText: p.ButtonText, IsSystem: 1}
}

// Callers provide the already ordered immutable publication, not Admin drafts.
func (s *Account) UnreadPopupAnnouncements(publications []PopupAnnouncement, now time.Time) []gamestate.PopupProfile {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var result []gamestate.PopupProfile
	for _, p := range publications {
		if !p.Enabled || p.PublishedAt <= 0 || p.StartUnix > now.Unix() || p.EndUnix > 0 && now.Unix() >= p.EndUnix {
			continue
		}
		if _, read := s.popupReadIDs[p.ID]; !read {
			result = append(result, p.Profile())
		}
	}
	return result
}
