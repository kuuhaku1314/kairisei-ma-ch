package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
)

const (
	cnNormalQuestAreaMin = 100001
	cnNormalQuestAreaMax = 100018
	cnGeneratedGroupMin  = 700000000
	cnGeneratedGroupMax  = 800000000
)

// IsMainQuestArea reports whether a team battle group is a 主线 stage area (MainQuestShow).
func IsMainQuestArea(areaID int) bool {
	return areaID >= cnNormalQuestAreaMin && areaID <= cnNormalQuestAreaMax
}

type stageQuestPublicationProgress struct {
	StageQuest struct {
		AreaID int `json:"areaid"`
		Stages []struct {
			StageID     int `json:"stageid"`
			IsClearDone int `json:"is_clear_done"`
		} `json:"stage_object"`
	} `json:"stage_quest"`
}

// projectCNTeamBattlePublication is the JSON boundary for callers that need the
// complete stock DTO. Publication itself works on a single decoded catalog.
func projectCNTeamBattlePublication(
	configuration json.RawMessage,
	stageQuests map[int]json.RawMessage,
	limitedGroupIDs []int,
	tutorialNormalQuest bool,
	tutorialActivity bool,
) (json.RawMessage, error) {
	catalog, err := decodeTeamBattleCatalog(configuration)
	if err != nil {
		return nil, err
	}
	if err := catalog.project(stageQuests, limitedGroupIDs, tutorialNormalQuest, tutorialActivity); err != nil {
		return nil, err
	}
	return catalog.MarshalJSON()
}

// Keep the stock client's four list contracts distinct: account-progression
// normal quests, 3D activities, limited/key 2D activities and ordinary 2D
// activities. Assemble and validate the plan before changing catalog contents.
func (catalog *TeamBattleCatalog) project(
	stageQuests map[int]json.RawMessage,
	limitedGroupIDs []int,
	tutorialNormalQuest bool,
	tutorialActivity bool,
) error {
	unlocked, hasNormalQuest, err := unlockedCNNormalQuestAreas(stageQuests)
	if err != nil {
		return err
	}
	limitedIDs := make(map[int]struct{}, len(limitedGroupIDs))
	for _, groupID := range limitedGroupIDs {
		if groupID <= 0 {
			return errors.New("team battle publication contains an invalid limited group")
		}
		limitedIDs[groupID] = struct{}{}
	}
	groups := make(map[string][]teamBattleCatalogGroup, 4)
	seen := make(map[int]struct{})
	for _, group := range catalog.groups["9"] {
		if group.id <= 0 {
			return errors.New("team battle publication contains an invalid group")
		}
		seen[group.id] = struct{}{}
		switch {
		case group.areaID >= cnNormalQuestAreaMin && group.areaID <= cnNormalQuestAreaMax:
			if progress, available := unlocked[group.areaID]; available {
				if err := group.validateNormalProgress(progress, tutorialNormalQuest && group.areaID == cnNormalQuestAreaMin); err != nil {
					return err
				}
				groups["9"] = append(groups["9"], group)
			}
		case group.id > cnGeneratedGroupMin && group.id < cnGeneratedGroupMax:
			is3D, err := group.is3D()
			if err != nil {
				return err
			}
			// Historical limited 3D activities still belong to the 3D list.
			key := "12"
			if is3D {
				key = "10"
			} else if _, limited := limitedIDs[group.id]; limited {
				key = "11"
			}
			groups[key] = append(groups[key], group)
		case !hasNormalQuest:
			groups["9"] = append(groups["9"], group)
		}
	}
	// Retain explicitly categorized groups without duplicating generated ones.
	for _, key := range teamBattleCategoryKeys[1:] {
		for _, group := range catalog.groups[key] {
			if group.id <= 0 {
				return fmt.Errorf("decode team battle category %s identity", key)
			}
			if _, duplicate := seen[group.id]; duplicate {
				continue
			}
			seen[group.id] = struct{}{}
			groups[key] = append(groups[key], group)
		}
	}
	for _, key := range teamBattleCategoryKeys {
		if err := sortCNTeamBattleGroupsByFirstPublication(groups[key], key == "9"); err != nil {
			return err
		}
	}
	if tutorialActivity {
		for _, key := range []string{"10", "12"} {
			if len(groups[key]) == 0 {
				return errors.New("CN activity tutorial categories are unavailable")
			}
			group := &groups[key][0]
			if err := group.selectTutorialBoss(); err != nil {
				return err
			}
			// Quest 1047 guides the first family and its first difficulty.
			// Other published families return after this onboarding step.
			groups[key] = groups[key][:1]
		}
	}
	for index := range groups["9"] {
		group := &groups["9"][index]
		if progress, available := unlocked[group.areaID]; available {
			group.applyNormalProgress(progress, tutorialNormalQuest && group.areaID == cnNormalQuestAreaMin)
		}
	}
	catalog.groups = groups
	return nil
}

