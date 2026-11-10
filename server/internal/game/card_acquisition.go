package game

import (
	"encoding/json"
	"fmt"
	"math"
	"slices"
	"strings"
	"time"

	"kairisei.local/server/internal/gamestate"
)

// Only retain rows for the requested card IDs, rather than allocating an
// index of every obtainable card for each detail-window request.
type cardSourceIndex map[int]map[howToGetCardEntry]struct{}

// CardSourcePolicy carries the request's current publication rules. Account
// progress, schedules and reward eligibility remain owned by their sources.
type CardSourcePolicy struct {
	BattleGroups   map[int]struct{}
	GachaPublished func(int) bool
}

func (sources cardSourceIndex) add(cardID int, entry howToGetCardEntry) {
	if entries, requested := sources[cardID]; requested {
		entries[entry] = struct{}{}
	}
}

func (sources cardSourceIndex) addReward(reward gamestate.Reward, entry howToGetCardEntry) {
	if (reward.Type == 6 || reward.Type == 13) && reward.Num > 0 {
		sources.add(reward.RewardTypeID, entry)
	}
}

func (s *Account) battleCardSourcesLocked(profiles []gamestate.TeamBattleRewardProfile, allowedGroups map[int]struct{}, sources cardSourceIndex) error {
	if len(profiles) == 0 {
		return nil
	}
	var catalog *TeamBattleCatalog
	if s.teamBattleSolo != nil && s.teamBattleSolo.definitions != nil {
		var err error
		catalog, err = s.teamBattleCatalogLocked()
		if err != nil {
			return err
		}
	}
	type destination struct {
		entry howToGetCardEntry
		clear bool
		area  int
		tower int
		multi bool
	}
	destinations := map[int][]destination{}
	if catalog != nil {
		for _, key := range teamBattleCategoryKeys {
			for _, group := range catalog.groups[key] {
				if key != "9" && allowedGroups != nil {
					if _, allowed := allowedGroups[group.id]; !allowed {
						continue
					}
				}
				name, _ := group.fields["4"].(string)
				stage, _ := teamBattleCatalogInteger(group.fields["1"])
				for _, boss := range group.bosses {
					locked, _ := teamBattleCatalogInteger(boss.fields["26"])
					if locked != 0 {
						continue
					}
					difficulty, _ := boss.fields["4"].(string)
					entry := howToGetCardEntry{Type: 10, ContentID: boss.id, Text: strings.TrimSpace(name + " " + difficulty)}
					if stageType := gamestate.BattleStageType(stage); stageType == gamestate.BattleStageAlchemist || stageType == gamestate.BattleStageAlchemistSP {
						entry.Subtype, entry.ContentID = 4, group.id // ALCHEMIST
					} else if group.areaID >= cnNormalQuestAreaMin && group.areaID <= cnNormalQuestAreaMax {
						entry.Subtype, entry.ContentID = 5, group.areaID // MAINQUEST
					} else if group.areaID > 0 {
						entry.Subtype, entry.ContentID = 3, group.areaID // STAGE
					}
					only, err := teamBattleCatalogInteger(boss.fields["1"])
					if err != nil {
						return err
					}
					start, err := teamBattleCatalogInteger(boss.fields["24"])
					if err != nil {
						return err
					}
					state, _ := teamBattleCatalogInteger(boss.fields["10"])
					rules := TeamBattleEntryRules{OnlyMyDeck: int(only), StartRule: int(start), StageType: gamestate.BattleStageType(stage)}
					destinations[boss.id] = append(destinations[boss.id], destination{entry: entry, clear: state >= 2, area: group.areaID, multi: rules.AllowsMultiplayer()})
				}
			}
		}
	}
	for id, tower := range s.towerQuestProfiles {
		progress, exists := s.towerQuestProgress[id]
		if !s.towerOpenLocked(id) || !exists {
			continue
		}
		for _, floor := range tower.Floors {
			var boss struct {
				ID int `json:"0"`
			}
			if err := json.Unmarshal(floor.Boss, &boss); err != nil {
				return err
			}
			if s.disabledTeamBattleBossIDs[boss.ID] {
				continue
			}
			subtype := 1 // TOWER
			if tower.StageType == gamestate.BattleStageAlchemist || tower.StageType == gamestate.BattleStageAlchemistSP {
				subtype = 4 // ALCHEMIST
			}
			entry := howToGetCardEntry{Type: 10, Subtype: subtype, ContentID: id, Text: fmt.Sprintf("%s 第%d层", tower.Name, floor.Floor)}
			destinations[boss.ID] = append(destinations[boss.ID], destination{entry: entry, tower: id, clear: slices.Contains(progress.ClearedFloors, floor.Floor)})
		}
	}
	add := sources.addReward
	for _, profile := range profiles {
		for _, target := range destinations[profile.BossID] {
			if profile.TowerID != target.tower || profile.StageQuestAreaID != target.area {
				continue
			}
			// Follow the settlement contract: explicit part drops replace the
			// aggregate tangible rewards, including an explicitly empty list.
			if profile.EnemyDrops == nil {
				for _, reward := range profile.ResultRewards {
					add(reward, target.entry)
				}
			} else {
				for _, drop := range profile.EnemyDrops {
					if drop.ChancePerMillion == nil || *drop.ChancePerMillion > 0 {
						add(drop.Reward, target.entry)
					}
				}
			}
			if s.teamBattleFameBonus.ChanceMaximum > 0 {
				entry := target.entry
				entry.Text += "（名声奖励）"
				for _, reward := range TeamBattleFamePool(profile, s.teamBattleFameBonus) {
					add(reward, entry)
				}
			}
			if !target.clear {
				entry := target.entry
				entry.Text += "（首次通关）"
				for _, reward := range profile.FirstClearRewards {
					add(reward, entry)
				}
			}
			if policy := profile.ScorePolicy; policy != nil {
				claimed := s.teamBattleScores[profile.BossID].Claimed
				for grade, rewards := range policy.Grades {
					if claimed&(uint16(1)<<grade) != 0 {
						continue
					}
					entry := target.entry
					entry.Text += fmt.Sprintf("（圣剑杯第%d档奖励）", grade+1)
					for _, reward := range rewards.RewardList() {
						add(reward, entry)
					}
				}
			}
			if s.teamBattleHostBonus.RewardCount > 0 && target.multi {
				for _, reward := range profile.ResultRewards {
					if slices.Contains(s.teamBattleHostBonus.EligibleRewardTypes, reward.Type) {
						entry := target.entry
						entry.Text += "（组队房主奖励）"
						add(reward, entry)
						break
					}
				}
			}
		}
	}
	return nil
}

