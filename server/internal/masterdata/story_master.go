package masterdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"slices"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

const maxStoryMasterBytes = 4 * 1024 * 1024

type storyMasterStory struct {
	StoryMainID        int    `json:"story_mainid"`
	StateFlag          int    `json:"state_flag"`
	Title              string `json:"title"`
	UnlockText         string `json:"unlock_text"`
	RewardTypeFlag     int    `json:"reward_type_flag"`
	ScriptSegmentCount int    `json:"script_segment_count"`
}

type storyMasterSection struct {
	StoryMainSectionID int                `json:"story_main_sectionid"`
	SectionTitle       string             `json:"section_title"`
	Stories            []storyMasterStory `json:"stories"`
}

type storyMasterPart struct {
	StoryMainPartID int                  `json:"story_main_partid"`
	Name            string               `json:"name"`
	Sections        []storyMasterSection `json:"sections"`
}

type storyMasterSubStory struct {
	StorySubID         int              `json:"story_subid"`
	StateFlag          int              `json:"state_flag"`
	Title              string           `json:"title"`
	UnlockText         string           `json:"unlock_text"`
	FeatureReward      gamestate.Reward `json:"feature_reward"`
	ScriptSegmentCount int              `json:"script_segment_count"`
}

type storyMasterSubSection struct {
	StorySubSectionID   int                   `json:"story_sub_sectionid"`
	StorySubSectionType int                   `json:"story_sub_section_type"`
	SectionTitle        string                `json:"section_title"`
	PictID              int                   `json:"pictid"`
	Stories             []storyMasterSubStory `json:"stories"`
}

type storyMasterSubCharacter struct {
	StoryMainCharacterID int                     `json:"story_main_charaid"`
	Name                 string                  `json:"name"`
	PictID               int                     `json:"pictid"`
	Sections             []storyMasterSubSection `json:"sections"`
}

type storyMasterEventStory struct {
	Sub       storyMasterSubStory             `json:"sub"`
	Materials []gamestate.StoryUnlockMaterial `json:"materials"`
}

type storyMasterEvent struct {
	StoryEventID int                     `json:"story_eventid"`
	Name         string                  `json:"name"`
	PictID       int                     `json:"pictid"`
	Stories      []storyMasterEventStory `json:"stories"`
}

type storyEventMapping struct {
	StoryEventID        int      `json:"story_eventid"`
	ScriptChapterLabels []string `json:"script_chapter_labels"`
	StoryIDs            []int    `json:"story_ids"`
	PictID              int      `json:"pictid"`
	PictCandidates      []int    `json:"pict_candidates"`
	TopologyEvidence    string   `json:"topology_evidence"`
	PictIDEvidence      string   `json:"pictid_evidence"`
}

type storyBattleReference struct {
	StoryKind      string `json:"story_kind"`
	StoryID        int    `json:"story_id"`
	StoryBattleIDs []int  `json:"story_battle_ids"`
}

type storyBattleCatalog struct {
	OfficialIDs     []int                  `json:"official_ids"`
	ReferencedIDs   []int                  `json:"referenced_ids"`
	UnreferencedIDs []int                  `json:"unreferenced_ids"`
	References      []storyBattleReference `json:"references"`
}

type storySubSectionMapping struct {
	StorySubSectionID int    `json:"story_sub_sectionid"`
	CardName          string `json:"card_name"`
	ScriptChapter     string `json:"script_chapter"`
	StoryIDs          []int  `json:"story_ids"`
	Evidence          string `json:"evidence"`
}