func (group teamBattleCatalogGroup) validateNormalProgress(progress stageQuestPublicationProgress, tutorial bool) error {
	if progress.StageQuest.AreaID != group.areaID || len(progress.StageQuest.Stages) != len(group.bosses) {
		return fmt.Errorf("decode normal quest area %d progress", group.areaID)
	}
	for index, stage := range progress.StageQuest.Stages {
		if stage.StageID != group.bosses[index].id || stage.IsClearDone < 0 || stage.IsClearDone > 1 {
			return fmt.Errorf("normal quest area %d progression differs from its group", group.areaID)
		}
	}
	if tutorial && (group.areaID != cnNormalQuestAreaMin || len(group.bosses) == 0 || progress.StageQuest.Stages[0].IsClearDone != 0) {
		return errors.New("normal quest tutorial does not target the fresh first area")
	}
	return nil
}

// StageQuest tracks clears internally; publishing stock stage type 11/15 would
// open conquest/occupation UI. Regular TeamSlSt uses type 0 and per-boss locks.
func (group *teamBattleCatalogGroup) applyNormalProgress(progress stageQuestPublicationProgress, tutorial bool) {
	firstUncleared := len(group.bosses)
	cleared := 0
	for index, stage := range progress.StageQuest.Stages {
		if stage.IsClearDone == 0 && firstUncleared == len(group.bosses) {
			firstUncleared = index
		}
		state, lock := json.Number("0"), json.Number("0")
		if stage.IsClearDone != 0 {
			state = json.Number("2")
			cleared++
		}
		if index > firstUncleared {
			lock = json.Number("1")
		}
		group.bosses[index].fields["10"] = state
		group.bosses[index].fields["26"] = lock
	}
	group.fields["main_quest_achievement_max"] = len(progress.StageQuest.Stages)
	group.fields["main_quest_achievement_num"] = cleared
	// The first onboarding guide targets row zero and cannot scroll.
	if tutorial {
		group.bosses = group.bosses[:1]
	}
	group.fields["1"] = json.Number("0")
}

// Quest 1047 targets row zero. The local archive groups official difficulties
// by visual family, so publish its earliest/easiest identity for that guide.
func (group *teamBattleCatalogGroup) selectTutorialBoss() error {
	if len(group.bosses) == 0 {
		return errors.New("decode activity tutorial bosses")
	}
	selected := group.bosses[0]
	for _, boss := range group.bosses {
		if boss.id <= 0 {
			return errors.New("decode activity tutorial boss identity")
		}
		if boss.id < selected.id {
			selected = boss
		}
	}
	group.bosses = []teamBattleCatalogBoss{selected}
	return nil
}

