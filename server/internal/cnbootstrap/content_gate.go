package cnbootstrap

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

type cnRuntimeContentGate struct {
	TeamBattleGroups      int
	TeamBattleBossEntries int
	UserBuffProfiles      int
	UserBuffReferences    int
	StageQuestStages      int
	ExploreStages         int
	ExploreFloors         int
	MainStories           int
	CNMainStories         int
	SubStorySections      int
	SubStories            int
	StoryEvents           int
	EventStories          int
	StoryBattleIDs        int
	StoryBattleReferences int
	EventPageProfiles     int
	PopupProfiles         int
}

type cnGateTeamBattleSolo struct {
	Groups        []masterdata.GateTeamBattleGroup `json:"9"`
	EventGroups   []json.RawMessage                `json:"10"`
	SpecialGroups []json.RawMessage                `json:"11"`
	OtherGroups   []json.RawMessage                `json:"12"`
	Notifications []json.RawMessage                `json:"14"`
}

type cnGateStageQuestBoss struct {
	BossID      int    `json:"bossid"`
	Difficulty  string `json:"difficulty"`
	BPUse       int    `json:"bp_use"`
	PictID      int    `json:"pictid"`
	State       int    `json:"state"`
	RewardCards []struct {
		CardID int `json:"cardid"`
	} `json:"reward_cardids"`
	RewardSpheres []json.RawMessage `json:"reward_sphrids"`
	IsModel       int               `json:"is_model"`
	UserBuffIDs   []int             `json:"user_buff_id"`
	Awake         json.RawMessage   `json:"awake"`
	Challenge     []json.RawMessage `json:"challenge"`
	Unlock        json.RawMessage   `json:"unlock"`
}

type cnGateStageQuest struct {
	StageQuest struct {
		AreaID      int `json:"areaid"`
		StageObject []struct {
			StageID         int               `json:"stageid"`
			RequireStageIDs []int             `json:"require_stageid"`
			IsReleased      int               `json:"is_release_now"`
			TalkInfo        []json.RawMessage `json:"talk_info"`
			RaidBoss        []struct {
				BossGroup struct {
					GroupID          int                    `json:"boss_groupid"`
					StageQuestAreaID int                    `json:"stage_quest_areaid"`
					Bosses           []cnGateStageQuestBoss `json:"bosses"`
					Stories          []json.RawMessage      `json:"stories"`
					ButtonStrings1   []string               `json:"button_str1"`
					ButtonStrings2   []string               `json:"button_str2"`
				} `json:"boss_group"`
				ClearReward  []json.RawMessage `json:"clear_reward"`
				AttackReward []json.RawMessage `json:"attack_reward"`
			} `json:"raid_boss"`
		} `json:"stage_object"`
	} `json:"stage_quest"`
	StageClear    []json.RawMessage `json:"stage_clear"`
	NewClearStage []int             `json:"new_clear_stage"`
}

