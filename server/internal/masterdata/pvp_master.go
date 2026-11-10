package masterdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"

	"kairisei.local/server/internal/game"
)

const maxPVPMasterBytes = 1024 * 1024

func LoadPVPRuntimeMaster(masterPath string) (game.PVPConfig, error) {
	if masterPath == "" {
		return game.PVPConfig{}, errors.New("CN PVP runtime config is required")
	}
	content, err := os.ReadFile(masterPath)
	if err != nil {
		return game.PVPConfig{}, fmt.Errorf("read CN PVP runtime config: %w", err)
	}
	if len(content) == 0 || len(content) > maxPVPMasterBytes {
		return game.PVPConfig{}, errors.New("CN PVP runtime config must be non-empty and at most one MiB")
	}
	var config game.PVPConfig
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&config); err != nil {
		return game.PVPConfig{}, fmt.Errorf("decode CN PVP runtime config: %w", err)
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return game.PVPConfig{}, errors.New("CN PVP runtime config has trailing JSON")
		}
		return game.PVPConfig{}, fmt.Errorf("decode trailing CN PVP runtime config: %w", err)
	}
	if config.ChallengeMax != 5 || config.CostInitial <= 0 || config.TurnMax <= 0 || config.HoldMax <= 0 ||
		config.RankWinPoint <= 0 || config.RankLosePoint > 0 || config.ReplayResult < 0 || config.ReplayResult > 1 {
		return game.PVPConfig{}, errors.New("CN PVP runtime rules are invalid")
	}
	if len(config.Fields) != 8 || len(config.Ranks) == 0 {
		return game.PVPConfig{}, errors.New("CN PVP runtime config is missing official fields or ranks")
	}
	seenFields := make(map[int]struct{}, len(config.Fields))
	for _, field := range config.Fields {
		if field.FieldID <= 0 {
			return game.PVPConfig{}, errors.New("CN PVP runtime config contains an invalid field")
		}
		if _, exists := seenFields[field.FieldID]; exists {
			return game.PVPConfig{}, errors.New("CN PVP runtime config contains a duplicate field")
		}
		seenFields[field.FieldID] = struct{}{}
	}
	ranks := append([]game.PVPRankConfig(nil), config.Ranks...)
	sort.Slice(ranks, func(left, right int) bool { return ranks[left].PointMin < ranks[right].PointMin })
	nextPoint := 0
	for _, rank := range ranks {
		if rank.RankID < 0 || rank.Name == "" || rank.PointMin != nextPoint || rank.PointMax < rank.PointMin {
			return game.PVPConfig{}, errors.New("CN PVP runtime config contains an invalid rank range")
		}
		nextPoint = rank.PointMax + 1
	}
	return config, nil
}
