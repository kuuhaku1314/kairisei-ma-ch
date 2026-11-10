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

const maxExploreMasterBytes = 1024 * 1024

type exploreMap struct {
	ID int `json:"id"`
}

type exploreRuntimeMaster struct {
	Maps        []exploreMap             `json:"maps"`
	Stages      []gamestate.ExploreStage `json:"stages"`
	FloorRarity int                      `json:"floor_rarity"`
	Events      []json.RawMessage        `json:"events"`
}

func LoadExploreRuntimeMaster(masterPath string) (exploreRuntimeMaster, error) {
	if masterPath == "" {
		return exploreRuntimeMaster{}, errors.New("CN Explore runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return exploreRuntimeMaster{}, fmt.Errorf("resolve CN Explore runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return exploreRuntimeMaster{}, fmt.Errorf("open CN Explore runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return exploreRuntimeMaster{}, fmt.Errorf("stat CN Explore runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxExploreMasterBytes {
		return exploreRuntimeMaster{}, errors.New("CN Explore runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxExploreMasterBytes+1))
	var master exploreRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return exploreRuntimeMaster{}, fmt.Errorf("decode CN Explore runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return exploreRuntimeMaster{}, err
	}
	if err := validateExploreRuntimeMaster(master); err != nil {
		return exploreRuntimeMaster{}, err
	}
	return master, nil
}

func validateExploreRuntimeMaster(master exploreRuntimeMaster) error {
	if len(master.Maps) == 0 || len(master.Stages) == 0 {
		return errors.New("CN Explore runtime master source is incomplete")
	}
	maps := make(map[int]struct{}, len(master.Maps))
	for _, exploreMap := range master.Maps {
		if exploreMap.ID <= 0 {
			return fmt.Errorf("invalid CN Explore map %d", exploreMap.ID)
		}
		if _, exists := maps[exploreMap.ID]; exists {
			return fmt.Errorf("duplicate CN Explore map %d", exploreMap.ID)
		}
		maps[exploreMap.ID] = struct{}{}
	}
	stages := master.Stages
	stageIDs := make(map[int]struct{}, len(stages))
	floorIDs := make(map[int]struct{})
	for _, stage := range stages {
		if stage.ExploreStageID <= 0 || stage.StageName == "" || stage.StageMapID <= 0 ||
			stage.StageType < 0 || stage.LimitSeconds <= 0 || len(stage.Floors) == 0 {
			return errors.New("CN Explore runtime stage is incomplete")
		}
		if _, duplicate := stageIDs[stage.ExploreStageID]; duplicate {
			return fmt.Errorf("duplicate CN Explore runtime stage %d", stage.ExploreStageID)
		}
		stageIDs[stage.ExploreStageID] = struct{}{}
		if _, exists := maps[stage.StageMapID]; !exists {
			return fmt.Errorf("CN Explore runtime stage references unknown map %d", stage.StageMapID)
		}
		for _, floor := range stage.Floors {
			if floor.ExploreFloorID <= 0 || floor.FloorName == "" {
				return errors.New("CN Explore runtime floor is incomplete")
			}
			if _, duplicate := floorIDs[floor.ExploreFloorID]; duplicate {
				return fmt.Errorf("duplicate CN Explore runtime floor %d", floor.ExploreFloorID)
			}
			floorIDs[floor.ExploreFloorID] = struct{}{}
		}
	}
	if master.FloorRarity < 0 || master.FloorRarity > 1 || len(master.Events) == 0 {
		return errors.New("CN Explore runtime event sequence is incomplete")
	}
	for index, raw := range master.Events {
		var event struct {
			TreasureBoxes []json.RawMessage `json:"treasureboxes"`
			Symbols       []json.RawMessage `json:"symbols"`
		}
		if err := json.Unmarshal(raw, &event); err != nil {
			return fmt.Errorf("decode CN Explore event %d: %w", index, err)
		}
		if (len(event.TreasureBoxes) == 0) == (len(event.Symbols) == 0) {
			return fmt.Errorf("CN Explore event %d must contain exactly one event kind", index)
		}
	}
	return nil
}

func ApplyExploreRuntimeMaster(state *gamestate.State, master exploreRuntimeMaster) error {
	state.Explore.Stage = master.Stages[0]
	state.Explore.Stage.Floors = append([]gamestate.ExploreFloor(nil), master.Stages[0].Floors...)
	stages := master.Stages
	state.Explore.Stages = make([]gamestate.ExploreStage, len(stages))
	knownStageIDs := make(map[int]struct{}, len(stages))
	for index, stage := range stages {
		state.Explore.Stages[index] = stage
		state.Explore.Stages[index].Floors = append([]gamestate.ExploreFloor(nil), stage.Floors...)
		knownStageIDs[stage.ExploreStageID] = struct{}{}
	}
	if state.Explore.StageCursor < 0 || state.Explore.StageCursor >= len(stages) {
		state.Explore.StageCursor = 0
	}
	if state.Explore.Active {
		if _, exists := knownStageIDs[state.Explore.ActiveStageID]; !exists {
			state.Explore.ActiveStageID = master.Stages[0].ExploreStageID
		}
	} else {
		state.Explore.ActiveStageID = 0
	}
	state.Explore.FloorRarity = master.FloorRarity
	state.Explore.Events = make([]json.RawMessage, len(master.Events))
	for index, event := range master.Events {
		state.Explore.Events[index] = append(json.RawMessage(nil), event...)
	}
	if len(state.Avatars) != 4 {
		return errors.New("CN Explore runtime requires four persisted avatars")
	}
	state.Explore.Avatar = gamestate.ExploreAvatar{
		CostumeID:     state.Avatars[0].CostumeID,
		AvatarPartIDs: append([]int(nil), state.Avatars[0].AvatarPartIDs...),
	}
	return nil
}
