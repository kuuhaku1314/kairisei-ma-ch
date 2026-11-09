package masterdata

import (
	"errors"

	"kairisei.local/server/internal/gamestate"
)

// Both the master loader and the assembled-runtime gate consume this policy.
// Keep one contract so a reviewed event is not rejected as the old empty page.
func ValidateEventPageProfile(profile gamestate.EventPageProfile) error {
	if profile.EventID < 0 || profile.BackgroundPictID < 0 ||
		profile.HomeButtonPictID < 0 || profile.EndTime != 2147483647 ||
		profile.InfoURL != "" || profile.UpdateInfo == "" || profile.ItemIDs == nil ||
		profile.Buttons == nil || profile.IsNewSolo != 0 || profile.IsNewMulti != 0 {
		return errors.New("CN EventPage profile is invalid")
	}
	if profile.EventID == 0 {
		if profile.BackgroundPictID != 0 || profile.HomeButtonPictID != 0 || len(profile.Buttons) != 0 {
			return errors.New("invalid inactive CN EventPage template")
		}
	} else if profile.EventID != 11801010 || profile.BackgroundPictID != 1 || profile.HomeButtonPictID != 1 ||
		len(profile.ItemIDs) != 0 || len(profile.Buttons) != 1 ||
		profile.Buttons[0] != (gamestate.EventPageButton{Type: 6, PictID: 6, MoveTo: "eventstory"}) {
		return errors.New("invalid CN permanent event replay")
	}
	return nil
}
