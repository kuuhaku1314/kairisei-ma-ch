package cnbootstrap

import (
	"fmt"
	"net/http"
	"path/filepath"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/httpapi"
	"kairisei.local/server/internal/masterdata"
)

type cnRuntimeStatePreparer func(gamestate.State, bool) (gamestate.State, error)

func (prepare cnRuntimeStatePreparer) prepareCatalog(state gamestate.State) (gamestate.State, error) {
	return prepare(state, true)
}

// Account loading has already attached the current Database.CatalogState and
// overlaid private progress. Do not rebuild those immutable battle definitions.
func (prepare cnRuntimeStatePreparer) prepareAccount(state gamestate.State) (gamestate.State, error) {
	return prepare(state, false)
}

// loadCNRuntimeStatePreparer reads immutable master data once per server.
func loadCNRuntimeStatePreparer(masters MastersConfig, pvpConfig game.PVPConfig, playerProgression gamestate.PlayerProgressionPolicy, loginBonusPolicy gamestate.LoginBonusPolicy, availableBundles map[string]struct{}) (cnRuntimeStatePreparer, error) {
	cardMaster, err := masterdata.LoadCardRuntimeMaster(masters.Cards)
	if err != nil {
		return nil, err
	}
	exploreMaster, err := masterdata.LoadExploreRuntimeMaster(masters.Explore)
	if err != nil {
		return nil, err
	}
	storyMaster, err := masterdata.LoadStoryRuntimeMaster(masters.Story)
	if err != nil {
		return nil, err
	}
	naviMaster, err := masterdata.LoadNaviRuntimeMaster(masters.Navi)
	if err != nil {
		return nil, err
	}
	itemMaster, err := masterdata.LoadItemRuntimeMaster(masters.Items)
	if err != nil {
		return nil, err
	}
	avatarMaster, err := masterdata.LoadAvatarRuntimeMaster(masters.Avatar)
	if err != nil {
		return nil, err
	}
	stampMaster, err := masterdata.LoadStampRuntimeMaster(masters.Stamps)
	if err != nil {
		return nil, err
	}
	honorMaster, err := masterdata.LoadHonorRuntimeMaster(masters.Honors)
	if err != nil {
		return nil, err
	}
	var battleMaster masterdata.BattleRuntimeMaster
	var normalQuestMaster masterdata.NormalQuestRuntimeMaster
	if masters.Battle != "" {
		battleMaster, err = masterdata.LoadBattleRuntimeMaster(masters.Battle)
		if err != nil {
			return nil, err
		}
		normalQuestMasterPath := filepath.Join(
			filepath.Dir(masters.Battle),
			"cn602-normal-quest-runtime.json",
		)
		normalQuestMaster, err = masterdata.LoadNormalQuestRuntimeMaster(normalQuestMasterPath)
		if err != nil {
			return nil, err
		}
	}
	return func(runtimeState gamestate.State, buildBattleDefinitions bool) (gamestate.State, error) {
		var err error
		if runtimeState.User.Comment == "LOCAL OFFLINE PROFILE" {
			runtimeState.User.Comment = "请多关照！"
		}
		runtimeState.User.InviteID = accountstore.InviteID(runtimeState.User.UserID)
		runtimeState.User.CardMax = gamestate.CardCapacityLimit
		runtimeState.User.CardContainerMax = max(runtimeState.User.CardContainerMax, gamestate.CardContainerCapacityDefault)
		runtimeState.User.SphereMax = max(runtimeState.User.SphereMax, gamestate.SphereCapacityDefault)
		runtimeState.User.BuddyMax = max(runtimeState.User.BuddyMax, gamestate.BuddyCapacityDefault)
		_, err = masterdata.ApplyPlayerProgressionRuntimeMaster(&runtimeState, playerProgression)
		if err != nil {
			return gamestate.State{}, err
		}
		_, err = masterdata.ApplyLoginBonusRuntimeMaster(&runtimeState, loginBonusPolicy)
		if err != nil {
			return gamestate.State{}, err
		}
		if pvpConfig.ConfigVersion > 0 {
			masterdata.NormalizePVPState(&runtimeState, pvpConfig)
		}
		normalizeCN602StampDeck(&runtimeState)
		_, err = masterdata.ApplyCardRuntimeMaster(&runtimeState, cardMaster)
		if err != nil {
			return gamestate.State{}, err
		}
		if err := masterdata.ApplyExploreRuntimeMaster(&runtimeState, exploreMaster); err != nil {
			return gamestate.State{}, err
		}
		masterdata.ApplyStoryRuntimeMaster(&runtimeState, storyMaster)
		if masters.Battle != "" {
			if buildBattleDefinitions {
				if err := masterdata.ApplyBattleRuntimeMaster(&runtimeState, battleMaster); err != nil {
					return gamestate.State{}, err
				}
			} else {
				masterdata.NormalizeBattleAccountProgress(&runtimeState)
			}
			// Normal-quest clears also update their battle entrance progress.
			// Keep that account-specific reconciliation on both paths.
			if err := masterdata.ApplyNormalQuestRuntimeMaster(&runtimeState, normalQuestMaster); err != nil {
				return gamestate.State{}, err
			}
		}
		if err := masterdata.ApplyNaviRuntimeMaster(&runtimeState, naviMaster); err != nil {
			return gamestate.State{}, err
		}
		_, err = masterdata.ApplyItemRuntimeMaster(&runtimeState, itemMaster)
		if err != nil {
			return gamestate.State{}, err
		}
		_, err = masterdata.ApplyAvatarRuntimeMaster(&runtimeState, avatarMaster)
		if err != nil {
			return gamestate.State{}, err
		}
		if err := masterdata.ApplyAvatarShopAssetClosure(&runtimeState, availableBundles); err != nil {
			return gamestate.State{}, err
		}
		_, err = masterdata.ApplyStampRuntimeMaster(&runtimeState, stampMaster)
		if err != nil {
			return gamestate.State{}, err
		}
		_, err = masterdata.ApplyHonorRuntimeMaster(&runtimeState, honorMaster)
		if err != nil {
			return gamestate.State{}, err
		}
		return runtimeState, nil

	}, nil
}