type storyRuntimeMaster struct {
	SchemaVersion            int                         `json:"schema_version"`
	ClientProfile            string                      `json:"client_profile"`
	Source                   json.RawMessage             `json:"source"`
	NormalParts              []storyMasterPart           `json:"normal_parts"`
	CNParts                  []storyMasterPart           `json:"cn_parts"`
	SubCharacters            []storyMasterSubCharacter   `json:"sub_characters"`
	SubSectionMappings       []storySubSectionMapping    `json:"sub_section_mappings"`
	Events                   []storyMasterEvent          `json:"events"`
	EventMappings            []storyEventMapping         `json:"event_mappings"`
	RewardPolicy             gamestate.StoryRewardPolicy `json:"reward_policy"`
	EventPageProfile         gamestate.EventPageProfile  `json:"event_page_profile"`
	StoryBattles             storyBattleCatalog          `json:"story_battles"`
	NormalStoryCount         int                         `json:"normal_story_count"`
	CNStoryCount             int                         `json:"cn_story_count"`
	SubStoryCount            int                         `json:"sub_story_count"`
	EventStoryCount          int                         `json:"event_story_count"`
	NormalMissingTalkIDs     []int                       `json:"normal_missing_talk_ids"`
	CNMissingTalkIDs         []int                       `json:"cn_missing_talk_ids"`
	NormalOrphanScriptIDs    []int                       `json:"normal_orphan_script_ids"`
	CNOrphanScriptIDs        []int                       `json:"cn_orphan_script_ids"`
	SubOrphanScriptIDs       []int                       `json:"sub_orphan_script_ids"`
	SubMissingTalkIDs        []int                       `json:"sub_missing_talk_ids"`
	SubMissingPictSectionIDs []int                       `json:"sub_missing_pict_section_ids"`
	UnmappedSubSectionIDs    []int                       `json:"unmapped_sub_section_ids"`
	ExcludedTestScriptIDs    []int                       `json:"excluded_test_script_ids"`
	ScriptLabelFallbackIDs   []int                       `json:"script_label_fallback_ids"`
}

