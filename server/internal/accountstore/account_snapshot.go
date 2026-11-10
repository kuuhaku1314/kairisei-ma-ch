package accountstore

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"

	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/masterdata"
)

// Bound the remaining account metadata separately from the 4 MiB seed limit.
// Inventory and presents live in individual SQL rows, not in this payload.
const maxAccountSnapshotBytes = 16 * 1024 * 1024

// cnAccountSnapshot is the durable account contract. Public definitions are
// loaded from the versioned seed/runtime catalog, never copied into these rows.
// Global operator changes remain in cn_global_operation.
type accountSnapshot struct {
	Components                   accountComponentHashes                    `json:"row_components,omitempty"`
	InventorySequence            gamestate.InventorySequenceState          `json:"inventory_sequence,omitempty"`
	BurstProgress                [4]uint8                                  `json:"burst_progress"`
	Navigation                   gamestate.NavigationState                 `json:"navigation,omitempty"`
	SchemaVersion                int                                       `json:"schema_version"`
	TeamBattleScores             map[int]gamestate.TeamBattleScoreProgress `json:"team_battle_scores,omitempty"`
	TeamBattleSoloScores         map[int]gamestate.TeamBattleScoreProgress `json:"team_battle_solo_scores,omitempty"`
	LocalShop                    gamestate.LocalShopState                  `json:"local_shop,omitempty"`
	User                         gamestate.User                            `json:"user"`
	Onboarding                   gamestate.OnboardingState                 `json:"onboarding"`
	BattlePoint                  gamestate.BattlePointState                `json:"battle_point"`
	PVP                          gamestate.PVPPlayerState                  `json:"pvp"`
	Spheres                      []gamestate.Sphere                        `json:"spheres"`
	Decks                        []gamestate.Deck                          `json:"decks"`
	Avatars                      []gamestate.Avatar                        `json:"avatars"`
	AvatarParts                  []int                                     `json:"avatar_parts,omitempty"`
	Costume                      json.RawMessage                           `json:"costume,omitempty"`
	Buddy                        gamestate.Buddy                           `json:"buddy"`
	Buddies                      []gamestate.Buddy                         `json:"buddies,omitempty"`
	Friends                      gamestate.FriendCollectionState           `json:"friends"`
	Stamps                       gamestate.StampCollectionState            `json:"stamps"`
	Honors                       gamestate.HonorCollectionState            `json:"honors"`
	Engagement                   gamestate.EngagementState                 `json:"engagement"`
	Options                      gamestate.OptionState                     `json:"options"`
	LoginBonus                   gamestate.LoginBonusState                 `json:"login_bonus"`
	EventShopPurchases           []gamestate.EventShopPurchase             `json:"event_shop_purchases,omitempty"`
	TradeShopPurchases           []gamestate.TradeShopPurchase             `json:"trade_shop_purchases,omitempty"`
	GachaSelections              []gamestate.GachaSelection                `json:"gacha_selections,omitempty"`
	GachaDailyClaims             []gamestate.GachaDailyClaim               `json:"gacha_daily_claims,omitempty"`
	FriendPointInboxCursor       int64                                     `json:"friend_point_inbox_cursor,omitempty"`
	CardDevelopment              gamestate.CardDevelopmentState            `json:"card_development"`
	SupportDeck                  gamestate.SupportDeckState                `json:"support_deck"`
	TeamBattleSchedule           gamestate.TeamBattleScheduleState         `json:"team_battle_schedule,omitempty"`
	TeamBattleResultReceipts     []gamestate.TeamBattleResultReceipt       `json:"team_battle_result_receipts,omitempty"`
	TeamBattleStartReceipts      []gamestate.TeamBattleStartReceipt        `json:"team_battle_start_receipts,omitempty"`
	TeamBattleContinueReceipts   []gamestate.TeamBattleContinueReceipt     `json:"team_battle_continue_receipts,omitempty"`
	TeamBattleSoloResultReceipts []gamestate.TeamBattleSoloResultReceipt   `json:"team_battle_solo_result_receipts,omitempty"`
	ExploreResultReceipt         *gamestate.ExploreResultReceipt           `json:"explore_result_receipt,omitempty"`
	PVPResultReceipts            []gamestate.PVPResultReceipt              `json:"pvp_result_receipts,omitempty"`
	TowerQuestProgress           []gamestate.TowerQuestProgress            `json:"tower_quest_progress,omitempty"`
	Progress                     catalogProgress                           `json:"progress"`
	ExploreProgress              exploreProgress                           `json:"explore_progress"`
}

type exploreProgress struct {
	ActiveRewards      *[]gamestate.Reward `json:"active_rewards,omitempty"`
	StageCursor        int                 `json:"stage_cursor"`
	ActiveStageID      int                 `json:"active_stage_id"`
	Active             bool                `json:"active"`
	ArthurType         int8                `json:"arthur_type"`
	DeckIndex          int8                `json:"deck_index"`
	StartedAtUnix      int64               `json:"started_at_unix"`
	APNextRecoveryUnix int64               `json:"ap_next_recovery_unix"`
}

