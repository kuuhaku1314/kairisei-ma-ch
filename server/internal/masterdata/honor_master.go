package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"kairisei.local/server/internal/gamestate"
)

const maxHonorMasterBytes = 4 * 1024 * 1024

type honorDefinition struct {
	HonorID      int    `json:"honor_id"`
	Name         string `json:"name"`
	SlotMask     int    `json:"slot_mask"`
	SameCardID   int    `json:"same_card_id"`
	GetType      int    `json:"get_type"`
	DefaultOwned bool   `json:"default_owned"`
}

type honorRuntimeMaster struct {
	SchemaVersion int    `json:"schema_version"`
	ClientProfile string `json:"client_profile"`

	Honors []honorDefinition `json:"honors"`
}

func LoadHonorRuntimeMaster(masterPath string) (honorRuntimeMaster, error) {
	if masterPath == "" {
		return honorRuntimeMaster{}, errors.New("CN honor runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return honorRuntimeMaster{}, fmt.Errorf("resolve CN honor runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return honorRuntimeMaster{}, fmt.Errorf("open CN honor runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return honorRuntimeMaster{}, fmt.Errorf("stat CN honor runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxHonorMasterBytes {
		return honorRuntimeMaster{}, errors.New("CN honor runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxHonorMasterBytes+1))
	var master honorRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return honorRuntimeMaster{}, fmt.Errorf("decode CN honor runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return honorRuntimeMaster{}, err
	}
	if err := validateHonorRuntimeMaster(master); err != nil {
		return honorRuntimeMaster{}, err
	}
	return master, nil
}

func validateHonorRuntimeMaster(master honorRuntimeMaster) error {
	if master.SchemaVersion != 1 || master.ClientProfile != "cn602-bootstrap" {
		return errors.New("CN honor runtime master identity is invalid")
	}
	if len(master.Honors) == 0 {
		return errors.New("CN honor runtime master source is incomplete")
	}
	seen := make(map[int]struct{}, len(master.Honors))
	defaultCount := 0
	for _, honor := range master.Honors {
		if honor.HonorID <= 0 || honor.SlotMask < 1 || honor.SlotMask > 15 || honor.SameCardID < 0 || honor.GetType < 0 || honor.GetType > 4 {
			return fmt.Errorf("invalid CN honor %d", honor.HonorID)
		}
		if honor.Name == "" && honor.HonorID != 10000000 {
			return fmt.Errorf("CN honor %d has no display name", honor.HonorID)
		}
		if _, exists := seen[honor.HonorID]; exists {
			return fmt.Errorf("duplicate CN honor %d", honor.HonorID)
		}
		seen[honor.HonorID] = struct{}{}
		if honor.DefaultOwned {
			defaultCount++
		}
	}
	if defaultCount == 0 {
		return errors.New("CN honor runtime master has no official default-owned rows")
	}
	return nil
}

func ApplyHonorRuntimeMaster(state *gamestate.State, master honorRuntimeMaster) (bool, error) {
	definitions := make([]gamestate.CollectionRewardDefinition, 0, len(master.Honors))
	for _, honor := range master.Honors {
		definitions = append(definitions, gamestate.CollectionRewardDefinition{Type: 18, ID: honor.HonorID, Name: honor.Name, Detail: "称号 · 领取后在称号设置中使用"})
	}
	state.SetCollectionRewardDefinitions(18, definitions)
	available := make(map[int]struct{}, len(master.Honors))
	defaults := make([]int, 0)
	for _, honor := range master.Honors {
		available[honor.HonorID] = struct{}{}
		if honor.DefaultOwned {
			defaults = append(defaults, honor.HonorID)
		}
	}
	changed := false
	if len(state.Honors.HonorIDs) == 0 {
		state.Honors.HonorIDs = append([]int(nil), defaults...)
		changed = true
	}
	owned := make(map[int]struct{}, len(state.Honors.HonorIDs))
	for _, honorID := range state.Honors.HonorIDs {
		if _, exists := available[honorID]; !exists {
			return false, fmt.Errorf("configured owned honor %d is absent from official master", honorID)
		}
		owned[honorID] = struct{}{}
	}
	for _, honorID := range state.Honors.DeckHonorIDs {
		if honorID == 0 {
			continue
		}
		if _, exists := owned[honorID]; !exists {
			return false, fmt.Errorf("configured honor deck references unowned honor %d", honorID)
		}
	}
	return changed, nil
}