func LoadStoryRuntimeMaster(masterPath string) (storyRuntimeMaster, error) {
	if masterPath == "" {
		return storyRuntimeMaster{}, errors.New("CN story runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return storyRuntimeMaster{}, fmt.Errorf("resolve CN story runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return storyRuntimeMaster{}, fmt.Errorf("open CN story runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return storyRuntimeMaster{}, fmt.Errorf("stat CN story runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStoryMasterBytes {
		return storyRuntimeMaster{}, errors.New("CN story runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxStoryMasterBytes+1))
	decoder.DisallowUnknownFields()
	var master storyRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return storyRuntimeMaster{}, fmt.Errorf("decode CN story runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return storyRuntimeMaster{}, err
	}
	if err := validateStoryRuntimeMaster(master); err != nil {
		return storyRuntimeMaster{}, err
	}
	return master, nil
}

func validateStoryRuntimeMaster(master storyRuntimeMaster) error {
	if master.SchemaVersion != 5 || master.ClientProfile != "cn602-bootstrap" {
		return errors.New("CN story runtime master identity is invalid")
	}
	if len(master.Source) == 0 || bytes.Equal(master.Source, []byte("null")) {
		return errors.New("CN story runtime master source is incomplete")
	}
	normalCount, err := validateStoryMasterParts(master.NormalParts, make(map[int]struct{}))
	if err != nil {
		return fmt.Errorf("validate normal story catalog: %w", err)
	}
	cnCount, err := validateStoryMasterParts(master.CNParts, make(map[int]struct{}))
	if err != nil {
		return fmt.Errorf("validate CN story catalog: %w", err)
	}
	subCount, err := validateStoryMasterSubCharacters(
		master.SubCharacters,
		master.SubSectionMappings,
	)
	if err != nil {
		return fmt.Errorf("validate sub story catalog: %w", err)
	}
	eventCount, err := validateStoryMasterEvents(master.Events, master.EventMappings)
	if err != nil {
		return fmt.Errorf("validate event story catalog: %w", err)
	}
	if err := validateStoryBattleCatalog(master); err != nil {
		return fmt.Errorf("validate story fixed-battle catalog: %w", err)
	}
	if err := validateStoryRewardPolicy(master); err != nil {
		return fmt.Errorf("validate story first-clear policy: %w", err)
	}
	profile := master.EventPageProfile
	if err := ValidateEventPageProfile(profile); err != nil {
		return err
	}
	if profile.EventID != 0 {
		found := false
		for _, event := range master.Events {
			if event.StoryEventID == 100964 && len(event.Stories) == 5 {
				found = true
			}
		}
		if !found {
			return errors.New("CN permanent event replay has no complete story")
		}
	}
	if normalCount != master.NormalStoryCount ||
		cnCount != master.CNStoryCount ||
		subCount != master.SubStoryCount ||
		eventCount != master.EventStoryCount ||
		normalCount == 0 || cnCount == 0 || subCount == 0 || eventCount == 0 {
		return errors.New("CN story runtime master counts are inconsistent")
	}
	seenMissingPictSections := make(map[int]struct{}, len(master.SubMissingPictSectionIDs))
	for _, sectionID := range master.SubMissingPictSectionIDs {
		if sectionID <= 0 {
			return errors.New("CN story missing-pict section ID is invalid")
		}
		if _, duplicate := seenMissingPictSections[sectionID]; duplicate {
			return fmt.Errorf("duplicate CN story missing-pict section ID %d", sectionID)
		}
		seenMissingPictSections[sectionID] = struct{}{}
	}
	seenUnmappedSections := make(map[int]struct{}, len(master.UnmappedSubSectionIDs))
	for _, sectionID := range master.UnmappedSubSectionIDs {
		if sectionID <= 0 {
			return errors.New("story unmapped section ID is invalid")
		}
		if _, duplicate := seenUnmappedSections[sectionID]; duplicate {
			return fmt.Errorf("duplicate story unmapped section ID %d", sectionID)
		}
		if _, overlap := seenMissingPictSections[sectionID]; overlap {
			return fmt.Errorf("story section %d is both unmapped and missing a picture", sectionID)
		}
		for _, mapping := range master.SubSectionMappings {
			if mapping.StorySubSectionID == sectionID {
				return fmt.Errorf("story section %d is both published and unmapped", sectionID)
			}
		}
		seenUnmappedSections[sectionID] = struct{}{}
	}
	return nil
}

func validateStoryRewardPolicy(master storyRuntimeMaster) error {
	policy := master.RewardPolicy
	if policy.ConfigVersion != 1 || policy.State != "PASS" ||
		len(policy.SourceState) != 2 ||
		policy.SourceState["client_contract"] != "CONFIRMED" ||
		policy.SourceState["reward_schedule"] != "PLACEHOLDER" {
		return errors.New("story first-clear policy identity is invalid")
	}
	for name, reward := range map[string]gamestate.Reward{
		"main":  policy.MainFirstClear,
		"sub":   policy.SubFirstClear,
		"event": policy.EventFirstClear,
	} {
		if reward.Type != 10 || reward.Num <= 0 || reward.Num > 2147483647 || reward.RewardTypeID != 0 ||
			reward.CardLevel != 0 || reward.CardFame != 0 || reward.CardLove != 0 ||
			reward.CardSkillLevels == nil || len(reward.CardSkillLevels) != 0 {
			return fmt.Errorf("%s story first-clear reward is invalid", name)
		}
	}
	for _, character := range master.SubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				if !reflect.DeepEqual(story.FeatureReward, policy.SubFirstClear) {
					return fmt.Errorf("character story %d feature reward differs from policy", story.StorySubID)
				}
			}
		}
	}
	for _, event := range master.Events {
		for _, story := range event.Stories {
			if !reflect.DeepEqual(story.Sub.FeatureReward, policy.EventFirstClear) {
				return fmt.Errorf("event story %d feature reward differs from policy", story.Sub.StorySubID)
			}
		}
	}
	return nil
}

func validateStoryMasterEvents(
	events []storyMasterEvent,
	mappings []storyEventMapping,
) (int, error) {
	if len(events) == 0 || len(events) != len(mappings) {
		return 0, errors.New("event story catalog or mappings are incomplete")
	}
	eventIDs := make(map[int]struct{}, len(events))
	storyIDs := make(map[int]struct{})
	count := 0
	for eventIndex, event := range events {
		mapping := mappings[eventIndex]
		if event.StoryEventID <= 0 || event.Name == "" || event.PictID <= 0 || len(event.Stories) == 0 ||
			mapping.StoryEventID != event.StoryEventID || mapping.PictID != event.PictID ||
			len(mapping.ScriptChapterLabels) == 0 || len(mapping.StoryIDs) != len(event.Stories) ||
			len(mapping.PictCandidates) == 0 ||
			mapping.TopologyEvidence != "CONFIRMED_STORY_ID_PREFIX" ||
			(mapping.PictIDEvidence != "INFERRED_FIRST_SCRIPT_CARD_PICT" &&
				mapping.PictIDEvidence != "INFERRED_SCRIPT_CHARACTER_SELECTION" &&
				mapping.PictIDEvidence != "CONFIRMED_OFFLINE_DIRECTORY") {
			return 0, fmt.Errorf("event story %d mapping is incomplete", event.StoryEventID)
		}
		if !slices.Contains(mapping.PictCandidates, event.PictID) {
			return 0, fmt.Errorf("event story %d portrait is outside its source candidates", event.StoryEventID)
		}
		if _, duplicate := eventIDs[event.StoryEventID]; duplicate {
			return 0, fmt.Errorf("duplicate event story %d", event.StoryEventID)
		}
		eventIDs[event.StoryEventID] = struct{}{}
		for storyIndex, wrapper := range event.Stories {
			story := wrapper.Sub
			if story.StorySubID <= 0 || story.StateFlag&^(8|16) != 0 || story.Title == "" ||
				story.ScriptSegmentCount <= 0 || story.FeatureReward.CardSkillLevels == nil ||
				wrapper.Materials == nil || mapping.StoryIDs[storyIndex] != story.StorySubID {
				return 0, fmt.Errorf("event sub story %d has invalid static fields", story.StorySubID)
			}
			if _, duplicate := storyIDs[story.StorySubID]; duplicate {
				return 0, fmt.Errorf("duplicate event sub story %d", story.StorySubID)
			}
			storyIDs[story.StorySubID] = struct{}{}
			for _, material := range wrapper.Materials {
				if material.PayType < 1 || material.PayType > 7 || material.PayTypeID < 0 || material.Price <= 0 {
					return 0, fmt.Errorf("event sub story %d has invalid unlock material", story.StorySubID)
				}
			}
			count++
		}
	}
	return count, nil
}

func validateStoryBattleCatalog(master storyRuntimeMaster) error {
	storyKinds := map[string]map[int]bool{
		"main":    make(map[int]bool),
		"cn_main": make(map[int]bool),
		"sub":     make(map[int]bool),
	}
	for _, part := range master.NormalParts {
		for _, section := range part.Sections {
			for _, story := range section.Stories {
				storyKinds["main"][story.StoryMainID] = story.StateFlag&8 != 0
			}
		}
	}
	for _, part := range master.CNParts {
		for _, section := range part.Sections {
			for _, story := range section.Stories {
				storyKinds["cn_main"][story.StoryMainID] = story.StateFlag&8 != 0
			}
		}
	}
	for _, character := range master.SubCharacters {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				storyKinds["sub"][story.StorySubID] = story.StateFlag&8 != 0
			}
		}
	}
	for _, event := range master.Events {
		for _, wrapper := range event.Stories {
			storyKinds["sub"][wrapper.Sub.StorySubID] = wrapper.Sub.StateFlag&8 != 0
		}
	}

	official := make(map[int]struct{}, len(master.StoryBattles.OfficialIDs))
	for _, battleID := range master.StoryBattles.OfficialIDs {
		if battleID <= 0 {
			return errors.New("official story battle ID is invalid")
		}
		if _, duplicate := official[battleID]; duplicate {
			return fmt.Errorf("duplicate official story battle ID %d", battleID)
		}
		official[battleID] = struct{}{}
	}
	if len(official) == 0 {
		return errors.New("official story battle catalog is empty")
	}
	referenced := make(map[int]struct{}, len(master.StoryBattles.ReferencedIDs))
	for _, battleID := range master.StoryBattles.ReferencedIDs {
		if _, exists := official[battleID]; !exists {
			return fmt.Errorf("referenced story battle %d is not official", battleID)
		}
		if _, duplicate := referenced[battleID]; duplicate {
			return fmt.Errorf("duplicate referenced story battle ID %d", battleID)
		}
		referenced[battleID] = struct{}{}
	}
	unreferenced := make(map[int]struct{}, len(master.StoryBattles.UnreferencedIDs))
	for _, battleID := range master.StoryBattles.UnreferencedIDs {
		if _, exists := official[battleID]; !exists {
			return fmt.Errorf("unreferenced story battle %d is not official", battleID)
		}
		if _, overlap := referenced[battleID]; overlap {
			return fmt.Errorf("story battle %d is both referenced and unreferenced", battleID)
		}
		if _, duplicate := unreferenced[battleID]; duplicate {
			return fmt.Errorf("duplicate unreferenced story battle ID %d", battleID)
		}
		unreferenced[battleID] = struct{}{}
	}
	if len(referenced)+len(unreferenced) != len(official) {
		return errors.New("story battle partition does not close official IDs")
	}

	referencePairs := make(map[string]struct{}, len(master.StoryBattles.References))
	referenceIDs := make(map[int]struct{})
	for _, reference := range master.StoryBattles.References {
		stories, exists := storyKinds[reference.StoryKind]
		if !exists || reference.StoryID <= 0 || !stories[reference.StoryID] || len(reference.StoryBattleIDs) == 0 {
			return fmt.Errorf("story battle reference %s/%d is invalid", reference.StoryKind, reference.StoryID)
		}
		pair := fmt.Sprintf("%s:%d", reference.StoryKind, reference.StoryID)
		if _, duplicate := referencePairs[pair]; duplicate {
			return fmt.Errorf("duplicate story battle reference %s", pair)
		}
		referencePairs[pair] = struct{}{}
		seenBattleIDs := make(map[int]struct{}, len(reference.StoryBattleIDs))
		for _, battleID := range reference.StoryBattleIDs {
			if _, exists := referenced[battleID]; !exists {
				return fmt.Errorf("story %s references unpublished fixed battle %d", pair, battleID)
			}
			if _, duplicate := seenBattleIDs[battleID]; duplicate {
				return fmt.Errorf("story %s repeats fixed battle %d", pair, battleID)
			}
			seenBattleIDs[battleID] = struct{}{}
			referenceIDs[battleID] = struct{}{}
		}
	}
	for storyKind, stories := range storyKinds {
		for storyID, hasBattle := range stories {
			_, hasReference := referencePairs[fmt.Sprintf("%s:%d", storyKind, storyID)]
			if hasBattle != hasReference {
				return fmt.Errorf("story fixed-battle flag/reference differs for %s/%d", storyKind, storyID)
			}
		}
	}
	if len(referenceIDs) != len(referenced) {
		return errors.New("story fixed-battle references do not cover referenced IDs")
	}
	return nil
}

