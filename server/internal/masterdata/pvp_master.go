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
	"kairisei.local/server/internal/gamestate"
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
	if config.SchemaVersion != 2 || config.ClientProfile != "cn602-bootstrap" || config.ConfigVersion <= 0 {
		return game.PVPConfig{}, errors.New("CN PVP runtime config identity is invalid")
	}
	if config.ChallengeMax != 5 || config.CostInitial <= 0 || config.TurnMax <= 0 || config.HoldMax <= 0 ||
		config.RankWinPoint <= 0 || config.RankLosePoint > 0 || config.ReplayResult < 0 || config.ReplayResult > 1 {
		return game.PVPConfig{}, errors.New("CN PVP runtime rules are invalid")
	}
	if config.EngineMode != "server_replay" && config.EngineMode != "client_native_local" {
		return game.PVPConfig{}, errors.New("CN PVP runtime engine mode is invalid")
	}
	if (config.ResultSource != game.PVPResultFixedPolicy && config.ResultSource != game.PVPResultRecordedReplay) || (config.ResultSource == game.PVPResultRecordedReplay && config.EngineMode != game.PvpEngineServerReplay) {
		return game.PVPConfig{}, errors.New("CN PVP result source is invalid")
	}
	if config.EngineMode == "server_replay" && len(config.ResultCommands) == 0 {
		return game.PVPConfig{}, errors.New("CN PVP server replay mode requires result commands")
	}
	if len(config.Fields) != 8 || len(config.Ranks) == 0 {
		return game.PVPConfig{}, errors.New("CN PVP runtime config is missing official fields or ranks")
	}
	seenFields := make(map[int]struct{}, len(config.Fields))
	for _, field := range config.Fields {
		if field.FieldID <= 0 || field.MapID <= 0 || field.BGMID <= 0 {
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
		if rank.RankID < 0 || rank.Name == "" || rank.PointMin != nextPoint || rank.PointMax < rank.PointMin || rank.WinCoin <= 0 {
			return game.PVPConfig{}, errors.New("CN PVP runtime config contains an invalid rank range")
		}
		nextPoint = rank.PointMax + 1
	}
	return config, nil
}

func NormalizePVPState(state *gamestate.State, config game.PVPConfig) bool {
	changed := false
	if state.PVPConfigVersion == 0 {
		state.PVP.Challenge = config.ChallengeMax
		changed = true
	}
	if state.PVPConfigVersion < config.ConfigVersion {
		state.PVPConfigVersion = config.ConfigVersion
		changed = true
	}
	if state.PVP.NextBattleID <= 0 {
		state.PVP.NextBattleID = 1
		changed = true
	}
	if state.PVP.DefenseDecks == nil {
		state.PVP.DefenseDecks = []gamestate.PVPDeckSelection{}
		changed = true
	}
	if state.PVP.History == nil {
		state.PVP.History = []gamestate.PVPMatch{}
		changed = true
	}
	return changed
}
