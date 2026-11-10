package masterdata

import (
	"bytes"
	"encoding/json"
	"fmt"

	"kairisei.local/server/internal/gamestate"
)

type gateTeamBattleBoss struct {
	battleEntryRules
	BossID      int    `json:"0"`
	Difficulty  string `json:"4"`
	BPUse       int    `json:"5"`
	PictID      int    `json:"9"`
	State       int    `json:"10"`
	RewardCards []struct {
		CardID int `json:"0"`
	} `json:"12"`
	RewardSpheres []json.RawMessage `json:"13"`
	IsModel       int               `json:"14"`
	UserBuffIDs   []int             `json:"15"`
	Awake         json.RawMessage   `json:"18"`
	Challenge     []json.RawMessage `json:"25"`
	Unlock        json.RawMessage   `json:"27"`
}

type GateTeamBattleGroup struct {
	GroupID          int                       `json:"0"`
	StageType        gamestate.BattleStageType `json:"1"`
	IsReleased       int                       `json:"3"`
	Name             string                    `json:"4"`
	PictID           int                       `json:"7"`
	StageQuestAreaID int                       `json:"9"`
	Bosses           []gateTeamBattleBoss      `json:"10"`
	Stories          []json.RawMessage         `json:"11"`
	ButtonStrings1   []string                  `json:"12"`
	ButtonStrings2   []string                  `json:"13"`
}

func ValidateTeamBattleBossGate(boss gateTeamBattleBoss) error {
	if !boss.battleEntryRules.valid() || boss.BossID <= 0 || boss.Difficulty == "" || boss.BPUse <= 0 || boss.PictID <= 0 ||
		boss.State < 0 || boss.State > 2 || (boss.IsModel != 0 && boss.IsModel != 1) ||
		boss.RewardCards == nil || boss.RewardSpheres == nil ||
		(boss.UserBuffIDs != nil && len(boss.UserBuffIDs) == 0) ||
		len(boss.Awake) == 0 || bytes.Equal(boss.Awake, []byte("null")) ||
		boss.Challenge == nil || len(boss.Unlock) == 0 || bytes.Equal(boss.Unlock, []byte("null")) {
		return fmt.Errorf("boss %d DTO is unavailable or incomplete", boss.BossID)
	}
	for _, reward := range boss.RewardCards {
		if reward.CardID <= 0 {
			return fmt.Errorf("boss %d references an invalid card %d", boss.BossID, reward.CardID)
		}
	}
	return nil
}