func validateStoryMasterParts(parts []storyMasterPart, storyIDs map[int]struct{}) (int, error) {
	if len(parts) == 0 {
		return 0, errors.New("story catalog has no parts")
	}
	partIDs := make(map[int]struct{}, len(parts))
	sectionIDs := make(map[int]struct{})
	count := 0
	for _, part := range parts {
		if part.StoryMainPartID <= 0 || part.Name == "" || len(part.Sections) == 0 {
			return 0, errors.New("story part is incomplete")
		}
		if _, duplicate := partIDs[part.StoryMainPartID]; duplicate {
			return 0, fmt.Errorf("duplicate story part %d", part.StoryMainPartID)
		}
		partIDs[part.StoryMainPartID] = struct{}{}
		for _, section := range part.Sections {
			if section.StoryMainSectionID <= 0 || section.SectionTitle == "" || len(section.Stories) == 0 {
				return 0, errors.New("story section is incomplete")
			}
			if _, duplicate := sectionIDs[section.StoryMainSectionID]; duplicate {
				return 0, fmt.Errorf("duplicate story section %d", section.StoryMainSectionID)
			}
			sectionIDs[section.StoryMainSectionID] = struct{}{}
			for _, story := range section.Stories {
				if story.StoryMainID <= 0 || story.Title == "" || story.ScriptSegmentCount <= 0 || story.RewardTypeFlag < 0 || story.StateFlag&^8 != 0 {
					return 0, fmt.Errorf("story %d has invalid static fields", story.StoryMainID)
				}
				if _, duplicate := storyIDs[story.StoryMainID]; duplicate {
					return 0, fmt.Errorf("duplicate story ID %d", story.StoryMainID)
				}
				storyIDs[story.StoryMainID] = struct{}{}
				count++
			}
		}
	}
	return count, nil
}

