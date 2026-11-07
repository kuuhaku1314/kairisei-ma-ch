package game

import (
	"errors"
	"fmt"
	"slices"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// GachaConfiguration is supplied by the local operations store, under the
// account request lock. Player counters and daily claims remain account-owned.
type GachaConfiguration struct {
	Disabled bool
	// Operator marks an operator-created pool that is not part of the catalog; accounts append it.
	Operator  bool
	Profile   gamestate.GachaProfile
	StartUnix int64
	EndUnix   int64
}

type GachaConfigurator interface {
	ApplyGachaConfiguration(uint64, []GachaConfiguration)
}

func (s *Account) ApplyGachaConfiguration(revision uint64, configs []GachaConfiguration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.gachaRevision == revision {
		return
	}
	listed := make(map[int]bool, len(configs))
	for _, config := range configs {
		listed[config.Profile.GachaID] = true
	}
	s.gachas = slices.DeleteFunc(s.gachas, func(profile gamestate.GachaProfile) bool {
		if profile.PublicationKey != "operator" || listed[profile.GachaID] {
			return false
		}
		delete(s.operatorGachaPlays, profile.GachaID)
		delete(s.gachaSelections, profile.GachaID)
		delete(s.gachaDailyClaims, profile.GachaID)
		return true
	})
	for _, config := range configs {
		current := findGachaProfile(s.gachas, config.Profile.GachaID)
		if current == nil && config.Operator {
			profile := CloneGachaProfiles([]gamestate.GachaProfile{config.Profile})[0]
			profile.PublicationKey = "operator"
			profile.PlayCount = s.operatorGachaPlays[profile.GachaID]
			delete(s.operatorGachaPlays, profile.GachaID)
			if validateGachaSelection(profile, s.gachaSelections[profile.GachaID]) != nil {
				delete(s.gachaSelections, profile.GachaID)
			}
			s.gachas = append(s.gachas, profile)
			continue
		}
		if current == nil || isOnboardingGachaID(current.GachaID) {
			continue
		}
		profile := CloneGachaProfiles([]gamestate.GachaProfile{config.Profile})[0]
		if config.Operator {
			profile.PublicationKey = "operator"
		}
		profile.PlayCount = current.PlayCount
		*current = profile
		if validateGachaSelection(profile, s.gachaSelections[profile.GachaID]) != nil {
			delete(s.gachaSelections, profile.GachaID)
		}
	}
	s.gachaWindows = make(map[int][2]int64, len(configs))
	for _, config := range configs {
		s.gachaWindows[config.Profile.GachaID] = [2]int64{config.StartUnix, config.EndUnix}
		if config.Disabled {
			s.gachaWindows[config.Profile.GachaID] = [2]int64{0, 1}
		}
	}
	s.gachaRevision = revision
}

func (s *Account) gachaScheduledLocked(id int) bool {
	window, configured := s.gachaWindows[id]
	if !configured {
		return true
	}
	now := time.Now().Unix()
	return now >= window[0] && (window[1] == 0 || now < window[1])
}

// PreviewGachaWeights shares the integer rounding used by GachaOddsShow.
// Values are percentages multiplied by 100000 and always total 10000000.
func PreviewGachaWeights(weights []int) ([]int, error) { return scaledGachaOdds(weights) }

type GachaOddsStage struct {
	Name      string             `json:"name"`
	Rarity    int                `json:"rarity,omitempty"`
	DrawCount int                `json:"draw_count"`
	CardIDs   []int              `json:"card_ids"`
	Odds      []int              `json:"odds_scaled"`
	Rewards   []gamestate.Reward `json:"rewards,omitempty"`
}

// PreviewGachaStages follows playGacha's two disjoint rarity pools. Both the
// client odds dialog and the operator preview consume these distributions.
func PreviewGachaStages(profile gamestate.GachaProfile, rarities map[int]int) ([]GachaOddsStage, error) {
	profile = profile.CurrentStep()
	if len(profile.RewardPool) > 0 {
		weights := make([]int, len(profile.RewardPool))
		for i, entry := range profile.RewardPool {
			weights[i] = entry.Weight
		}
		odds, err := scaledGachaOdds(weights)
		if err != nil {
			return nil, err
		}
		return []GachaOddsStage{{Name: profile.Name, DrawCount: profile.CardNum, Rewards: GachaPoolRewards(profile), Odds: odds}}, nil
	}
	if len(profile.CardIDs) != len(profile.CardWeights) {
		return nil, errors.New("gacha card and weight lengths differ")
	}
	rules := [][2]int{{0, profile.CardNum}}
	if profile.GuaranteedCount > 0 {
		rules = [][2]int{{profile.GuaranteedRarityRank, profile.GuaranteedCount}, {profile.RemainderRarityRank, profile.CardNum - profile.GuaranteedCount}}
	}
	stages := make([]GachaOddsStage, 0, len(rules))
	for _, rule := range rules {
		stage := GachaOddsStage{Name: profile.Name, Rarity: rule[0], DrawCount: rule[1]}
		weights := []int{}
		for i, id := range profile.CardIDs {
			if rule[0] == 0 || rarities[id] == rule[0] {
				stage.CardIDs = append(stage.CardIDs, id)
				weights = append(weights, profile.CardWeights[i])
			}
		}
		if rule[0] != 0 {
			stage.Name = fmt.Sprintf("%s · 稀有度 %d · %d 抽", profile.Name, rule[0], rule[1])
		}
		var err error
		stage.Odds, err = scaledGachaOdds(weights)
		if err != nil {
			return nil, err
		}
		stages = append(stages, stage)
	}
	return stages, nil
}

func (s *Account) GachaOddsStages(profile gamestate.GachaProfile) ([]GachaOddsStage, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rarities := make(map[int]int, len(profile.CardIDs))
	for _, id := range profile.CardIDs {
		rarities[id] = s.cardDefinitions[id].RarityRank
	}
	return PreviewGachaStages(profile, rarities)
}
