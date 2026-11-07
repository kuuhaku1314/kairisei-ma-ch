package game

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"kairisei.local/server/battlecodec"
	"kairisei.local/server/internal/gamestate"
)

type TeamBattleContinueReport struct {
	PayType       int8     `json:"pay_type"`
	Progress      int      `json:"progress"`
	InputCommands []string `json:"input_cmd"`
	EnemyDeadBits []int    `json:"enemy_dead_bit"`
}

const (
	MaxTeamBattleSegments            = 64
	nativeBattleCommandMaxOpcode     = 34
	nativeBattleCommandStartOpcode   = 22
	NativePVPCommandStartOpcode      = 23
	nativeBattleCommandTurnOpcode    = 10
	nativeBattleCommandMaxTurnPhases = 100
)

func validateNativeTeamBattleContinueReport(
	progress int,
	inputCommands []string,
	enemyDeadBits []int,
	enemyTypes []int8,
) error {
	if len(enemyTypes) == 0 || len(enemyTypes) > MaxTeamBattleSegments ||
		len(inputCommands) != len(enemyTypes) || len(enemyDeadBits) != len(enemyTypes) ||
		progress < 1 || progress > len(enemyTypes) {
		return errors.New("team battle continue report does not match the active battle")
	}
	activeIndex := progress - 1
	for index := range enemyTypes {
		command := strings.TrimSpace(inputCommands[index])
		if enemyDeadBits[index] < 0 {
			return fmt.Errorf("battle segment %d: team battle continue enemy state is invalid", index)
		}
		switch {
		case index < activeIndex:
			if err := ValidateNativeTeamBattleCompletedSegment(
				command, enemyDeadBits[index], enemyTypes[index] == 4,
			); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		case index == activeIndex:
			if err := ValidateNativeBattleCommand(command, true); err != nil {
				return fmt.Errorf("battle segment %d: %w", index, err)
			}
		default:
			if command != "" || enemyDeadBits[index] != 0 {
				return fmt.Errorf("battle segment %d contains future battle state", index)
			}
		}
	}
	return nil
}

func ValidateNativeTeamBattleCompletedSegment(command string, enemyDeadBits int, optional bool) error {
	command = strings.TrimSpace(command)
	if optional && command == "" && enemyDeadBits == 0 {
		return nil
	}
	if enemyDeadBits < 0 {
		return errors.New("local team battle enemy state is invalid")
	}
	// Native victory is independent of the death mask: Nameless's official
	// PARTS_ALL_BREAK action sets ENEMY_AWAKE_FLAG_SET and FORCE_BATTLE_END
	// while its body survives (captured clear report: mask 6). Keep this mask
	// intact for per-enemy drops; do not require or fabricate a body kill.
	return ValidateNativeBattleCommand(command, true)
}

func ValidateNativeBattleCommand(command string, required bool) error {
	return ValidateNativeBattleCommandStart(command, required, nativeBattleCommandStartOpcode)
}

func ValidateNativeBattleCommandStart(command string, required bool, expectedStartOpcode int) error {
	if command == "" {
		if required {
			return errors.New("clear result has no native battle command")
		}
		return nil
	}

	lineCount := 0
	firstOpcode := -1
	startCount := 0
	turnCount := 0
	for _, line := range strings.Split(strings.ReplaceAll(command, "\r\n", "\n"), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Split(line, ",")
		if len(fields) != battlecodec.InputCommandFieldCount {
			return errors.New("native battle command row has an invalid field count")
		}
		values := make([]int64, battlecodec.InputCommandFieldCount)
		for index, field := range fields {
			value, err := strconv.ParseInt(strings.TrimSpace(field), 10, 32)
			if err != nil {
				return errors.New("native battle command row contains a non-int32 field")
			}
			values[index] = value
		}
		if values[0] < 0 {
			return errors.New("native battle command row contains an invalid HP checksum")
		}
		opcode := values[1]
		if opcode < 0 || opcode > nativeBattleCommandMaxOpcode {
			return errors.New("native battle command row contains an unknown opcode")
		}
		if opcode == int64(expectedStartOpcode) {
			startCount++
		}
		if opcode == nativeBattleCommandTurnOpcode {
			turnCount++
			if turnCount > nativeBattleCommandMaxTurnPhases {
				return errors.New("native battle command exceeds the verifier turn limit")
			}
		}
		if lineCount == 0 {
			firstOpcode = int(opcode)
		}
		lineCount++
	}
	if lineCount == 0 {
		return errors.New("native battle command has no rows")
	}
	// Each native battle API records its own START opcode before any phases.
	// This also rejects a syntactically valid hand-written row as a clear report.
	if firstOpcode != expectedStartOpcode || startCount != 1 {
		return errors.New("native battle command does not begin with START")
	}
	return nil
}

