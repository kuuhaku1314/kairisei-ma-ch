package masterdata

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"kairisei.local/server/internal/gamestate"
)

func RequireJSONEOF(decoder *json.Decoder) error {
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("CN save contains multiple JSON values")
		}
		return fmt.Errorf("decode CN save trailing data: %w", err)
	}
	return nil
}

func ValidateTeamBattleReplaySegments(replay gamestate.TeamBattleReplay) error {
	if len(replay.Battles) == 0 {
		return nil
	}
	if len(replay.Battles) > 64 ||
		replay.Battles[0].EnemyPartyID != replay.EnemyPartyID ||
		replay.Battles[0].EnemyType != replay.EnemyType {
		return errors.New("CN save team battle segment projection is invalid")
	}
	for _, segment := range replay.Battles {
		if segment.EnemyPartyID <= 0 || segment.EnemyType < 0 || segment.EnemyType > 4 {
			return errors.New("CN save team battle segment is invalid")
		}
	}
	return nil
}

func ValidPersistedRewardShape(reward gamestate.Reward) bool {
	if reward.Num <= 0 || reward.CardSkillLevels == nil {
		return false
	}
	switch reward.Type {
	case 0, 4, 10, 12:
		return reward.RewardTypeID == 0
	case 6:
		return reward.RewardTypeID > 0 && reward.CardLevel >= 0 && reward.CardFame >= 0 && reward.CardLove >= 0
	case 8, 13, 15, 19:
		return reward.RewardTypeID > 0
	case 14, 16, 18:
		return reward.RewardTypeID > 0 && reward.Num == 1
	default:
		return false
	}
}

func ValidateStageQuestConfiguration(content json.RawMessage) error {
	if len(content) == 0 || bytes.Equal(content, []byte("null")) {
		return errors.New("CN save requires stage quest configuration")
	}
	var response struct {
		StageQuest struct {
			AreaID      int    `json:"areaid"`
			AreaName    string `json:"area_name"`
			AreaPictID  int    `json:"area_pictid"`
			ResetTime   int    `json:"reset_time"`
			StageObject []struct {
				StageID         int   `json:"stageid"`
				RequireStageIDs []int `json:"require_stageid"`
				StageType       int   `json:"stage_type"`
				TalkInfo        []any `json:"talk_info"`
				RaidBoss        []struct {
					BossGroup struct {
						BossGroupID      int                       `json:"boss_groupid"`
						StageType        gamestate.BattleStageType `json:"stage_type"`
						Name             string                    `json:"name"`
						PictID           int                       `json:"pictid"`
						StageQuestAreaID int                       `json:"stage_quest_areaid"`
						Bosses           []struct {
							BossID        int             `json:"bossid"`
							Difficulty    string          `json:"difficulty"`
							PictID        int             `json:"pictid"`
							RewardCardIDs []any           `json:"reward_cardids"`
							RewardSphrIDs []any           `json:"reward_sphrids"`
							UserBuffIDs   []int           `json:"user_buff_id"`
							Awake         json.RawMessage `json:"awake"`
							Challenge     []any           `json:"challenge"`
							Unlock        json.RawMessage `json:"unlock"`
						} `json:"bosses"`
						Stories    []any    `json:"stories"`
						ButtonStr1 []string `json:"button_str1"`
						ButtonStr2 []string `json:"button_str2"`
					} `json:"boss_group"`
					ClearReward  []any `json:"clear_reward"`
					AttackReward []any `json:"attack_reward"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
		StageClear    []any `json:"stage_clear"`
		NewClearStage []int `json:"new_clear_stage"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return fmt.Errorf("decode CN stage quest configuration: %w", err)
	}
	stageQuest := response.StageQuest
	if stageQuest.AreaID <= 0 || stageQuest.AreaName == "" ||
		stageQuest.AreaPictID <= 0 || stageQuest.ResetTime <= 0 ||
		len(stageQuest.StageObject) == 0 || response.StageClear == nil ||
		response.NewClearStage == nil {
		return errors.New("CN save stage quest configuration is incomplete")
	}
	stageIDs := make(map[int]struct{}, len(stageQuest.StageObject))
	for _, stage := range stageQuest.StageObject {
		if stage.StageID <= 0 || stage.StageType <= 0 || stage.RequireStageIDs == nil ||
			stage.TalkInfo == nil || len(stage.RaidBoss) == 0 {
			return errors.New("CN save stage quest object is incomplete")
		}
		if _, exists := stageIDs[stage.StageID]; exists {
			return fmt.Errorf("duplicate CN stage quest stage ID %d", stage.StageID)
		}
		stageIDs[stage.StageID] = struct{}{}
		for _, raid := range stage.RaidBoss {
			group := raid.BossGroup
			if group.BossGroupID <= 0 || (group.StageType != gamestate.BattleStageStageQuest && group.StageType != gamestate.BattleStageMainStageQuest) ||
				group.Name == "" || group.PictID <= 0 ||
				group.StageQuestAreaID != stageQuest.AreaID || len(group.Bosses) == 0 ||
				group.Stories == nil || group.ButtonStr1 == nil || group.ButtonStr2 == nil ||
				raid.ClearReward == nil || raid.AttackReward == nil {
				return errors.New("CN save stage quest boss group is incomplete")
			}
			for _, boss := range group.Bosses {
				if boss.BossID <= 0 || boss.Difficulty == "" || boss.PictID <= 0 ||
					boss.RewardCardIDs == nil || boss.RewardSphrIDs == nil ||
					(boss.UserBuffIDs != nil && len(boss.UserBuffIDs) == 0) || len(boss.Awake) == 0 ||
					bytes.Equal(boss.Awake, []byte("null")) || boss.Challenge == nil ||
					len(boss.Unlock) == 0 || bytes.Equal(boss.Unlock, []byte("null")) {
					return errors.New("CN save stage quest boss is incomplete")
				}
			}
		}
	}
	for _, stage := range stageQuest.StageObject {
		for _, required := range stage.RequireStageIDs {
			if _, exists := stageIDs[required]; !exists {
				return fmt.Errorf("unknown CN stage quest prerequisite ID %d", required)
			}
		}
	}
	return nil
}

func StageQuestAreaID(content json.RawMessage) (int, error) {
	var response struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(content, &response); err != nil || response.StageQuest.AreaID <= 0 {
		return 0, errors.New("CN save stage quest area identity is invalid")
	}
	return response.StageQuest.AreaID, nil
}
