package masterdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"kairisei.local/server/internal/gamestate"
)

const (
	maxNormalQuestRuntimeBytes = 4 * 1024 * 1024
	normalQuestAreaMin         = 100001
	normalQuestAreaMax         = 100018
)

type NormalQuestRuntimeMaster struct {
	MainQuestSection         *gamestate.MainQuestSectionDefinition         `json:"main_quest_section,omitempty"`
	MainQuestReadingSections []gamestate.MainQuestReadingSectionDefinition `json:"main_quest_reading_sections,omitempty"`
	EntryGroups              []json.RawMessage                             `json:"entry_groups"`
	Areas                    []json.RawMessage                             `json:"areas"`
	Replays                  []gamestate.TeamBattleReplay                  `json:"replays"`
	Rewards                  []gamestate.TeamBattleRewardProfile           `json:"rewards"`
	Summary                  struct {
		Areas                    int            `json:"areas"`
		EntryGroups              int            `json:"entry_groups"`
		Quests                   int            `json:"quests"`
		OrderedSegments          int            `json:"ordered_segments"`
		SelectedOfficialParties  int            `json:"selected_official_parties"`
		RewardContexts           int            `json:"reward_contexts"`
		SegmentCountDistribution map[string]int `json:"segment_count_distribution"`
	} `json:"summary"`
}

type normalQuestAreaIdentity struct {
	StageQuest struct {
		AreaID int `json:"areaid"`
		Stages []struct {
			StageID int `json:"stageid"`
			Raids   []struct {
				BossGroup struct {
					StageQuestAreaID int `json:"stage_quest_areaid"`
					Bosses           []struct {
						BossID     int `json:"bossid"`
						OnlyMyDeck int `json:"is_only_my_deck"`
						Continue   int `json:"is_continue"`
						StartRule  int `json:"start_rule"`
					} `json:"bosses"`
				} `json:"boss_group"`
			} `json:"raid_boss"`
		} `json:"stage_object"`
	} `json:"stage_quest"`
}