func RentalPartnerCount(selected []TeamBattleResultPartner) int {
	count := 0
	for _, partner := range selected {
		if !partner.IsSelf {
			count++
		}
	}
	return count
}

func stageQuestAreaID(configuration json.RawMessage) (int, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, fmt.Errorf("decode local StageQuest configuration: %w", err)
	}
	if configured.StageQuest.AreaID <= 0 {
		return 0, fmt.Errorf("local StageQuest area is unavailable")
	}
	return configured.StageQuest.AreaID, nil
}

func stageQuestBattleForBoss(
	configuration json.RawMessage,
	areaID int,
	bossID int,
) (int, int, bool, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
			Stages []struct {
				StageID  int `json:"stageid"`
				RaidBoss []struct {
					BossGroup struct {
						Bosses []struct {
							BossID int `json:"bossid"`
							BPUse  int `json:"bp_use"`
						} `json:"bosses"`
					} `json:"boss_group"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, 0, false, fmt.Errorf("decode local StageQuest battle: %w", err)
	}
	if configured.StageQuest.AreaID != areaID {
		return 0, 0, false, nil
	}
	for _, stage := range configured.StageQuest.Stages {
		for _, raid := range stage.RaidBoss {
			for _, boss := range raid.BossGroup.Bosses {
				if boss.BossID == bossID && stage.StageID > 0 && boss.BPUse > 0 {
					return stage.StageID, boss.BPUse, true, nil
				}
			}
		}
	}
	return 0, 0, false, nil
}

func releasedStageQuestBattleForBoss(
	configuration json.RawMessage,
	areaID int,
	bossID int,
) (int, int, bool, error) {
	var configured struct {
		StageQuest struct {
			AreaID int `json:"areaid"`
			Stages []struct {
				StageID    int `json:"stageid"`
				IsReleased int `json:"is_release_now"`
				RaidBoss   []struct {
					BossGroup struct {
						Bosses []struct {
							BossID int `json:"bossid"`
							BPUse  int `json:"bp_use"`
						} `json:"bosses"`
					} `json:"boss_group"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(configuration, &configured); err != nil {
		return 0, 0, false, fmt.Errorf("decode released local StageQuest battle: %w", err)
	}
	if configured.StageQuest.AreaID != areaID {
		return 0, 0, false, nil
	}
	for _, stage := range configured.StageQuest.Stages {
		if stage.IsReleased != 1 {
			continue
		}
		for _, raid := range stage.RaidBoss {
			for _, boss := range raid.BossGroup.Bosses {
				if boss.BossID == bossID && stage.StageID > 0 && boss.BPUse > 0 {
					return stage.StageID, boss.BPUse, true, nil
				}
			}
		}
	}
	return 0, 0, false, nil
}

func markStageQuestClear(
	configuration json.RawMessage,
	areaID int,
	stageID int,
) (json.RawMessage, bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest state: %w", err)
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest area: %w", err)
	}
	var configuredAreaID int
	if json.Unmarshal(stageQuest["areaid"], &configuredAreaID) != nil || configuredAreaID != areaID {
		return nil, false, errors.New("local StageQuest clear area does not match")
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil {
		return nil, false, fmt.Errorf("decode local StageQuest stages: %w", err)
	}
	found := false
	firstClear := false
	for index := range stages {
		var currentStageID, clearDone int
		if json.Unmarshal(stages[index]["stageid"], &currentStageID) != nil || currentStageID != stageID {
			continue
		}
		if json.Unmarshal(stages[index]["is_clear_done"], &clearDone) != nil {
			return nil, false, errors.New("local StageQuest clear state is invalid")
		}
		found = true
		firstClear = clearDone == 0
		stages[index]["is_clear_done"] = json.RawMessage("1")

		var raids []map[string]json.RawMessage
		if err := json.Unmarshal(stages[index]["raid_boss"], &raids); err != nil {
			return nil, false, fmt.Errorf("decode local StageQuest raid rewards: %w", err)
		}
		for raidIndex := range raids {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raids[raidIndex]["boss_group"], &group); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest raid boss group: %w", err)
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group["bosses"], &bosses); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest raid bosses: %w", err)
			}
			for bossIndex := range bosses {
				bosses[bossIndex]["state"] = json.RawMessage("2")
			}
			encodedBosses, err := json.Marshal(bosses)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest raid bosses: %w", err)
			}
			group["bosses"] = encodedBosses
			encodedGroup, err := json.Marshal(group)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest raid boss group: %w", err)
			}
			raids[raidIndex]["boss_group"] = encodedGroup

			var rewards []map[string]json.RawMessage
			if err := json.Unmarshal(raids[raidIndex]["clear_reward"], &rewards); err != nil {
				return nil, false, fmt.Errorf("decode local StageQuest clear rewards: %w", err)
			}
			for rewardIndex := range rewards {
				rewards[rewardIndex]["is_already"] = json.RawMessage("1")
			}
			encodedRewards, err := json.Marshal(rewards)
			if err != nil {
				return nil, false, fmt.Errorf("encode local StageQuest clear rewards: %w", err)
			}
			raids[raidIndex]["clear_reward"] = encodedRewards
		}
		encodedRaids, err := json.Marshal(raids)
		if err != nil {
			return nil, false, fmt.Errorf("encode local StageQuest raids: %w", err)
		}
		stages[index]["raid_boss"] = encodedRaids
		break
	}
	if !found {
		return nil, false, errors.New("local StageQuest clear stage is unavailable")
	}
	if firstClear {
		var newClear []int
		if err := json.Unmarshal(top["new_clear_stage"], &newClear); err != nil {
			return nil, false, fmt.Errorf("decode local StageQuest new-clear state: %w", err)
		}
		alreadyQueued := false
		for _, queued := range newClear {
			if queued == stageID {
				alreadyQueued = true
				break
			}
		}
		if !alreadyQueued {
			newClear = append(newClear, stageID)
		}
		encodedNewClear, err := json.Marshal(newClear)
		if err != nil {
			return nil, false, fmt.Errorf("encode local StageQuest new-clear state: %w", err)
		}
		top["new_clear_stage"] = encodedNewClear
	}
	encodedStages, err := json.Marshal(stages)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest stages: %w", err)
	}
	stageQuest["stage_object"] = encodedStages
	encodedStageQuest, err := json.Marshal(stageQuest)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest area: %w", err)
	}
	top["stage_quest"] = encodedStageQuest
	updated, err := json.Marshal(top)
	if err != nil {
		return nil, false, fmt.Errorf("encode local StageQuest state: %w", err)
	}
	return updated, firstClear, nil
}

