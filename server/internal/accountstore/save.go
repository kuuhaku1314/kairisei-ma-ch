package accountstore

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

const (
	maxSaveBytes                    = 4 * 1024 * 1024
	teamBattleDeckConfigVersion     = 3
	teamBattleRewardConfigVersion   = 6
	maxTeamBattleResultReceiptBytes = 1 << 20
	maxActiveBattleSegments         = 64
	ItemShopConfigVersion           = 2
	gachaConfigVersion              = 16
)

// cnSaveState is the editable, local character save. Transport routes and
// official-client metadata are deliberately excluded and supplied by the CN
// profile, so changing character data cannot change the local network boundary.
type saveState struct {
	TeamBattleScores               map[int]gamestate.TeamBattleScoreProgress `json:"team_battle_scores,omitempty"`
	TeamBattleSoloScores           map[int]gamestate.TeamBattleScoreProgress `json:"team_battle_solo_scores,omitempty"`
	LocalShop                      gamestate.LocalShopState                  `json:"local_shop,omitempty"`
	SchemaVersion                  int                                       `json:"schema_version"`
	User                           gamestate.User                            `json:"user"`
	PlayerProgressionConfigVersion int                                       `json:"player_progression_config_version"`
	CardProgressionConfigVersion   int                                       `json:"card_progression_config_version"`
	FeatureUnlockConfigVersion     int                                       `json:"feature_unlock_config_version"`
	Onboarding                     gamestate.OnboardingState                 `json:"onboarding"`
	ProfileConfigVersion           int                                       `json:"profile_config_version"`
	CurrencyConfigVersion          int                                       `json:"currency_config_version"`
	BattleLoadoutConfigVersion     int                                       `json:"battle_loadout_config_version"`
	BattleLoadoutCardUniqueIDs     []int64                                   `json:"battle_loadout_card_unique_ids,omitempty"`
	BattlePointConfigVersion       int                                       `json:"battle_point_config_version"`
	BattlePoint                    gamestate.BattlePointState                `json:"battle_point"`
	PVPConfigVersion               int                                       `json:"pvp_config_version"`
	PVP                            gamestate.PVPPlayerState                  `json:"pvp"`
	SphereConfigVersion            int                                       `json:"sphere_config_version"`
	Spheres                        []gamestate.Sphere                        `json:"spheres"`
	Cards                          []gamestate.Card                          `json:"cards"`
	ContainerCards                 []gamestate.Card                          `json:"container_cards,omitempty"`
	StackCards                     []gamestate.CardStack                     `json:"stack_cards"`
	Decks                          []gamestate.Deck                          `json:"decks"`
	Avatars                        []gamestate.Avatar                        `json:"avatars"`
	AvatarConfigVersion            int                                       `json:"avatar_config_version,omitempty"`
	AvatarParts                    []int                                     `json:"avatar_parts,omitempty"`
	Buddy                          gamestate.Buddy                           `json:"buddy"`
	BuddyConfigVersion             int                                       `json:"buddy_config_version,omitempty"`
	Buddies                        []gamestate.Buddy                         `json:"buddies,omitempty"`
	Profiles                       []string                                  `json:"profiles"`
	Friends                        gamestate.FriendCollectionState           `json:"friends"`
	Stamps                         gamestate.StampCollectionState            `json:"stamps"`
	Honors                         gamestate.HonorCollectionState            `json:"honors"`
	Story                          gamestate.StoryCatalogState               `json:"story"`
	ExploreConfigVersion           int                                       `json:"explore_config_version"`
	Explore                        gamestate.ExploreProgressState            `json:"explore"`
	Engagement                     gamestate.EngagementState                 `json:"engagement"`
	MissionDefinitions             map[int]gamestate.MissionDefinition       `json:"mission_definitions,omitempty"`
	Options                        gamestate.OptionState                     `json:"options"`
	LoginBonus                     gamestate.LoginBonusState                 `json:"login_bonus"`
	LocalAccountConfigVersion      int                                       `json:"local_account_config_version"`
	ItemShopConfigVersion          int                                       `json:"item_shop_config_version"`
	Items                          []gamestate.Item                          `json:"items"`
	ItemShopTabs                   []gamestate.ItemShopTab                   `json:"item_shop_tabs"`
	EventShopPurchases             []gamestate.EventShopPurchase             `json:"event_shop_purchases,omitempty"`
	TradeShopPurchases             []gamestate.TradeShopPurchase             `json:"trade_shop_purchases,omitempty"`
	GachaConfigVersion             int                                       `json:"gacha_config_version"`
	Gachas                         []gamestate.GachaProfile                  `json:"gachas"`
	GachaSelections                []gamestate.GachaSelection                `json:"gacha_selections,omitempty"`
	GachaDailyClaims               []gamestate.GachaDailyClaim               `json:"gacha_daily_claims,omitempty"`
	FriendPointInboxCursor         int64                                     `json:"friend_point_inbox_cursor,omitempty"`
	CardActions                    gamestate.CardActionState                 `json:"card_actions"`
	CardDevelopment                gamestate.CardDevelopmentState            `json:"card_development"`
	SupportDeckConfigVersion       int                                       `json:"support_deck_config_version,omitempty"`
	SupportDeck                    gamestate.SupportDeckState                `json:"support_deck"`
	StageQuestConfigVersion        int                                       `json:"stage_quest_config_version"`
	MainQuest                      json.RawMessage                           `json:"main_quest"`
	StageQuestAreas                []json.RawMessage                         `json:"stage_quest_areas,omitempty"`
	TeamBattleConfigVersion        int                                       `json:"team_battle_config_version"`
	TeamBattleSolo                 json.RawMessage                           `json:"team_battle_solo_show"`
	TeamBattleReplays              []gamestate.TeamBattleReplay              `json:"team_battle_replays"`
	TeamBattleRewards              []gamestate.TeamBattleRewardProfile       `json:"team_battle_rewards"`
	TeamBattleSchedule             gamestate.TeamBattleScheduleState         `json:"team_battle_schedule,omitempty"`
	TeamBattleResultReceipts       []gamestate.TeamBattleResultReceipt       `json:"team_battle_result_receipts,omitempty"`
	TeamBattleStartReceipts        []gamestate.TeamBattleStartReceipt        `json:"team_battle_start_receipts,omitempty"`
	TeamBattleContinueReceipts     []gamestate.TeamBattleContinueReceipt     `json:"team_battle_continue_receipts,omitempty"`
	TeamBattleSoloResultReceipts   []gamestate.TeamBattleSoloResultReceipt   `json:"team_battle_solo_result_receipts,omitempty"`
	ExploreResultReceipt           *gamestate.ExploreResultReceipt           `json:"explore_result_receipt,omitempty"`
	PVPResultReceipts              []gamestate.PVPResultReceipt              `json:"pvp_result_receipts,omitempty"`
	ActiveTeamBattle               *gamestate.TeamBattleActiveState          `json:"active_team_battle,omitempty"`
	TowerQuestConfigVersion        int                                       `json:"tower_quest_config_version,omitempty"`
	TowerQuestProgress             []gamestate.TowerQuestProgress            `json:"tower_quest_progress,omitempty"`
	Costume                        json.RawMessage                           `json:"costume"`
}

