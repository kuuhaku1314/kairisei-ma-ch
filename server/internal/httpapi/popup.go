package httpapi

import (
	"kairisei.local/server/internal/gamestate"
	"net/http"
)

func (a *API) popupExec(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PopupID  int  `json:"popupid"`
		IsSystem int8 `json:"is_system"`
	}
	if err := decodeExact(request, []string{"popupid", "is_system"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile := gamestate.PopupProfile{}
	for _, publication := range a.popupAnnouncements {
		if publication.ID == payload.PopupID {
			profile = publication.Profile()
			break
		}
	}
	if err := a.account.AcknowledgePopup(profile, payload.PopupID, payload.IsSystem); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}