func validateCNRunnableContent(state gamestate.State) (cnRuntimeContentGate, error) {
	var summary cnRuntimeContentGate
	// This gate checks runtime content/configuration. Mutable player balances
	// and recovery clocks are validated by accountstore at the save boundary.
	if state.BattlePoint.RecoverySeconds != 3*60 {
		return summary, errors.New("CN runtime battle point recovery contract is incomplete")
	}
	cardIDs := make(map[int]struct{}, len(state.CardTemplates))
	for _, card := range state.CardTemplates {
		if card.CardID > 0 {
			cardIDs[card.CardID] = struct{}{}
		}
	}
	if len(cardIDs) == 0 {
		return summary, errors.New("CN runtime content gate requires the official card master")
	}

	userBuffProfiles := make(map[int]struct{}, len(state.UserBuffProfiles))
	for _, profile := range state.UserBuffProfiles {
		if profile.UserBuffID <= 0 {
			return summary, errors.New("CN runtime user-buff profile is invalid")
		}
		if _, duplicate := userBuffProfiles[profile.UserBuffID]; duplicate {
			return summary, fmt.Errorf("duplicate CN runtime user-buff profile %d", profile.UserBuffID)
		}
		userBuffProfiles[profile.UserBuffID] = struct{}{}
	}
	if len(userBuffProfiles) == 0 {
		return summary, errors.New("CN runtime user-buff profiles are unavailable")
	}
	summary.UserBuffProfiles = len(userBuffProfiles)

	contexts, bossIDs, err := validateCNTeamBattleGate(state.TeamBattleSolo, userBuffProfiles, &summary)
	if err != nil {
		return summary, err
	}
	stageQuestAreas := state.StageQuestAreas
	if len(stageQuestAreas) == 0 {
		stageQuestAreas = []json.RawMessage{state.MainQuest}
	}
	stageContexts := make(map[[3]int]struct{})
	seenStageQuestAreas := make(map[int]struct{}, len(stageQuestAreas))
	for _, area := range stageQuestAreas {
		var identity cnGateStageQuest
		if json.Unmarshal(area, &identity) != nil || identity.StageQuest.AreaID <= 0 {
			return summary, errors.New("CN runtime StageQuest area identity is invalid")
		}
		if _, duplicate := seenStageQuestAreas[identity.StageQuest.AreaID]; duplicate {
			return summary, fmt.Errorf("duplicate CN runtime StageQuest area %d", identity.StageQuest.AreaID)
		}
		seenStageQuestAreas[identity.StageQuest.AreaID] = struct{}{}
		areaContexts, areaErr := validateCNStageQuestGate(area, contexts, userBuffProfiles, &summary)
		if areaErr != nil {
			return summary, areaErr
		}
		for key := range areaContexts {
			stageContexts[key] = struct{}{}
		}
	}
	for key := range contexts {
		if key[1] > 0 {
			delete(contexts, key)
		}
	}
	for key := range stageContexts {
		contexts[key] = struct{}{}
	}
	if err := validateCNBattleProfilesGate(state, contexts, bossIDs); err != nil {
		return summary, err
	}
	if err := validateCNExploreGate(state.Explore, &summary); err != nil {
		return summary, err
	}
	if err := validateCNStoryGate(state.Story, &summary); err != nil {
		return summary, err
	}
	if err := masterdata.ValidateEventPageProfile(state.EventPageProfile); err != nil {
		return summary, fmt.Errorf("CN runtime EventPage profile: %w", err)
	}
	summary.EventPageProfiles = 1
	return summary, nil
}