func validateStoryMasterSubCharacters(
	characters []storyMasterSubCharacter,
	mappings []storySubSectionMapping,
) (int, error) {
	if len(characters) == 0 || len(mappings) == 0 {
		return 0, errors.New("sub story catalog is empty")
	}
	mappingBySectionID := make(map[int]storySubSectionMapping, len(mappings))
	for _, mapping := range mappings {
		if mapping.StorySubSectionID <= 0 ||
			mapping.CardName == "" ||
			mapping.ScriptChapter == "" ||
			len(mapping.StoryIDs) == 0 ||
			(mapping.Evidence != "CONFIRMED" && mapping.Evidence != "INFERRED") {
			return 0, errors.New("sub story section mapping is incomplete")
		}
		if _, duplicate := mappingBySectionID[mapping.StorySubSectionID]; duplicate {
			return 0, fmt.Errorf("duplicate sub story mapping %d", mapping.StorySubSectionID)
		}
		mappingBySectionID[mapping.StorySubSectionID] = mapping
	}

	characterIDs := make(map[int]struct{}, len(characters))
	sectionIDs := make(map[int]struct{})
	storyIDs := make(map[int]struct{})
	count := 0
	for _, character := range characters {
		if character.StoryMainCharacterID <= 0 || character.Name == "" || len(character.Sections) == 0 {
			return 0, errors.New("sub story character is incomplete")
		}
		if _, duplicate := characterIDs[character.StoryMainCharacterID]; duplicate {
			return 0, fmt.Errorf("duplicate sub story character %d", character.StoryMainCharacterID)
		}
		characterIDs[character.StoryMainCharacterID] = struct{}{}
		for _, section := range character.Sections {
			if section.StorySubSectionID <= 0 ||
				(section.StorySubSectionType != 0 && section.StorySubSectionType != 1) ||
				section.SectionTitle == "" || section.PictID <= 0 || len(section.Stories) == 0 {
				return 0, fmt.Errorf("sub story section %d is incomplete", section.StorySubSectionID)
			}
			if _, duplicate := sectionIDs[section.StorySubSectionID]; duplicate {
				return 0, fmt.Errorf("duplicate sub story section %d", section.StorySubSectionID)
			}
			sectionIDs[section.StorySubSectionID] = struct{}{}
			mapping, exists := mappingBySectionID[section.StorySubSectionID]
			if !exists || mapping.CardName != section.SectionTitle || len(mapping.StoryIDs) != len(section.Stories) {
				return 0, fmt.Errorf("sub story section %d mapping differs", section.StorySubSectionID)
			}
			for storyIndex, story := range section.Stories {
				if story.StorySubID <= 0 ||
					story.StateFlag&^8 != 0 ||
					story.Title == "" ||
					story.ScriptSegmentCount <= 0 ||
					story.FeatureReward.Type < 0 ||
					story.FeatureReward.Num < 0 ||
					story.FeatureReward.RewardTypeID < 0 ||
					story.FeatureReward.CardSkillLevels == nil {
					return 0, fmt.Errorf("sub story %d has invalid static fields", story.StorySubID)
				}
				if mapping.StoryIDs[storyIndex] != story.StorySubID {
					return 0, fmt.Errorf("sub story section %d story order differs", section.StorySubSectionID)
				}
				if _, duplicate := storyIDs[story.StorySubID]; duplicate {
					return 0, fmt.Errorf("duplicate sub story ID %d", story.StorySubID)
				}
				storyIDs[story.StorySubID] = struct{}{}
				count++
			}
		}
	}
	if len(mappingBySectionID) != len(sectionIDs) {
		return 0, errors.New("sub story mappings contain unused sections")
	}
	return count, nil
}