func LoadSaveState(savePath string) (gamestate.State, error) {
	if savePath == "" {
		return gamestate.State{}, errors.New("CN save path is required")
	}
	absolute, err := filepath.Abs(savePath)
	if err != nil {
		return gamestate.State{}, fmt.Errorf("resolve CN save path: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return gamestate.State{}, fmt.Errorf("open CN save: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return gamestate.State{}, fmt.Errorf("stat CN save: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxSaveBytes {
		return gamestate.State{}, errors.New("CN save must be a non-empty regular JSON file within the size limit")
	}
	content, err := io.ReadAll(io.LimitReader(file, maxSaveBytes+1))
	if err != nil {
		return gamestate.State{}, fmt.Errorf("read CN save: %w", err)
	}
	return DecodeSaveState(content)
}

func DecodeSaveState(content []byte) (gamestate.State, error) {
	if len(content) == 0 || len(content) > maxSaveBytes {
		return gamestate.State{}, errors.New("CN save JSON must be non-empty and within the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	var save saveState
	if err := decoder.Decode(&save); err != nil {
		return gamestate.State{}, fmt.Errorf("decode CN save: %w", err)
	}
	if err := masterdata.RequireJSONEOF(decoder); err != nil {
		return gamestate.State{}, err
	}
	if err := normalizeBossUserBuffSentinels(&save); err != nil {
		return gamestate.State{}, err
	}
	// BuddyConfigVersion establishes that the inventory was initialized. Older
	// local snapshots (including accounts interrupted during creation) encoded an
	// empty inventory as null or omitted because of a nil-slice clone and the
	// compact save tag. Normalize that one known representation before validation.
	if save.BuddyConfigVersion > 0 && save.Buddies == nil {
		save.Buddies = []gamestate.Buddy{}
	}
	if err := ValidateSave(save); err != nil {
		return gamestate.State{}, err
	}
	return releaseStateFromSave(save), nil
}

// Older local snapshots encoded an absent boss user-buff/key condition as an
// empty array. The CN managed client distinguishes that from null: any
// non-null array means that a key condition exists, even when it contains no
// usable ID. Normalize only the two known boss DTO paths while loading so old
// accounts retain their progress without turning every battle into an expired
// key dungeon. Current generators and content gates reject new empty arrays.
func normalizeBossUserBuffSentinels(save *saveState) error {
	var err error
	save.TeamBattleSolo, err = normalizeTeamBattleUserBuffSentinels(save.TeamBattleSolo)
	if err != nil {
		return fmt.Errorf("normalize CN TeamBattle user-buff sentinels: %w", err)
	}
	save.MainQuest, err = normalizeStageQuestUserBuffSentinels(save.MainQuest)
	if err != nil {
		return fmt.Errorf("normalize CN StageQuest user-buff sentinels: %w", err)
	}
	for index, area := range save.StageQuestAreas {
		save.StageQuestAreas[index], err = normalizeStageQuestUserBuffSentinels(area)
		if err != nil {
			return fmt.Errorf("normalize CN StageQuest area %d user-buff sentinels: %w", index, err)
		}
	}
	return nil
}

func normalizeTeamBattleUserBuffSentinels(content json.RawMessage) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil {
		return nil, err
	}
	changed := false
	for _, category := range []string{"9", "10", "11", "12"} {
		var groups []map[string]json.RawMessage
		if err := json.Unmarshal(top[category], &groups); err != nil {
			return nil, fmt.Errorf("decode category %s: %w", category, err)
		}
		categoryChanged := false
		for groupIndex := range groups {
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(groups[groupIndex]["10"], &bosses); err != nil {
				return nil, fmt.Errorf("decode category %s group %d bosses: %w", category, groupIndex, err)
			}
			bossesChanged := false
			for bossIndex := range bosses {
				normalized, fieldChanged, err := normalizeEmptyIntArraySentinel(bosses[bossIndex], "15")
				if err != nil {
					return nil, fmt.Errorf("category %s group %d boss %d: %w", category, groupIndex, bossIndex, err)
				}
				bosses[bossIndex] = normalized
				bossesChanged = bossesChanged || fieldChanged
			}
			if !bossesChanged {
				continue
			}
			encodedBosses, err := json.Marshal(bosses)
			if err != nil {
				return nil, err
			}
			groups[groupIndex]["10"] = encodedBosses
			categoryChanged = true
		}
		if !categoryChanged {
			continue
		}
		encodedGroups, err := json.Marshal(groups)
		if err != nil {
			return nil, err
		}
		top[category] = encodedGroups
		changed = true
	}
	if !changed {
		return content, nil
	}
	return json.Marshal(top)
}

func normalizeStageQuestUserBuffSentinels(content json.RawMessage) (json.RawMessage, error) {
	var top map[string]json.RawMessage
	if err := json.Unmarshal(content, &top); err != nil {
		return nil, err
	}
	var stageQuest map[string]json.RawMessage
	if err := json.Unmarshal(top["stage_quest"], &stageQuest); err != nil {
		return nil, err
	}
	var stages []map[string]json.RawMessage
	if err := json.Unmarshal(stageQuest["stage_object"], &stages); err != nil {
		return nil, err
	}
	changed := false
	for stageIndex := range stages {
		var raids []map[string]json.RawMessage
		if err := json.Unmarshal(stages[stageIndex]["raid_boss"], &raids); err != nil {
			return nil, fmt.Errorf("decode stage %d raid bosses: %w", stageIndex, err)
		}
		raidsChanged := false
		for raidIndex := range raids {
			var group map[string]json.RawMessage
			if err := json.Unmarshal(raids[raidIndex]["boss_group"], &group); err != nil {
				return nil, fmt.Errorf("decode stage %d raid %d group: %w", stageIndex, raidIndex, err)
			}
			var bosses []map[string]json.RawMessage
			if err := json.Unmarshal(group["bosses"], &bosses); err != nil {
				return nil, fmt.Errorf("decode stage %d raid %d bosses: %w", stageIndex, raidIndex, err)
			}
			bossesChanged := false
			for bossIndex := range bosses {
				normalized, fieldChanged, err := normalizeEmptyIntArraySentinel(bosses[bossIndex], "user_buff_id")
				if err != nil {
					return nil, fmt.Errorf("stage %d raid %d boss %d: %w", stageIndex, raidIndex, bossIndex, err)
				}
				bosses[bossIndex] = normalized
				bossesChanged = bossesChanged || fieldChanged
			}
			if !bossesChanged {
				continue
			}
			encodedBosses, err := json.Marshal(bosses)
			if err != nil {
				return nil, err
			}
			group["bosses"] = encodedBosses
			encodedGroup, err := json.Marshal(group)
			if err != nil {
				return nil, err
			}
			raids[raidIndex]["boss_group"] = encodedGroup
			raidsChanged = true
		}
		if !raidsChanged {
			continue
		}
		encodedRaids, err := json.Marshal(raids)
		if err != nil {
			return nil, err
		}
		stages[stageIndex]["raid_boss"] = encodedRaids
		changed = true
	}
	if !changed {
		return content, nil
	}
	encodedStages, err := json.Marshal(stages)
	if err != nil {
		return nil, err
	}
	stageQuest["stage_object"] = encodedStages
	encodedStageQuest, err := json.Marshal(stageQuest)
	if err != nil {
		return nil, err
	}
	top["stage_quest"] = encodedStageQuest
	return json.Marshal(top)
}

func normalizeEmptyIntArraySentinel(
	owner map[string]json.RawMessage,
	field string,
) (map[string]json.RawMessage, bool, error) {
	raw, exists := owner[field]
	if !exists || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return owner, false, nil
	}
	var values []int
	if err := json.Unmarshal(raw, &values); err != nil {
		return nil, false, fmt.Errorf("decode %s: %w", field, err)
	}
	if len(values) != 0 {
		return owner, false, nil
	}
	owner[field] = json.RawMessage("null")
	return owner, true, nil
}

func releaseStateFromSave(save saveState) gamestate.State {
	return gamestate.State{
		SchemaVersion:                  save.SchemaVersion,
		User:                           save.User,
		PlayerProgressionConfigVersion: save.PlayerProgressionConfigVersion,
		CardProgressionConfigVersion:   save.CardProgressionConfigVersion,
		FeatureUnlockConfigVersion:     save.FeatureUnlockConfigVersion,
		Onboarding:                     save.Onboarding,
		ProfileConfigVersion:           save.ProfileConfigVersion,
		CurrencyConfigVersion:          save.CurrencyConfigVersion,
		BattleLoadoutConfigVersion:     save.BattleLoadoutConfigVersion,
		BattleLoadoutCardUniqueIDs:     append([]int64(nil), save.BattleLoadoutCardUniqueIDs...),
		BattlePointConfigVersion:       save.BattlePointConfigVersion,
		BattlePoint:                    save.BattlePoint,
		PVPConfigVersion:               save.PVPConfigVersion,
		PVP:                            cloneReleasePVPState(save.PVP),
		SphereConfigVersion:            save.SphereConfigVersion,
		// Preserve an explicitly empty inventory as [] rather than nil. The CN
		// client and save validator distinguish initialized-empty from absent.
		Spheres:             append([]gamestate.Sphere{}, save.Spheres...),
		Cards:               save.Cards,
		ContainerCards:      save.ContainerCards,
		StackCards:          save.StackCards,
		Decks:               save.Decks,
		Avatars:             save.Avatars,
		AvatarConfigVersion: save.AvatarConfigVersion,
		AvatarParts:         append([]int(nil), save.AvatarParts...),
		Buddy:               save.Buddy,
		BuddyConfigVersion:  save.BuddyConfigVersion,
		// Preserve initialized-empty Buddy inventories across decode/clone for the
		// same reason as spheres above.
		Buddies:                      append([]gamestate.Buddy{}, save.Buddies...),
		Profiles:                     save.Profiles,
		Friends:                      save.Friends,
		Stamps:                       save.Stamps,
		Honors:                       save.Honors,
		Story:                        save.Story,
		ExploreConfigVersion:         save.ExploreConfigVersion,
		Explore:                      save.Explore,
		Engagement:                   save.Engagement,
		MissionDefinitions:           save.MissionDefinitions,
		Options:                      save.Options,
		LoginBonus:                   save.LoginBonus,
		LocalAccountConfigVersion:    save.LocalAccountConfigVersion,
		ItemShopConfigVersion:        save.ItemShopConfigVersion,
		Items:                        save.Items,
		ItemShopTabs:                 save.ItemShopTabs,
		EventShopPurchases:           append([]gamestate.EventShopPurchase(nil), save.EventShopPurchases...),
		TradeShopPurchases:           append([]gamestate.TradeShopPurchase(nil), save.TradeShopPurchases...),
		TeamBattleScores:             save.TeamBattleScores,
		TeamBattleSoloScores:         save.TeamBattleSoloScores,
		LocalShop:                    save.LocalShop,
		GachaConfigVersion:           save.GachaConfigVersion,
		Gachas:                       save.Gachas,
		GachaSelections:              cloneGachaSelections(save.GachaSelections),
		GachaDailyClaims:             append([]gamestate.GachaDailyClaim(nil), save.GachaDailyClaims...),
		FriendPointInboxCursor:       save.FriendPointInboxCursor,
		CardActions:                  save.CardActions,
		CardDevelopment:              cloneCardDevelopmentState(save.CardDevelopment),
		SupportDeckConfigVersion:     save.SupportDeckConfigVersion,
		SupportDeck:                  cloneSupportDeckState(save.SupportDeck),
		StageQuestConfigVersion:      save.StageQuestConfigVersion,
		MainQuest:                    save.MainQuest,
		StageQuestAreas:              cloneRawMessages(save.StageQuestAreas),
		TeamBattleConfigVersion:      save.TeamBattleConfigVersion,
		TeamBattleSolo:               save.TeamBattleSolo,
		TeamBattleReplays:            save.TeamBattleReplays,
		TeamBattleRewards:            save.TeamBattleRewards,
		TeamBattleSchedule:           cloneTeamBattleScheduleState(save.TeamBattleSchedule),
		TeamBattleResultReceipts:     cloneTeamBattleResultReceipts(save.TeamBattleResultReceipts),
		TeamBattleStartReceipts:      append([]gamestate.TeamBattleStartReceipt(nil), save.TeamBattleStartReceipts...),
		TeamBattleContinueReceipts:   append([]gamestate.TeamBattleContinueReceipt(nil), save.TeamBattleContinueReceipts...),
		TeamBattleSoloResultReceipts: cloneTeamBattleSoloResultReceipts(save.TeamBattleSoloResultReceipts),
		ExploreResultReceipt:         cloneExploreResultReceipt(save.ExploreResultReceipt),
		PVPResultReceipts:            clonePVPResultReceipts(save.PVPResultReceipts),
		ActiveTeamBattle:             cloneActiveTeamBattleState(save.ActiveTeamBattle),
		TowerQuestConfigVersion:      save.TowerQuestConfigVersion,
		TowerQuestProgress:           cloneTowerQuestProgressState(save.TowerQuestProgress),
		Costume:                      save.Costume,
	}
}

func ValidateSave(save saveState) error {
	if save.SchemaVersion != 1 {
		return errors.New("CN save schema_version must be 1")
	}
	if save.FriendPointInboxCursor < 0 {
		return errors.New("CN save friend-point inbox cursor is invalid")
	}
	if save.User.UserID <= 0 || save.User.ActiveArthurType < 1 || save.User.ActiveArthurType > 4 ||
		save.User.Level <= 0 || save.User.Experience < 0 || save.User.NowLevelExperience < 0 ||
		save.User.NextLevelExperience < 0 ||
		save.User.AP < 0 || save.User.APMax <= 0 || save.User.AP > save.User.APMax ||
		save.User.BP < 0 || save.User.BPMax <= 0 || save.User.BP > math.MaxInt32 ||
		save.User.CardMax <= 0 || save.User.CardContainerMax <= 0 || save.User.SphereMax <= 0 ||
		save.User.Gold < 0 || save.User.FriendPoint < 0 || save.User.Coin < 0 || save.User.CoinFree < 0 ||
		save.User.TutorialFlag < 0 ||
		save.User.TutorialFlag >= int64(1)<<gamestate.TutorialFlagCount {
		return errors.New("CN save user state is invalid")
	}
	if save.PlayerProgressionConfigVersion < 0 {
		return errors.New("CN save player progression state is invalid")
	}
	if save.CardProgressionConfigVersion < 0 {
		return errors.New("CN save card progression configuration is invalid")
	}
	if save.LoginBonus.ConfigVersion < 0 || save.LoginBonus.CycleDay < 0 || save.LoginBonus.BeginnerDay < 0 ||
		save.LoginBonus.TotalClaims < 0 {
		return errors.New("CN save login bonus state is invalid")
	}
	for index, job := range save.User.Jobs {
		if job.HP < 0 || job.Attack < 0 || job.Magic < 0 || job.Mind < 0 ||
			(index == 0 && job != (gamestate.JobParameter{})) {
			return errors.New("CN save player job state is invalid")
		}
	}
	if len(save.Cards) == 0 || len(save.Decks) == 0 {
		return errors.New("CN save requires at least one card and deck")
	}
	if save.CardDevelopment.Stive < 0 {
		return errors.New("CN save card development stive is invalid")
	}
	if training := save.CardDevelopment.Training; training != nil {
		if training.UniqueID <= 0 || training.BeginAtUnix <= 0 || training.Fame <= 0 {
			return errors.New("CN save card fame training is invalid")
		}
		found := false
		for _, card := range save.Cards {
			if card.UniqueID == training.UniqueID {
				found = card.IsLock == 1
				break
			}
		}
		if !found {
			return errors.New("CN save card fame training card is missing or unlocked")
		}
	}
	if save.SupportDeckConfigVersion < 0 ||
		(save.SupportDeckConfigVersion > 0 && len(save.SupportDeck.UnlockSlotNums) != 4) {
		return errors.New("CN save support-deck progression is invalid")
	}
	for _, count := range save.SupportDeck.UnlockSlotNums {
		if count < 0 || count > 10 {
			return errors.New("CN save support-deck unlocked slot count is invalid")
		}
	}
	collectedCardIDs := make(map[int]struct{}, len(save.SupportDeck.CardCollectionIDs))
	for _, cardID := range save.SupportDeck.CardCollectionIDs {
		if cardID <= 0 {
			return errors.New("CN save card collection contains an invalid card ID")
		}
		if _, duplicate := collectedCardIDs[cardID]; duplicate {
			return fmt.Errorf("CN save card collection repeats card ID %d", cardID)
		}
		collectedCardIDs[cardID] = struct{}{}
	}
	cardUniqueIDs := make(map[int64]struct{}, len(save.Cards)+len(save.ContainerCards))
	maxLoveIDs := make(map[int]bool, len(save.SupportDeck.CardCollectionLoveMaxIDs))
	for _, id := range save.SupportDeck.CardCollectionLoveMaxIDs {
		if _, acquired := collectedCardIDs[id]; !acquired || maxLoveIDs[id] {
			return fmt.Errorf("CN save card collection love maximum is invalid: %d", id)
		}
		maxLoveIDs[id] = true
	}
	for _, inventory := range [][]gamestate.Card{save.Cards, save.ContainerCards} {
		for _, card := range inventory {
			if card.UniqueID <= 0 || card.CardID <= 0 || card.IsLock < 0 || card.IsLock > 1 {
				return fmt.Errorf("CN save card %d is invalid", card.UniqueID)
			}
			if _, duplicate := cardUniqueIDs[card.UniqueID]; duplicate {
				return fmt.Errorf("CN save card unique ID %d is duplicated", card.UniqueID)
			}
			cardUniqueIDs[card.UniqueID] = struct{}{}
		}
	}
	if len(save.Avatars) != 4 {
		return errors.New("CN save requires one avatar for each of the four Arthur types")
	}
	for _, avatar := range save.Avatars {
		if avatar.CostumeID < 0 || len(avatar.AvatarPartIDs) != 7 {
			return errors.New("CN save avatar state is invalid")
		}
	}
	if save.AvatarConfigVersion < 0 || (save.AvatarConfigVersion > 0 && save.AvatarParts == nil) {
		return errors.New("CN save Avatar inventory configuration is invalid")
	}
	avatarParts := make(map[int]struct{}, len(save.AvatarParts))
	for _, partID := range save.AvatarParts {
		if partID <= 0 {
			return errors.New("CN save Avatar inventory contains an invalid part")
		}
		if _, duplicate := avatarParts[partID]; duplicate {
			return fmt.Errorf("CN save Avatar part %d is duplicated", partID)
		}
		avatarParts[partID] = struct{}{}
	}
	if save.Explore.APRecoverySeconds <= 0 {
		return errors.New("CN save requires a positive AP recovery interval")
	}
	if save.BattlePointConfigVersion < 0 ||
		(save.BattlePointConfigVersion > 0 &&
			(save.BattlePoint.RecoverySeconds <= 0 ||
				save.BattlePoint.NextRecoveryUnix < 0 ||
				(save.User.BP < save.User.BPMax && save.BattlePoint.NextRecoveryUnix == 0) ||
				(save.User.BP >= save.User.BPMax && save.BattlePoint.NextRecoveryUnix != 0))) {
		return errors.New("CN save battle point recovery configuration is invalid")
	}
	if save.PVPConfigVersion < 0 || save.PVP.Challenge < 0 || save.PVP.ChallengeDay < 0 || save.PVP.NextBattleID < 0 ||
		(save.PVPConfigVersion > 0 && (save.PVP.NextBattleID == 0 || save.PVP.DefenseDecks == nil || save.PVP.History == nil)) {
		return fmt.Errorf(
			"CN save PVP state is invalid (version=%d challenge=%d next_battle_id=%d defense_nil=%t history_nil=%t)",
			save.PVPConfigVersion, save.PVP.Challenge, save.PVP.NextBattleID,
			save.PVP.DefenseDecks == nil, save.PVP.History == nil,
		)
	}
	// Existing over-capacity inventory must remain loadable so players can sell it.
	if save.SphereConfigVersion < 0 || (save.SphereConfigVersion > 0 && save.Spheres == nil) {
		return errors.New("CN save sphere inventory is invalid")
	}
	sphereUniqueIDs := make(map[int64]struct{}, len(save.Spheres))
	for _, sphere := range save.Spheres {
		if sphere.UniqueID <= 0 || sphere.SphereID <= 0 || sphere.Level <= 0 ||
			sphere.Experience < 0 || sphere.NextLevelExperience < 0 || sphere.NowLevelExperience < 0 ||
			sphere.AddExperience <= 0 || sphere.BaseAddPrice <= 0 || sphere.IsLock < 0 || sphere.IsLock > 1 {
			return fmt.Errorf("CN save sphere %d is invalid", sphere.UniqueID)
		}
		if _, duplicate := sphereUniqueIDs[sphere.UniqueID]; duplicate {
			return fmt.Errorf("CN save sphere unique ID %d is duplicated", sphere.UniqueID)
		}
		sphereUniqueIDs[sphere.UniqueID] = struct{}{}
	}
	if save.BuddyConfigVersion < 0 ||
		(save.BuddyConfigVersion > 0 && save.Buddies == nil) {
		return fmt.Errorf(
			"CN save buddy inventory is invalid (version=%d nil=%t count=%d max=%d onboarding=%d)",
			save.BuddyConfigVersion, save.Buddies == nil, len(save.Buddies),
			save.User.BuddyMax, save.Onboarding.ConfigVersion,
		)
	}
	buddyUniqueIDs := make(map[int64]struct{}, len(save.Buddies))
	for _, buddy := range save.Buddies {
		if buddy.UniqueID <= 0 || buddy.BuddyID <= 0 || buddy.Level <= 0 || buddy.Experience < 0 ||
			buddy.NextLevelExperience < 0 || buddy.NowLevelExperience < 0 || buddy.AddExperience <= 0 ||
			buddy.BaseAddPrice <= 0 || buddy.IsLock < 0 || buddy.IsLock > 1 {
			return fmt.Errorf("CN save buddy %d is invalid", buddy.UniqueID)
		}
		if _, duplicate := buddyUniqueIDs[buddy.UniqueID]; duplicate {
			return fmt.Errorf("CN save buddy unique ID %d is duplicated", buddy.UniqueID)
		}
		buddyUniqueIDs[buddy.UniqueID] = struct{}{}
	}
	if save.ExploreConfigVersion < 0 || save.Explore.StageCursor < 0 || save.Explore.ActiveStageID < 0 {
		return errors.New("CN save Explore config version must not be negative")
	}
	if save.Options.GameEnableFlag < 0 || save.Options.PushEnableFlag < 0 {
		return errors.New("CN save option flags must not be negative")
	}
	if save.ItemShopConfigVersion < 0 {
		return errors.New("CN save item shop config version must not be negative")
	}
	if save.LocalAccountConfigVersion < 0 {
		return errors.New("CN save local account configuration is invalid")
	}
	if save.ItemShopConfigVersion >= ItemShopConfigVersion {
		if len(save.ItemShopTabs) != 5 || save.Items == nil {
			return errors.New("CN save item shop configuration is incomplete")
		}
		seenItems := make(map[int]struct{}, len(save.Items))
		for _, item := range save.Items {
			if item.ItemID <= 0 || item.Num < 0 || item.LimitTime < 0 {
				return errors.New("CN save item state is invalid")
			}
			if _, exists := seenItems[item.ItemID]; exists {
				return fmt.Errorf("CN save item ID %d is duplicated", item.ItemID)
			}
			seenItems[item.ItemID] = struct{}{}
		}
		seenLineups := make(map[int]struct{})
		for tabIndex, tab := range save.ItemShopTabs {
			if tab.TabType != tabIndex || tab.Lineup == nil {
				return errors.New("CN save item shop tab shape is invalid")
			}
			for _, lineup := range tab.Lineup {
				if lineup.LineupID <= 0 || lineup.PictID <= 0 || lineup.LineupName == "" ||
					(lineup.PayType != 1 && lineup.PayType != 3) || lineup.PayTypeID != 0 || lineup.Price <= 0 ||
					lineup.StockNum < 0 || lineup.StockRemain < 0 || lineup.StockType != 0 ||
					lineup.AppearEnd != -1 || lineup.BuyNumMax <= 0 || len(lineup.Interiors) == 0 {
					return errors.New("CN save item shop lineup is invalid")
				}
				if lineup.Hidden && !validHiddenItemShopLineup(lineup) {
					return errors.New("CN save hidden item shop lineup is invalid")
				}
				if _, exists := seenLineups[lineup.LineupID]; exists {
					return fmt.Errorf("CN save item shop lineup ID %d is duplicated", lineup.LineupID)
				}
				seenLineups[lineup.LineupID] = struct{}{}
				for _, interior := range lineup.Interiors {
					if interior.BuyType != 1 || interior.BuyTypeID <= 0 || interior.Num <= 0 {
						return errors.New("CN save item shop interior is invalid")
					}
				}
			}
		}
	}
	if save.GachaConfigVersion < 0 {
		return errors.New("CN save gacha config version must not be negative")
	}
	if save.GachaConfigVersion >= gachaConfigVersion {
		if len(save.Gachas) == 0 {
			return errors.New("CN save gacha configuration is incomplete")
		}
		seenGachas := make(map[int]gamestate.GachaProfile, len(save.Gachas))
		for _, gacha := range save.Gachas {
			if gacha.GachaID <= 0 || gacha.Name == "" || gacha.BuyMessage == "" ||
				gacha.CategoryNum <= 0 || gacha.CategoryPictID <= 0 || gacha.OrderNum < 0 ||
				gacha.GroupID <= 0 ||
				(gacha.PayType != 2 && gacha.PayType != 3 && gacha.PayType != 4 && gacha.PayType != 6) ||
				(gacha.PayType == 4 && gacha.PayTypeID <= 0) ||
				(gacha.PayType != 4 && gacha.PayTypeID != 0) ||
				gacha.Price <= 0 || gacha.CardNum <= 0 ||
				gacha.CardNumMax < gacha.CardNum || gacha.EndTime <= 0 ||
				gacha.UserSelectMax < 0 || gacha.UserSelectMax > len(gacha.CardIDs) ||
				gacha.PlayCount < 0 || (len(gacha.CardIDs) == 0 && len(gacha.RewardPool) == 0) ||
				len(gacha.CardWeights) != len(gacha.CardIDs) ||
				!validGachaResultPolicy(gacha) {
				return fmt.Errorf("CN save gacha %d profile is invalid (arthur_type=%d)", gacha.GachaID, gacha.ArthurType)
			}
			if err := gamestate.ValidateGachaRules(gacha); err != nil {
				return err
			}
			if !validGachaBannerKey(gacha.BannerKey) ||
				!validGachaPublicationKey(gacha.PublicationKey) ||
				(gacha.PublicationKey != "" && gacha.PublicationKey != "operator" && gacha.BannerKey == "") {
				return errors.New("CN save gacha banner policy is invalid")
			}
			if gacha.DailyFirstFree {
				if gacha.PayType != 2 || gacha.GachaType != 1 {
					return errors.New("CN save daily-first-free gacha policy is invalid")
				}
			}
			if _, exists := seenGachas[gacha.GachaID]; exists {
				return fmt.Errorf("CN save gacha ID %d is duplicated", gacha.GachaID)
			}
			seenGachas[gacha.GachaID] = gacha
			seenCards := make(map[int]struct{}, len(gacha.CardIDs))
			for index, cardID := range gacha.CardIDs {
				if cardID <= 0 {
					return errors.New("CN save gacha card ID is invalid")
				}
				if gacha.CardWeights[index] <= 0 {
					return errors.New("CN save gacha card weight is invalid")
				}
				if _, exists := seenCards[cardID]; exists {
					return fmt.Errorf("CN save gacha %d repeats card ID %d", gacha.GachaID, cardID)
				}
				seenCards[cardID] = struct{}{}
			}
		}
		seenSelections := make(map[int]struct{}, len(save.GachaSelections))
		for _, selection := range save.GachaSelections {
			gacha, exists := seenGachas[selection.GachaID]
			if !exists && selection.GachaID < gamestate.OperatorGachaFirstID {
				return fmt.Errorf("CN save gacha selection %d has no profile", selection.GachaID)
			}
			if _, duplicate := seenSelections[selection.GachaID]; duplicate {
				return fmt.Errorf("CN save gacha selection %d is duplicated", selection.GachaID)
			}
			if exists {
				if err := validateGachaSelection(gacha, selection.Rewards); err != nil {
					return err
				}
			}
			seenSelections[selection.GachaID] = struct{}{}
		}
		seenDailyClaims := make(map[int]struct{}, len(save.GachaDailyClaims))
		for _, claim := range save.GachaDailyClaims {
			_, exists := seenGachas[claim.GachaID]
			if (!exists && claim.GachaID < gamestate.OperatorGachaFirstID) || claim.PlayCount < 0 || claim.PlayCount > math.MaxInt32 ||
				!masterdata.LoginBonusDayPattern.MatchString(claim.Day) {
				return errors.New("CN save gacha daily claim is invalid")
			}
			if _, duplicate := seenDailyClaims[claim.GachaID]; duplicate {
				return fmt.Errorf("CN save gacha daily claim %d is duplicated", claim.GachaID)
			}
			seenDailyClaims[claim.GachaID] = struct{}{}
		}
	}
	if save.FeatureUnlockConfigVersion < 0 {
		return errors.New("CN save feature unlock config version must not be negative")
	}
	if save.Onboarding.ConfigVersion < 0 || save.Onboarding.ConfigVersion > masterdata.OnboardingConfigVersion ||
		save.Onboarding.Step < 0 ||
		(save.Onboarding.ConfigVersion == 0 && (save.Onboarding.Step != 0 ||
			save.Onboarding.CurrentAnnounced || save.Onboarding.PendingClearQuestID != 0)) ||
		(save.Onboarding.ConfigVersion == masterdata.LegacyOnboardingConfigVersion && save.Onboarding.Step > 7) ||
		(save.Onboarding.ConfigVersion == masterdata.OnboardingConfigVersion &&
			save.Onboarding.Step > masterdata.OnboardingStepCount) ||
		(save.Onboarding.ConfigVersion > 0 && save.Onboarding.PendingClearQuestID != 0 &&
			!isOnboardingQuestID(save.Onboarding.PendingClearQuestID)) {
		return errors.New("CN save onboarding state is invalid")
	}
	seenEventShopPurchases := make(map[int]struct{}, len(save.EventShopPurchases))
	for _, purchase := range save.EventShopPurchases {
		if purchase.LineupID <= 0 || purchase.Count <= 0 {
			return errors.New("CN save event shop purchase is invalid")
		}
		if _, duplicate := seenEventShopPurchases[purchase.LineupID]; duplicate {
			return fmt.Errorf("CN save event shop purchase %d is duplicated", purchase.LineupID)
		}
		seenEventShopPurchases[purchase.LineupID] = struct{}{}
	}
	seenTradeShopPurchases := make(map[int]struct{}, len(save.TradeShopPurchases))
	for _, purchase := range save.TradeShopPurchases {
		if purchase.LineupID <= 0 || purchase.Count <= 0 {
			return errors.New("CN save trade shop purchase is invalid")
		}
		if _, duplicate := seenTradeShopPurchases[purchase.LineupID]; duplicate {
			return fmt.Errorf("CN save trade shop purchase %d is duplicated", purchase.LineupID)
		}
		seenTradeShopPurchases[purchase.LineupID] = struct{}{}
	}
	seenPopupReadIDs := make(map[int]struct{}, len(save.Engagement.PopupReadIDs))
	for _, popupID := range save.Engagement.PopupReadIDs {
		if popupID <= 0 {
			return errors.New("CN save popup read state contains an invalid ID")
		}
		if _, duplicate := seenPopupReadIDs[popupID]; duplicate {
			return fmt.Errorf("CN save popup read state repeats ID %d", popupID)
		}
		seenPopupReadIDs[popupID] = struct{}{}
	}
	if save.ProfileConfigVersion < 0 {
		return errors.New("CN save profile config version must not be negative")
	}
	if save.BattleLoadoutConfigVersion < 0 {
		return errors.New("CN save battle loadout config version must not be negative")
	}
	seenBattleLoadoutCardUniqueIDs := make(map[int64]struct{}, len(save.BattleLoadoutCardUniqueIDs))
	for _, uniqueID := range save.BattleLoadoutCardUniqueIDs {
		if save.BattleLoadoutConfigVersion == 0 || uniqueID <= 0 {
			return errors.New("CN save battle loadout migration metadata is invalid")
		}
		if _, exists := seenBattleLoadoutCardUniqueIDs[uniqueID]; exists {
			return fmt.Errorf("CN save battle loadout card unique ID %d is duplicated", uniqueID)
		}
		seenBattleLoadoutCardUniqueIDs[uniqueID] = struct{}{}
	}
	seenFeatureIDs := make(map[uint]struct{}, len(save.User.UnlockedFeatureIDs))
	for _, featureID := range save.User.UnlockedFeatureIDs {
		if featureID >= 63 {
			return fmt.Errorf("CN save unlocked feature id %d exceeds the signed protocol flag boundary", featureID)
		}
		if _, exists := seenFeatureIDs[featureID]; exists {
			return fmt.Errorf("CN save unlocked feature id %d is duplicated", featureID)
		}
		seenFeatureIDs[featureID] = struct{}{}
	}
	if save.StageQuestConfigVersion < 0 {
		return errors.New("CN save stage quest config version must not be negative")
	}
	if save.StageQuestConfigVersion > 0 {
		if err := masterdata.ValidateStageQuestConfiguration(save.MainQuest); err != nil {
			return err
		}
		seenStageQuestAreas := make(map[int]struct{}, len(save.StageQuestAreas))
		for _, area := range save.StageQuestAreas {
			if err := masterdata.ValidateStageQuestConfiguration(area); err != nil {
				return err
			}
			areaID, err := masterdata.StageQuestAreaID(area)
			if err != nil {
				return err
			}
			if _, duplicate := seenStageQuestAreas[areaID]; duplicate {
				return fmt.Errorf("duplicate CN stage quest area ID %d", areaID)
			}
			seenStageQuestAreas[areaID] = struct{}{}
		}
		if len(seenStageQuestAreas) > 0 {
			defaultAreaID, err := masterdata.StageQuestAreaID(save.MainQuest)
			if err != nil {
				return err
			}
			if _, exists := seenStageQuestAreas[defaultAreaID]; !exists {
				return errors.New("CN save default stage quest is absent from stage_quest_areas")
			}
		}
	}
	if save.TeamBattleConfigVersion < 0 {
		return errors.New("CN save team battle config version must not be negative")
	}
	if save.TeamBattleConfigVersion > 0 &&
		(len(save.TeamBattleSolo) == 0 || len(save.TeamBattleReplays) == 0) {
		return errors.New("CN save team battle configuration is incomplete")
	}
	if save.TeamBattleConfigVersion >= teamBattleRewardConfigVersion {
		if len(save.TeamBattleRewards) == 0 {
			return errors.New("CN save team battle reward configuration is incomplete")
		}
		seenProfiles := make(map[[3]int]struct{}, len(save.TeamBattleRewards))
		for _, profile := range save.TeamBattleRewards {
			key := [3]int{profile.BossID, profile.StageQuestAreaID, profile.StageQuestStageID}
			if profile.BossID <= 0 || profile.StageQuestAreaID < 0 || profile.StageQuestStageID < 0 ||
				(profile.StageQuestAreaID == 0) != (profile.StageQuestStageID == 0) ||
				len(profile.ResultRewards) == 0 {
				return errors.New("CN save team battle reward profile is invalid")
			}
			if _, duplicate := seenProfiles[key]; duplicate {
				return errors.New("CN save team battle reward profiles must be unique")
			}
			seenProfiles[key] = struct{}{}
			for _, rewards := range [][]gamestate.Reward{profile.ResultRewards, profile.FirstClearRewards} {
				for _, reward := range rewards {
					if !masterdata.ValidPersistedRewardShape(reward) {
						return errors.New("CN save team battle reward is invalid")
					}
				}
			}
		}
	}
	for mode, groupIDs := range map[string][]int{
		"solo":  save.TeamBattleSchedule.SoloPushGroupIDs,
		"multi": save.TeamBattleSchedule.MultiPushGroupIDs,
	} {
		if len(groupIDs) > 50 {
			return fmt.Errorf("CN save %s team battle schedule exceeds the client limit", mode)
		}
		seenGroupIDs := make(map[int]struct{}, len(groupIDs))
		for _, groupID := range groupIDs {
			if groupID <= 0 {
				return fmt.Errorf("CN save %s team battle schedule has an invalid group", mode)
			}
			if _, duplicate := seenGroupIDs[groupID]; duplicate {
				return fmt.Errorf("CN save %s team battle schedule repeats a group", mode)
			}
			seenGroupIDs[groupID] = struct{}{}
		}
	}
	if len(save.TeamBattleResultReceipts) > 32 {
		return errors.New("CN save has too many team battle result receipts")
	}
	if err := gamestate.ValidateTeamBattleContinueReceipts(save.TeamBattleContinueReceipts); err != nil {
		return err
	}
	if len(save.TeamBattleStartReceipts) > 512 {
		return errors.New("CN save has too many multiplayer start receipts")
	}
	startRooms := make(map[int64]struct{})
	for _, receipt := range save.TeamBattleStartReceipts {
		if receipt.RoomID <= 0 || receipt.BossID <= 0 || receipt.BPUse < 0 || receipt.MedalItemID < 0 || receipt.MedalUse < 0 ||
			(receipt.MedalItemID == 0) != (receipt.MedalUse == 0) || (receipt.MedalUse > 0 && receipt.BPUse != 0) {
			return errors.New("CN save multiplayer start receipt is invalid")
		}
		if _, duplicate := startRooms[receipt.RoomID]; duplicate {
			return errors.New("CN save multiplayer start receipt is duplicated")
		}
		startRooms[receipt.RoomID] = struct{}{}
	}
	seenReceiptRooms := make(map[int64]struct{}, len(save.TeamBattleResultReceipts))
	for _, receipt := range save.TeamBattleResultReceipts {
		if receipt.RoomID <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Snapshot) > maxTeamBattleResultReceiptBytes || (len(receipt.Snapshot) != 0 && !json.Valid(receipt.Snapshot)) {
			return errors.New("CN save team battle result receipt is invalid")
		}
		if _, duplicate := seenReceiptRooms[receipt.RoomID]; duplicate {
			return errors.New("CN save team battle result receipt room is duplicated")
		}
		seenReceiptRooms[receipt.RoomID] = struct{}{}
	}
	if len(save.TeamBattleSoloResultReceipts) > 32 {
		return errors.New("CN save has too many solo team battle result receipts")
	}
	seenSoloReceiptDigests := make(map[string]struct{}, len(save.TeamBattleSoloResultReceipts))
	for _, receipt := range save.TeamBattleSoloResultReceipts {
		if !isLowerSHA256(receipt.RequestSHA256) || receipt.BossID <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Snapshot) > maxTeamBattleResultReceiptBytes ||
			(len(receipt.Snapshot) != 0 && !json.Valid(receipt.Snapshot)) {
			return errors.New("CN save solo team battle result receipt is invalid")
		}
		if _, duplicate := seenSoloReceiptDigests[receipt.RequestSHA256]; duplicate {
			return errors.New("CN save solo team battle result receipt digest is duplicated")
		}
		seenSoloReceiptDigests[receipt.RequestSHA256] = struct{}{}
	}
	if receipt := save.ExploreResultReceipt; receipt != nil {
		if receipt.StartedAtUnix <= 0 || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || len(receipt.Response) > maxTeamBattleResultReceiptBytes ||
			!json.Valid(receipt.Response) {
			return errors.New("CN save Explore result receipt is invalid")
		}
		var method map[string]json.RawMessage
		if err := json.Unmarshal(receipt.Response, &method); err != nil || method["user"] == nil ||
			method["result_rewards"] == nil || method["deck_cards"] == nil {
			return errors.New("CN save Explore result receipt response is incomplete")
		}
	}
	if len(save.PVPResultReceipts) > 32 {
		return errors.New("CN save has too many PVP result receipts")
	}
	seenPVPReceiptBattles := make(map[int]struct{}, len(save.PVPResultReceipts))
	for _, receipt := range save.PVPResultReceipts {
		if receipt.BattleID <= 0 || !isLowerSHA256(receipt.RequestSHA256) || receipt.ClaimedAtUnix <= 0 ||
			len(receipt.Response) == 0 || len(receipt.Response) > maxTeamBattleResultReceiptBytes ||
			!json.Valid(receipt.Response) {
			return errors.New("CN save PVP result receipt is invalid")
		}
		var method map[string]json.RawMessage
		if err := json.Unmarshal(receipt.Response, &method); err != nil || method["result"] == nil ||
			method["bf_pvp_point"] == nil || method["af_pvp_point"] == nil || method["challenge"] == nil {
			return errors.New("CN save PVP result receipt response is incomplete")
		}
		if _, duplicate := seenPVPReceiptBattles[receipt.BattleID]; duplicate {
			return errors.New("CN save PVP result receipt battle is duplicated")
		}
		seenPVPReceiptBattles[receipt.BattleID] = struct{}{}
	}
	if err := validateActiveTeamBattle(save.ActiveTeamBattle); err != nil {
		return err
	}
	if save.TowerQuestConfigVersion < 0 ||
		(save.TowerQuestConfigVersion > 0 && len(save.TowerQuestProgress) == 0) {
		return errors.New("CN save tower quest configuration is invalid")
	}
	seenTowerIDs := make(map[int]struct{}, len(save.TowerQuestProgress))
	for _, progress := range save.TowerQuestProgress {
		if progress.TowerID <= 0 || progress.Floor < 0 || progress.LastBattleFloor < 0 ||
			progress.LoseCount < 0 || progress.LastResultLoseCount < 0 ||
			(progress.LastResult != "" && progress.LastResult != "win" && progress.LastResult != "lose") ||
			(progress.LastRankUp && progress.LastResult != "win") {
			return errors.New("CN save tower quest progress is invalid")
		}
		if _, duplicate := seenTowerIDs[progress.TowerID]; duplicate {
			return errors.New("CN save tower quest ID is duplicated")
		}
		seenTowerIDs[progress.TowerID] = struct{}{}
		seenFloors := make(map[int]struct{}, len(progress.ClearedFloors))
		for _, floor := range progress.ClearedFloors {
			if floor <= 0 {
				return errors.New("CN save tower cleared floor is invalid")
			}
			if _, duplicate := seenFloors[floor]; duplicate {
				return errors.New("CN save tower cleared floor is duplicated")
			}
			seenFloors[floor] = struct{}{}
		}
	}
	// CardMgr.GetChangeDeckList clears is_active when an edited deck becomes
	// incomplete. Saving that draft is valid; entering battle checks the actual
	// chosen decks (and any own-deck AI replacements) separately.
	for _, replay := range save.TeamBattleReplays {
		if replay.BossID <= 0 || replay.EnemyPartyID <= 0 ||
			replay.EnemyType < 0 || replay.EnemyType > 4 ||
			replay.Seed < 0 || replay.CostInitial < 0 ||
			replay.BurstGaugeInitial < 0 || replay.HoldMax <= 0 || replay.EndTurn < 0 {
			return errors.New("CN save team battle replay is invalid")
		}
		if err := masterdata.ValidateTeamBattleReplaySegments(replay); err != nil {
			return err
		}
	}
	if len(save.Costume) == 0 || bytes.Equal(save.Costume, []byte("null")) {
		return errors.New("CN save requires costume data")
	}
	return nil
}

