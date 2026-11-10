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

const maxNaviMasterBytes = 1024 * 1024

const naviPresentationLive2D = 0

type naviDefinition struct {
	PresentationKind int    `json:"presentation_kind,omitempty"`
	NaviID           int    `json:"navi_id"`
	Name             string `json:"name"`
	PictID           int    `json:"pict_id"`
	ItemPictID       int    `json:"item_pict_id"`
	Live2DFolder     string `json:"live2d_folder"`
	Live2DBundle     string `json:"live2d_bundle"`

	VoiceID         int  `json:"voice_id"`
	ClientPublished bool `json:"client_published"`
}

type naviLocalPurchase struct {
	Price int `json:"price"`
}

type naviRuntimeMaster struct {
	SchemaVersion int    `json:"schema_version"`
	ClientProfile string `json:"client_profile"`

	LocalPurchase naviLocalPurchase `json:"local_purchase"`
	Navigators    []naviDefinition  `json:"navigators"`
}

func LoadNaviRuntimeMaster(masterPath string) (naviRuntimeMaster, error) {
	if masterPath == "" {
		return naviRuntimeMaster{}, errors.New("CN navigator runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return naviRuntimeMaster{}, fmt.Errorf("resolve CN navigator runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return naviRuntimeMaster{}, fmt.Errorf("open CN navigator runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return naviRuntimeMaster{}, fmt.Errorf("stat CN navigator runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxNaviMasterBytes {
		return naviRuntimeMaster{}, errors.New("CN navigator runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxNaviMasterBytes+1))
	var master naviRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return naviRuntimeMaster{}, fmt.Errorf("decode CN navigator runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return naviRuntimeMaster{}, err
	}
	if err := validateNaviRuntimeMaster(master); err != nil {
		return naviRuntimeMaster{}, err
	}
	return master, nil
}

func validateNaviRuntimeMaster(master naviRuntimeMaster) error {
	if master.SchemaVersion != 3 || master.ClientProfile != "cn602-bootstrap" {
		return errors.New("CN navigator runtime master identity is invalid")
	}
	if master.LocalPurchase.Price <= 0 {
		return errors.New("CN navigator local purchase profile is invalid")
	}
	if len(master.Navigators) == 0 {
		return errors.New("CN navigator runtime master source is incomplete")
	}
	seen := make(map[int]struct{}, len(master.Navigators))
	published := 0
	for _, navigator := range master.Navigators {

		if navigator.PresentationKind != naviPresentationLive2D ||
			navigator.NaviID < 0 || navigator.NaviID > 127 || navigator.Name == "" ||
			navigator.PictID <= 0 || navigator.ItemPictID <= 0 ||
			navigator.Live2DFolder == "" || navigator.VoiceID < 0 {
			return fmt.Errorf("invalid CN navigator %d", navigator.NaviID)
		}
		if _, exists := seen[navigator.NaviID]; exists {
			return fmt.Errorf("duplicate CN navigator %d", navigator.NaviID)
		}
		expectedBundle := fmt.Sprintf("live2d/live2d_%s.dat", navigator.Live2DFolder)
		if navigator.ClientPublished {
			if navigator.Live2DBundle != expectedBundle {
				return fmt.Errorf("published CN navigator %d has no presentation resource closure", navigator.NaviID)
			}
			published++
		} else {
			if navigator.Live2DBundle != "" {
				return fmt.Errorf("excluded CN navigator %d must not publish a bundle", navigator.NaviID)
			}
		}
		seen[navigator.NaviID] = struct{}{}
	}
	if published == 0 {
		return errors.New("CN navigator catalog has no published entries")
	}
	return nil
}

func ApplyNaviRuntimeMaster(state *gamestate.State, master naviRuntimeMaster) error {
	state.User.NaviCatalogIDs = make([]int8, 0, len(master.Navigators))
	available := make(map[int8]struct{}, len(master.Navigators))
	for _, navigator := range master.Navigators {
		if !navigator.ClientPublished {
			continue
		}
		id := int8(navigator.NaviID)
		state.User.NaviCatalogIDs = append(state.User.NaviCatalogIDs, id)
		available[id] = struct{}{}
	}
	for _, id := range state.User.SelectableNaviIDs {
		if _, exists := available[id]; !exists {
			return fmt.Errorf("configured selectable navigator %d is absent from official master", id)
		}
	}
	state.User.NaviPurchasePrice = master.LocalPurchase.Price
	return nil
}
