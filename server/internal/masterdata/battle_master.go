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
	"sort"

	"kairisei.local/server/internal/gamestate"
)

// Matches the generator's bounded JSON input/output budget. The complete CN
// encounter registry exceeds 8 MiB after restoring omitted difficulties.
const maxBattleRuntimeMasterBytes = 32 * 1024 * 1024

const (
	battleGeneratedGroupIDMin = 700000000
	battleGeneratedGroupIDMax = 800000000
	battleRetiredTrainingID   = 200000
	battleRetiredStageID      = 200001
)

var BattleMaterialGroupIcons = map[int]int{
	200000: 2,
	200001: 4,
	200002: 4,
	200003: 4,
	200004: 4,
	200005: 3,
	200006: 4,
	200007: 4,
}

func isBattleRetiredSeedGroupID(groupID int) bool {
	return groupID == battleRetiredTrainingID || groupID == battleRetiredStageID
}

type BattleRuntimeMaster struct {
	LocalProfile           json.RawMessage                      `json:"local_profile"`
	ExcludedBossIDs        []int                                `json:"excluded_existing_boss_ids"`
	DeduplicatedFamilies   []battleDeduplicatedFamily           `json:"deduplicated_families"`
	Groups                 []json.RawMessage                    `json:"groups"`
	OwnDeckVariants        []battleOwnDeckVariant               `json:"own_deck_variants,omitempty"`
	OptionalOwnDeckBossIDs []int                                `json:"-"`
	Replays                []gamestate.TeamBattleReplay         `json:"replays"`
	Rewards                []gamestate.TeamBattleRewardProfile  `json:"rewards"`
	Recommendations        []gamestate.TeamBattleRecommendation `json:"recommendations"`
	PastBossGroups         []json.RawMessage                    `json:"past_boss_groups"`
	ScheduleGroupIDs       []int                                `json:"schedule_group_ids"`
	TowerProfiles          []gamestate.TowerQuestProfile        `json:"tower_profiles"`
}

type battleDeduplicatedFamily struct {
	CanonicalFamilyID int `json:"canonical_family_id"`
	DuplicateFamilyID int `json:"duplicate_family_id"`

	BossAliases []struct {
		DuplicateBossID int `json:"duplicate_boss_id"`
		CanonicalBossID int `json:"canonical_boss_id"`
	} `json:"boss_aliases"`
}

type battleEntryRules struct {
	OnlyMyDeck int `json:"1"`
	Continue   int `json:"7"`
	StartRule  int `json:"24"`
}

func (rules battleEntryRules) valid() bool {
	// Original CN TeamBattleBossInfo: two booleans and TEAMBATTLE_START_RULE
	// NORMAL / SOLO_ONLY_MYDECK / SOLO_ONLY / MULTI_ONLY (0..3).
	return rules.OnlyMyDeck >= 0 && rules.OnlyMyDeck <= 1 &&
		rules.Continue >= 0 && rules.Continue <= 1 &&
		rules.StartRule >= 0 && rules.StartRule <= 3
}

type BattleGroupIdentity struct {
	StageType int    `json:"1"`
	GroupID   int    `json:"0"`
	IconIndex int    `json:"3"`
	Name      string `json:"4"`
	PictureID int    `json:"7"`
	Bosses    []struct {
		battleEntryRules
		BossID      int    `json:"0"`
		Difficulty  string `json:"4"`
		BPUse       int    `json:"5"`
		BPUseHalf   int    `json:"6"`
		State       int    `json:"10"`
		Description string `json:"11"`
		IsModel     int    `json:"14"`
		IsDailyRank int    `json:"17"`
		RewardCards []struct {
			CardID int `json:"0"`
			IsNew  int `json:"1"`
		} `json:"12"`
		RewardSpheres []struct {
			SphereID int `json:"0"`
			IsNew    int `json:"1"`
		} `json:"13"`
	} `json:"10"`
}

type BattlePastBossGroupIdentity struct {
	GroupID      int    `json:"0"`
	PastNumber   int    `json:"5"`
	PastName     string `json:"6"`
	Cards        []int  `json:"7"`
	EvolvedCards []int  `json:"8"`
	Bosses       []struct {
		battleEntryRules
		BossID      int `json:"0"`
		PictureID   int `json:"9"`
		IsDailyRank int `json:"17"`
	} `json:"13"`
}

type battleBossIdentity struct {
	battleEntryRules
	BossID      int `json:"0"`
	BPUse       int `json:"5"`
	IsDailyRank int `json:"17"`
	RewardCards []struct {
		CardID int `json:"0"`
		IsNew  int `json:"1"`
	} `json:"12"`
	RewardSpheres []struct {
		SphereID int `json:"0"`
		IsNew    int `json:"1"`
	} `json:"13"`
}