func validHiddenItemShopLineup(lineup gamestate.ItemShopLineup) bool {
	return lineup.LineupID == 992001 && lineup.PayType == 3 && lineup.PayTypeID == 0 &&
		lineup.Price == 1 && lineup.BuyNumMax == 5000 && len(lineup.Interiors) == 1 &&
		lineup.Interiors[0].BuyType == 1 && lineup.Interiors[0].BuyTypeID == 9010 &&
		lineup.Interiors[0].Num == 1
}

func validGachaBannerKey(value string) bool {
	switch value {
	case "historical_duozi", "historical_tianke", "historical_youmo", "historical_youmo10",
		"", "local_new_year", "local_standard", "local_friend", "local_first", "local_first_multi",
		"five_star_ticket", "element_fire", "element_ice", "element_wind",
		"element_light", "element_dark", "rare_ticket_201805", "unowned_ticket_201805", "lucky_bag_opera",
		"lucky_bag_skuld", "lucky_bag_constantine", "lucky_bag_merchant",
		"lucky_bag_summer_duo", "lucky_bag_yalin":
		return true
	default:
		return false
	}
}

func validGachaPublicationKey(value string) bool {
	switch value {
	case "historical_duozi", "historical_tianke", "historical_youmo", "", "water_coin", "operator", "element_fire", "element_ice", "element_wind",
		"element_light", "element_dark", "rare_ticket_201805", "unowned_ticket_201805", "lucky_bag_opera",
		"lucky_bag_skuld", "lucky_bag_constantine", "lucky_bag_merchant",
		"lucky_bag_summer_duo", "lucky_bag_yalin":
		return true
	default:
		return false
	}
}

