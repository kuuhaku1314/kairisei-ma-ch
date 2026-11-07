package masterdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"

	"kairisei.local/server/internal/gamestate"
)

const maxNaviMasterBytes = 1024 * 1024

const naviPresentationLive2D = 0

type naviDefinition struct {
	PresentationKind  int    `json:"presentation_kind,omitempty"`
	NaviID            int    `json:"navi_id"`
	Name              string `json:"name"`
	PictID            int    `json:"pict_id"`
	ItemPictID        int    `json:"item_pict_id"`
	Live2DFolder      string `json:"live2d_folder"`
	Live2DBundle      string `json:"live2d_bundle"`
	Live2DSourceState string `json:"live2d_source_state"`
	VoiceID           int    `json:"voice_id"`
	ClientPublished   bool   `json:"client_published"`
}

type naviLocalPurchase struct {
	Evidence string `json:"evidence"`
	Currency string `json:"currency"`
	Price    int    `json:"price"`
}

type naviPublicationPolicy struct {
	Evidence             string `json:"evidence"`
	Live2DBundleRequired *bool  `json:"live2d_bundle_required"`
	VoiceSourceRequired  *bool  `json:"voice_source_required"`
	VoiceFallback        string `json:"voice_fallback"`
}

type naviSummary struct {
	OfficialNavigatorRows    int   `json:"official_navigator_rows"`
	ImportedNavigatorRows    int   `json:"imported_navigator_rows,omitempty"`
	ClientPublished          int   `json:"client_published"`
	ExcludedMissingLive2D    int   `json:"excluded_missing_live2d"`
	ExcludedMissingLive2DIDs []int `json:"excluded_missing_live2d_ids"`
}

type naviRuntimeMaster struct {
	SchemaVersion int                   `json:"schema_version"`
	ClientProfile string                `json:"client_profile"`
	Source        json.RawMessage       `json:"source"`
	Publication   naviPublicationPolicy `json:"publication_policy"`
	LocalPurchase naviLocalPurchase     `json:"local_purchase"`
	Summary       naviSummary           `json:"summary"`
	Navigators    []naviDefinition      `json:"navigators"`
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
	decoder.DisallowUnknownFields()
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
	if master.LocalPurchase.Evidence != "PLACEHOLDER_LOCAL_PROFILE" ||
		master.LocalPurchase.Currency != "CRYSTAL" || master.LocalPurchase.Price <= 0 {
		return errors.New("CN navigator local purchase profile is invalid")
	}
	if (master.Publication.Evidence != "CONFIRMED_CN_OFFICIAL_ASSET_MAP" && master.Publication.Evidence != "INFERRED") ||
		master.Publication.Live2DBundleRequired == nil || !*master.Publication.Live2DBundleRequired ||
		master.Publication.VoiceSourceRequired == nil || *master.Publication.VoiceSourceRequired ||
		master.Publication.VoiceFallback != "OPTIONAL_SILENT_FALLBACK" {
		return errors.New("CN navigator publication policy is invalid")
	}
	if len(master.Source) == 0 || bytes.Equal(master.Source, []byte("null")) || len(master.Navigators) == 0 {
		return errors.New("CN navigator runtime master source is incomplete")
	}
	seen := make(map[int]struct{}, len(master.Navigators))
	published := 0
	imported := 0
	excluded := make([]int, 0)
	for _, navigator := range master.Navigators {
		if navigator.Live2DSourceState == "PRESENT_IN_JP_DERIVED_OVERLAY" || navigator.Live2DSourceState == "PRESENT_IN_WORKSHOP_OVERLAY" || navigator.Live2DSourceState == "PRESENT_IN_USER_DERIVED_OVERLAY" {
			imported++
		}
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
			validSource := navigator.Live2DSourceState == "PRESENT_IN_CN_OFFICIAL_ASSET_MAP" ||
				(master.Publication.Evidence == "INFERRED" &&
					(navigator.Live2DSourceState == "PRESENT_IN_PRIVATE_DERIVED_OVERLAY" ||
						navigator.Live2DSourceState == "PRESENT_IN_JP_DERIVED_OVERLAY" ||
						navigator.Live2DSourceState == "PRESENT_IN_WORKSHOP_OVERLAY" ||
						navigator.Live2DSourceState == "PRESENT_IN_USER_DERIVED_OVERLAY"))
			if navigator.Live2DBundle != expectedBundle || !validSource {
				return fmt.Errorf("published CN navigator %d has no presentation resource closure", navigator.NaviID)
			}
			published++
		} else {
			if navigator.Live2DBundle != "" || navigator.Live2DSourceState != "ABSENT_FROM_CN_OFFICIAL_ASSET_MAP" {
				return fmt.Errorf("excluded CN navigator %d has an invalid source state", navigator.NaviID)
			}
			excluded = append(excluded, navigator.NaviID)
		}
		seen[navigator.NaviID] = struct{}{}
	}
	if master.Summary.OfficialNavigatorRows < 0 || master.Summary.ImportedNavigatorRows < 0 ||
		master.Summary.OfficialNavigatorRows+master.Summary.ImportedNavigatorRows != len(master.Navigators) ||
		master.Summary.ImportedNavigatorRows != imported ||
		master.Summary.ClientPublished != published || published == 0 ||
		master.Summary.ExcludedMissingLive2D != len(excluded) ||
		!slices.Equal(master.Summary.ExcludedMissingLive2DIDs, excluded) {
		return errors.New("CN navigator runtime master summary is inconsistent")
	}
	return nil
}

func ApplyNaviRuntimeMaster(state *gamestate.State, master naviRuntimeMaster) error {
	state.User.NaviCatalogIDs = make([]int8, 0, master.Summary.ClientPublished)
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