func validateCNTeamBattleGate(
	content json.RawMessage,
	userBuffProfiles map[int]struct{},
	summary *cnRuntimeContentGate,
) (map[[3]int]struct{}, map[int]struct{}, error) {
	var solo cnGateTeamBattleSolo
	if len(content) == 0 || bytes.Equal(content, []byte("null")) || json.Unmarshal(content, &solo) != nil {
		return nil, nil, errors.New("CN runtime TeamBattleSolo DTO is invalid")
	}
	if len(solo.Groups) == 0 || solo.EventGroups == nil || solo.SpecialGroups == nil ||
		solo.OtherGroups == nil || solo.Notifications == nil {
		return nil, nil, errors.New("CN runtime TeamBattleSolo arrays must be non-null")
	}
	groups := make(map[int]struct{}, len(solo.Groups))
	contexts := make(map[[3]int]struct{})
	bossIDs := make(map[int]struct{})
	for _, group := range solo.Groups {
		if group.GroupID <= 0 || group.Name == "" || group.PictID <= 0 || group.IsReleased < 1 || group.IsReleased > 7 ||
			group.StageQuestAreaID < 0 || len(group.Bosses) == 0 || group.Stories == nil ||
			group.ButtonStrings1 == nil || group.ButtonStrings2 == nil {
			return nil, nil, fmt.Errorf("CN runtime TeamBattle group %d is unavailable or incomplete", group.GroupID)
		}
		if group.StageQuestAreaID == 0 && group.StageType != 0 && group.StageType != 13 {
			return nil, nil, fmt.Errorf("standalone CN TeamBattle group %d has a StageQuest type", group.GroupID)
		}
		if group.StageQuestAreaID > 0 && group.StageType != 0 && group.StageType != 11 && group.StageType != 15 {
			return nil, nil, fmt.Errorf("CN StageQuest group %d has an unsupported stage type", group.GroupID)
		}
		if _, duplicate := groups[group.GroupID]; duplicate {
			return nil, nil, fmt.Errorf("duplicate CN TeamBattle group %d", group.GroupID)
		}
		groups[group.GroupID] = struct{}{}
		seenBosses := make(map[int]struct{}, len(group.Bosses))
		for _, boss := range group.Bosses {
			if err := masterdata.ValidateTeamBattleBossGate(boss); err != nil {
				return nil, nil, fmt.Errorf("CN TeamBattle group %d: %w", group.GroupID, err)
			}
			if err := validateCNUserBuffReferences(boss.BossID, boss.UserBuffIDs, userBuffProfiles); err != nil {
				return nil, nil, err
			}
			summary.UserBuffReferences += len(boss.UserBuffIDs)
			if _, duplicate := seenBosses[boss.BossID]; duplicate {
				return nil, nil, fmt.Errorf("CN TeamBattle group %d repeats boss %d", group.GroupID, boss.BossID)
			}
			seenBosses[boss.BossID] = struct{}{}
			bossIDs[boss.BossID] = struct{}{}
			contexts[[3]int{boss.BossID, group.StageQuestAreaID, 0}] = struct{}{}
			summary.TeamBattleBossEntries++
		}
		summary.TeamBattleGroups++
	}
	return contexts, bossIDs, nil
}

func validateCNUserBuffReferences(
	bossID int,
	userBuffIDs []int,
	profiles map[int]struct{},
) error {
	seen := make(map[int]struct{}, len(userBuffIDs))
	for _, userBuffID := range userBuffIDs {
		if userBuffID <= 0 {
			return fmt.Errorf("boss %d references invalid user buff %d", bossID, userBuffID)
		}
		if _, exists := profiles[userBuffID]; !exists {
			return fmt.Errorf("boss %d references unknown user buff %d", bossID, userBuffID)
		}
		if _, duplicate := seen[userBuffID]; duplicate {
			return fmt.Errorf("boss %d repeats user buff %d", bossID, userBuffID)
		}
		seen[userBuffID] = struct{}{}
	}
	return nil
}

func validateCNStageQuestBossGate(boss cnGateStageQuestBoss) error {
	if boss.BossID <= 0 || boss.Difficulty == "" || boss.BPUse <= 0 || boss.PictID <= 0 ||
		boss.State < 0 || boss.State > 2 || (boss.IsModel != 0 && boss.IsModel != 1) ||
		boss.RewardCards == nil || boss.RewardSpheres == nil ||
		(boss.UserBuffIDs != nil && len(boss.UserBuffIDs) == 0) ||
		len(boss.Awake) == 0 || bytes.Equal(boss.Awake, []byte("null")) ||
		boss.Challenge == nil || len(boss.Unlock) == 0 || bytes.Equal(boss.Unlock, []byte("null")) {
		return fmt.Errorf("boss %d DTO is unavailable or incomplete", boss.BossID)
	}
	for _, reward := range boss.RewardCards {
		if reward.CardID <= 0 {
			return fmt.Errorf("boss %d references an invalid card %d", boss.BossID, reward.CardID)
		}
	}
	return nil
}