func validGachaResultPolicy(gacha gamestate.GachaProfile) bool {
	configured := gacha.GuaranteedRarityRank != 0 || gacha.GuaranteedCount != 0 ||
		gacha.RemainderRarityRank != 0
	if !configured {
		return true
	}
	return gacha.GuaranteedRarityRank >= 1 && gacha.GuaranteedRarityRank <= 8 &&
		gacha.GuaranteedCount > 0 && gacha.GuaranteedCount < gacha.CardNum &&
		gacha.RemainderRarityRank >= 1 && gacha.RemainderRarityRank <= 8 &&
		gacha.RemainderRarityRank != gacha.GuaranteedRarityRank
}

func isOnboardingQuestID(questID int) bool {
	return questID >= 1011 && questID <= 1014 || questID == 1020 ||
		questID >= 1031 && questID <= 1034 ||
		questID >= 1041 && questID <= 1049
}

func SaveFromState(state gamestate.State) saveState {
	cardActions := state.CardActions
	// Evolution templates are immutable official master data and must not be
	// copied into the mutable player save after every state change.
	cardActions.EvolutionTransitions = []gamestate.EvolutionTransition{}
	save := saveState{
		SchemaVersion:                  state.SchemaVersion,
		User:                           state.User,
		PlayerProgressionConfigVersion: state.PlayerProgressionConfigVersion,
		CardProgressionConfigVersion:   state.CardProgressionConfigVersion,
		FeatureUnlockConfigVersion:     state.FeatureUnlockConfigVersion,
		Onboarding:                     state.Onboarding,
		ProfileConfigVersion:           state.ProfileConfigVersion,
		CurrencyConfigVersion:          state.CurrencyConfigVersion,
		BattleLoadoutConfigVersion:     state.BattleLoadoutConfigVersion,
		BattleLoadoutCardUniqueIDs:     append([]int64(nil), state.BattleLoadoutCardUniqueIDs...),
		BattlePointConfigVersion:       state.BattlePointConfigVersion,
		BattlePoint:                    state.BattlePoint,
		PVPConfigVersion:               state.PVPConfigVersion,
		PVP:                            cloneReleasePVPState(state.PVP),
		SphereConfigVersion:            state.SphereConfigVersion,
		Spheres:                        append([]gamestate.Sphere{}, state.Spheres...),
		Cards:                          state.Cards,
		ContainerCards:                 state.ContainerCards,
		StackCards:                     state.StackCards,
		Decks:                          state.Decks,
		Avatars:                        state.Avatars,
		AvatarConfigVersion:            state.AvatarConfigVersion,
		AvatarParts:                    append([]int(nil), state.AvatarParts...),
		Buddy:                          state.Buddy,
		BuddyConfigVersion:             state.BuddyConfigVersion,
		Buddies:                        append([]gamestate.Buddy{}, state.Buddies...),
		Profiles:                       state.Profiles,
		Friends:                        state.Friends,
		Stamps:                         state.Stamps,
		Honors:                         state.Honors,
		Story:                          state.Story,
		ExploreConfigVersion:           state.ExploreConfigVersion,
		Explore:                        state.Explore,
		Engagement:                     state.Engagement,
		MissionDefinitions:             state.MissionDefinitions,
		Options:                        state.Options,
		LoginBonus:                     state.LoginBonus,
		LocalAccountConfigVersion:      state.LocalAccountConfigVersion,
		ItemShopConfigVersion:          state.ItemShopConfigVersion,
		Items:                          state.Items,
		ItemShopTabs:                   state.ItemShopTabs,
		EventShopPurchases:             append([]gamestate.EventShopPurchase(nil), state.EventShopPurchases...),
		TradeShopPurchases:             append([]gamestate.TradeShopPurchase(nil), state.TradeShopPurchases...),
		TeamBattleScores:               state.TeamBattleScores,
		TeamBattleSoloScores:           state.TeamBattleSoloScores,
		LocalShop:                      state.LocalShop,
		GachaConfigVersion:             state.GachaConfigVersion,
		Gachas:                         state.Gachas,
		GachaSelections:                cloneGachaSelections(state.GachaSelections),
		GachaDailyClaims:               append([]gamestate.GachaDailyClaim(nil), state.GachaDailyClaims...),
		FriendPointInboxCursor:         state.FriendPointInboxCursor,
		CardActions:                    cardActions,
		CardDevelopment:                cloneCardDevelopmentState(state.CardDevelopment),
		SupportDeckConfigVersion:       state.SupportDeckConfigVersion,
		SupportDeck:                    cloneSupportDeckState(state.SupportDeck),
		StageQuestConfigVersion:        state.StageQuestConfigVersion,
		MainQuest:                      state.MainQuest,
		StageQuestAreas:                cloneRawMessages(state.StageQuestAreas),
		TeamBattleConfigVersion:        state.TeamBattleConfigVersion,
		TeamBattleSolo:                 state.TeamBattleSolo,
		TeamBattleReplays:              state.TeamBattleReplays,
		TeamBattleRewards:              state.TeamBattleRewards,
		TeamBattleSchedule:             cloneTeamBattleScheduleState(state.TeamBattleSchedule),
		TeamBattleResultReceipts:       cloneTeamBattleResultReceipts(state.TeamBattleResultReceipts),
		TeamBattleStartReceipts:        append([]gamestate.TeamBattleStartReceipt(nil), state.TeamBattleStartReceipts...),
		TeamBattleContinueReceipts:     append([]gamestate.TeamBattleContinueReceipt(nil), state.TeamBattleContinueReceipts...),
		TeamBattleSoloResultReceipts:   cloneTeamBattleSoloResultReceipts(state.TeamBattleSoloResultReceipts),
		ExploreResultReceipt:           cloneExploreResultReceipt(state.ExploreResultReceipt),
		PVPResultReceipts:              clonePVPResultReceipts(state.PVPResultReceipts),
		ActiveTeamBattle:               cloneActiveTeamBattleState(state.ActiveTeamBattle),
		TowerQuestConfigVersion:        state.TowerQuestConfigVersion,
		TowerQuestProgress:             cloneTowerQuestProgressState(state.TowerQuestProgress),
		Costume:                        state.Costume,
	}
	return save
}

