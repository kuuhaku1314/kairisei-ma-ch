package httpapi

import (
	"kairisei.local/server/internal/game"
)

func (h *accountBusinessHandler) ApplyContentConfiguration(config game.ContentConfiguration) {
	if config.Revision == 0 || h.contentRevision == config.Revision {
		return
	}
	h.api.initialState = accountSnapshotBase(game.ApplyContentState(h.api.initialState, config))
	h.api.account.ApplyTradeShopConfiguration(config.State.TradeShopProfiles)
	h.api.account.ApplyBattleCatalogConfiguration(config.BattleCatalog, config.State.DisabledTeamBattleBossIDs)
	h.api.account.ApplyTowerQuestConfiguration(config.State.TowerQuestProfiles)
	h.contentRevision = config.Revision
}

func (h *accountBusinessHandler) ApplyTowerAdmission(policy game.TowerAdmission) {
	h.api.account.ApplyTowerAdmission(policy)
}
