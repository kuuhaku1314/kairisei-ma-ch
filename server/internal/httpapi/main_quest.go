package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/gamestate"
)

func (a *API) mainQuestShow(writer http.ResponseWriter, request *http.Request) {
	sections := make([]any, 0, 1)
	if definition := a.initialState.NormalQuestSection; definition != nil {
		catalog, err := a.account.TeamBattleCatalog()
		if err != nil {
			a.writeStoreError(writer, err)
			return
		}
		groups := catalog.MainQuestGroups()
		for index, group := range groups {
			named, err := teamBattleBossGroupNamedDTO(group)
			if err != nil {
				a.writeStoreError(writer, err)
				return
			}
			// These decorations belong to the CN room/list DTO, not the named
			// MainQuestShow consumer. All shared quest fields use one mapper.
			for _, key := range []string{"iconindex", "appear_begin", "button_str1", "button_str2"} {
				delete(named, key)
			}
			for _, value := range named["bosses"].([]any) {
				boss := value.(map[string]any)
				trimQuestBossDecorations(boss)
			}
			groups[index] = named
		}
		sections = append(sections, map[string]any{
			"sectionid": definition.SectionID, "title": definition.Title,
			"bg_pictid": definition.BGPictID, "icon_pictid": definition.IconPictID,
			"boss_groups": groups, "is_lock": 0,
			"unlock": map[string]any{
				"logical_op": 0, "story_tab_name": "", "story_sectionid": 0,
				"story_section_title": "", "story_title": "",
			},
		})
	}
	cleared := make(map[int]bool)
	for _, id := range a.account.QuestStoryClearedIDs() {
		cleared[id] = true
	}
	for _, section := range a.initialState.MainQuestReadingSections {
		groups := make([]any, 0, len(section.BossGroups))
		for _, group := range section.BossGroups {
			stories := make([]any, 0, len(group.Stories))
			complete := 0
			for _, story := range group.Stories {
				state := 1
				if cleared[story.StoryTeamBattleID] {
					state = 2
					complete++
				}
				stories = append(stories, map[string]any{
					"story_teambattleid": story.StoryTeamBattleID, "talkid": story.TalkID,
					"state_flag": state, "title": story.Title, "is_lock": 0,
					"result_costumeid": story.ResultCostumeID, "unlock": readingStoryUnlock(),
				})
			}
			groups = append(groups, map[string]any{
				"boss_groupid": group.GroupID, "name": group.Name, "pictid": group.PictID,
				"stage_type": gamestate.BattleStageMainQuest, "searchid": 0,
				"appear_end": 0, "recommended_arthur": 0, "stage_quest_areaid": 0,
				"bosses": []any{}, "stories": stories, "bonus_flag": 0, "bonus_end_time": 0,
				"main_quest_achievement_num": complete, "main_quest_achievement_max": len(stories),
			})
		}
		sections = append(sections, map[string]any{
			"sectionid": section.SectionID, "title": section.Title, "bg_pictid": section.BGPictID,
			"icon_pictid": section.IconPictID, "boss_groups": groups, "is_lock": 0, "unlock": readingStoryUnlock(),
		})
	}
	if a.account.ClearPendingStageQuest() && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{"sections": sections})
}

func readingStoryUnlock() map[string]any {
	return map[string]any{
		"logical_op": 0, "boss_infos": []any{}, "story_tab_name": "", "story_sectionid": 0,
		"story_section_title": "", "story_title": "", "story_sub_title": []string{},
	}
}

func trimQuestBossDecorations(boss map[string]any) {
	for _, key := range []string{"needrank", "autofight", "need_hp", "mission_related", "is_punished", "start_rule"} {
		delete(boss, key)
	}
}