func cloneTeamBattleResultReceipts(source []gamestate.TeamBattleResultReceipt) []gamestate.TeamBattleResultReceipt {
	result := make([]gamestate.TeamBattleResultReceipt, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Snapshot = append(json.RawMessage(nil), source[index].Snapshot...)
	}
	return result
}

func cloneTeamBattleSoloResultReceipts(source []gamestate.TeamBattleSoloResultReceipt) []gamestate.TeamBattleSoloResultReceipt {
	result := make([]gamestate.TeamBattleSoloResultReceipt, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Snapshot = append(json.RawMessage(nil), source[index].Snapshot...)
	}
	return result
}

func cloneExploreResultReceipt(source *gamestate.ExploreResultReceipt) *gamestate.ExploreResultReceipt {
	if source == nil {
		return nil
	}
	result := *source
	result.Response = append(json.RawMessage(nil), source.Response...)
	return &result
}

func clonePVPResultReceipts(source []gamestate.PVPResultReceipt) []gamestate.PVPResultReceipt {
	result := make([]gamestate.PVPResultReceipt, len(source))
	for index := range source {
		result[index] = source[index]
		result[index].Response = append(json.RawMessage(nil), source[index].Response...)
	}
	return result
}

func isLowerSHA256(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, character := range value {
		if (character < '0' || character > '9') && (character < 'a' || character > 'f') {
			return false
		}
	}
	return true
}