func LoadNormalQuestRuntimeMaster(masterPath string) (NormalQuestRuntimeMaster, error) {
	if masterPath == "" {
		return NormalQuestRuntimeMaster{}, errors.New("CN normal quest runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return NormalQuestRuntimeMaster{}, fmt.Errorf("resolve CN normal quest runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return NormalQuestRuntimeMaster{}, fmt.Errorf("open CN normal quest runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return NormalQuestRuntimeMaster{}, fmt.Errorf("stat CN normal quest runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxNormalQuestRuntimeBytes {
		return NormalQuestRuntimeMaster{}, errors.New("CN normal quest runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxNormalQuestRuntimeBytes+1))
	var master NormalQuestRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return NormalQuestRuntimeMaster{}, fmt.Errorf("decode CN normal quest runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return NormalQuestRuntimeMaster{}, err
	}
	if err := validateNormalQuestRuntimeMaster(master); err != nil {
		return NormalQuestRuntimeMaster{}, err
	}
	return master, nil
}

func validateNormalQuestRuntimeMaster(master NormalQuestRuntimeMaster) error {
	if len(master.EntryGroups) != 18 || len(master.Areas) != 18 ||
		len(master.Replays) != 50 || len(master.Rewards) != 100 {
		return errors.New("CN normal quest runtime master identity or coverage is invalid")
	}
	if section := master.MainQuestSection; section != nil &&
		(section.SectionID <= 0 || section.Title == "" || section.BGPictID <= 0 || section.IconPictID <= 0) {
		return errors.New("normal quest directory header is invalid")
	}
	if err := validateMainQuestReadingSections(master); err != nil {
		return err
	}

	entryBosses := make(map[int][2]int, len(master.Replays))
	entryRules := make(map[int]battleEntryRules, len(master.Replays))
	areaIDs := make(map[int]struct{}, len(master.EntryGroups))
	for _, raw := range master.EntryGroups {
		var group GateTeamBattleGroup
		if err := json.Unmarshal(raw, &group); err != nil ||
			group.GroupID < normalQuestAreaMin || group.GroupID > normalQuestAreaMax ||
			group.StageQuestAreaID != group.GroupID || group.StageType != 0 ||
			group.IsReleased != 1 || group.Name == "" || group.PictID <= 0 ||
			len(group.Bosses) == 0 || group.Stories == nil ||
			group.ButtonStrings1 == nil || group.ButtonStrings2 == nil {
			return errors.New("CN normal quest entry group is invalid")
		}
		if _, duplicate := areaIDs[group.GroupID]; duplicate {
			return fmt.Errorf("duplicate CN normal quest area %d", group.GroupID)
		}
		areaIDs[group.GroupID] = struct{}{}
		for _, boss := range group.Bosses {
			if err := ValidateTeamBattleBossGate(boss); err != nil || boss.IsModel != 0 ||
				boss.BossID/100 != group.GroupID {
				return fmt.Errorf("CN normal quest entry group %d contains invalid boss %d", group.GroupID, boss.BossID)
			}
			if _, duplicate := entryBosses[boss.BossID]; duplicate {
				return fmt.Errorf("duplicate CN normal quest boss %d", boss.BossID)
			}
			entryBosses[boss.BossID] = [2]int{group.GroupID, boss.BPUse}
			entryRules[boss.BossID] = boss.battleEntryRules
		}
	}
	for areaID := normalQuestAreaMin; areaID <= normalQuestAreaMax; areaID++ {
		if _, exists := areaIDs[areaID]; !exists {
			return fmt.Errorf("CN normal quest area %d is absent", areaID)
		}
	}
	if len(entryBosses) != 50 {
		return errors.New("CN normal quest entry boss coverage is invalid")
	}

	stageContexts := make(map[[3]int]struct{}, len(entryBosses))
	for _, raw := range master.Areas {
		if err := ValidateStageQuestConfiguration(raw); err != nil {
			return fmt.Errorf("validate CN normal quest area: %w", err)
		}
		var area normalQuestAreaIdentity
		if err := json.Unmarshal(raw, &area); err != nil {
			return fmt.Errorf("decode CN normal quest area identity: %w", err)
		}
		areaID := area.StageQuest.AreaID
		if _, exists := areaIDs[areaID]; !exists {
			return fmt.Errorf("CN normal quest DTO has unknown area %d", areaID)
		}
		for _, stage := range area.StageQuest.Stages {
			if stage.StageID/100 != areaID || len(stage.Raids) != 1 ||
				stage.Raids[0].BossGroup.StageQuestAreaID != areaID ||
				len(stage.Raids[0].BossGroup.Bosses) != 1 ||
				stage.Raids[0].BossGroup.Bosses[0].BossID != stage.StageID {
				return fmt.Errorf("CN normal quest area %d stage %d identity is invalid", areaID, stage.StageID)
			}
			if _, exists := entryBosses[stage.StageID]; !exists {
				return fmt.Errorf("CN normal quest stage %d is absent from its entry group", stage.StageID)
			}
			boss := stage.Raids[0].BossGroup.Bosses[0]
			if (battleEntryRules{OnlyMyDeck: boss.OnlyMyDeck, Continue: boss.Continue, StartRule: boss.StartRule}) != entryRules[stage.StageID] {
				return fmt.Errorf("CN normal quest stage %d entry rules differ from its entry group", stage.StageID)
			}
			key := [3]int{stage.StageID, areaID, stage.StageID}
			if _, duplicate := stageContexts[key]; duplicate {
				return fmt.Errorf("duplicate CN normal quest stage context %v", key)
			}
			stageContexts[key] = struct{}{}
		}
	}
	if len(stageContexts) != len(entryBosses) {
		return errors.New("CN normal quest StageQuest coverage is incomplete")
	}

	replayIDs := make(map[int]struct{}, len(master.Replays))
	selectedParties := make(map[int]struct{})
	segmentCounts := make(map[string]int)
	orderedSegments := 0
	for _, replay := range master.Replays {
		entry, exists := entryBosses[replay.BossID]
		if !exists || replay.EnemyPartyID != replay.BossID || replay.EnemyType != 1 ||
			replay.Seed != replay.BossID || replay.CostInitial != 3 ||
			replay.BurstGaugeInitial != 0 || replay.HoldMax != 6 || replay.EndTurn != 0 ||
			len(replay.Battles) < 3 || len(replay.Battles) > 6 ||
			replay.Battles[0].EnemyPartyID != replay.EnemyPartyID ||
			replay.Battles[0].EnemyType != replay.EnemyType || entry[0] != replay.BossID/100 {
			return fmt.Errorf("CN normal quest replay %d is invalid", replay.BossID)
		}
		if _, duplicate := replayIDs[replay.BossID]; duplicate {
			return fmt.Errorf("duplicate CN normal quest replay %d", replay.BossID)
		}
		replayIDs[replay.BossID] = struct{}{}
		for _, segment := range replay.Battles {
			if segment.EnemyPartyID/100 != entry[0] || segment.EnemyType != 1 {
				return fmt.Errorf("CN normal quest replay %d contains an invalid segment", replay.BossID)
			}
			if _, duplicate := selectedParties[segment.EnemyPartyID]; duplicate {
				return fmt.Errorf("CN normal quest party %d is selected more than once", segment.EnemyPartyID)
			}
			selectedParties[segment.EnemyPartyID] = struct{}{}
			orderedSegments++
		}
		segmentCounts[fmt.Sprint(len(replay.Battles))]++
	}
	if len(replayIDs) != len(entryBosses) || len(selectedParties) != 228 || orderedSegments != 228 {
		return errors.New("CN normal quest replay segment coverage is incomplete")
	}

	rewardContexts := make(map[[3]int]struct{}, len(master.Rewards))
	for _, profile := range master.Rewards {
		entry, exists := entryBosses[profile.BossID]
		key := [3]int{profile.BossID, profile.StageQuestAreaID, profile.StageQuestStageID}
		isDirectEntry := profile.StageQuestAreaID == 0 && profile.StageQuestStageID == 0
		isStageEntry := profile.StageQuestAreaID == entry[0] && profile.StageQuestStageID == profile.BossID
		if !exists || (!isDirectEntry && !isStageEntry) ||
			len(profile.ResultRewards) != 3 ||
			(isDirectEntry && len(profile.FirstClearRewards) != 0) ||
			(isStageEntry && len(profile.FirstClearRewards) != 1) {
			return fmt.Errorf("CN normal quest reward context %v is invalid", key)
		}
		if _, duplicate := rewardContexts[key]; duplicate {
			return fmt.Errorf("duplicate CN normal quest reward context %v", key)
		}
		rewardContexts[key] = struct{}{}
		for _, reward := range append(append([]gamestate.Reward(nil), profile.ResultRewards...), profile.FirstClearRewards...) {
			if !ValidPersistedRewardShape(reward) {
				return fmt.Errorf("CN normal quest reward context %v has an invalid reward", key)
			}
		}
	}
	for bossID, entry := range entryBosses {
		for _, key := range [][3]int{{bossID, 0, 0}, {bossID, entry[0], bossID}} {
			if _, exists := rewardContexts[key]; !exists {
				return fmt.Errorf("CN normal quest reward context %v is absent", key)
			}
		}
	}
	if master.Summary.Areas != len(master.Areas) ||
		master.Summary.EntryGroups != len(master.EntryGroups) ||
		master.Summary.Quests != len(master.Replays) ||
		master.Summary.OrderedSegments != orderedSegments ||
		master.Summary.SelectedOfficialParties != len(selectedParties) ||
		master.Summary.RewardContexts != len(master.Rewards) ||
		len(master.Summary.SegmentCountDistribution) != len(segmentCounts) {
		return errors.New("CN normal quest runtime summary is inconsistent")
	}
	for count, expected := range segmentCounts {
		if master.Summary.SegmentCountDistribution[count] != expected {
			return fmt.Errorf("CN normal quest segment-count summary %s is inconsistent", count)
		}
	}
	return nil
}

func ApplyNormalQuestRuntimeMaster(state *gamestate.State, master NormalQuestRuntimeMaster) error {
	if err := mergeNormalQuestAreas(state, master.Areas); err != nil {
		return err
	}
	if err := mergeNormalQuestEntryGroups(state, master.EntryGroups); err != nil {
		return err
	}
	managedBosses := make(map[int]struct{}, len(master.Replays))
	for _, replay := range master.Replays {
		managedBosses[replay.BossID] = struct{}{}
	}
	replays := make([]gamestate.TeamBattleReplay, 0, len(state.TeamBattleReplays)+len(master.Replays))
	for _, replay := range state.TeamBattleReplays {
		if _, managed := managedBosses[replay.BossID]; !managed {
			replays = append(replays, replay)
		}
	}
	replays = append(replays, master.Replays...)
	state.TeamBattleReplays = replays
	rewards := make([]gamestate.TeamBattleRewardProfile, 0, len(state.TeamBattleRewards)+len(master.Rewards))
	for _, profile := range state.TeamBattleRewards {
		if _, managed := managedBosses[profile.BossID]; !managed {
			rewards = append(rewards, profile)
		}
	}
	rewards = append(rewards, master.Rewards...)
	state.TeamBattleRewards = rewards
	state.NormalQuestSection = master.MainQuestSection
	state.MainQuestReadingSections = master.MainQuestReadingSections
	return nil
}

func validateMainQuestReadingSections(master NormalQuestRuntimeMaster) error {
	sections := make(map[int]bool)
	groups := make(map[int]bool)
	stories := make(map[int]bool)
	if master.MainQuestSection != nil {
		sections[master.MainQuestSection.SectionID] = true
	}
	for _, section := range master.MainQuestReadingSections {
		if section.SectionID <= 0 || section.Title == "" || section.BGPictID <= 0 || section.IconPictID <= 0 ||
			sections[section.SectionID] || len(section.BossGroups) == 0 {
			return errors.New("main quest reading section is invalid or duplicated")
		}
		sections[section.SectionID] = true
		for _, group := range section.BossGroups {
			if group.GroupID <= 0 || group.Name == "" || group.PictID <= 0 || groups[group.GroupID] || len(group.Stories) == 0 {
				return errors.New("main quest reading group is invalid or duplicated")
			}
			groups[group.GroupID] = true
			for _, story := range group.Stories {
				if story.StoryTeamBattleID <= 0 || story.TalkID <= 0 || story.Title == "" || story.ResultCostumeID < 0 || stories[story.StoryTeamBattleID] {
					return errors.New("main quest reading story is invalid or duplicated")
				}
				stories[story.StoryTeamBattleID] = true
			}
		}
	}
	return nil
}

func mergeNormalQuestEntryGroups(state *gamestate.State, masterGroups []json.RawMessage) error {
	var solo map[string]json.RawMessage
	if err := json.Unmarshal(state.TeamBattleSolo, &solo); err != nil {
		return fmt.Errorf("decode CN TeamBattleSoloShow for normal quests: %w", err)
	}
	var persisted []json.RawMessage
	if err := json.Unmarshal(solo["9"], &persisted); err != nil {
		return fmt.Errorf("decode CN TeamBattle groups for normal quests: %w", err)
	}
	states := make(map[[2]int]int)
	kept := make([]json.RawMessage, 0, len(persisted))
	for _, raw := range persisted {
		var group GateTeamBattleGroup
		if err := json.Unmarshal(raw, &group); err != nil {
			return fmt.Errorf("decode persisted CN TeamBattle group: %w", err)
		}
		for _, boss := range group.Bosses {
			states[[2]int{boss.BossID, group.StageQuestAreaID}] = boss.State
		}
		managed := group.GroupID >= normalQuestAreaMin && group.GroupID <= normalQuestAreaMax &&
			group.StageQuestAreaID == group.GroupID
		if !managed {
			kept = append(kept, append(json.RawMessage(nil), raw...))
		}
	}
	clearedStates, err := collectNormalQuestClearedBossStates(state.StageQuestAreas)
	if err != nil {
		return err
	}
	for identity, clearedState := range clearedStates {
		if clearedState > states[identity] {
			states[identity] = clearedState
		}
	}
	merged := make([]json.RawMessage, 0, len(masterGroups)+len(kept))
	for _, raw := range masterGroups {
		var group map[string]json.RawMessage
		var identity GateTeamBattleGroup
		if err := json.Unmarshal(raw, &group); err != nil || json.Unmarshal(raw, &identity) != nil {
			return errors.New("decode CN normal quest entry group")
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["10"], &bosses); err != nil {
			return fmt.Errorf("decode CN normal quest entry bosses: %w", err)
		}
		for index, boss := range identity.Bosses {
			if persistedState, exists := states[[2]int{boss.BossID, identity.StageQuestAreaID}]; exists {
				bosses[index]["10"] = json.RawMessage(fmt.Sprint(persistedState))
			}
		}
		encodedBosses, err := json.Marshal(bosses)
		if err != nil {
			return err
		}
		group["10"] = encodedBosses
		encoded, err := json.Marshal(group)
		if err != nil {
			return err
		}
		merged = append(merged, encoded)
	}
	merged = append(merged, kept...)
	encodedGroups, err := json.Marshal(merged)
	if err != nil {
		return err
	}
	solo["9"] = encodedGroups
	state.TeamBattleSolo, err = json.Marshal(solo)
	return err
}

func collectNormalQuestClearedBossStates(
	areas []json.RawMessage,
) (map[[2]int]int, error) {
	states := make(map[[2]int]int)
	for _, raw := range areas {
		var progress NormalQuestProgress
		if err := json.Unmarshal(raw, &progress); err != nil {
			return nil, fmt.Errorf("decode CN normal quest progress for TeamBattle state: %w", err)
		}
		areaID := progress.StageQuest.AreaID
		if areaID < normalQuestAreaMin || areaID > normalQuestAreaMax {
			continue
		}
		for _, stage := range progress.StageQuest.Stages {
			if stage.IsClearDone == 0 {
				continue
			}
			for _, raid := range stage.Raids {
				for _, boss := range raid.BossGroup.Bosses {
					if boss.BossID > 0 {
						states[[2]int{boss.BossID, areaID}] = 2
					}
				}
			}
		}
	}
	return states, nil
}

func mergeNormalQuestAreas(state *gamestate.State, masterAreas []json.RawMessage) error {
	defaultAreaID, err := StageQuestAreaID(state.MainQuest)
	if err != nil {
		return err
	}
	persistedByID := make(map[int]json.RawMessage, len(state.StageQuestAreas)+1)
	persistedByID[defaultAreaID] = append(json.RawMessage(nil), state.MainQuest...)
	for _, raw := range state.StageQuestAreas {
		areaID, areaErr := StageQuestAreaID(raw)
		if areaErr != nil {
			return areaErr
		}
		persistedByID[areaID] = append(json.RawMessage(nil), raw...)
	}
	masterByID := make(map[int]json.RawMessage, len(masterAreas))
	for _, raw := range masterAreas {
		areaID, areaErr := StageQuestAreaID(raw)
		if areaErr != nil {
			return areaErr
		}
		masterByID[areaID] = raw
	}
	areaIDs := make([]int, 0, len(masterByID))
	for areaID := range masterByID {
		areaIDs = append(areaIDs, areaID)
	}
	sort.Ints(areaIDs)
	if len(areaIDs) == 0 {
		return errors.New("CN normal quest runtime has no canonical default area")
	}
	merged := make([]json.RawMessage, 0, len(persistedByID)+len(masterByID))
	for _, areaID := range areaIDs {
		area := append(json.RawMessage(nil), masterByID[areaID]...)
		if persisted, exists := persistedByID[areaID]; exists {
			area, err = MergeNormalQuestAreaProgress(area, persisted)
			if err != nil {
				return fmt.Errorf("merge CN normal quest area %d progress: %w", areaID, err)
			}
		}
		merged = append(merged, area)
		delete(persistedByID, areaID)
	}
	delete(persistedByID, battleRetiredStageID)
	extraIDs := make([]int, 0, len(persistedByID))
	for areaID := range persistedByID {
		extraIDs = append(extraIDs, areaID)
	}
	sort.Ints(extraIDs)
	for _, areaID := range extraIDs {
		merged = append(merged, persistedByID[areaID])
	}
	state.MainQuest = append(json.RawMessage(nil), merged[0]...)
	state.StageQuestAreas = merged
	return nil
}

type NormalQuestProgress struct {
	StageQuest struct {
		AreaID int `json:"areaid"`
		Stages []struct {
			StageID     int `json:"stageid"`
			IsClearDone int `json:"is_clear_done"`
			Raids       []struct {
				BossGroup struct {
					Bosses []struct {
						BossID int `json:"bossid"`
						State  int `json:"state"`
					} `json:"bosses"`
				} `json:"boss_group"`
				ClearRewards []struct {
					IsAlready int `json:"is_already"`
				} `json:"clear_reward"`
			} `json:"raid_boss"`
		} `json:"stage_object"`
	} `json:"stage_quest"`
	StageClear    json.RawMessage `json:"stage_clear"`
	NewClearStage json.RawMessage `json:"new_clear_stage"`
}

func MergeNormalQuestAreaProgress(staticArea, persistedArea json.RawMessage) (json.RawMessage, error) {
	var progress NormalQuestProgress
	if err := json.Unmarshal(persistedArea, &progress); err != nil {
		return nil, err
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(staticArea, &top); err != nil {
		return nil, err
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, err
	}
	var staticAreaID int
	if json.Unmarshal(stageQuest["areaid"], &staticAreaID) != nil || staticAreaID != progress.StageQuest.AreaID {
		return nil, errors.New("CN normal quest area progress identity differs from static catalog")
	}
	progressByStage := make(map[int]struct {
		ClearDone int
		BossState map[int]int
		Already   [][]int
	}, len(progress.StageQuest.Stages))
	for _, stage := range progress.StageQuest.Stages {
		value := struct {
			ClearDone int
			BossState map[int]int
			Already   [][]int
		}{ClearDone: stage.IsClearDone, BossState: make(map[int]int), Already: make([][]int, len(stage.Raids))}
		for raidIndex, raid := range stage.Raids {
			for _, boss := range raid.BossGroup.Bosses {
				value.BossState[boss.BossID] = boss.State
			}
			value.Already[raidIndex] = make([]int, len(raid.ClearRewards))
			for rewardIndex, reward := range raid.ClearRewards {
				value.Already[raidIndex][rewardIndex] = reward.IsAlready
			}
		}
		progressByStage[stage.StageID] = value
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil {
		return nil, err
	}
	for _, stage := range stages {
		var stageID int
		if json.Unmarshal(stage["stageid"], &stageID) != nil {
			return nil, errors.New("decode static CN normal quest stage identity")
		}
		value, exists := progressByStage[stageID]
		if !exists {
			continue
		}
		stage["is_clear_done"] = json.RawMessage(fmt.Sprint(value.ClearDone))
		var raids []map[string]json.RawMessage
		if err := json.Unmarshal(stage["raid_boss"], &raids); err != nil {
			return nil, err
		}
		for raidIndex, raid := range raids {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raid["boss_group"], &group); err != nil {
				return nil, err
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group["bosses"], &bosses); err != nil {
				return nil, err
			}
			for _, boss := range bosses {
				var bossID int
				if json.Unmarshal(boss["bossid"], &bossID) == nil {
					if value.ClearDone != 0 {
						boss["state"] = json.RawMessage("2")
					} else if state, present := value.BossState[bossID]; present {
						boss["state"] = json.RawMessage(fmt.Sprint(state))
					}
				}
			}
			encodedBosses, err := json.Marshal(bosses)
			if err != nil {
				return nil, err
			}
			group["bosses"] = encodedBosses
			encodedGroup, err := json.Marshal(group)
			if err != nil {
				return nil, err
			}
			raid["boss_group"] = encodedGroup
			var clearRewards []map[string]json.RawMessage
			if err := json.Unmarshal(raid["clear_reward"], &clearRewards); err != nil {
				return nil, err
			}
			if raidIndex < len(value.Already) {
				for rewardIndex := range clearRewards {
					if rewardIndex < len(value.Already[raidIndex]) {
						clearRewards[rewardIndex]["is_already"] = json.RawMessage(fmt.Sprint(value.Already[raidIndex][rewardIndex]))
					}
				}
			}
			encodedRewards, err := json.Marshal(clearRewards)
			if err != nil {
				return nil, err
			}
			raid["clear_reward"] = encodedRewards
		}
		encodedRaids, err := json.Marshal(raids)
		if err != nil {
			return nil, err
		}
		stage["raid_boss"] = encodedRaids
	}
	encodedStages, err := json.Marshal(stages)
	if err != nil {
		return nil, err
	}
	stageQuest["stage_object"] = encodedStages
	encodedStageQuest, err := json.Marshal(stageQuest)
	if err != nil {
		return nil, err
	}
	top["stage_quest"] = encodedStageQuest
	if len(progress.StageClear) > 0 && !bytes.Equal(progress.StageClear, []byte("null")) {
		top["stage_clear"] = append(json.RawMessage(nil), progress.StageClear...)
	}
	if len(progress.NewClearStage) > 0 && !bytes.Equal(progress.NewClearStage, []byte("null")) {
		top["new_clear_stage"] = append(json.RawMessage(nil), progress.NewClearStage...)
	}
	return json.Marshal(top)
}