func validateCNStageQuestGate(
	content json.RawMessage,
	teamBattleContexts map[[3]int]struct{},
	userBuffProfiles map[int]struct{},
	summary *cnRuntimeContentGate,
) (map[[3]int]struct{}, error) {
	var response cnGateStageQuest
	if len(content) == 0 || bytes.Equal(content, []byte("null")) || json.Unmarshal(content, &response) != nil {
		return nil, errors.New("CN runtime StageQuest DTO is invalid")
	}
	quest := response.StageQuest
	if quest.AreaID <= 0 || len(quest.StageObject) == 0 || response.StageClear == nil || response.NewClearStage == nil {
		return nil, errors.New("CN runtime StageQuest root is incomplete")
	}
	stageIDs := make(map[int]struct{}, len(quest.StageObject))
	contexts := make(map[[3]int]struct{})
	for _, stage := range quest.StageObject {
		if stage.StageID <= 0 || stage.IsReleased != 1 || stage.RequireStageIDs == nil ||
			stage.TalkInfo == nil || len(stage.RaidBoss) == 0 {
			return nil, fmt.Errorf("CN StageQuest stage %d is unavailable or incomplete", stage.StageID)
		}
		if _, duplicate := stageIDs[stage.StageID]; duplicate {
			return nil, fmt.Errorf("duplicate CN StageQuest stage %d", stage.StageID)
		}
		stageIDs[stage.StageID] = struct{}{}
		for _, raid := range stage.RaidBoss {
			group := raid.BossGroup
			if group.GroupID <= 0 || group.StageQuestAreaID != quest.AreaID || len(group.Bosses) == 0 ||
				group.Stories == nil || group.ButtonStrings1 == nil || group.ButtonStrings2 == nil ||
				raid.ClearReward == nil || raid.AttackReward == nil {
				return nil, fmt.Errorf("CN StageQuest stage %d boss group is incomplete", stage.StageID)
			}
			for _, boss := range group.Bosses {
				if err := validateCNStageQuestBossGate(boss); err != nil {
					return nil, fmt.Errorf("CN StageQuest stage %d: %w", stage.StageID, err)
				}
				if err := validateCNUserBuffReferences(boss.BossID, boss.UserBuffIDs, userBuffProfiles); err != nil {
					return nil, err
				}
				if _, published := teamBattleContexts[[3]int{boss.BossID, quest.AreaID, 0}]; !published {
					return nil, fmt.Errorf("CN StageQuest boss %d is absent from TeamBattleSoloShow", boss.BossID)
				}
				key := [3]int{boss.BossID, quest.AreaID, stage.StageID}
				contexts[key] = struct{}{}
			}
		}
		summary.StageQuestStages++
	}
	for _, stage := range quest.StageObject {
		for _, required := range stage.RequireStageIDs {
			if _, exists := stageIDs[required]; !exists {
				return nil, fmt.Errorf("CN StageQuest stage %d references unknown prerequisite %d", stage.StageID, required)
			}
		}
	}
	return contexts, nil
}

func validateCNBattleProfilesGate(
	state gamestate.State,
	contexts map[[3]int]struct{},
	bossIDs map[int]struct{},
) error {
	replays := make(map[int]struct{}, len(state.TeamBattleReplays))
	for _, replay := range state.TeamBattleReplays {
		if _, duplicate := replays[replay.BossID]; duplicate {
			return fmt.Errorf("duplicate CN TeamBattle replay for boss %d", replay.BossID)
		}
		replays[replay.BossID] = struct{}{}
	}
	for bossID := range bossIDs {
		if _, exists := replays[bossID]; !exists {
			return fmt.Errorf("CN TeamBattle boss %d has no replay profile", bossID)
		}
	}
	rewards := make(map[[3]int]struct{}, len(state.TeamBattleRewards))
	for _, profile := range state.TeamBattleRewards {
		key := [3]int{profile.BossID, profile.StageQuestAreaID, profile.StageQuestStageID}
		if _, duplicate := rewards[key]; duplicate {
			return fmt.Errorf("duplicate CN TeamBattle reward profile %v", key)
		}
		rewards[key] = struct{}{}
	}
	for key := range contexts {
		if _, exists := rewards[key]; !exists {
			return fmt.Errorf("CN TeamBattle context %v has no reward profile", key)
		}
	}
	return nil
}