func cloneActiveTeamBattleState(source *gamestate.TeamBattleActiveState) *gamestate.TeamBattleActiveState {
	if source == nil {
		return nil
	}
	result := *source
	result.ClearDecks = gamestate.CloneBattleClearDecks(source.ClearDecks)
	result.BattleEnemyTypes = append([]int8(nil), source.BattleEnemyTypes...)
	result.ContinueReceipts = append([]string(nil), source.ContinueReceipts...)
	result.DropPlan = append([]gamestate.TeamBattleEnemyDrop(nil), source.DropPlan...)
	for index := range result.DropPlan {
		result.DropPlan[index].Reward.CardSkillLevels = slices.Clone(result.DropPlan[index].Reward.CardSkillLevels)
	}
	result.FameSources = append([]gamestate.TeamBattleFameSourceState(nil), source.FameSources...)
	result.FriendPointRentalCredits = append(
		[]gamestate.TeamBattleRentalCreditState(nil), source.FriendPointRentalCredits...,
	)
	result.SelectedPartners = append([]gamestate.TeamBattleResultPartnerState(nil), source.SelectedPartners...)
	for index := range result.SelectedPartners {
		result.SelectedPartners[index].HonorIDs = append([]int(nil), source.SelectedPartners[index].HonorIDs...)
	}
	return &result
}