func materializeStoryParts(source []storyMasterPart, persisted []gamestate.StoryMainPart) []gamestate.StoryMainPart {
	progress := make(map[int]int)
	for _, part := range persisted {
		for _, section := range part.Sections {
			for _, story := range section.Stories {
				progress[story.StoryMainID] = story.StateFlag & 7
			}
		}
	}
	parts := make([]gamestate.StoryMainPart, len(source))
	for partIndex, part := range source {
		parts[partIndex] = gamestate.StoryMainPart{
			StoryMainPartID: part.StoryMainPartID,
			Name:            part.Name,
			Sections:        make([]gamestate.StoryMainSection, len(part.Sections)),
		}
		for sectionIndex, section := range part.Sections {
			parts[partIndex].Sections[sectionIndex] = gamestate.StoryMainSection{
				StoryMainSectionID: section.StoryMainSectionID,
				SectionTitle:       section.SectionTitle,
				Stories:            make([]gamestate.StoryMain, len(section.Stories)),
			}
			for storyIndex, story := range section.Stories {
				parts[partIndex].Sections[sectionIndex].Stories[storyIndex] = gamestate.StoryMain{
					StoryMainID:    story.StoryMainID,
					StateFlag:      story.StateFlag | progress[story.StoryMainID],
					Title:          story.Title,
					UnlockText:     story.UnlockText,
					RewardTypeFlag: story.RewardTypeFlag,
				}
			}
		}
	}
	NormalizeStoryProgress(parts)
	return parts
}

