package game

import (
	"kairisei.local/server/internal/gamestate"
)

const pvpHistoryLimit = 50

const (
	PvpEngineServerReplay      = "server_replay"
	PvpEngineClientNativeLocal = "client_native_local"
	PVPResultFixedPolicy       = "fixed_local_policy"
	PVPResultRecordedReplay    = "recorded_replay"
)

type PVPFieldConfig struct {
	FieldID int `json:"fieldid"`
	MapID   int `json:"mapid"`
	BGMID   int `json:"bgmid"`
}

type PVPRankConfig struct {
	RankID   int    `json:"rank_id"`
	Name     string `json:"name"`
	PointMin int    `json:"point_min"`
	PointMax int    `json:"point_max"`
	WinCoin  int    `json:"win_coin"`
}

type PVPResultCommand struct {
	InputAPI  int    `json:"input_api"`
	ResultCmd string `json:"result_cmd"`
}

// PVPConfig contains static local rules. Official client-owned field and rank
// rows are kept separate from mutable account state in SQLite.
type PVPConfig struct {
	ResultSource   string             `json:"result_source"`
	EngineMode     string             `json:"engine_mode"`
	ChallengeMax   int                `json:"challenge_max"`
	CostInitial    int                `json:"cost_initial"`
	TurnMax        int                `json:"turn_max"`
	HoldMax        int                `json:"hold_max"`
	GimmickID      int                `json:"gimmickid"`
	RankWinPoint   int                `json:"rank_win_point"`
	RankLosePoint  int                `json:"rank_lose_point"`
	ReplayResult   int                `json:"replay_result"`
	ResetStartTime int                `json:"reset_start_time"`
	ResetEndTime   int                `json:"reset_end_time"`
	Fields         []PVPFieldConfig   `json:"fields"`
	Ranks          []PVPRankConfig    `json:"ranks"`
	ResultCommands []PVPResultCommand `json:"result_cmds"`
}

type PVPAccountRepository interface {
	ListPVPOpponents(userID int) ([]gamestate.State, error)
}

func ClonePVPDeckSelections(source []gamestate.PVPDeckSelection) []gamestate.PVPDeckSelection {
	result := make([]gamestate.PVPDeckSelection, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].CardUniqueIDs = append([]int64(nil), source[index].CardUniqueIDs...)
		result[index].SupportCardUniqueIDs = append([]int64(nil), source[index].SupportCardUniqueIDs...)
		result[index].SphereUniqueIDs = append([]int64(nil), source[index].SphereUniqueIDs...)
		result[index].BuddyUniqueIDs = append([]int64(nil), source[index].BuddyUniqueIDs...)
	}
	return result
}

func ClonePVPState(source gamestate.PVPPlayerState) gamestate.PVPPlayerState {
	result := source
	result.DefenseDecks = ClonePVPDeckSelections(source.DefenseDecks)
	result.History = make([]gamestate.PVPMatch, len(source.History))
	copy(result.History, source.History)
	if source.ActiveMatch != nil {
		active := *source.ActiveMatch
		result.ActiveMatch = &active
	}
	return result
}

func BoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
