package httpapi

import (
	"errors"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/multiplayer"
)

func (h *accountBusinessHandler) ChargeMultiplayerContinue(request multiplayer.BattleContinue) (multiplayer.ContinueBalance, error) {
	a := h.api
	if request.UserID != a.initialState.User.UserID || request.RoomID <= 0 || request.BossID <= 0 || request.Sequence <= 0 {
		return multiplayer.ContinueBalance{}, errors.New("multiplayer continue identity is invalid")
	}
	balance, err := a.account.ChargeMultiplayerContinue(game.MultiplayerContinuation{RoomID: request.RoomID, BossID: request.BossID, Sequence: request.Sequence}, a.initialState, a.persistState)
	return multiplayer.ContinueBalance{Coin: balance.Coin, CoinFree: balance.CoinFree}, err
}