func stageQuestAllStagesCleared(configuration json.RawMessage) (bool, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(configuration, &top); err != nil {
		return false, fmt.Errorf("decode local StageQuest completion state: %w", err)
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return false, fmt.Errorf("decode local StageQuest completion area: %w", err)
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil || len(stages) == 0 {
		return false, errors.New("local StageQuest completion stages are invalid")
	}
	for _, stage := range stages {
		var clearDone int
		if err := json.Unmarshal(stage["is_clear_done"], &clearDone); err != nil {
			return false, errors.New("local StageQuest completion flag is invalid")
		}
		if clearDone == 0 {
			return false, nil
		}
	}
	return true, nil
}

func TeamBattleRewardProfileForContext(
	profiles []gamestate.TeamBattleRewardProfile,
	context TeamBattleContext,
) (gamestate.TeamBattleRewardProfile, bool) {
	for _, profile := range profiles {
		if profile.BossID == context.BossID &&
			profile.StageQuestAreaID == context.StageQuestAreaID &&
			profile.StageQuestStageID == context.StageQuestStageID &&
			profile.TowerID == context.TowerID &&
			profile.TowerFloor == context.TowerFloor {
			return profile, true
		}
	}
	return gamestate.TeamBattleRewardProfile{}, false
}

func SelectPartnerDeck(decks []DeckInfo, arthurType int8) (DeckInfo, bool) {
	for _, deck := range decks {
		if deck.ArthurType == arthurType && deck.IsActive != 0 {
			return deck, true
		}
	}
	for _, deck := range decks {
		if deck.ArthurType == arthurType {
			return deck, true
		}
	}
	return DeckInfo{}, false
}

func PartnerLeaderCard(deck DeckInfo, cards map[int64]CardInfo) (CardInfo, bool) {
	leaderIndex := int(deck.LeaderCardIndex)
	if leaderIndex >= 0 && leaderIndex < len(deck.CardUniqueIDs) {
		if card, exists := cards[deck.CardUniqueIDs[leaderIndex]]; exists {
			return card, true
		}
	}
	for _, uniqueID := range deck.CardUniqueIDs {
		if card, exists := cards[uniqueID]; exists {
			return card, true
		}
	}
	return CardInfo{}, false
}