func NormalizeStoryProgress(parts []gamestate.StoryMainPart) {
	opened := map[int]bool{}
	for partIndex := range parts {
		chain := game.MainStoryProgressionChain(parts[partIndex].StoryMainPartID)
		for sectionIndex := range parts[partIndex].Sections {
			for storyIndex := range parts[partIndex].Sections[sectionIndex].Stories {
				story := &parts[partIndex].Sections[sectionIndex].Stories[storyIndex]
				static := story.StateFlag & 8
				if story.StateFlag&2 != 0 {
					story.StateFlag = static | 2
					story.UnlockText = ""
					continue
				}
				if !opened[chain] {
					story.StateFlag = static | 1
					story.UnlockText = ""
					opened[chain] = true
					continue
				}
				story.StateFlag = static | 4
			}
		}
	}
}

func materializeSubStories(
	source []storyMasterSubCharacter,
	persisted []gamestate.StorySubCharacter,
	cards []gamestate.Card,
) []gamestate.StorySubCharacter {
	progress := make(map[int]int)
	unlockedSections := make(map[int]bool)
	for _, character := range persisted {
		for _, section := range character.Sections {
			for _, story := range section.Stories {
				progress[story.StorySubID] = story.StateFlag & 7
				if story.StateFlag&3 != 0 {
					unlockedSections[section.StorySubSectionID] = true
				}
			}
		}
	}
	for _, card := range cards {
		unlockedSections[card.CardID] = true
	}

	characters := make([]gamestate.StorySubCharacter, len(source))
	for characterIndex, character := range source {
		characters[characterIndex] = gamestate.StorySubCharacter{
			StoryMainCharacterID: character.StoryMainCharacterID,
			Name:                 character.Name,
			PictID:               character.PictID,
			Sections:             make([]gamestate.StorySubSection, len(character.Sections)),
		}
		for sectionIndex, section := range character.Sections {
			unlocked := unlockedSections[section.StorySubSectionID]
			characters[characterIndex].Sections[sectionIndex] = gamestate.StorySubSection{
				StorySubSectionID:   section.StorySubSectionID,
				StorySubSectionType: section.StorySubSectionType,
				SectionTitle:        section.SectionTitle,
				PictID:              section.PictID,
				Stories:             make([]gamestate.StorySub, len(section.Stories)),
			}
			firstUncleared := true
			for storyIndex, story := range section.Stories {
				stateFlag := story.StateFlag & 8
				unlockText := "通关前一话后解锁"
				if progress[story.StorySubID]&2 != 0 {
					stateFlag |= 2
					unlockText = ""
				} else if unlocked && firstUncleared {
					stateFlag |= 1
					unlockText = ""
					firstUncleared = false
				} else {
					stateFlag |= 4
					if !unlocked && storyIndex == 0 {
						unlockText = "获得" + section.SectionTitle + "后解锁"
					}
				}
				featureReward := story.FeatureReward
				featureReward.CardSkillLevels = append([]int16{}, story.FeatureReward.CardSkillLevels...)
				characters[characterIndex].Sections[sectionIndex].Stories[storyIndex] = gamestate.StorySub{
					StorySubID:    story.StorySubID,
					StateFlag:     stateFlag,
					Title:         story.Title,
					UnlockText:    unlockText,
					FeatureReward: featureReward,
				}
			}
		}
	}
	return characters
}

