package admin

import (
	"fmt"
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// Mission targets name every BOSS and story ID that validateMissionTargets accepts, so the
// 任务与成就 pickers offer exactly the IDs a series can be published with.

type missionBossTarget struct {
	BossID     int    `json:"boss_id"`
	Category   string `json:"category"` // main | boss | past | material | tower | other
	GroupID    int    `json:"group_id,omitempty"`
	Group      string `json:"group"`
	Difficulty string `json:"difficulty,omitempty"`
	OwnDeckOf  int    `json:"own_deck_of,omitempty"` // the standard BOSS of an own-deck variant
}

type missionStoryTarget struct {
	StoryID  int    `json:"story_id"`
	Category string `json:"category"` // main | alchemist | quest (七圣王) | cn_main | sub | event
	Group    string `json:"group"`
	Title    string `json:"title"`
}

type missionRankTarget struct {
	RankID int    `json:"rank_id"`
	Name   string `json:"name"`
}

func (a *API) missionTargets(w http.ResponseWriter, _ *http.Request) {
	catalog, err := a.operations.storage.CatalogState()
	if err != nil {
		WriteAdminError(w, 500, err.Error())
		return
	}
	ranks := make([]missionRankTarget, 0, len(a.pvpRanks))
	for _, rank := range a.pvpRanks {
		ranks = append(ranks, missionRankTarget{RankID: rank.RankID, Name: rank.Name})
	}
	a.operations.configMu.RLock()
	bosses := a.missionBossTargetsLocked(catalog)
	a.operations.configMu.RUnlock()
	WriteAdminJSON(w, 200, map[string]any{"bosses": bosses, "stories": missionStoryTargets(catalog), "pvp_ranks": ranks})
}

// missionBossTargetsLocked lists the activity and past groups in catalog order, then the towers and any
// other reward profile, each ID once. Names come from the drop catalog that Boss 掉落 shows. The caller
// holds operations.configMu (the series action holds it for writing).
func (a *API) missionBossTargetsLocked(catalog gamestate.State) []missionBossTarget {
	c := a.operations.content
	towers := map[int]DropBoss{}
	if c != nil {
		for _, row := range c.towerBosses {
			towers[row.BossID] = row
		}
	}
	result := []missionBossTarget{}
	seen := map[int]bool{}
	add := func(id int, group string) {
		if id <= 0 || seen[id] {
			return
		}
		seen[id] = true
		target := missionBossTarget{BossID: id, Category: "other", Group: group}
		if c != nil {
			source := c.rewardBossID(id)
			if row, ok := c.bosses[source]; ok {
				target.Category, target.GroupID, target.Group, target.Difficulty = row.Category, row.GroupID, row.Name, row.Difficulty
				if game.IsMainQuestArea(row.GroupID) {
					target.Category = "main"
				}
				if source != id {
					target.OwnDeckOf = source
				}
			} else if row, ok := towers[id]; ok {
				target.Category, target.GroupID, target.Group, target.Difficulty = "tower", row.GroupID, row.Name, row.Difficulty
			}
		}
		result = append(result, target)
	}
	for _, groups := range [][]AdminBattleGroup{a.groups, a.pastGroups} {
		for _, g := range groups {
			for _, id := range g.BossIDs {
				add(id, g.Name)
			}
		}
	}
	for _, profile := range catalog.TeamBattleRewards {
		add(profile.BossID, "")
	}
	return result
}

// missionMainQuestBossLocked reports whether a BOSS is a 主线 stage. Main stages belong to 普通 achievements;
// every other BOSS belongs to the timed tabs. The caller holds operations.configMu.
func (a *API) missionMainQuestBossLocked(id int) bool {
	c := a.operations.content
	if c == nil {
		return false
	}
	row, ok := c.bosses[c.rewardBossID(id)]
	return ok && game.IsMainQuestArea(row.GroupID)
}

// missionStoryTargets lists the story IDs a 指定剧情已完成 rule can name, grouped as the client shows them:
// main parts 1–2 as 主线 and part 3 as 炼金 (its own MainStoryProgressionChain), then the 七圣王 (圣王篇)
// reading stories, 国服专属, character and event stories.
func missionStoryTargets(catalog gamestate.State) []missionStoryTarget {
	result := []missionStoryTarget{}
	seen := map[int]bool{}
	add := func(id int, category, group, title string) {
		if id <= 0 || seen[id] {
			return
		}
		seen[id] = true
		result = append(result, missionStoryTarget{StoryID: id, Category: category, Group: group, Title: title})
	}
	for _, parts := range []struct {
		cn    bool
		parts []gamestate.StoryMainPart
	}{{false, catalog.Story.MainParts}, {true, catalog.Story.CNMainParts}} {
		for _, part := range parts.parts {
			category := "main"
			if parts.cn {
				category = "cn_main"
			} else if game.MainStoryProgressionChain(part.StoryMainPartID) != 0 {
				category = "alchemist"
			}
			for _, section := range part.Sections {
				for _, story := range section.Stories {
					add(story.StoryMainID, category, part.Name+" · "+section.SectionTitle, story.Title)
				}
			}
		}
	}
	for _, section := range catalog.MainQuestReadingSections {
		for _, group := range section.BossGroups {
			for _, story := range group.Stories {
				add(story.StoryTeamBattleID, "quest", section.Title+" · "+group.Name, story.Title)
			}
		}
	}
	for _, character := range catalog.Story.SubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				add(story.StorySubID, "sub", character.Name+" · "+section.SectionTitle, story.Title)
			}
		}
	}
	for _, event := range catalog.Story.Events {
		for _, story := range event.Stories {
			add(story.Sub.StorySubID, "event", event.Name, story.Sub.Title)
		}
	}
	// The 暗堕型 stories are character stories; listed here only if the character catalog lacks them.
	jobs := [...]string{"", "佣兵", "富豪", "盗贼", "歌姬", "炼金"}
	for _, quest := range gamestate.BurstQuests() {
		job := fmt.Sprintf("职业 %d", quest.ArthurType)
		if int(quest.ArthurType) < len(jobs) {
			job = jobs[quest.ArthurType]
		}
		for i, storyID := range quest.StoryIDs {
			add(storyID, "sub", "暗堕型 "+job+"亚瑟", fmt.Sprintf("第 %d 段", i+1))
		}
	}
	return result
}