func newCNBusinessHandler(config httpapi.Config, prepareRuntimeState func(gamestate.State) (gamestate.State, error)) (http.Handler, error) {
	config.CardShowProjection = projectCNCardShow
	runtimeState := config.InitialState
	stateBefore, err := fingerprintCNAccountState(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("encode CN runtime state before master application: %w", err)
	}
	runtimeState, err = prepareRuntimeState(runtimeState)
	if err != nil {
		return nil, err
	}
	contentGate, err := validateCNRunnableContent(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("validate CN runtime content gate: %w", err)
	}
	stateAfter, err := fingerprintCNAccountState(runtimeState)
	if err != nil {
		return nil, fmt.Errorf("encode CN runtime state after master application: %w", err)
	}
	if stateBefore != stateAfter {
		if err := config.PersistState(runtimeState); err != nil {
			return nil, fmt.Errorf("persist validated CN runtime state migration: %w", err)
		}
	}
	config.Logger.Info(
		"validated CN runtime content gate",
		"team_battle_groups", contentGate.TeamBattleGroups,
		"team_battle_boss_entries", contentGate.TeamBattleBossEntries,
		"user_buff_profiles", contentGate.UserBuffProfiles,
		"user_buff_references", contentGate.UserBuffReferences,
		"stage_quest_stages", contentGate.StageQuestStages,
		"explore_stages", contentGate.ExploreStages,
		"explore_floors", contentGate.ExploreFloors,
		"main_stories", contentGate.MainStories,
		"cn_main_stories", contentGate.CNMainStories,
		"sub_story_sections", contentGate.SubStorySections,
		"sub_stories", contentGate.SubStories,
		"story_events", contentGate.StoryEvents,
		"event_stories", contentGate.EventStories,
		"story_battle_ids", contentGate.StoryBattleIDs,
		"story_battle_references", contentGate.StoryBattleReferences,
		"popup_profiles", contentGate.PopupProfiles,
	)
	runtimeState.SchemaVersion = 1
	runtimeState.ReleaseID = "cn602-bootstrap"
	runtimeState.SourceBuild = "cn-official-6.0.2"
	runtimeState.CatalogVersion = cn602CatalogVersion
	runtimeState.State = "LOCAL"
	config.InitialState = runtimeState
	return httpapi.New(config)
}

func normalizeCN602StampDeck(state *gamestate.State) bool {
	const stampDeckSlots = 12
	if len(state.Stamps.DeckStampIDs) == stampDeckSlots {
		return false
	}
	deck := make([]int, stampDeckSlots)
	copy(deck, state.Stamps.DeckStampIDs)
	state.Stamps.DeckStampIDs = deck
	return true
}