func accountSnapshotFromState(state gamestate.State) (accountSnapshot, error) {
	progress, err := collectCatalogProgress(state)
	if err != nil {
		return accountSnapshot{}, err
	}
	return accountSnapshot{
		InventorySequence:            state.InventorySequence,
		Navigation:                   state.Navigation,
		BurstProgress:                state.BurstProgress,
		SchemaVersion:                saveSnapshotSchemaVersion,
		TeamBattleScores:             state.TeamBattleScores,
		TeamBattleSoloScores:         state.TeamBattleSoloScores,
		LocalShop:                    state.LocalShop,
		User:                         state.User,
		Onboarding:                   state.Onboarding,
		BattlePoint:                  state.BattlePoint,
		PVP:                          state.PVP,
		Spheres:                      state.Spheres,
		Decks:                        state.Decks,
		Avatars:                      state.Avatars,
		AvatarParts:                  state.AvatarParts,
		Costume:                      state.Costume,
		Buddy:                        state.Buddy,
		Buddies:                      state.Buddies,
		Friends:                      gamestate.FriendCollectionState{Users: []gamestate.Friend{}},
		Stamps:                       state.Stamps,
		Honors:                       state.Honors,
		Engagement:                   state.Engagement,
		Options:                      state.Options,
		LoginBonus:                   state.LoginBonus,
		EventShopPurchases:           state.EventShopPurchases,
		TradeShopPurchases:           state.TradeShopPurchases,
		GachaSelections:              state.GachaSelections,
		GachaDailyClaims:             state.GachaDailyClaims,
		FriendPointInboxCursor:       state.FriendPointInboxCursor,
		CardDevelopment:              state.CardDevelopment,
		SupportDeck:                  state.SupportDeck,
		TeamBattleSchedule:           state.TeamBattleSchedule,
		TeamBattleResultReceipts:     state.TeamBattleResultReceipts,
		TeamBattleStartReceipts:      state.TeamBattleStartReceipts,
		TeamBattleContinueReceipts:   state.TeamBattleContinueReceipts,
		TeamBattleSoloResultReceipts: state.TeamBattleSoloResultReceipts,
		ExploreResultReceipt:         state.ExploreResultReceipt,
		PVPResultReceipts:            state.PVPResultReceipts,
		TowerQuestProgress:           state.TowerQuestProgress,
		Progress:                     progress,
		ExploreProgress: exploreProgress{
			ActiveRewards: state.Explore.ActiveRewards,
			StageCursor:   state.Explore.StageCursor, ActiveStageID: state.Explore.ActiveStageID,
			Active: state.Explore.Active, ArthurType: state.Explore.ArthurType, DeckIndex: state.Explore.DeckIndex,
			StartedAtUnix: state.Explore.StartedAtUnix, APNextRecoveryUnix: state.Explore.APNextRecoveryUnix,
		},
	}, nil
}

// applyAccountData replaces only account-owned fields. Catalog maps/slices
// may be shared with other accounts and must not be mutated here.
func (snapshot accountSnapshot) applyAccountData(state *gamestate.State) {
	state.InventorySequence = snapshot.InventorySequence
	state.TeamBattleScores = snapshot.TeamBattleScores
	state.TeamBattleSoloScores = snapshot.TeamBattleSoloScores
	state.LocalShop = snapshot.LocalShop
	state.User = snapshot.User
	state.Onboarding = snapshot.Onboarding
	state.BattlePoint = snapshot.BattlePoint
	state.PVP = snapshot.PVP
	state.Spheres = snapshot.Spheres
	state.Decks = snapshot.Decks
	state.Avatars = snapshot.Avatars
	state.AvatarParts = snapshot.AvatarParts
	if len(snapshot.Costume) > 0 {
		state.Costume = snapshot.Costume
	}
	state.Buddy = snapshot.Buddy
	state.Buddies = snapshot.Buddies
	state.Friends = snapshot.Friends
	state.Stamps = snapshot.Stamps
	state.Honors = snapshot.Honors
	state.Engagement = snapshot.Engagement
	state.Options = snapshot.Options
	state.LoginBonus = snapshot.LoginBonus
	state.EventShopPurchases = snapshot.EventShopPurchases
	state.TradeShopPurchases = snapshot.TradeShopPurchases
	state.GachaSelections = snapshot.GachaSelections
	state.GachaDailyClaims = snapshot.GachaDailyClaims
	state.FriendPointInboxCursor = snapshot.FriendPointInboxCursor
	state.CardDevelopment = snapshot.CardDevelopment
	state.SupportDeck = snapshot.SupportDeck
	state.TeamBattleSchedule = snapshot.TeamBattleSchedule
	state.TeamBattleResultReceipts = snapshot.TeamBattleResultReceipts
	state.TeamBattleStartReceipts = snapshot.TeamBattleStartReceipts
	state.TeamBattleContinueReceipts = snapshot.TeamBattleContinueReceipts
	state.TeamBattleSoloResultReceipts = snapshot.TeamBattleSoloResultReceipts
	state.ExploreResultReceipt = snapshot.ExploreResultReceipt
	state.PVPResultReceipts = snapshot.PVPResultReceipts
	state.ActiveTeamBattle = nil
	state.Navigation = snapshot.Navigation
	state.BurstProgress = snapshot.BurstProgress
	state.TowerQuestProgress = snapshot.TowerQuestProgress
	state.Explore.StageCursor = snapshot.ExploreProgress.StageCursor
	state.Explore.ActiveStageID = snapshot.ExploreProgress.ActiveStageID
	state.Explore.Active = snapshot.ExploreProgress.Active
	state.Explore.ActiveRewards = snapshot.ExploreProgress.ActiveRewards
	state.Explore.ArthurType = snapshot.ExploreProgress.ArthurType
	state.Explore.DeckIndex = snapshot.ExploreProgress.DeckIndex
	state.Explore.StartedAtUnix = snapshot.ExploreProgress.StartedAtUnix
	state.Explore.APNextRecoveryUnix = snapshot.ExploreProgress.APNextRecoveryUnix
}

