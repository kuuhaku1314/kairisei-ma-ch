package game

import (
	"encoding/json"
	"log/slog"
	"maps"

	"kairisei.local/server/internal/gamestate"
)

// ContentConfiguration is a shared immutable operations snapshot. It contains
// public definitions only; purchase counts and accepted battle plans stay with
// their existing owners. Call under the account request lock, like gacha policy.
type ContentConfiguration struct {
	BattleCatalog *TeamBattleDefinitions
	Revision      uint64
	State         gamestate.State
}

func NewContentConfiguration(revision uint64, state gamestate.State) (ContentConfiguration, error) {
	catalog, err := NewTeamBattleDefinitions(state.TeamBattleSolo)
	if err != nil {
		return ContentConfiguration{}, err
	}
	return ContentConfiguration{Revision: revision, State: state, BattleCatalog: catalog}, nil
}

type ContentConfigurator interface{ ApplyContentConfiguration(ContentConfiguration) }

func ApplyContentState(state gamestate.State, config ContentConfiguration) gamestate.State {
	if config.Revision == 0 {
		return state
	}
	state.TeamBattleRewards = config.State.TeamBattleRewards
	state.TeamBattleReplays = config.State.TeamBattleReplays
	state.TowerQuestProfiles = config.State.TowerQuestProfiles
	state.Explore.Events = config.State.Explore.Events
	if config.BattleCatalog != nil {
		if catalog, err := newBattleCatalogState(state.TeamBattleSolo, config.BattleCatalog); err == nil {
			state.TeamBattleSolo = catalog.saved()
		} else {
			slog.Error("apply shared battle catalog to account", "revision", config.Revision, "error", err)
		}
	}
	state.TeamBattlePastBossGroups = config.State.TeamBattlePastBossGroups
	state.DisabledTeamBattleBossIDs = config.State.DisabledTeamBattleBossIDs
	state.TeamBattlePastBossGroups = FilterBattlePastBosses(state.TeamBattlePastBossGroups, state.DisabledTeamBattleBossIDs)
	state.TradeShopProfiles = config.State.TradeShopProfiles
	return state
}

func (s *Account) ApplyTradeShopConfiguration(shops []gamestate.TradeShopProfile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tradeShopProfiles = make(map[int]gamestate.TradeShopProfile, len(shops))
	for _, shop := range shops {
		s.tradeShopProfiles[shop.TradeShopID] = shop
	}
}

func (s *Account) ApplyBattleCatalogConfiguration(definitions *TeamBattleDefinitions, disabled map[int]bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if definitions != nil {
		if s.teamBattleSolo == nil {
			s.teamBattleSolo = &battleCatalogState{progress: map[battleBossKey]battleBossProgress{}}
		}
		s.teamBattleSolo.definitions = definitions
		s.teamBattleSolo.encoded = nil
	}
	s.disabledTeamBattleBossIDs = disabled
}

// Quest 1047's fixed activity guide must remain playable even when Admin closes
// Ice Dragon. The exception belongs to this account's final onboarding step,
// never to the shared publication configuration or its other difficulties.
func (s *Account) battlePublicationDisabledBossesLocked() map[int]bool {
	disabled := s.disabledTeamBattleBossIDs
	const tutorialBossID = 30010102
	if disabled[tutorialBossID] && s.onboarding.Step == gamestate.OnboardingStepCount-1 {
		disabled = maps.Clone(disabled)
		delete(disabled, tutorialBossID)
	}
	return disabled
}

// Keep the full catalog in the account so closing an entry never erases clears.
func filterBattleGroupBosses(raw json.RawMessage, key string, disabled map[int]bool) json.RawMessage {
	var group map[string]json.RawMessage
	if json.Unmarshal(raw, &group) != nil {
		return raw
	}
	var bosses []json.RawMessage
	if json.Unmarshal(group[key], &bosses) != nil {
		return raw
	}
	kept := make([]json.RawMessage, 0, len(bosses))
	for _, boss := range bosses {
		var identity struct {
			ID int `json:"0"`
		}
		if json.Unmarshal(boss, &identity) != nil {
			return raw
		}
		if !disabled[identity.ID] {
			kept = append(kept, boss)
		}
	}
	group[key], _ = json.Marshal(kept)
	result, _ := json.Marshal(group)
	return result
}

func FilterBattlePastBosses(groups []json.RawMessage, disabled map[int]bool) []json.RawMessage {
	if len(disabled) == 0 {
		return groups
	}
	result := make([]json.RawMessage, len(groups))
	for i, raw := range groups {
		result[i] = filterBattleGroupBosses(raw, "13", disabled)
	}
	return result
}

func filterBattleCatalog(raw json.RawMessage, disabled map[int]bool) json.RawMessage {
	if len(disabled) == 0 {
		return raw
	}
	var top map[string]json.RawMessage
	if json.Unmarshal(raw, &top) != nil {
		return raw
	}
	for _, key := range []string{"9", "10", "11", "12"} {
		if len(top[key]) == 0 {
			continue
		}
		var groups []json.RawMessage
		if json.Unmarshal(top[key], &groups) != nil {
			return raw
		}
		for i, group := range groups {
			groups[i] = filterBattleGroupBosses(group, "10", disabled)
		}
		top[key], _ = json.Marshal(groups)
	}
	result, _ := json.Marshal(top)
	return result
}

// Replace shared tower definitions without touching player floor/rating progress.
func (s *Account) ApplyTowerQuestConfiguration(profiles []gamestate.TowerQuestProfile) {
	if profiles == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.towerQuestProfiles = make(map[int]gamestate.TowerQuestProfile, len(profiles))
	for _, profile := range profiles {
		s.towerQuestProfiles[profile.TowerID] = profile
	}
}