func validateActiveTeamBattle(active *gamestate.TeamBattleActiveState) error {
	if active == nil {
		return nil
	}
	if active.EntryRole < gamestate.BattleEntrySolo || active.EntryRole > gamestate.BattleEntryMultiplayerGuest {
		return errors.New("CN save active battle admission role is invalid")
	}
	if active.PrepaidRoomID < 0 || (active.PrepaidRoomID != 0 && active.EntryRole != gamestate.BattleEntryMultiplayerHost) {
		return errors.New("CN save active battle prepaid identity is invalid")
	}
	if len(active.DropPlan) > 512 || (!active.DropPlanSet && len(active.DropPlan) != 0) {
		return errors.New("CN save active battle drop plan is invalid")
	}
	for _, drop := range active.DropPlan {
		if drop.BattleIndex < 0 || drop.BattleIndex >= len(active.BattleEnemyTypes) || drop.EnemyIndex < 0 || drop.EnemyIndex >= 4 || drop.ChancePerMillion != nil || !masterdata.ValidPersistedRewardShape(drop.Reward) {
			return errors.New("CN save active battle drop row is invalid")
		}
	}
	if active.BossID <= 0 || len(active.BattleEnemyTypes) == 0 ||
		len(active.BattleEnemyTypes) > maxActiveBattleSegments {
		return errors.New("CN save active team battle identity is invalid")
	}
	for _, enemyType := range active.BattleEnemyTypes {
		if enemyType < 0 || enemyType > 4 {
			return errors.New("CN save active team battle enemy type is invalid")
		}
	}
	stageBattle := active.StageQuestAreaID != 0 || active.StageQuestStageID != 0
	towerBattle := active.TowerID != 0 || active.TowerFloor != 0 || active.ItemID != 0 || active.ItemUse != 0
	if (active.StageQuestAreaID == 0) != (active.StageQuestStageID == 0) ||
		stageBattle && towerBattle || active.BPUse < 0 {
		return errors.New("CN save active team battle locator is invalid")
	}
	switch {
	case stageBattle:
		if !active.EntryRole.PaysEntryCost() && active.BPUse != 0 {
			return errors.New("CN save active StageQuest cost is invalid")
		}
	case towerBattle:
		if active.TowerID <= 0 || active.TowerFloor <= 0 || active.ItemID < 0 ||
			active.ItemUse < 0 || (active.ItemUse > 0 && active.ItemID == 0) {
			return errors.New("CN save active tower battle cost is invalid")
		}
	default:
		if !active.EntryRole.PaysEntryCost() && active.BPUse != 0 {
			return errors.New("CN save active standalone battle cost is invalid")
		}
	}
	if strings.TrimSpace(active.FameSeed) == "" || len(active.FameSeed) > 512 ||
		len(active.FameSources) == 0 || len(active.FameSources) > 4 {
		return errors.New("CN save active team battle fame source is invalid")
	}
	seenFameTypes := make(map[int]struct{}, len(active.FameSources))
	for _, source := range active.FameSources {
		if source.MemberType < 1 || source.MemberType > 4 || source.LeaderFame < 1 || source.LeaderFame > 100 {
			return errors.New("CN save active team battle fame row is invalid")
		}
		if _, duplicate := seenFameTypes[source.MemberType]; duplicate {
			return errors.New("CN save active team battle fame type is duplicated")
		}
		seenFameTypes[source.MemberType] = struct{}{}
	}
	if active.HostBonusMemberType < 0 || active.HostBonusMemberType > 4 {
		return errors.New("CN save active team battle host bonus is invalid")
	}
	if active.HostBonusMemberType != 0 {
		if _, exists := seenFameTypes[active.HostBonusMemberType]; !exists || active.EntryRole != gamestate.BattleEntryMultiplayerHost {
			return errors.New("CN save active team battle host bonus source is unavailable")
		}
	}
	if active.FriendPointPartners < 0 || active.FriendPointPartners > 3 || active.FriendPointReward < 0 ||
		len(active.FriendPointRentalCredits) > active.FriendPointPartners ||
		len(active.FriendPointRentalEventKey) > 512 ||
		(len(active.FriendPointRentalCredits) == 0) != (strings.TrimSpace(active.FriendPointRentalEventKey) == "") {
		return errors.New("CN save active team battle friend-point state is invalid")
	}
	seenRentalOwners := make(map[int]struct{}, len(active.FriendPointRentalCredits))
	for _, credit := range active.FriendPointRentalCredits {
		if credit.OwnerUserID <= 0 || credit.FriendPoint <= 0 {
			return errors.New("CN save active team battle rental credit is invalid")
		}
		if _, duplicate := seenRentalOwners[credit.OwnerUserID]; duplicate {
			return errors.New("CN save active team battle rental owner is duplicated")
		}
		seenRentalOwners[credit.OwnerUserID] = struct{}{}
	}
	rentalPartners := 0
	for _, partner := range active.SelectedPartners {
		if !partner.IsSelf {
			rentalPartners++
		}
	}
	if len(active.SelectedPartners) != 0 && (len(active.SelectedPartners) > 3 || rentalPartners != active.FriendPointPartners) {
		return errors.New("CN save active team battle partner count is invalid")
	}
	seenPartnerUsers := make(map[int]struct{}, len(active.SelectedPartners))
	for _, partner := range active.SelectedPartners {
		if partner.UserID <= 0 || partner.ArthurType < 1 || partner.ArthurType > 4 || partner.Level <= 0 ||
			partner.LeaderCardID <= 0 || partner.LeaderLevel <= 0 || partner.LeaderFame <= 0 ||
			partner.LeaderFame > 100 || partner.PVPPoint < 0 || len(partner.Name) > 1024 ||
			len(partner.Comment) > 4096 || len(partner.HonorIDs) > 64 {
			return errors.New("CN save active team battle result partner is invalid")
		}
		if _, duplicate := seenPartnerUsers[partner.UserID]; duplicate && !partner.IsSelf {
			return errors.New("CN save active team battle partner user is duplicated")
		}
		seenPartnerUsers[partner.UserID] = struct{}{}
		for _, honorID := range partner.HonorIDs {
			// The client serializes unused honor slots as zero. Preserve those
			// sentinels so an otherwise valid selected partner can survive a
			// server restart between battle start and settlement.
			if honorID < 0 {
				return errors.New("CN save active team battle partner honor is invalid")
			}
		}
	}
	return nil
}