func LoadBattleRuntimeMaster(masterPath string) (BattleRuntimeMaster, error) {
	if masterPath == "" {
		return BattleRuntimeMaster{}, errors.New("CN battle runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return BattleRuntimeMaster{}, fmt.Errorf("resolve CN battle runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return BattleRuntimeMaster{}, fmt.Errorf("open CN battle runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return BattleRuntimeMaster{}, fmt.Errorf("stat CN battle runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxBattleRuntimeMasterBytes {
		return BattleRuntimeMaster{}, errors.New("CN battle runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxBattleRuntimeMasterBytes+1))
	var master BattleRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return BattleRuntimeMaster{}, fmt.Errorf("decode CN battle runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return BattleRuntimeMaster{}, err
	}
	if err := validateBattleRuntimeMaster(master); err != nil {
		return BattleRuntimeMaster{}, err
	}
	return withConfigurableOwnDeckEntries(master)
}

func validateBattleRuntimeMaster(master BattleRuntimeMaster) error {
	if len(master.LocalProfile) == 0 || bytes.Equal(master.LocalProfile, []byte("null")) ||
		len(master.Groups) == 0 || len(master.Replays) == 0 ||
		len(master.Rewards) == 0 || len(master.Replays) != len(master.Rewards) ||
		len(master.Recommendations) == 0 || len(master.PastBossGroups) == 0 ||
		len(master.ScheduleGroupIDs) == 0 || len(master.ScheduleGroupIDs) > 50 ||
		len(master.TowerProfiles) == 0 {
		return errors.New("CN battle runtime master is incomplete")
	}
	var localProfile struct {
		RepeatableRewardPolicy struct {
			FameBonus gamestate.TeamBattleFameBonusPolicy `json:"fame_bonus"`
			HostBonus gamestate.TeamBattleHostBonusPolicy `json:"host_bonus"`
		} `json:"repeatable_reward_policy"`
	}
	if json.Unmarshal(master.LocalProfile, &localProfile) != nil {
		return errors.New("CN battle schedule local profile is invalid")
	}
	fameBonus := localProfile.RepeatableRewardPolicy.FameBonus
	if fameBonus.ChanceMaximum != 100 ||
		fameBonus.FullFameThreshold != 100 || fameBonus.FullFameRewardCount != 2 ||
		fameBonus.BonusFameAdd != 0 ||
		!slices.Equal(fameBonus.EligibleRewardTypes, []int{6, 8, 13, 15, 19}) {
		return errors.New("CN battle fame-bonus local profile is invalid")
	}
	hostBonus := localProfile.RepeatableRewardPolicy.HostBonus
	if hostBonus.RewardCount != 1 || hostBonus.RewardKind != 5 ||
		!slices.Equal(hostBonus.EligibleRewardTypes, []int{6, 8, 13, 15, 19}) {
		return errors.New("CN battle host-bonus local profile is invalid")
	}
	groupIDs := make(map[int]struct{}, len(master.Groups))
	bossIDs := make(map[int]struct{}, len(master.Replays))
	bossEntryRules := make(map[int]battleEntryRules, len(master.Replays))
	bossDropCardIDs := make(map[int][]int, len(master.Replays))
	scorePolicies := make(map[int]bool)
	for _, profile := range master.Rewards {
		if err := gamestate.ValidateTeamBattleScorePolicy(profile.ScorePolicy); err != nil {
			return err
		}
		if profile.ScorePolicy != nil {
			scorePolicies[profile.BossID] = true
		}
	}
	for _, raw := range master.Groups {
		var group BattleGroupIdentity
		if json.Unmarshal(raw, &group) != nil || group.PictureID <= 0 ||
			group.PictureID >= battleGeneratedGroupIDMax-battleGeneratedGroupIDMin ||
			group.Name == "" || len(group.Bosses) == 0 {
			return errors.New("CN battle runtime master contains an invalid group")
		}
		familyID := group.Bosses[0].BossID / 100
		expectedIconIndex, isMaterialGroup := BattleMaterialGroupIcons[familyID]
		if !isMaterialGroup && group.Bosses[0].IsModel == 1 {
			expectedIconIndex = 5
		} else if !isMaterialGroup {
			expectedIconIndex = 7
			for _, boss := range group.Bosses {
				if boss.Difficulty == "特级" || boss.Difficulty == "超级" || boss.Difficulty == "超弩级" ||
					boss.Difficulty == "地狱级" || boss.Difficulty == "断绝级" {
					expectedIconIndex = 6
					break
				}
			}
		}
		if group.IconIndex != expectedIconIndex {
			return fmt.Errorf("CN battle runtime group %d has icon %d, want %d", group.GroupID, group.IconIndex, expectedIconIndex)
		}
		if familyID <= 0 || group.GroupID <= battleGeneratedGroupIDMin || group.GroupID >= battleGeneratedGroupIDMax {
			return fmt.Errorf("CN battle runtime group %d differs from Boss family %d", group.GroupID, familyID)
		}
		if _, duplicate := groupIDs[group.GroupID]; duplicate {
			return fmt.Errorf("duplicate CN battle runtime group %d", group.GroupID)
		}
		groupIDs[group.GroupID] = struct{}{}
		for _, boss := range group.Bosses {
			if boss.BossID/100 != familyID {
				return fmt.Errorf("CN battle runtime group %d mixes Boss families", group.GroupID)
			}
			validDifficulty := boss.Difficulty != ""
			rewardCardIDs := make([]int, len(boss.RewardCards))
			seenRewardCardIDs := make(map[int]struct{}, len(boss.RewardCards))
			validRewardCards := len(boss.RewardCards) > 0
			for index, rewardCard := range boss.RewardCards {
				if rewardCard.CardID <= 0 || rewardCard.IsNew != 0 {
					validRewardCards = false
					break
				}
				if _, duplicate := seenRewardCardIDs[rewardCard.CardID]; duplicate {
					validRewardCards = false
					break
				}
				seenRewardCardIDs[rewardCard.CardID] = struct{}{}
				rewardCardIDs[index] = rewardCard.CardID
			}
			validRewardCards = validRewardCards && (isMaterialGroup || len(rewardCardIDs) == 1)
			if boss.BossID <= 0 || !validDifficulty ||
				(group.StageType == 13) != scorePolicies[boss.BossID] ||
				boss.IsModel < 0 || boss.IsModel > 1 ||
				boss.IsDailyRank != 1 || !boss.battleEntryRules.valid() ||
				boss.BPUse <= 0 || boss.BPUseHalf != (boss.BPUse+1)/2 ||
				boss.State < 0 || boss.State > 2 || !validRewardCards ||
				len(boss.RewardSpheres) != 0 {
				return fmt.Errorf("invalid CN battle runtime boss %d", boss.BossID)
			}
			if _, duplicate := bossIDs[boss.BossID]; duplicate {
				return fmt.Errorf("duplicate CN battle runtime boss %d", boss.BossID)
			}
			bossIDs[boss.BossID] = struct{}{}
			bossEntryRules[boss.BossID] = boss.battleEntryRules
			bossDropCardIDs[boss.BossID] = rewardCardIDs

		}
	}
	towerBossFloors := make(map[int][2]int)
	seenTowerIDs := make(map[int]struct{}, len(master.TowerProfiles))
	for _, profile := range master.TowerProfiles {
		if profile.TowerID <= 0 || profile.Name == "" ||
			profile.LoseCountMax <= 0 || profile.AllClearText == "" ||
			len(profile.Ranks) == 0 || len(profile.Floors) == 0 {
			return fmt.Errorf("invalid CN tower profile %d", profile.TowerID)
		}
		if _, duplicate := seenTowerIDs[profile.TowerID]; duplicate {
			return fmt.Errorf("duplicate CN tower profile %d", profile.TowerID)
		}
		seenTowerIDs[profile.TowerID] = struct{}{}
		ranks := make(map[int]struct{}, len(profile.Ranks))
		for index, rank := range profile.Ranks {
			if rank.Rank != index+1 || rank.RankName == "" {
				return fmt.Errorf("invalid CN tower rank %d:%d", profile.TowerID, rank.Rank)
			}
			ranks[rank.Rank] = struct{}{}
		}
		for index, floor := range profile.Floors {
			if floor.Floor != index+1 || !floor.ValidEntryCost() {
				return fmt.Errorf("invalid CN tower floor %d:%d", profile.TowerID, floor.Floor)
			}
			if _, exists := ranks[floor.Rank]; !exists {
				return fmt.Errorf("CN tower floor %d:%d has unknown rank", profile.TowerID, floor.Floor)
			}
			for _, reward := range floor.ClearRewards {
				if !ValidPersistedRewardShape(reward) {
					return fmt.Errorf("invalid tower first-clear reward at %d:%d", profile.TowerID, floor.Floor)
				}
			}
			if floor.Story.TalkID != 0 {
				if floor.Story.TalkID < 0 || floor.Story.Title == "" {
					return fmt.Errorf("invalid tower story at %d:%d", profile.TowerID, floor.Floor)
				}
				continue
			}
			var boss battleBossIdentity
			if json.Unmarshal(floor.Boss, &boss) != nil || boss.BossID <= 0 || boss.BPUse != 0 ||
				boss.IsDailyRank != 0 || !boss.battleEntryRules.valid() ||
				len(boss.RewardSpheres) != 0 {
				return fmt.Errorf("invalid CN tower boss at %d:%d", profile.TowerID, floor.Floor)
			}
			if _, duplicate := bossIDs[boss.BossID]; duplicate {
				return fmt.Errorf("duplicate CN battle runtime boss %d", boss.BossID)
			}
			bossIDs[boss.BossID] = struct{}{}
			bossEntryRules[boss.BossID] = boss.battleEntryRules
			for _, card := range boss.RewardCards {
				if card.CardID <= 0 || card.IsNew != 0 {
					return fmt.Errorf("invalid tower drop preview at %d:%d", profile.TowerID, floor.Floor)
				}
				bossDropCardIDs[boss.BossID] = append(bossDropCardIDs[boss.BossID], card.CardID)
			}
			towerBossFloors[boss.BossID] = [2]int{profile.TowerID, floor.Floor}
		}
	}
	duplicateFamilyIDs := make(map[int]struct{}, len(master.DeduplicatedFamilies))
	duplicateBossIDs := make(map[int]struct{})
	previousDuplicateFamilyID := 0
	for _, family := range master.DeduplicatedFamilies {
		// An older discovered alias can point to an already published larger ID.
		// The checks below require a published canonical and an unpublished alias.
		if family.CanonicalFamilyID <= 0 || family.DuplicateFamilyID <= 0 ||
			family.DuplicateFamilyID <= previousDuplicateFamilyID || len(family.BossAliases) == 0 {
			return errors.New("CN battle runtime family deduplication identity is invalid")
		}
		previousDuplicateFamilyID = family.DuplicateFamilyID
		if _, duplicate := duplicateFamilyIDs[family.DuplicateFamilyID]; duplicate {
			return fmt.Errorf("duplicate CN battle deduplicated family %d", family.DuplicateFamilyID)
		}
		duplicateFamilyIDs[family.DuplicateFamilyID] = struct{}{}
		previousSuffix := -1
		for _, alias := range family.BossAliases {
			suffix := alias.DuplicateBossID % 100
			if alias.DuplicateBossID/100 != family.DuplicateFamilyID ||
				alias.CanonicalBossID/100 != family.CanonicalFamilyID ||
				alias.DuplicateBossID == alias.CanonicalBossID ||
				(family.DuplicateFamilyID != family.CanonicalFamilyID && alias.CanonicalBossID%100 != suffix) ||
				suffix <= previousSuffix {
				return fmt.Errorf("invalid CN battle family alias %d", alias.DuplicateBossID)
			}
			previousSuffix = suffix
			if _, exists := bossIDs[alias.CanonicalBossID]; !exists {
				return fmt.Errorf("CN battle family alias has unknown canonical Boss %d", alias.CanonicalBossID)
			}
			if _, exists := bossIDs[alias.DuplicateBossID]; exists {
				return fmt.Errorf("CN battle family alias still publishes duplicate Boss %d", alias.DuplicateBossID)
			}
			if _, duplicate := duplicateBossIDs[alias.DuplicateBossID]; duplicate {
				return fmt.Errorf("duplicate CN battle family alias %d", alias.DuplicateBossID)
			}
			duplicateBossIDs[alias.DuplicateBossID] = struct{}{}
		}
	}
	if len(bossIDs) != len(master.Replays) || len(bossIDs) != len(master.Rewards) {
		return errors.New("CN battle replay and reward coverage is inconsistent")
	}
	expectedScheduleGroupIDs := make([]int, 0, len(groupIDs))
	for _, raw := range master.Groups {
		var group BattleGroupIdentity
		if json.Unmarshal(raw, &group) != nil {
			return errors.New("decode CN battle schedule group")
		}
		familyID := group.Bosses[0].BossID / 100
		if _, isMaterialGroup := BattleMaterialGroupIcons[familyID]; isMaterialGroup {
			expectedScheduleGroupIDs = append(expectedScheduleGroupIDs, group.GroupID)
		}
	}
	sort.Ints(expectedScheduleGroupIDs)
	if !slices.Equal(master.ScheduleGroupIDs, expectedScheduleGroupIDs) {
		return errors.New("CN battle limited groups differ from the strengthening-material profile")
	}
	allBossIDs := make(map[int]struct{}, len(bossIDs)+len(master.ExcludedBossIDs))
	for bossID := range bossIDs {
		allBossIDs[bossID] = struct{}{}
	}
	for _, bossID := range master.ExcludedBossIDs {
		if bossID <= 0 {
			return errors.New("CN battle runtime master has an invalid excluded boss")
		}
		if _, duplicate := allBossIDs[bossID]; duplicate {
			return fmt.Errorf("CN battle runtime master repeats excluded boss %d", bossID)
		}
		allBossIDs[bossID] = struct{}{}
	}
	for duplicateBossID := range duplicateBossIDs {
		if _, collision := allBossIDs[duplicateBossID]; collision {
			return fmt.Errorf("CN battle family alias collides with published Boss %d", duplicateBossID)
		}
	}
	seenRecommendationNames := make(map[string]struct{}, len(master.Recommendations))
	for _, recommendation := range master.Recommendations {
		if recommendation.BossID <= 0 || recommendation.Name == "" ||
			recommendation.Attr < 1 || recommendation.Attr > 9 ||
			len(recommendation.RecommendIDs) != 4 {
			return fmt.Errorf("invalid CN battle recommendation %d", recommendation.BossID)
		}
		if _, exists := allBossIDs[recommendation.BossID]; !exists {
			return fmt.Errorf("CN battle recommendation has unknown boss %d", recommendation.BossID)
		}
		if _, duplicate := seenRecommendationNames[recommendation.Name]; duplicate {
			return fmt.Errorf("duplicate CN battle recommendation name %q", recommendation.Name)
		}
		seenRecommendationNames[recommendation.Name] = struct{}{}
		for _, recommendationID := range recommendation.RecommendIDs {
			if recommendationID < 0 {
				return fmt.Errorf("CN battle recommendation %d has a negative strategy", recommendation.BossID)
			}
		}
	}

	seenPastBossGroups := make(map[int]struct{}, len(master.PastBossGroups))
	pastBossPeriodNames := make(map[int]string)
	currentPastBossPeriod := 0
	for _, raw := range master.PastBossGroups {
		var group BattlePastBossGroupIdentity
		if json.Unmarshal(raw, &group) != nil || group.GroupID <= 0 ||
			group.PastNumber <= 0 || group.PastName == "" ||
			len(group.Cards) != 1 || group.Cards[0] <= 0 ||
			len(group.EvolvedCards) != 1 || group.EvolvedCards[0] < 0 ||
			len(group.Bosses) == 0 {
			return fmt.Errorf("invalid CN battle past-boss group %d", group.GroupID)
		}
		if group.PastNumber < currentPastBossPeriod ||
			group.PastNumber > currentPastBossPeriod+1 {
			return fmt.Errorf("non-contiguous CN battle past-boss period %d", group.PastNumber)
		}
		if group.PastNumber == currentPastBossPeriod+1 {
			currentPastBossPeriod = group.PastNumber
			pastBossPeriodNames[group.PastNumber] = group.PastName
		} else if pastBossPeriodNames[group.PastNumber] != group.PastName {
			return fmt.Errorf("inconsistent CN battle past-boss period %d", group.PastNumber)
		}
		if _, exists := groupIDs[group.GroupID]; !exists {
			return fmt.Errorf("CN battle past-boss group has unknown source %d", group.GroupID)
		}
		if _, duplicate := seenPastBossGroups[group.GroupID]; duplicate {
			return fmt.Errorf("duplicate CN battle past-boss group %d", group.GroupID)
		}
		seenPastBossGroups[group.GroupID] = struct{}{}
		for _, boss := range group.Bosses {
			if boss.PictureID <= 0 || boss.IsDailyRank != 0 || !boss.battleEntryRules.valid() {
				return fmt.Errorf("CN battle past-boss group %d has invalid picture or entry rules", group.GroupID)
			}
			if _, exists := bossIDs[boss.BossID]; !exists {
				return fmt.Errorf("CN battle past-boss group %d has unknown boss %d", group.GroupID, boss.BossID)
			}
			if boss.battleEntryRules != bossEntryRules[boss.BossID] {
				return fmt.Errorf("CN battle past-boss %d differs from its entry rules", boss.BossID)
			}
		}
	}

	replayIDs := make(map[int]struct{}, len(master.Replays))
	replayByID := make(map[int]gamestate.TeamBattleReplay, len(master.Replays))
	for _, replay := range master.Replays {
		if replay.BossID <= 0 || replay.EnemyPartyID <= 0 || replay.HoldMax <= 0 {
			return fmt.Errorf("invalid CN battle runtime replay %d", replay.BossID)
		}
		if err := ValidateTeamBattleReplaySegments(replay); err != nil {
			return fmt.Errorf("invalid CN battle runtime replay %d: %w", replay.BossID, err)
		}
		if _, duplicate := replayIDs[replay.BossID]; duplicate {
			return fmt.Errorf("duplicate CN battle runtime replay %d", replay.BossID)
		}
		replayIDs[replay.BossID] = struct{}{}
		replayByID[replay.BossID] = replay
	}
	rewardIDs := make(map[int]struct{}, len(master.Rewards))
	for _, profile := range master.Rewards {
		if profile.BossID <= 0 || profile.StageQuestAreaID != 0 || profile.StageQuestStageID != 0 ||
			len(profile.ResultRewards) == 0 {
			return fmt.Errorf("invalid CN battle runtime reward %d", profile.BossID)
		}
		if _, exists := bossIDs[profile.BossID]; !exists {
			return fmt.Errorf("CN battle runtime reward has unknown boss %d", profile.BossID)
		}
		if _, duplicate := rewardIDs[profile.BossID]; duplicate {
			return fmt.Errorf("duplicate CN battle runtime reward %d", profile.BossID)
		}
		rewardIDs[profile.BossID] = struct{}{}
		segments := replayByID[profile.BossID].Battles
		if len(segments) == 0 {
			segments = []gamestate.TeamBattleReplayBattle{{EnemyType: replayByID[profile.BossID].EnemyType}}
		}
		if len(profile.EnemyDrops) > 512 {
			return fmt.Errorf("CN battle runtime %d drop table is too large", profile.BossID)
		}
		for _, drop := range profile.EnemyDrops {
			if drop.BattleIndex < 0 || drop.BattleIndex >= len(segments) || drop.EnemyIndex < 0 || drop.EnemyIndex >= 4 ||
				segments[drop.BattleIndex].EnemyType == 4 || !ValidPersistedRewardShape(drop.Reward) ||
				(drop.ChancePerMillion != nil && (*drop.ChancePerMillion < 0 || *drop.ChancePerMillion > 1000000)) {
				return fmt.Errorf("CN battle runtime %d has an invalid enemy drop", profile.BossID)
			}
		}
		if err := ValidateBattleDropSlots(profile.EnemyDrops); err != nil {
			return fmt.Errorf("CN battle runtime %d: %w", profile.BossID, err)
		}
		if tower, exists := towerBossFloors[profile.BossID]; exists {
			if profile.TowerID != tower[0] || profile.TowerFloor != tower[1] {
				return fmt.Errorf("invalid CN tower reward %d", profile.BossID)
			}
			// Tower material drops and first-clear grants belong to their own
			// packaged policy and GM editor, not ordinary Boss card/medal rates.
			for _, rewards := range [][]gamestate.Reward{profile.ResultRewards, profile.FirstClearRewards} {
				for _, reward := range rewards {
					if !ValidPersistedRewardShape(reward) {
						return fmt.Errorf("invalid tower reward payload %d", profile.BossID)
					}
				}
			}
			continue
		} else if profile.TowerID != 0 || profile.TowerFloor != 0 {
			return fmt.Errorf("non-tower reward %d has tower context", profile.BossID)
		}
		expectedDropCardIDs := bossDropCardIDs[profile.BossID]
		_, isMaterialBoss := BattleMaterialGroupIcons[profile.BossID/100]
		dropCardRewards := 0
		materialRewardCardIDs := make([]int, 0, len(expectedDropCardIDs))
		materialExperienceRewards := 0
		materialGoldRewards := 0
		for _, reward := range profile.ResultRewards {
			if isMaterialBoss {
				switch reward.Type {
				case 0:
					materialExperienceRewards++
				case 4:
					materialGoldRewards++
				case 13:
					if reward.Num != 1 || reward.CardLevel != 0 || reward.CardFame != 0 ||
						reward.CardLove != 0 || len(reward.CardSkillLevels) != 0 {
						return fmt.Errorf("CN material battle runtime reward differs from Boss %d", profile.BossID)
					}
					materialRewardCardIDs = append(materialRewardCardIDs, reward.RewardTypeID)
				default:
					return fmt.Errorf("CN material battle runtime Boss %d has unrelated reward type %d", profile.BossID, reward.Type)
				}
				continue
			}
			if reward.Type == 6 {
				dropCardRewards++
				if len(expectedDropCardIDs) != 1 || reward.RewardTypeID != expectedDropCardIDs[0] || reward.Num != 1 ||
					reward.CardLevel != 1 || reward.CardFame != 1 || reward.CardLove != 0 ||
					!slices.Equal(reward.CardSkillLevels, []int16{1}) {
					return fmt.Errorf("CN battle runtime drop card differs from Boss %d", profile.BossID)
				}
			}
		}
		if isMaterialBoss {
			if materialExperienceRewards != 1 || materialGoldRewards != 1 ||
				!slices.Equal(materialRewardCardIDs, expectedDropCardIDs) {
				return fmt.Errorf("CN material battle runtime Boss %d has an invalid settlement", profile.BossID)
			}
		} else {
			if dropCardRewards != 1 {
				return fmt.Errorf("CN battle runtime Boss %d has %d drop-card rewards", profile.BossID, dropCardRewards)
			}

		}
		for _, rewards := range [][]gamestate.Reward{profile.ResultRewards, profile.FirstClearRewards} {
			for _, reward := range rewards {
				if !ValidPersistedRewardShape(reward) {
					return fmt.Errorf("invalid CN battle runtime reward payload %d", profile.BossID)
				}
			}
		}
	}
	for bossID := range bossIDs {
		if _, exists := replayIDs[bossID]; !exists {
			return fmt.Errorf("CN battle runtime boss %d has no replay", bossID)
		}
	}
	for _, profile := range master.Rewards {
		if err := gamestate.ValidateTeamBattleScorePolicy(profile.ScorePolicy); err != nil {
			return err
		}
	}
	return validateBattleOwnDeckVariants(master)
}

func battleCanonicalBossAliases(families []battleDeduplicatedFamily) map[int]int {
	result := make(map[int]int)
	for _, family := range families {
		for _, alias := range family.BossAliases {
			result[alias.DuplicateBossID] = alias.CanonicalBossID
		}
	}
	return result
}

func collectBattlePersistedStates(
	persistedGroups []json.RawMessage,
	canonicalByDuplicate map[int]int,
) (map[int]int, error) {
	persistedStates := make(map[int]int)
	for _, raw := range persistedGroups {
		var group BattleGroupIdentity
		if json.Unmarshal(raw, &group) != nil {
			return nil, errors.New("decode persisted CN TeamBattle group identity")
		}
		for _, boss := range group.Bosses {
			targetBossID := boss.BossID
			if canonicalBossID, duplicate := canonicalByDuplicate[targetBossID]; duplicate {
				targetBossID = canonicalBossID
			}
			if boss.State > persistedStates[targetBossID] {
				persistedStates[targetBossID] = boss.State
			}
		}
	}
	return persistedStates, nil
}

func ApplyBattleRuntimeMaster(state *gamestate.State, master BattleRuntimeMaster) error {
	state.DisabledTeamBattleBossIDs = make(map[int]bool, len(master.OptionalOwnDeckBossIDs))
	for _, id := range master.OptionalOwnDeckBossIDs {
		state.DisabledTeamBattleBossIDs[id] = true
	}
	state.TeamBattleOwnDeckSources = make(map[int]int, len(master.OwnDeckVariants))
	for _, variant := range master.OwnDeckVariants {
		state.TeamBattleOwnDeckSources[variant.BossID] = variant.SourceBossID
	}
	var err error
	master, err = ExpandBattleEntries(master)
	if err != nil {
		return err
	}
	var localProfile struct {
		RepeatableRewardPolicy struct {
			FameBonus gamestate.TeamBattleFameBonusPolicy `json:"fame_bonus"`
			HostBonus gamestate.TeamBattleHostBonusPolicy `json:"host_bonus"`
		} `json:"repeatable_reward_policy"`
	}
	if err := json.Unmarshal(master.LocalProfile, &localProfile); err != nil {
		return errors.New("decode CN battle local profile")
	}
	fameBonus := localProfile.RepeatableRewardPolicy.FameBonus
	fameBonus.EligibleRewardTypes = append([]int(nil), fameBonus.EligibleRewardTypes...)
	state.TeamBattleFameBonusPolicy = fameBonus
	hostBonus := localProfile.RepeatableRewardPolicy.HostBonus
	hostBonus.EligibleRewardTypes = append([]int(nil), hostBonus.EligibleRewardTypes...)
	state.TeamBattleHostBonusPolicy = hostBonus

	var solo map[string]json.RawMessage
	if json.Unmarshal(state.TeamBattleSolo, &solo) != nil {
		return errors.New("decode persisted CN TeamBattleSoloShow")
	}
	var persistedGroups []json.RawMessage
	if json.Unmarshal(solo["9"], &persistedGroups) != nil {
		return errors.New("decode persisted CN TeamBattle groups")
	}
	canonicalByDuplicate := battleCanonicalBossAliases(master.DeduplicatedFamilies)
	// Account handlers categorize activity/key/2D groups before saving. A
	// master refresh must collect their progress before replacing definitions.
	allPersistedGroups := append([]json.RawMessage(nil), persistedGroups...)
	for _, category := range BattleProgressCategories[1:] {
		if len(solo[category]) == 0 {
			continue
		}
		var groups []json.RawMessage
		if err := json.Unmarshal(solo[category], &groups); err != nil {
			return fmt.Errorf("decode persisted CN TeamBattle category %s: %w", category, err)
		}
		allPersistedGroups = append(allPersistedGroups, groups...)
	}
	persistedStates, err := collectBattlePersistedStates(
		allPersistedGroups, canonicalByDuplicate,
	)
	if err != nil {
		return err
	}
	masterGroupIDs := make(map[int]struct{}, len(master.Groups))
	for _, raw := range master.Groups {
		var group BattleGroupIdentity
		if err := json.Unmarshal(raw, &group); err != nil {
			return err
		}
		masterGroupIDs[group.GroupID] = struct{}{}
	}
	keptGroups := make([]json.RawMessage, 0, len(persistedGroups)+len(master.Groups))
	for _, raw := range persistedGroups {
		var group BattleGroupIdentity
		if json.Unmarshal(raw, &group) != nil {
			return errors.New("decode persisted CN TeamBattle group identity")
		}
		_, currentGenerated := masterGroupIDs[group.GroupID]
		managedGenerated := group.GroupID > battleGeneratedGroupIDMin &&
			group.GroupID < battleGeneratedGroupIDMax
		if !currentGenerated && !managedGenerated && !isBattleRetiredSeedGroupID(group.GroupID) {
			keptGroups = append(keptGroups, append(json.RawMessage(nil), raw...))
		}
	}
	for _, raw := range master.Groups {
		var group map[string]json.RawMessage
		if err := json.Unmarshal(raw, &group); err != nil {
			return err
		}
		var identity BattleGroupIdentity
		if err := json.Unmarshal(raw, &identity); err != nil {
			return err
		}
		var bosses []map[string]json.RawMessage
		if err := json.Unmarshal(group["10"], &bosses); err != nil {
			return err
		}
		for index, boss := range identity.Bosses {
			if persisted, exists := persistedStates[boss.BossID]; exists {
				bosses[index]["10"] = json.RawMessage(fmt.Sprintf("%d", persisted))
			}
		}
		encodedBosses, err := json.Marshal(bosses)
		if err != nil {
			return err
		}
		group["10"] = encodedBosses
		encodedGroup, err := json.Marshal(group)
		if err != nil {
			return err
		}
		keptGroups = append(keptGroups, encodedGroup)
	}
	encodedGroups, err := json.Marshal(keptGroups)
	if err != nil {
		return err
	}
	solo["9"] = encodedGroups
	state.TeamBattleSolo, err = json.Marshal(solo)
	if err != nil {
		return err
	}

	managedBossIDs := make(map[int]struct{}, len(master.Replays)+len(canonicalByDuplicate))
	for _, replay := range master.Replays {
		managedBossIDs[replay.BossID] = struct{}{}
	}
	for duplicateBossID := range canonicalByDuplicate {
		managedBossIDs[duplicateBossID] = struct{}{}
	}
	replays := make([]gamestate.TeamBattleReplay, 0, len(state.TeamBattleReplays)+len(master.Replays))
	for _, replay := range state.TeamBattleReplays {
		if _, generated := managedBossIDs[replay.BossID]; !generated {
			replays = append(replays, replay)
		}
	}
	replays = append(replays, master.Replays...)
	state.TeamBattleReplays = replays

	rewards := make([]gamestate.TeamBattleRewardProfile, 0, len(state.TeamBattleRewards)+len(master.Rewards))
	for _, profile := range state.TeamBattleRewards {
		if _, generated := managedBossIDs[profile.BossID]; !generated || profile.StageQuestAreaID != 0 {
			rewards = append(rewards, profile)
		}
	}
	rewards = append(rewards, master.Rewards...)
	state.TeamBattleRewards = rewards
	state.TeamBattleRecommendations = make([]gamestate.TeamBattleRecommendation, len(master.Recommendations))
	for index, recommendation := range master.Recommendations {
		recommendation.RecommendIDs = append([]int(nil), recommendation.RecommendIDs...)
		state.TeamBattleRecommendations[index] = recommendation
	}
	state.TeamBattlePastBossGroups, err = ProjectPastBossDropCatalog(
		master.PastBossGroups, state.TeamBattleRewards, state.CardActions.EvolutionTransitions,
	)
	if err != nil {
		return err
	}
	state.TeamBattleScheduleGroupIDs = append([]int(nil), master.ScheduleGroupIDs...)
	state.TowerQuestProfiles = make([]gamestate.TowerQuestProfile, len(master.TowerProfiles))
	for index, profile := range master.TowerProfiles {
		cloned := profile
		cloned.Ranks = append([]gamestate.TowerQuestRankProfile(nil), profile.Ranks...)
		cloned.Floors = make([]gamestate.TowerQuestFloorProfile, len(profile.Floors))
		for floorIndex, floor := range profile.Floors {
			floor.Boss = append(json.RawMessage(nil), floor.Boss...)
			floor.ClearRewards = append([]gamestate.Reward(nil), floor.ClearRewards...)
			cloned.Floors[floorIndex] = floor
		}
		state.TowerQuestProfiles[index] = cloned
	}
	NormalizeBattleAccountProgress(state)
	return nil
}

// NormalizeBattleAccountProgress keeps account-owned schedule subscriptions and
// tower records valid against the already installed public battle catalog.
// It never changes shared definitions or reuses another account's progress.
func NormalizeBattleAccountProgress(state *gamestate.State) {
	allowedScheduleGroups := make(map[int]struct{}, len(state.TeamBattleScheduleGroupIDs))
	for _, groupID := range state.TeamBattleScheduleGroupIDs {
		allowedScheduleGroups[groupID] = struct{}{}
	}
	filterSchedulePush := func(values []int) []int {
		seen := make(map[int]struct{}, len(values))
		kept := make([]int, 0, len(values))
		for _, groupID := range values {
			if _, allowed := allowedScheduleGroups[groupID]; !allowed {
				continue
			}
			if _, duplicate := seen[groupID]; duplicate {
				continue
			}
			seen[groupID] = struct{}{}
			kept = append(kept, groupID)
		}
		sort.Ints(kept)
		return kept
	}
	state.TeamBattleSchedule.SoloPushGroupIDs = filterSchedulePush(
		state.TeamBattleSchedule.SoloPushGroupIDs,
	)
	state.TeamBattleSchedule.MultiPushGroupIDs = filterSchedulePush(
		state.TeamBattleSchedule.MultiPushGroupIDs,
	)
	persistedTower := make(map[int]gamestate.TowerQuestProgress, len(state.TowerQuestProgress))
	for _, progress := range state.TowerQuestProgress {
		if _, duplicate := persistedTower[progress.TowerID]; !duplicate {
			persistedTower[progress.TowerID] = progress
		}
	}
	normalizedProgress := make([]gamestate.TowerQuestProgress, 0, len(state.TowerQuestProfiles)+len(state.TowerQuestProgress))
	activeTowerIDs := make(map[int]struct{}, len(state.TowerQuestProfiles))
	for _, profile := range state.TowerQuestProfiles {
		activeTowerIDs[profile.TowerID] = struct{}{}
		progress, exists := persistedTower[profile.TowerID]
		if !exists {
			progress = gamestate.TowerQuestProgress{TowerID: profile.TowerID, Floor: 1}
		}
		progress.TowerID = profile.TowerID
		if progress.Floor < 0 || progress.Floor > len(profile.Floors) {
			progress.Floor = 1
		}
		if progress.LastBattleFloor < 0 || progress.LastBattleFloor > len(profile.Floors) {
			progress.LastBattleFloor = 0
		}
		if progress.LoseCount < 0 || progress.LoseCount >= profile.LoseCountMax {
			progress.LoseCount = 0
		}
		if progress.LastResult != "" && progress.LastResult != "win" && progress.LastResult != "lose" {
			progress.LastResult = ""
			progress.LastRankUp = false
			progress.LastResultLoseCount = 0
		}
		if progress.LastResultLoseCount < 0 || progress.LastResultLoseCount > profile.LoseCountMax {
			progress.LastResultLoseCount = 0
		}
		if progress.LastResult != "win" {
			progress.LastRankUp = false
		}
		seenCleared := make(map[int]struct{}, len(progress.ClearedFloors))
		cleared := make([]int, 0, len(progress.ClearedFloors))
		for _, floor := range progress.ClearedFloors {
			if floor <= 0 || floor > len(profile.Floors) {
				continue
			}
			if _, duplicate := seenCleared[floor]; duplicate {
				continue
			}
			seenCleared[floor] = struct{}{}
			cleared = append(cleared, floor)
		}
		sort.Ints(cleared)
		progress.ClearedFloors = cleared
		normalizedProgress = append(normalizedProgress, progress)
	}
	// Retired definitions have no runtime entrance, but their private records
	// remain in the account instead of being deleted by a content refresh.
	for _, progress := range state.TowerQuestProgress {
		if _, found := activeTowerIDs[progress.TowerID]; !found {
			normalizedProgress = append(normalizedProgress, progress)
		}
	}
	state.TowerQuestProgress = normalizedProgress
}
