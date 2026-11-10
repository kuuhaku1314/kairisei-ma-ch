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

const maxStampMasterBytes = 4 * 1024 * 1024

type stampDefinition struct {
	StampID       int    `json:"stamp_id"`
	Category      int    `json:"category"`
	Label         string `json:"label"`
	DisplayText   string `json:"display_text"`
	PictID        int    `json:"pict_id,omitempty"`
	SEID          int    `json:"se_id,omitempty"`
	VoiceID       int    `json:"voice_id,omitempty"`
	ArthurVoiceID string `json:"arthur_voice_id,omitempty"`
	DefaultOwned  bool   `json:"default_owned"`
}

type stampRuntimeMaster struct {
	CategoryOrder []int             `json:"category_order"`
	Stamps        []stampDefinition `json:"stamps"`
}

func LoadStampRuntimeMaster(masterPath string) (stampRuntimeMaster, error) {
	if masterPath == "" {
		return stampRuntimeMaster{}, errors.New("CN stamp runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return stampRuntimeMaster{}, fmt.Errorf("resolve CN stamp runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return stampRuntimeMaster{}, fmt.Errorf("open CN stamp runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return stampRuntimeMaster{}, fmt.Errorf("stat CN stamp runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxStampMasterBytes {
		return stampRuntimeMaster{}, errors.New("CN stamp runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxStampMasterBytes+1))
	var master stampRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return stampRuntimeMaster{}, fmt.Errorf("decode CN stamp runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return stampRuntimeMaster{}, err
	}
	if err := validateStampRuntimeMaster(master); err != nil {
		return stampRuntimeMaster{}, err
	}
	return master, nil
}

func validateStampRuntimeMaster(master stampRuntimeMaster) error {
	if len(master.Stamps) == 0 {
		return errors.New("CN stamp runtime master source is incomplete")
	}
	categories := make(map[int]struct{}, len(master.CategoryOrder))
	for _, category := range master.CategoryOrder {
		if category < 1 || category > 7 {
			return fmt.Errorf("invalid CN stamp category %d", category)
		}
		if _, exists := categories[category]; exists {
			return fmt.Errorf("duplicate CN stamp category %d", category)
		}
		categories[category] = struct{}{}
	}
	if len(categories) != 7 {
		return errors.New("CN stamp category order is incomplete")
	}
	seen := make(map[int]struct{}, len(master.Stamps))
	defaultCount := 0
	for _, stamp := range master.Stamps {
		if stamp.StampID <= 0 || stamp.Category < 1 || stamp.Category > 7 || stamp.Label == "" {
			return fmt.Errorf("invalid CN stamp %d", stamp.StampID)
		}
		if _, exists := seen[stamp.StampID]; exists {
			return fmt.Errorf("duplicate CN stamp %d", stamp.StampID)
		}
		seen[stamp.StampID] = struct{}{}
		if stamp.DefaultOwned {
			defaultCount++
		}
	}
	if defaultCount == 0 {
		return errors.New("CN stamp runtime master has no official default-owned rows")
	}
	return nil
}

func ApplyStampRuntimeMaster(state *gamestate.State, master stampRuntimeMaster) error {
	definitions := make([]gamestate.CollectionRewardDefinition, 0, len(master.Stamps))
	for _, stamp := range master.Stamps {
		name := stamp.DisplayText
		if name == "" {
			name = stamp.Label
		}
		kind, label := "dialog", "对话"
		if stamp.PictID > 0 {
			kind, label = "image", "表情"
			if stamp.SEID > 0 || stamp.VoiceID > 0 || stamp.ArthurVoiceID != "" {
				kind, label = "voice", "语音表情"
			}
		}
		definitions = append(definitions, gamestate.CollectionRewardDefinition{Type: 16, ID: stamp.StampID, Name: name,
			Detail: label + " · 领取后在聊天编成中使用", PictID: stamp.PictID, StampKind: kind})
	}
	state.SetCollectionRewardDefinitions(16, definitions)
	available := make(map[int]struct{}, len(master.Stamps))
	defaults := make([]int, 0)
	for _, stamp := range master.Stamps {
		available[stamp.StampID] = struct{}{}
		if stamp.DefaultOwned {
			defaults = append(defaults, stamp.StampID)
		}
	}
	if len(state.Stamps.StampIDs) == 0 {
		state.Stamps.StampIDs = append([]int(nil), defaults...)
	}
	owned := make(map[int]struct{}, len(state.Stamps.StampIDs))
	for _, stampID := range state.Stamps.StampIDs {
		if _, exists := available[stampID]; !exists {
			return fmt.Errorf("configured owned stamp %d is absent from official master", stampID)
		}
		owned[stampID] = struct{}{}
	}
	for _, stampID := range state.Stamps.DeckStampIDs {
		if stampID == 0 {
			continue
		}
		if _, exists := owned[stampID]; !exists {
			return fmt.Errorf("configured stamp deck references unowned stamp %d", stampID)
		}
	}
	return nil
}
