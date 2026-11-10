package game

import (
	"kairisei.local/server/internal/gamestate"
)

const pvpHistoryLimit = 50

type PVPFieldConfig struct {
	FieldID int `json:"fieldid"`
}

type PVPRankConfig struct {
	RankID   int    `json:"rank_id"`
	Name     string `json:"name"`
	PointMin int    `json:"point_min"`
	PointMax int    `json:"point_max"`
}

type PVPResultCommand struct {
	InputAPI  int    `json:"input_api"`
	ResultCmd string `json:"result_cmd"`
}

// PVPConfig contains static local rules. Official client-owned field and rank
// rows are kept separate from mutable account state in SQLite.
type PVPConfig struct {
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
	return result
}

func BoolInt(value bool) int {
	if value {
		return 1
	}
	return 0
}
