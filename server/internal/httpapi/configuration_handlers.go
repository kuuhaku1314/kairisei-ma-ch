package httpapi

import (
	"kairisei.local/server/internal/game"
)

func (h *accountBusinessHandler) ApplyPopupAnnouncements(publications []game.PopupAnnouncement) {
	h.api.popupAnnouncements = publications
}

func (h *accountBusinessHandler) ApplyEvolutionRestrictions(policy *game.EvolutionRestrictions) {
	h.api.account.ApplyEvolutionRestrictions(policy)
}

func (h *accountBusinessHandler) ApplyRuntimeSettings(settings game.RuntimeSettings) {
	h.api.account.ApplyRuntimeSettings(settings)
}

func (h *accountBusinessHandler) ApplyPlayerConfiguration(config game.PlayerConfiguration) {
	h.api.account.ApplyPlayerConfiguration(config)
}

func (h *accountBusinessHandler) ApplyGachaConfiguration(revision uint64, configs []game.GachaConfiguration) {
	h.api.account.ApplyGachaConfiguration(revision, configs)
}