func validateCNExploreGate(explore gamestate.ExploreProgressState, summary *cnRuntimeContentGate) error {
	stages := explore.Stages
	if len(stages) == 0 {
		stages = []gamestate.ExploreStage{explore.Stage}
	}
	if len(stages) == 0 || explore.Events == nil ||
		explore.APRecoverySeconds <= 0 || explore.Avatar.AvatarPartIDs == nil {
		return errors.New("CN runtime Explore DTO is unavailable or incomplete")
	}
	seenStages := make(map[int]struct{}, len(stages))
	seenFloors := make(map[int]struct{})
	for _, stage := range stages {
		if stage.ExploreStageID <= 0 || stage.StageName == "" || stage.StageMapID <= 0 ||
			stage.StateFlag&^7 != 0 || len(stage.Floors) == 0 {
			return errors.New("CN runtime Explore stage is incomplete")
		}
		if _, duplicate := seenStages[stage.ExploreStageID]; duplicate {
			return fmt.Errorf("duplicate CN Explore stage %d", stage.ExploreStageID)
		}
		seenStages[stage.ExploreStageID] = struct{}{}
		for _, floor := range stage.Floors {
			if floor.ExploreFloorID <= 0 || floor.FloorName == "" {
				return errors.New("CN runtime Explore floor is incomplete")
			}
			if _, duplicate := seenFloors[floor.ExploreFloorID]; duplicate {
				return fmt.Errorf("duplicate CN Explore floor %d", floor.ExploreFloorID)
			}
			seenFloors[floor.ExploreFloorID] = struct{}{}
			summary.ExploreFloors++
		}
		summary.ExploreStages++
	}
	if explore.Active {
		if _, exists := seenStages[explore.ActiveStageID]; !exists {
			return fmt.Errorf("active CN Explore stage %d is not published", explore.ActiveStageID)
		}
	}
	return nil
}