func cloneRawMessages(source []json.RawMessage) []json.RawMessage {
	result := make([]json.RawMessage, len(source))
	for index, value := range source {
		result[index] = append(json.RawMessage(nil), value...)
	}
	return result
}

func cloneTowerQuestProgressState(source []gamestate.TowerQuestProgress) []gamestate.TowerQuestProgress {
	result := make([]gamestate.TowerQuestProgress, len(source))
	for index, progress := range source {
		progress.ClearedFloors = append([]int(nil), progress.ClearedFloors...)
		progress.ClearRatings = maps.Clone(progress.ClearRatings)
		progress.BestClearTurns = maps.Clone(progress.BestClearTurns)
		result[index] = progress
	}
	return result
}

func validateGachaSelection(gacha gamestate.GachaProfile, rewards []gamestate.Reward) error {
	if gacha.UserSelectMax <= 0 || len(rewards) != gacha.UserSelectMax {
		return fmt.Errorf("CN save gacha selection %d has an invalid length", gacha.GachaID)
	}
	allowed := make(map[int]struct{}, len(gacha.CardIDs))
	for _, cardID := range gacha.CardIDs {
		allowed[cardID] = struct{}{}
	}
	seen := make(map[int]struct{}, len(rewards))
	for _, reward := range rewards {
		if reward.Type != 6 || reward.Num != 1 || reward.CardLevel != 1 ||
			reward.CardFame != 1 || reward.CardLove != 0 ||
			len(reward.CardSkillLevels) != 1 || reward.CardSkillLevels[0] != 1 {
			return fmt.Errorf("CN save gacha selection %d contains an invalid reward", gacha.GachaID)
		}
		if _, exists := allowed[reward.RewardTypeID]; !exists {
			return fmt.Errorf("CN save gacha selection %d contains an unavailable card", gacha.GachaID)
		}
		if _, duplicate := seen[reward.RewardTypeID]; duplicate {
			return fmt.Errorf("CN save gacha selection %d repeats card %d", gacha.GachaID, reward.RewardTypeID)
		}
		seen[reward.RewardTypeID] = struct{}{}
	}
	return nil
}

func cloneGachaSelections(source []gamestate.GachaSelection) []gamestate.GachaSelection {
	result := make([]gamestate.GachaSelection, len(source))
	for index, selection := range source {
		result[index].GachaID = selection.GachaID
		result[index].Rewards = make([]gamestate.Reward, len(selection.Rewards))
		for rewardIndex, reward := range selection.Rewards {
			result[index].Rewards[rewardIndex] = reward
			result[index].Rewards[rewardIndex].CardSkillLevels = append(
				[]int16(nil), reward.CardSkillLevels...,
			)
		}
	}
	return result
}

func cloneTeamBattleScheduleState(source gamestate.TeamBattleScheduleState) gamestate.TeamBattleScheduleState {
	return gamestate.TeamBattleScheduleState{
		SoloPushGroupIDs:  append([]int(nil), source.SoloPushGroupIDs...),
		MultiPushGroupIDs: append([]int(nil), source.MultiPushGroupIDs...),
	}
}

func cloneCardDevelopmentState(source gamestate.CardDevelopmentState) gamestate.CardDevelopmentState {
	result := source
	if source.Training != nil {
		training := *source.Training
		result.Training = &training
	}
	return result
}

func cloneSupportDeckState(source gamestate.SupportDeckState) gamestate.SupportDeckState {
	return gamestate.SupportDeckState{
		UnlockSlotNums:           append([]int8(nil), source.UnlockSlotNums...),
		CardCollectionIDs:        append([]int(nil), source.CardCollectionIDs...),
		CardCollectionLoveMaxIDs: append([]int(nil), source.CardCollectionLoveMaxIDs...),
	}
}

func cloneReleasePVPState(source gamestate.PVPPlayerState) gamestate.PVPPlayerState {
	result := source
	result.DefenseDecks = make([]gamestate.PVPDeckSelection, len(source.DefenseDecks))
	for index := range source.DefenseDecks {
		result.DefenseDecks[index] = source.DefenseDecks[index]
		result.DefenseDecks[index].CardUniqueIDs = append([]int64(nil), source.DefenseDecks[index].CardUniqueIDs...)
		result.DefenseDecks[index].SupportCardUniqueIDs = append([]int64(nil), source.DefenseDecks[index].SupportCardUniqueIDs...)
		result.DefenseDecks[index].SphereUniqueIDs = append([]int64(nil), source.DefenseDecks[index].SphereUniqueIDs...)
		result.DefenseDecks[index].BuddyUniqueIDs = append([]int64(nil), source.DefenseDecks[index].BuddyUniqueIDs...)
	}
	result.History = make([]gamestate.PVPMatch, len(source.History))
	copy(result.History, source.History)
	if source.ActiveMatch != nil {
		active := *source.ActiveMatch
		result.ActiveMatch = &active
	}
	return result
}