func (s *Account) gachaCardSourcesLocked(sources cardSourceIndex, published func(int) bool) {
	for _, gacha := range s.VisibleGachasLocked() {
		if published != nil && !published(gacha.GachaID) {
			continue
		}
		remaining := math.MaxInt - gacha.PlayCount
		if gacha.PlayCountMax > 0 {
			remaining = min(remaining, gacha.PlayCountMax-gacha.GroupPlayCount)
		}
		if remaining <= 0 {
			continue
		}
		entry := howToGetCardEntry{Type: 7, ContentID: gacha.GachaID, Text: gacha.Name}
		addPool := func(pool []gamestate.WeightedReward, row howToGetCardEntry) {
			for _, weighted := range pool {
				if weighted.Weight > 0 {
					sources.addReward(weighted.Reward, row)
				}
			}
		}
		if len(gacha.Steps) > 0 {
			for step := min(gacha.PlayCount, len(gacha.Steps)-1); step < len(gacha.Steps); step++ {
				if max(0, step-gacha.PlayCount) >= remaining {
					break
				}
				row := entry
				row.Text += fmt.Sprintf("（第%d阶段）", step+1)
				addPool(gacha.Steps[step].RewardPool, row)
			}
		} else if len(gacha.RewardPool) > 0 {
			addPool(gacha.RewardPool, entry)
		} else {
			for i, id := range gacha.CardIDs {
				if gacha.GuaranteedCount > 0 {
					rank := s.cardDefinitions[id].RarityRank
					if rank != gacha.GuaranteedRarityRank && rank != gacha.RemainderRarityRank {
						continue
					}
				}
				if i < len(gacha.CardWeights) && gacha.CardWeights[i] > 0 {
					sources.add(id, entry)
				}
			}
		}
		for _, gift := range gacha.Gifts {
			if gift.FromPlay > gacha.PlayCount+remaining || (gift.ToPlay > 0 && gift.ToPlay <= gacha.PlayCount) {
				continue
			}
			row := entry
			row.Text += "（附赠奖励）"
			for _, reward := range gift.Rewards {
				sources.addReward(reward, row)
			}
		}
		for _, gift := range gacha.RandomGifts {
			if gift.FromPlay > gacha.PlayCount+remaining || (gift.ToPlay > 0 && gift.ToPlay <= gacha.PlayCount) {
				continue
			}
			row := entry
			row.Text += "（随机附赠奖励）"
			addPool(gift.Pool, row)
		}
	}
}

func (s *Account) exchangeCardSourcesLocked(sources cardSourceIndex) {
	now := time.Now().Unix()
	for _, shop := range s.tradeShopProfiles {
		if !tradeShopOpen(shop, now) {
			continue
		}
		// Native CardExchangeMgr.IsShop expects the shop ID, not lineup/card ID.
		entry := howToGetCardEntry{Type: 8, ContentID: shop.TradeShopID, Text: shop.Name}
		for _, lineup := range shop.Lineups {
			if lineup.Disabled || (lineup.StockNum > 0 && s.tradeShopPurchases[lineup.LineupID] >= lineup.StockNum) {
				continue
			}
			for _, reward := range lineup.Rewards {
				sources.addReward(reward, entry)
			}
		}
	}
}

func (s *Account) missionCardSourcesLocked(sources cardSourceIndex) {
	if s.missionCatalog == nil {
		return
	}
	now := s.missionTimeLocked(time.Now()).Unix()
	for id, definition := range s.missionCatalog.definitions {
		progress := s.missionProgressLocked(definition, now)
		if !s.missionAvailableLocked(definition, progress, now) || progress.State == 2 {
			continue
		}
		for _, reward := range definition.Rewards {
			sources.addReward(reward, howToGetCardEntry{Type: 9, ContentID: id, Text: definition.Title})
		}
	}
}
