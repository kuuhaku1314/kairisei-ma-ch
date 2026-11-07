package httpapi

import (
	"errors"
	"net/http"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) writeStoreError(writer http.ResponseWriter, err error) {
	if !a.writeBusinessError(writer, err) {
		writeError(writer, http.StatusBadRequest, err.Error())
	}
}

// Returns false for transport, persistence and malformed-input failures. They
// retain their original status at the caller rather than becoming success.
func (a *API) writeBusinessError(writer http.ResponseWriter, err error) bool {
	var expected *game.BusinessError
	if !errors.As(err, &expected) {
		switch {
		case errors.Is(err, battlecore.ErrMultipleRoundTableCards):
			expected = &game.BusinessError{Code: -1, Message: "主卡组只能编入一张圆桌骑士卡牌，请调整卡组。"}
		case errors.Is(err, battlecore.ErrTooManySummonCards):
			expected = &game.BusinessError{Code: -1, Message: "炼金主卡组最多编入五张召唤骑士卡牌，请调整卡组。"}
		case errors.Is(err, battlecore.ErrAlchemistDeckProfession):
			expected = &game.BusinessError{Code: -1, Message: "炼金主卡组只能编入通用或炼金职业的卡牌，请调整卡组。"}
		default:
			return false
		}
	}
	// Original ProtoMgr.onCommon only displays errors for actions 1 (title)
	// and 2 (home). Action 0 silently invokes the endpoint's default callback;
	// in BuyNavi that callback even treats default res=0 as a navigator ID.
	// Use the supported home return to refresh the page without losing login.
	a.writeProtocolResponse(writer, map[string]any{}, []gamestate.PopupProfile{}, expected.Code, expected.Message, 2)
	return true
}
