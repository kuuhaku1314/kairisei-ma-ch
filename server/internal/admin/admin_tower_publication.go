package admin

import (
	"sort"
	"time"
)

// A tower is its own native catalog entry, never a synthetic ordinary Boss
// group. The Admin view separates operator selection from resource readiness.
type adminTowerPublicationOption struct {
	TowerID          int    `json:"tower_id"`
	Name             string `json:"name"`
	FloorCount       int    `json:"floor_count"`
	OperatorEnabled  bool   `json:"operator_enabled"`
	EffectiveEnabled bool   `json:"effective_enabled"`
}

func (a *API) towerPublicationOptions(policy adminPublicationState) []adminTowerPublicationOption {
	a.operations.configMu.RLock()
	defer a.operations.configMu.RUnlock()
	result := []adminTowerPublicationOption{}
	if a.operations.content == nil {
		return result
	}
	selected := make(map[int]bool, len(policy.GroupIDs))
	for _, id := range policy.GroupIDs {
		selected[id] = true
	}
	now := time.Now().Unix()
	withinWindow := now >= policy.StartUnix && (policy.EndUnix == 0 || now < policy.EndUnix)
	for _, tower := range a.operations.content.base.TowerQuestProfiles {
		operatorEnabled := policy.Mode == "all" || selected[tower.TowerID]
		result = append(result, adminTowerPublicationOption{
			TowerID: tower.TowerID, Name: tower.Name, FloorCount: len(tower.Floors),
			OperatorEnabled: operatorEnabled, EffectiveEnabled: operatorEnabled && withinWindow,
		})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].TowerID < result[j].TowerID })
	return result
}