func materializeEventStories(
	source []storyMasterEvent,
	persisted []gamestate.StoryEvent,
) []gamestate.StoryEvent {
	progress := make(map[int]int)
	for _, event := range persisted {
		for _, wrapper := range event.Stories {
			progress[wrapper.Sub.StorySubID] = wrapper.Sub.StateFlag & 3
		}
	}
	events := make([]gamestate.StoryEvent, len(source))
	for eventIndex, event := range source {
		events[eventIndex] = gamestate.StoryEvent{
			StoryEventID: event.StoryEventID,
			Name:         event.Name,
			PictID:       event.PictID,
			Stories:      make([]gamestate.StorySubEvent, len(event.Stories)),
		}
		firstUncleared := true
		for storyIndex, wrapper := range event.Stories {
			story := wrapper.Sub
			stateFlag := story.StateFlag & (8 | 16)
			unlockText := "通关前一话后解锁"
			if progress[story.StorySubID]&2 != 0 {
				stateFlag = stateFlag&8 | 2
				unlockText = ""
			} else if progress[story.StorySubID]&1 != 0 {
				stateFlag = stateFlag&8 | 1
				unlockText = ""
				firstUncleared = false
			} else if stateFlag&16 != 0 {
				unlockText = "需要活动材料解锁"
				firstUncleared = false
			} else if firstUncleared {
				stateFlag |= 1
				unlockText = ""
				firstUncleared = false
			} else {
				stateFlag |= 4
			}
			featureReward := story.FeatureReward
			featureReward.CardSkillLevels = append([]int16{}, story.FeatureReward.CardSkillLevels...)
			events[eventIndex].Stories[storyIndex] = gamestate.StorySubEvent{
				Sub: gamestate.StorySub{
					StorySubID:    story.StorySubID,
					StateFlag:     stateFlag,
					Title:         story.Title,
					UnlockText:    unlockText,
					FeatureReward: featureReward,
				},
				Materials: append([]gamestate.StoryUnlockMaterial{}, wrapper.Materials...),
			}
		}
	}
	return events
}

func ApplyStoryRuntimeMaster(state *gamestate.State, master storyRuntimeMaster) bool {
	normalParts := materializeStoryParts(master.NormalParts, state.Story.MainParts)
	cnParts := materializeStoryParts(master.CNParts, state.Story.CNMainParts)
	ownedCards := make([]gamestate.Card, 0, len(state.Cards)+len(state.ContainerCards))
	ownedCards = append(ownedCards, state.Cards...)
	ownedCards = append(ownedCards, state.ContainerCards...)
	subCharacters := materializeSubStories(
		master.SubCharacters,
		state.Story.SubCharacters,
		ownedCards,
	)
	collected := make(map[int]struct{}, len(state.SupportDeck.CardCollectionIDs))
	for _, cardID := range state.SupportDeck.CardCollectionIDs {
		collected[cardID] = struct{}{}
	}
	gamestate.UnlockCollectedCharacterStories(subCharacters, collected)
	events := materializeEventStories(master.Events, state.Story.Events)
	changed := !reflect.DeepEqual(state.Story.MainParts, normalParts) ||
		!reflect.DeepEqual(state.Story.CNMainParts, cnParts) ||
		!reflect.DeepEqual(state.Story.SubCharacters, subCharacters) ||
		!reflect.DeepEqual(state.Story.Events, events)
	state.Story.MainParts = normalParts
	state.Story.CNMainParts = cnParts
	state.Story.SubCharacters = subCharacters
	state.Story.Events = events
	state.StoryRewardPolicy = master.RewardPolicy
	state.StoryRewardPolicy.MainFirstClear.CardSkillLevels = append([]int16{}, master.RewardPolicy.MainFirstClear.CardSkillLevels...)
	state.StoryRewardPolicy.SubFirstClear.CardSkillLevels = append([]int16{}, master.RewardPolicy.SubFirstClear.CardSkillLevels...)
	state.StoryRewardPolicy.EventFirstClear.CardSkillLevels = append([]int16{}, master.RewardPolicy.EventFirstClear.CardSkillLevels...)
	state.StoryRewardPolicy.SourceState = make(map[string]string, len(master.RewardPolicy.SourceState))
	for key, value := range master.RewardPolicy.SourceState {
		state.StoryRewardPolicy.SourceState[key] = value
	}
	state.EventPageProfile = master.EventPageProfile
	state.EventPageProfile.ItemIDs = append([]int{}, master.EventPageProfile.ItemIDs...)
	state.EventPageProfile.Buttons = append([]gamestate.EventPageButton{}, master.EventPageProfile.Buttons...)
	state.Story.StoryBattleIDs = append([]int(nil), master.StoryBattles.OfficialIDs...)
	state.Story.StoryBattleReferences = make([]gamestate.StoryBattleReference, len(master.StoryBattles.References))
	for index, reference := range master.StoryBattles.References {
		state.Story.StoryBattleReferences[index] = gamestate.StoryBattleReference{
			StoryKind:      reference.StoryKind,
			StoryID:        reference.StoryID,
			StoryBattleIDs: append([]int(nil), reference.StoryBattleIDs...),
		}
	}
	return changed
}
