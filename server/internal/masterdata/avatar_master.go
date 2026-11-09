package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"

	"kairisei.local/server/internal/gamestate"
)

const (
	maxAvatarMasterBytes = 2 * 1024 * 1024
	avatarDeckSlots      = 7
	avatarArthurMask     = 30
	avatarSlotMask       = 127
)

type avatarRuntimeMaster struct {
	SchemaVersion int    `json:"schema_version"`
	ClientProfile string `json:"client_profile"`

	LocalAccountConfigVersion int                                    `json:"local_account_config_version"`
	InitialOwnedPartIDs       []int                                  `json:"initial_owned_part_ids"`
	DefaultDecks              map[string][]int                       `json:"default_decks"`
	ShopPolicy                gamestate.AvatarShopPolicy             `json:"shop_policy"`
	PartDefinitions           []gamestate.AvatarPartDefinition       `json:"part_definitions"`
	Series                    []gamestate.AvatarSeriesDefinition     `json:"series"`
	SeriesCompletions         []gamestate.AvatarSeriesCompletion     `json:"series_completions"`
	CostumeRewards            []gamestate.CollectionRewardDefinition `json:"costume_rewards"`
}

func LoadAvatarRuntimeMaster(masterPath string) (avatarRuntimeMaster, error) {
	if masterPath == "" {
		return avatarRuntimeMaster{}, errors.New("CN Avatar runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return avatarRuntimeMaster{}, fmt.Errorf("resolve CN Avatar runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return avatarRuntimeMaster{}, fmt.Errorf("open CN Avatar runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return avatarRuntimeMaster{}, fmt.Errorf("stat CN Avatar runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxAvatarMasterBytes {
		return avatarRuntimeMaster{}, errors.New("CN Avatar runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxAvatarMasterBytes+1))
	var master avatarRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return avatarRuntimeMaster{}, fmt.Errorf("decode CN Avatar runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return avatarRuntimeMaster{}, err
	}
	if err := validateAvatarRuntimeMaster(master); err != nil {
		return avatarRuntimeMaster{}, err
	}
	return master, nil
}

func validateAvatarRuntimeMaster(master avatarRuntimeMaster) error {
	if master.SchemaVersion != 1 || master.ClientProfile != "cn602-bootstrap" {
		return errors.New("CN Avatar runtime master identity is invalid")
	}
	if master.LocalAccountConfigVersion <= 0 || len(master.PartDefinitions) == 0 ||
		len(master.Series) == 0 || len(master.SeriesCompletions) == 0 ||
		len(master.InitialOwnedPartIDs) == 0 || len(master.CostumeRewards) == 0 {
		return errors.New("CN Avatar runtime master is incomplete")
	}
	costumeIDs := make(map[int]struct{}, len(master.CostumeRewards))
	for _, row := range master.CostumeRewards {
		if row.Type != 14 || row.ID <= 0 || row.PictID <= 0 || row.Name == "" {
			return fmt.Errorf("invalid costume reward %d", row.ID)
		}
		if _, exists := costumeIDs[row.ID]; exists {
			return fmt.Errorf("duplicate costume reward %d", row.ID)
		}
		costumeIDs[row.ID] = struct{}{}
	}
	if master.ShopPolicy.PayType != 1 || master.ShopPolicy.PayTypeID != 0 || master.ShopPolicy.Price <= 0 ||
		master.ShopPolicy.AppearEnd < 0 || master.ShopPolicy.NewAppearEnd < 0 {
		return errors.New("CN Avatar local shop policy is invalid")
	}
	seriesIDs := make(map[int]struct{}, len(master.Series))
	for _, series := range master.Series {
		if series.SeriesID < 0 || series.Name == "" {
			return fmt.Errorf("invalid CN Avatar series %d", series.SeriesID)
		}
		if _, duplicate := seriesIDs[series.SeriesID]; duplicate {
			return fmt.Errorf("duplicate CN Avatar series %d", series.SeriesID)
		}
		seriesIDs[series.SeriesID] = struct{}{}
	}
	partDefinitions := make(map[int]gamestate.AvatarPartDefinition, len(master.PartDefinitions))
	for _, part := range master.PartDefinitions {
		if part.PartID <= 0 || part.Name == "" || part.IconPictID <= 0 ||
			part.ArthurMask <= 0 || part.ArthurMask&^avatarArthurMask != 0 ||
			part.SlotMask <= 0 || part.SlotMask&^avatarSlotMask != 0 ||
			(part.IsNewList != 0 && part.IsNewList != 1) {
			return fmt.Errorf("invalid CN Avatar part %d", part.PartID)
		}
		if _, exists := seriesIDs[part.SeriesID]; !exists {
			return fmt.Errorf("CN Avatar part %d references unknown series %d", part.PartID, part.SeriesID)
		}
		if _, duplicate := partDefinitions[part.PartID]; duplicate {
			return fmt.Errorf("duplicate CN Avatar part %d", part.PartID)
		}
		partDefinitions[part.PartID] = part
	}
	owned := make(map[int]struct{}, len(master.InitialOwnedPartIDs))
	for _, partID := range master.InitialOwnedPartIDs {
		if _, exists := partDefinitions[partID]; !exists {
			return fmt.Errorf("initial CN Avatar part %d is absent from official definitions", partID)
		}
		if _, duplicate := owned[partID]; duplicate {
			return fmt.Errorf("initial CN Avatar part %d is duplicated", partID)
		}
		owned[partID] = struct{}{}
	}
	if len(master.DefaultDecks) != 4 {
		return errors.New("CN Avatar runtime master requires four default decks")
	}
	for arthurType := 1; arthurType <= 4; arthurType++ {
		parts, exists := master.DefaultDecks[strconv.Itoa(arthurType)]
		if !exists || len(parts) != avatarDeckSlots {
			return fmt.Errorf("CN Avatar default deck %d is invalid", arthurType)
		}
		for _, partID := range parts {
			if partID == 0 {
				continue
			}
			if _, exists := owned[partID]; !exists {
				return fmt.Errorf("CN Avatar default deck %d references unowned part %d", arthurType, partID)
			}
			if partDefinitions[partID].ArthurMask&(1<<arthurType) == 0 {
				return fmt.Errorf("CN Avatar default deck %d part %d has an incompatible Arthur mask", arthurType, partID)
			}
		}
	}
	completionIDs := make(map[int]struct{}, len(master.SeriesCompletions))
	for _, completion := range master.SeriesCompletions {
		if completion.CompletionID <= 0 || completion.Name == "" || len(completion.PartIDs) == 0 || len(completion.Rewards) == 0 {
			return fmt.Errorf("invalid CN Avatar series completion %d", completion.CompletionID)
		}
		if _, duplicate := completionIDs[completion.CompletionID]; duplicate {
			return fmt.Errorf("duplicate CN Avatar series completion %d", completion.CompletionID)
		}
		completionIDs[completion.CompletionID] = struct{}{}
		partIDs := make(map[int]struct{}, len(completion.PartIDs))
		for _, partID := range completion.PartIDs {
			if _, exists := partDefinitions[partID]; !exists {
				return fmt.Errorf("CN Avatar completion %d references unknown part %d", completion.CompletionID, partID)
			}
			if _, duplicate := partIDs[partID]; duplicate {
				return fmt.Errorf("CN Avatar completion %d repeats part %d", completion.CompletionID, partID)
			}
			partIDs[partID] = struct{}{}
		}
		for _, reward := range completion.Rewards {
			if reward.Type != "SPHR" || reward.Num <= 0 || reward.RewardID <= 0 {
				return fmt.Errorf("CN Avatar completion %d has an unsupported reward", completion.CompletionID)
			}
		}
	}
	return nil
}

func ApplyAvatarRuntimeMaster(state *gamestate.State, master avatarRuntimeMaster) (bool, error) {
	state.SetCollectionRewardDefinitions(14, master.CostumeRewards)
	state.AvatarPartDefinitions = append([]gamestate.AvatarPartDefinition(nil), master.PartDefinitions...)
	state.AvatarDefaultDecks = make([][]int, 4)
	for arthurType := 1; arthurType <= 4; arthurType++ {
		state.AvatarDefaultDecks[arthurType-1] = append([]int(nil), master.DefaultDecks[strconv.Itoa(arthurType)]...)
	}
	state.AvatarSeries = append([]gamestate.AvatarSeriesDefinition(nil), master.Series...)
	state.AvatarSeriesCompletions = cloneAvatarSeriesCompletions(master.SeriesCompletions)
	state.AvatarShopPolicy = master.ShopPolicy
	definitions := make(map[int]gamestate.AvatarPartDefinition, len(master.PartDefinitions))
	for _, definition := range master.PartDefinitions {
		definitions[definition.PartID] = definition
	}
	for _, completion := range master.SeriesCompletions {
		for _, reward := range completion.Rewards {
			if _, exists := sphereDefinitionByID(state.SphereDefinitions, reward.RewardID); !exists {
				return false, fmt.Errorf("CN Avatar completion %d references unknown official sphere %d", completion.CompletionID, reward.RewardID)
			}
		}
	}
	changed := false
	owned := make(map[int]struct{}, len(state.AvatarParts)+len(master.InitialOwnedPartIDs))
	for _, partID := range state.AvatarParts {
		if _, exists := definitions[partID]; !exists {
			return false, fmt.Errorf("persisted CN Avatar part %d is absent from official definitions", partID)
		}
		if _, duplicate := owned[partID]; duplicate {
			return false, fmt.Errorf("persisted CN Avatar part %d is duplicated", partID)
		}
		owned[partID] = struct{}{}
	}
	if state.AvatarConfigVersion < master.LocalAccountConfigVersion {
		for _, partID := range master.InitialOwnedPartIDs {
			if _, exists := owned[partID]; !exists {
				owned[partID] = struct{}{}
				changed = true
			}
		}
		state.AvatarConfigVersion = master.LocalAccountConfigVersion
		changed = true
	}
	if len(state.Avatars) != 4 {
		return false, errors.New("persisted CN Avatar state requires four Arthur decks")
	}
	for index, avatar := range state.Avatars {
		if len(avatar.AvatarPartIDs) != avatarDeckSlots {
			return false, fmt.Errorf("persisted CN Avatar deck %d is invalid", index+1)
		}
		for slot, partID := range avatar.AvatarPartIDs {
			if partID == 0 {
				continue
			}
			definition, exists := definitions[partID]
			if !exists {
				return false, fmt.Errorf("persisted CN Avatar deck %d references unknown part %d", index+1, partID)
			}
			if err := validateAvatarPartEquipOrDefault(definition, index+1, slot, partID, state.AvatarDefaultDecks); err != nil {
				return false, fmt.Errorf("persisted CN Avatar deck %d: %w", index+1, err)
			}
			if _, exists := owned[partID]; !exists {
				owned[partID] = struct{}{}
				changed = true
			}
		}
	}
	state.AvatarParts = make([]int, 0, len(owned))
	for partID := range owned {
		state.AvatarParts = append(state.AvatarParts, partID)
	}
	sort.Ints(state.AvatarParts)
	return changed, nil
}

func ApplyAvatarShopAssetClosure(state *gamestate.State, availableBundles map[string]struct{}) error {
	if len(availableBundles) == 0 {
		return errors.New("CN Avatar shop asset closure requires available bundles")
	}
	partIDs := make([]int, 0, len(state.AvatarPartDefinitions))
	for _, definition := range state.AvatarPartDefinitions {
		iconBundle := fmt.Sprintf("avatar_parts/icon/avatar_%05d_icon.dat", definition.IconPictID)
		modelBundle := fmt.Sprintf("avatar_parts/parts/avatar_%05d_parts.dat", definition.PartID)
		if _, iconAvailable := availableBundles[iconBundle]; !iconAvailable {
			continue
		}
		if _, modelAvailable := availableBundles[modelBundle]; !modelAvailable {
			continue
		}
		partIDs = append(partIDs, definition.PartID)
	}
	sort.Ints(partIDs)
	state.AvatarShopPartIDs = partIDs
	return nil
}

func validateAvatarPartEquip(definition gamestate.AvatarPartDefinition, arthurType int, slot int) error {
	if arthurType < 1 || arthurType > 4 || slot < 0 || slot >= avatarDeckSlots {
		return errors.New("Avatar equip position is invalid")
	}
	if definition.PartID <= 0 || definition.ArthurMask&(1<<arthurType) == 0 {
		return fmt.Errorf("part %d cannot be equipped by Arthur %d", definition.PartID, arthurType)
	}
	if definition.SlotMask&(1<<slot) == 0 {
		return fmt.Errorf("part %d cannot be equipped in slot %d", definition.PartID, slot)
	}
	return nil
}

func validateAvatarPartEquipOrDefault(definition gamestate.AvatarPartDefinition, arthurType int, slot int, partID int, defaults [][]int) error {
	if arthurType >= 1 && arthurType <= len(defaults) && slot >= 0 && slot < len(defaults[arthurType-1]) &&
		defaults[arthurType-1][slot] == partID {
		if definition.ArthurMask&(1<<arthurType) == 0 {
			return fmt.Errorf("part %d cannot be equipped by Arthur %d", definition.PartID, arthurType)
		}
		return nil
	}
	return validateAvatarPartEquip(definition, arthurType, slot)
}

func sphereDefinitionByID(definitions []gamestate.SphereDefinition, sphereID int) (gamestate.SphereDefinition, bool) {
	for _, definition := range definitions {
		if definition.SphereID == sphereID {
			return definition, true
		}
	}
	return gamestate.SphereDefinition{}, false
}

func cloneAvatarSeriesCompletions(source []gamestate.AvatarSeriesCompletion) []gamestate.AvatarSeriesCompletion {
	result := make([]gamestate.AvatarSeriesCompletion, len(source))
	for index, completion := range source {
		result[index] = completion
		result[index].PartIDs = append([]int(nil), completion.PartIDs...)
		result[index].Rewards = append([]gamestate.AvatarCompletionReward(nil), completion.Rewards...)
	}
	return result
}
