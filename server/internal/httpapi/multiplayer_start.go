package httpapi

import (
	"errors"
	"net/http"
	"slices"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
)

type accountBusinessHandler struct {
	http.Handler
	api             *API
	contentRevision uint64
}

// RetainAccountCache reports business outcome, including protocol errors sent
// with HTTP 200. The coordinator reloads durable state after any rejection.
func (h *accountBusinessHandler) RetainAccountCache() bool { return !h.api.requestRejected }

func (h *accountBusinessHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.api.requestRejected = false
	h.api.account.SetMissionRequestTime(time.Now())
	defer h.api.account.SetMissionRequestTime(time.Time{})
	h.Handler.ServeHTTP(w, r)
}

func (h *accountBusinessHandler) ApplyMissionConfiguration(c game.MissionConfiguration) {
	h.api.account.ApplyMissionConfiguration(c)
}

func (h *accountBusinessHandler) HasUnsettledSoloBattle() bool {
	return h.api.account.HasUnsettledSoloBattle()
}

func (h *accountBusinessHandler) ChargeMultiplayerStart(start multiplayer.BattleStart, persist func(gamestate.State) error) error {
	a := h.api
	if start.RoomID <= 0 || start.BossID <= 0 || start.BPUse < 0 || persist == nil {
		return errors.New("multiplayer start identity is invalid")
	}
	admission := game.MultiplayerAdmission{RoomID: start.RoomID, BossID: start.BossID, BPUse: start.BPUse, CheckOnly: start.CheckOnly}
	var err error
	if start.OwnerUserID == a.initialState.User.UserID {
		err = a.account.ChargeMultiplayerStart(admission, a.initialState, persist)
	} else if slices.Contains(start.GuestUserIDs, a.initialState.User.UserID) {
		err = a.account.ChargeMultiplayerEntry(admission, a.initialState, persist)
	} else {
		return errors.New("multiplayer start participant is invalid")
	}
	if errors.Is(err, game.ErrMultiplayerBattlePoints) || errors.Is(err, game.ErrMultiplayerMedals) {
		return &multiplayer.BattleStartDenied{UserID: a.initialState.User.UserID, Message: err.Error()}
	}
	return err
}