func validateCNStoryGate(story gamestate.StoryCatalogState, summary *cnRuntimeContentGate) error {
	if story.MainParts == nil || story.CNMainParts == nil || story.SubCharacters == nil || story.Events == nil {
		return errors.New("CN runtime story catalog arrays must be non-null")
	}
	mainCount, err := validateCNMainStoryParts(story.MainParts)
	if err != nil {
		return err
	}
	cnCount, err := validateCNMainStoryParts(story.CNMainParts)
	if err != nil {
		return err
	}
	summary.MainStories = mainCount
	summary.CNMainStories = cnCount

	seenSubStories := make(map[int]struct{})
	for _, character := range story.SubCharacters {
		if character.StoryMainCharacterID <= 0 || character.Name == "" || character.Sections == nil {
			return errors.New("CN runtime character story catalog is incomplete")
		}
		for _, section := range character.Sections {
			if section.StorySubSectionID <= 0 || section.SectionTitle == "" || section.PictID <= 0 || len(section.Stories) == 0 {
				return fmt.Errorf("CN character story section %d is incomplete", section.StorySubSectionID)
			}
			flags := make([]int, 0, len(section.Stories))
			for _, item := range section.Stories {
				if item.StorySubID <= 0 || item.Title == "" || item.FeatureReward.CardSkillLevels == nil {
					return fmt.Errorf("CN character story section %d has an incomplete story", section.StorySubSectionID)
				}
				if _, duplicate := seenSubStories[item.StorySubID]; duplicate {
					return fmt.Errorf("duplicate CN character story %d", item.StorySubID)
				}
				seenSubStories[item.StorySubID] = struct{}{}
				flags = append(flags, item.StateFlag)
				summary.SubStories++
			}
			if err := validateCNStoryProgress(flags, true); err != nil {
				return fmt.Errorf("CN character story section %d availability: %w", section.StorySubSectionID, err)
			}
			summary.SubStorySections++
		}
	}

	seenEvents := make(map[int]struct{}, len(story.Events))
	seenEventStories := make(map[int]struct{})
	for _, event := range story.Events {
		if event.StoryEventID <= 0 || event.Name == "" || event.PictID <= 0 || len(event.Stories) == 0 {
			return fmt.Errorf("CN event story %d is incomplete", event.StoryEventID)
		}
		if _, duplicate := seenEvents[event.StoryEventID]; duplicate {
			return fmt.Errorf("duplicate CN event story %d", event.StoryEventID)
		}
		seenEvents[event.StoryEventID] = struct{}{}
		flags := make([]int, 0, len(event.Stories))
		for _, wrapper := range event.Stories {
			item := wrapper.Sub
			if item.StorySubID <= 0 || item.Title == "" || item.FeatureReward.CardSkillLevels == nil || wrapper.Materials == nil {
				return fmt.Errorf("CN event story %d has an incomplete episode", event.StoryEventID)
			}
			if _, duplicate := seenEventStories[item.StorySubID]; duplicate {
				return fmt.Errorf("duplicate CN event episode %d", item.StorySubID)
			}
			seenEventStories[item.StorySubID] = struct{}{}
			flags = append(flags, item.StateFlag)
			summary.EventStories++
		}
		if err := validateCNStoryProgress(flags, true); err != nil {
			return fmt.Errorf("CN event story %d availability: %w", event.StoryEventID, err)
		}
		summary.StoryEvents++
	}
	if story.StoryBattleIDs == nil || story.StoryBattleReferences == nil {
		return errors.New("CN runtime story fixed-battle arrays must be non-null")
	}
	officialBattles := make(map[int]struct{}, len(story.StoryBattleIDs))
	for _, battleID := range story.StoryBattleIDs {
		if battleID <= 0 {
			return fmt.Errorf("CN runtime story battle ID %d is invalid", battleID)
		}
		if _, duplicate := officialBattles[battleID]; duplicate {
			return fmt.Errorf("duplicate CN runtime story battle ID %d", battleID)
		}
		officialBattles[battleID] = struct{}{}
	}
	storyIDsByKind := map[string]map[int]struct{}{
		"main":    make(map[int]struct{}),
		"cn_main": make(map[int]struct{}),
		"sub":     make(map[int]struct{}),
	}
	for _, part := range story.MainParts {
		for _, section := range part.Sections {
			for _, item := range section.Stories {
				storyIDsByKind["main"][item.StoryMainID] = struct{}{}
			}
		}
	}
	for _, part := range story.CNMainParts {
		for _, section := range part.Sections {
			for _, item := range section.Stories {
				storyIDsByKind["cn_main"][item.StoryMainID] = struct{}{}
			}
		}
	}
	for _, character := range story.SubCharacters {
		for _, section := range character.Sections {
			for _, item := range section.Stories {
				storyIDsByKind["sub"][item.StorySubID] = struct{}{}
			}
		}
	}
	for _, event := range story.Events {
		for _, wrapper := range event.Stories {
			storyIDsByKind["sub"][wrapper.Sub.StorySubID] = struct{}{}
		}
	}
	seenReferences := make(map[string]struct{}, len(story.StoryBattleReferences))
	for _, reference := range story.StoryBattleReferences {
		kindStories, exists := storyIDsByKind[reference.StoryKind]
		if !exists {
			return fmt.Errorf("CN runtime story battle kind %q is invalid", reference.StoryKind)
		}
		if _, exists := kindStories[reference.StoryID]; !exists || len(reference.StoryBattleIDs) == 0 {
			return fmt.Errorf("CN runtime story battle reference %s/%d is invalid", reference.StoryKind, reference.StoryID)
		}
		key := fmt.Sprintf("%s:%d", reference.StoryKind, reference.StoryID)
		if _, duplicate := seenReferences[key]; duplicate {
			return fmt.Errorf("duplicate CN runtime story battle reference %s", key)
		}
		seenReferences[key] = struct{}{}
		for _, battleID := range reference.StoryBattleIDs {
			if _, exists := officialBattles[battleID]; !exists {
				return fmt.Errorf("CN runtime story %s references unknown fixed battle %d", key, battleID)
			}
		}
	}
	summary.StoryBattleIDs = len(officialBattles)
	summary.StoryBattleReferences = len(seenReferences)
	if summary.MainStories == 0 || summary.CNMainStories == 0 || summary.SubStorySections == 0 || summary.StoryEvents == 0 {
		return errors.New("CN runtime story catalog has an empty content class")
	}
	if summary.StoryBattleIDs == 0 || summary.StoryBattleReferences == 0 {
		return errors.New("CN runtime story fixed-battle catalog is empty")
	}
	return nil
}

