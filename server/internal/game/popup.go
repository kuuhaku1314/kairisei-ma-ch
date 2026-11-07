package game

import (
	"errors"

	"kairisei.local/server/internal/gamestate"
)

func (s *Account) AcknowledgePopup(profile gamestate.PopupProfile, popupID int, isSystem int8) error {
	if profile.PopupID <= 0 || popupID != profile.PopupID || isSystem != profile.IsSystem {
		return errors.New("popup acknowledgement does not match the published local profile")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.popupReadIDs[popupID] = struct{}{}
	return nil
}
