package httpapi

import (
	"encoding/json"

	"kairisei.local/server/internal/gamestate"
)

// The modern solo receiver reads this named extension alongside its retained
// numeric categories. Roots identify towers; they do not expose floor bosses as
// ordinary solo battles or consume TowerQuestShow's one-shot result.
func (a *API) alchemistGroups() ([]map[string]any, error) {
	entries := a.account.TowerQuestDirectory()
	groups := make([]map[string]any, 0, len(entries))
	for _, entry := range entries {
		var picture struct {
			ID int `json:"9"`
		}
		if len(entry.PictureBoss) != 0 {
			if err := json.Unmarshal(entry.PictureBoss, &picture); err != nil {
				return nil, err
			}
		}
		stories := entry.Stories
		if stories == nil {
			stories = []gamestate.TowerQuestStoryProfile{}
		}
		groups = append(groups, map[string]any{
			"boss_groupid": entry.TowerID, "stage_type": gamestate.BattleStageAlchemist,
			"searchid": 0, "name": entry.Name, "appear_begin": 0, "appear_end": 0,
			"pictid": picture.ID, "recommended_arthur": 5, "stage_quest_areaid": 0,
			"bosses": []any{}, "stories": []any{}, "bonus_flag": 0, "bonus_end_time": 0,
			"stage_quest_state": []int{}, "tower_quest_state": []int{entry.State},
			"tower_stories": stories, "buttons": []any{},
			"main_quest_achievement_max": 0, "main_quest_achievement_num": 0,
		})
	}
	return groups, nil
}