func validateCNMainStoryParts(parts []gamestate.StoryMainPart) (int, error) {
	flags := make(map[int][]int)
	seenParts := make(map[int]struct{}, len(parts))
	seenSections := make(map[int]struct{})
	seenStories := make(map[int]struct{})
	for _, part := range parts {
		if part.StoryMainPartID <= 0 || part.Name == "" || len(part.Sections) == 0 {
			return 0, errors.New("CN main story part is incomplete")
		}
		if _, duplicate := seenParts[part.StoryMainPartID]; duplicate {
			return 0, fmt.Errorf("duplicate CN main story part %d", part.StoryMainPartID)
		}
		seenParts[part.StoryMainPartID] = struct{}{}
		chain := game.MainStoryProgressionChain(part.StoryMainPartID)
		for _, section := range part.Sections {
			if section.StoryMainSectionID <= 0 || section.SectionTitle == "" || len(section.Stories) == 0 {
				return 0, fmt.Errorf("CN main story section %d is incomplete", section.StoryMainSectionID)
			}
			if _, duplicate := seenSections[section.StoryMainSectionID]; duplicate {
				return 0, fmt.Errorf("duplicate CN main story section %d", section.StoryMainSectionID)
			}
			seenSections[section.StoryMainSectionID] = struct{}{}
			for _, item := range section.Stories {
				if item.StoryMainID <= 0 || item.Title == "" {
					return 0, fmt.Errorf("CN main story section %d has an incomplete story", section.StoryMainSectionID)
				}
				if _, duplicate := seenStories[item.StoryMainID]; duplicate {
					return 0, fmt.Errorf("duplicate CN main story %d", item.StoryMainID)
				}
				seenStories[item.StoryMainID] = struct{}{}
				flags[chain] = append(flags[chain], item.StateFlag)
			}
		}
	}
	if len(flags) == 0 {
		return 0, errors.New("story sequence is empty")
	}
	for chain, states := range flags {
		if err := validateCNStoryProgress(states, false); err != nil {
			return 0, fmt.Errorf("main story chain %d availability: %w", chain, err)
		}
	}
	return len(seenStories), nil
}

func validateCNStoryProgress(flags []int, allowAllLocked bool) error {
	if len(flags) == 0 {
		return errors.New("story sequence is empty")
	}
	phase := 0
	newCount := 0
	clearCount := 0
	lockedCount := 0
	for _, flag := range flags {
		if flag&16 != 0 {
			if flag&7 != 0 {
				return fmt.Errorf("story state flag %d mixes event lock with dynamic state", flag)
			}
			phase = 2
			lockedCount++
			continue
		}
		switch dynamic := flag & 7; dynamic {
		case 2:
			if phase != 0 {
				return errors.New("CLEAR appears after NEW or LOCK")
			}
			clearCount++
		case 1:
			if phase > 1 || newCount != 0 {
				return errors.New("story sequence has more than one NEW boundary")
			}
			phase = 1
			newCount++
		case 4:
			phase = 2
			lockedCount++
		default:
			return fmt.Errorf("story state flag %d has an invalid dynamic state", flag)
		}
	}
	if newCount == 0 && clearCount != len(flags) && !(allowAllLocked && lockedCount == len(flags)) {
		return errors.New("story sequence has no playable NEW boundary")
	}
	return nil
}