// INFERRED chronology from official master identities, not invented dates.
// pict_id is a reusable visual identity and cannot represent publication order.
func sortCNTeamBattleGroupsByFirstPublication(groups []teamBattleCatalogGroup, normal bool) error {
	primary := func(group teamBattleCatalogGroup) int {
		if normal {
			return group.areaID
		}
		first := 0
		for _, boss := range group.bosses {
			if boss.id > 0 && (first == 0 || boss.id < first) {
				first = boss.id
			}
		}
		return first
	}
	type orderedGroup struct {
		group     teamBattleCatalogGroup
		primaryID int
	}
	ordered := make([]orderedGroup, len(groups))
	for index, group := range groups {
		primaryID := primary(group)
		if group.id <= 0 || primaryID <= 0 {
			return fmt.Errorf("team battle group %d has no publication identity", group.id)
		}
		ordered[index] = orderedGroup{group, primaryID}
	}
	sort.SliceStable(ordered, func(left, right int) bool {
		if ordered[left].primaryID != ordered[right].primaryID {
			return ordered[left].primaryID < ordered[right].primaryID
		}
		return ordered[left].group.id < ordered[right].group.id
	})
	for index := range ordered {
		groups[index] = ordered[index].group
	}
	return nil
}

func (group teamBattleCatalogGroup) is3D() (bool, error) {
	if len(group.bosses) == 0 {
		return false, fmt.Errorf("team battle group %d has no bosses", group.id)
	}
	is3D := group.bosses[0].model == 1
	for _, boss := range group.bosses {
		if boss.model != 0 && boss.model != 1 {
			return false, fmt.Errorf("team battle group %d has an invalid render mode", group.id)
		}
		if (boss.model == 1) != is3D {
			return false, fmt.Errorf("team battle group %d mixes 2D and 3D bosses", group.id)
		}
	}
	return is3D, nil
}

func unlockedCNNormalQuestAreas(
	stageQuests map[int]json.RawMessage,
) (map[int]stageQuestPublicationProgress, bool, error) {
	areaIDs := make([]int, 0, len(stageQuests))
	progress := make(map[int]stageQuestPublicationProgress, len(stageQuests))
	for areaID, raw := range stageQuests {
		if areaID < cnNormalQuestAreaMin || areaID > cnNormalQuestAreaMax {
			continue
		}
		var area stageQuestPublicationProgress
		if err := json.Unmarshal(raw, &area); err != nil || area.StageQuest.AreaID != areaID ||
			len(area.StageQuest.Stages) == 0 {
			return nil, false, fmt.Errorf("decode normal quest area %d progress", areaID)
		}
		areaIDs = append(areaIDs, areaID)
		progress[areaID] = area
	}
	if len(areaIDs) == 0 {
		return map[int]stageQuestPublicationProgress{}, false, nil
	}
	sort.Ints(areaIDs)
	unlocked := make(map[int]stageQuestPublicationProgress, len(areaIDs))
	previousComplete := true
	for _, areaID := range areaIDs {
		if !previousComplete {
			break
		}
		unlocked[areaID] = progress[areaID]
		previousComplete = true
		for _, stage := range progress[areaID].StageQuest.Stages {
			if stage.IsClearDone == 0 {
				previousComplete = false
				break
			}
		}
	}
	return unlocked, true, nil
}

func projectCNStageQuestPublication(configuration json.RawMessage) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, err
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, err
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil || len(stages) == 0 {
		return nil, errors.New("normal quest stage catalog is empty")
	}
	previousClear := true
	for index := range stages {
		released := previousClear
		var isClear int
		if err := json.Unmarshal(stages[index]["is_clear_done"], &isClear); err != nil {
			return nil, errors.New("decode normal quest clear state")
		}
		if released {
			stages[index]["is_release_now"] = json.RawMessage("1")
		} else {
			stages[index]["is_release_now"] = json.RawMessage("0")
		}
		previousClear = released && isClear != 0
	}
	encoded, err := json.Marshal(stages)
	if err != nil {
		return nil, err
	}
	stageQuest["stage_object"] = encoded
	encoded, err = json.Marshal(stageQuest)
	if err != nil {
		return nil, err
	}
	top["stage_quest"] = encoded
	return json.Marshal(top)
}
