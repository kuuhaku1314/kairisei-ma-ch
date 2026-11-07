package admin

import (
	"encoding/json"
	"errors"
	"net/http"
)

// Publication is per client catalog. Shared boss IDs still use one drop table.
func (a *API) bossCatalog(r *http.Request) (string, []AdminBattleGroup, map[int]struct{}, error) {
	switch r.URL.Query().Get("catalog") {
	case "", "activity":
		return teamBattlePublicationKey, a.groups, a.knownGroups, nil
	case "past":
		known := make(map[int]struct{}, len(a.pastGroups))
		for _, g := range a.pastGroups {
			known[g.GroupID] = struct{}{}
		}
		return PastBattlePublicationKey, a.pastGroups, known, nil
	case "tower":
		// Whole towers, not Boss groups: the publication's group IDs are tower IDs.
		a.operations.configMu.RLock()
		defer a.operations.configMu.RUnlock()
		known := make(map[int]struct{})
		if a.operations.content != nil {
			for _, tower := range a.operations.content.base.TowerQuestProfiles {
				known[tower.TowerID] = struct{}{}
			}
		}
		return towerPublicationKey, []AdminBattleGroup{}, known, nil
	default:
		return "", nil, nil, errors.New("未知副本目录")
	}
}

func buildAdminPastGroups(rawGroups []json.RawMessage, groups []AdminBattleGroup, cards map[string]AdminCatalogEntry) ([]AdminBattleGroup, error) {
	byBoss := make(map[int]AdminBattleGroup)
	for _, g := range groups {
		for _, id := range g.BossIDs {
			byBoss[id] = g
		}
	}
	result := make([]AdminBattleGroup, 0, len(rawGroups))
	for _, raw := range rawGroups {
		var past struct {
			ID       int    `json:"0"`
			Name     string `json:"4"`
			PastName string `json:"6"`
			Cards    []int  `json:"7"`
			Bosses   []struct {
				ID         int    `json:"0"`
				Difficulty string `json:"4"`
			} `json:"13"`
		}
		if err := json.Unmarshal(raw, &past); err != nil {
			return nil, err
		}
		g := AdminBattleGroup{GroupID: past.ID, Name: past.Name, PastName: past.PastName, Category: "2d", BossIDs: []int{}, Difficulties: []string{}, SegmentCounts: []int{}}
		for _, b := range past.Bosses {
			source, ok := byBoss[b.ID]
			if !ok {
				return nil, errors.New("往期BOSS缺少可运营的原副本身份")
			}
			segments := 1
			for i, id := range source.BossIDs {
				if id == b.ID {
					segments = source.SegmentCounts[i]
					break
				}
			}
			g.Category = source.Category
			g.BossIDs = append(g.BossIDs, b.ID)
			g.Difficulties = append(g.Difficulties, b.Difficulty)
			g.SegmentCounts = append(g.SegmentCounts, segments)
			g.MaxSegments = max(g.MaxSegments, segments)
		}
		g.BossCount = len(g.BossIDs)
		if len(past.Cards) > 0 {
			card := cards[adminCatalogKey(6, past.Cards[0])]
			g.ImageURL, g.PictureID = card.ImageURL, card.PictID
		}
		result = append(result, g)
	}
	return result, nil
}