// Large inventories and balances have relational owners. Keep only the small
// configuration/progress fragments here; no duplicate zero-valued wallet fields.
func EncodeAccountMetadata(state gamestate.State) ([]byte, error) {
	if err := ValidateSave(SaveFromState(state)); err != nil {
		return nil, err
	}
	snapshot, err := accountSnapshotFromState(state)
	if err != nil {
		return nil, err
	}
	snapshot.Components, err = accountComponentDigests(state)
	if err != nil {
		return nil, err
	}
	type metadataUser struct {
		gamestate.User
		Gold        *int `json:"gold,omitempty"`
		Coin        *int `json:"coin,omitempty"`
		CoinFree    *int `json:"coin_free,omitempty"`
		FriendPoint *int `json:"friend_point,omitempty"`
		PVPPoint    *int `json:"pvp_point,omitempty"`
	}
	type metadataEngagement struct {
		Initialized  bool                `json:"initialized"`
		Missions     []gamestate.Mission `json:"missions"`
		PopupReadIDs []int               `json:"popup_read_ids,omitempty"`
	}
	content, err := json.Marshal(struct {
		accountSnapshot
		User       metadataUser       `json:"user"`
		Engagement metadataEngagement `json:"engagement"`
	}{snapshot, metadataUser{User: snapshot.User}, metadataEngagement{snapshot.Engagement.Initialized, snapshot.Engagement.Missions, snapshot.Engagement.PopupReadIDs}})
	if err != nil {
		return nil, err
	}
	if len(content) > maxAccountSnapshotBytes {
		return nil, errors.New("CN account metadata exceeds the size limit")
	}
	return content, nil
}

func DecodeAccountSnapshot(content []byte) (accountSnapshot, error) {
	var snapshot accountSnapshot
	if len(content) == 0 || len(content) > maxAccountSnapshotBytes {
		return snapshot, errors.New("CN account data is empty or exceeds the size limit")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	if err := decoder.Decode(&snapshot); err != nil {
		return snapshot, fmt.Errorf("decode CN account data: %w", err)
	}
	if err := masterdata.RequireJSONEOF(decoder); err != nil {
		return snapshot, err
	}
	if snapshot.SchemaVersion != saveSnapshotSchemaVersion || snapshot.User.UserID < PrimaryUserID {
		return snapshot, errors.New("unsupported CN account data schema or identity; use a fresh database")
	}
	return snapshot, nil
}

func (storage *Database) decodeAccount(tx *sql.Tx, content []byte) (gamestate.State, error) {
	snapshot, err := DecodeAccountSnapshot(content)
	if err != nil {
		return gamestate.State{}, err
	}
	state, err := storage.CatalogState()
	if err != nil {
		return gamestate.State{}, err
	}
	// Minimal test/seed configurations keep templates in their initial cards;
	// production always supplies the complete immutable CardTemplates master.
	if len(state.CardTemplates) == 0 {
		state.CardTemplates = state.Cards
	}
	snapshot.applyAccountData(&state)
	if err := readAccountRows(tx, &state); err != nil {
		return gamestate.State{}, err
	}
	// A fresh seed has no tower progress yet; definitions are public, while
	// the initial floor belongs to the newly created account.
	for _, profile := range state.TowerQuestProfiles {
		found := false
		for _, progress := range state.TowerQuestProgress {
			found = found || progress.TowerID == profile.TowerID
		}
		if !found {
			state.TowerQuestProgress = append(state.TowerQuestProgress, gamestate.TowerQuestProgress{TowerID: profile.TowerID, Floor: 1})
		}
	}
	if err := InstallOnboardingGacha(&state); err != nil {
		return gamestate.State{}, err
	}
	if err := snapshot.Progress.apply(&state); err != nil {
		return gamestate.State{}, err
	}
	if err := ValidateSave(SaveFromState(state)); err != nil {
		return gamestate.State{}, fmt.Errorf("validate CN account data: %w", err)
	}
	return state, nil
}

func (storage *Database) CatalogState() (gamestate.State, error) {
	if storage.catalog != nil {
		return *storage.catalog, nil
	}
	return LoadSaveState(storage.seedPath)
}
