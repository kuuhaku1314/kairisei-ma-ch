package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/multiplayer"
)

func (a *API) battleRecoveryShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		AbandonedRoomIDs []int64 `json:"abandoned_roomids"`
	}
	if err := decodeExact(request, []string{"abandoned_roomids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(payload.AbandonedRoomIDs) > 32 {
		writeError(writer, http.StatusBadRequest, "too many pending battle departures")
		return
	}
	for _, id := range payload.AbandonedRoomIDs {
		if id <= 0 {
			writeError(writer, http.StatusBadRequest, "invalid battle departure room")
			return
		}
	}
	var entry *multiplayer.RecoveryEntry
	if a.multiplayer != nil {
		for _, id := range payload.AbandonedRoomIDs {
			a.multiplayer.AbandonRecoveryFor(a.initialState.User.UserID, id)
		}
		// An explicit departure takes precedence over auto-entry on this login.
		if len(payload.AbandonedRoomIDs) == 0 {
			entry = a.multiplayer.RecoveryFor(a.initialState.User.UserID)
		}
	}
	if entry != nil {
		group, err := multiplayerRoomBossGroupWire(entry.BossGroup, entry.BossID)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
		entry.BossGroup = group
	}
	response := map[string]any{
		"recovery": entry,
		"battlesv": map[string]any{"host": a.battleSV.Host, "port": a.battleSV.Port},
	}
	if entry != nil {
		response["user"] = a.userPayload()
	}
	a.writeProtocol(writer, response)
}
