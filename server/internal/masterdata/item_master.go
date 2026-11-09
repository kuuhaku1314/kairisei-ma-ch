package masterdata

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"kairisei.local/server/internal/gamestate"
)

const maxItemMasterBytes = 2 * 1024 * 1024

type ItemRuntimeMaster struct {
	SchemaVersion int    `json:"schema_version"`
	ClientProfile string `json:"client_profile"`

	LocalAccountConfigVersion int                             `json:"local_account_config_version,omitempty"`
	LocalAccountInitialItems  []gamestate.Item                `json:"local_account_initial_items,omitempty"`
	TeamBattleMedalItemID     int                             `json:"team_battle_medal_item_id,omitempty"`
	UserBuffProfiles          []gamestate.UserBuffProfile     `json:"user_buff_profiles"`
	Items                     []gamestate.ItemDefinition      `json:"items"`
	ItemGachaProfiles         []gamestate.ItemGachaProfile    `json:"item_gacha_profiles"`
	ItemGachaSourceGapIDs     []int                           `json:"item_gacha_source_gap_ids"`
	ItemExchangeProfiles      []gamestate.ItemExchangeProfile `json:"item_exchange_profiles"`

	ItemExchangePolicy  itemExchangePolicy             `json:"item_exchange_policy"`
	ItemLackTipProfiles []gamestate.ItemLackTipProfile `json:"item_lack_tip_profiles"`

	ItemLackTipPolicy itemLackTipPolicy            `json:"item_lack_tip_policy"`
	EventShopProfiles []gamestate.EventShopProfile `json:"event_shop_profiles"`

	TradeShopProfiles      []gamestate.TradeShopProfile `json:"trade_shop_profiles"`
	LocalTradeShopProfiles []gamestate.TradeShopProfile `json:"local_trade_shop_profiles,omitempty"`
	TradeShopPolicy        tradeShopPolicy              `json:"trade_shop_policy"`

	SkippedInactiveItemIDs []int `json:"skipped_inactive_item_ids"`
}

type tradeShopPolicy struct {
	ConfigVersion       int    `json:"config_version"`
	CurrencyItemID      int    `json:"currency_item_id"`
	PricePerCard        int    `json:"price_per_card"`
	StockPerCard        int    `json:"stock_per_card"`
	CardAcquisitionText string `json:"card_acquisition_text"`
	Selection           string `json:"selection"`
}

type itemExchangePolicy struct {
	ConfigVersion int    `json:"config_version"`
	NeedNum       int    `json:"need_num"`
	EventIDOffset int    `json:"event_id_offset"`
	Selection     string `json:"selection"`
}

type itemLackTipPolicy struct {
	ConfigVersion         int    `json:"config_version"`
	ReachableIndices      []int  `json:"reachable_indices"`
	NavigationPublication string `json:"navigation_publication"`
}

type eventShopSourceAudit struct {
	TextAssetCount              int      `json:"textasset_count"`
	CandidatePaths              []string `json:"candidate_paths"`
	PackagedWebFiles            []string `json:"packaged_web_files"`
	PackagedEventShopCandidates []string `json:"packaged_event_shop_candidates"`
}

func LoadItemRuntimeMaster(masterPath string) (ItemRuntimeMaster, error) {
	if masterPath == "" {
		return ItemRuntimeMaster{}, errors.New("CN item runtime master path is required")
	}
	absolute, err := filepath.Abs(masterPath)
	if err != nil {
		return ItemRuntimeMaster{}, fmt.Errorf("resolve CN item runtime master: %w", err)
	}
	file, err := os.Open(absolute)
	if err != nil {
		return ItemRuntimeMaster{}, fmt.Errorf("open CN item runtime master: %w", err)
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return ItemRuntimeMaster{}, fmt.Errorf("stat CN item runtime master: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxItemMasterBytes {
		return ItemRuntimeMaster{}, errors.New("CN item runtime master must be a non-empty regular JSON file within the size limit")
	}
	decoder := json.NewDecoder(io.LimitReader(file, maxItemMasterBytes+1))
	var master ItemRuntimeMaster
	if err := decoder.Decode(&master); err != nil {
		return ItemRuntimeMaster{}, fmt.Errorf("decode CN item runtime master: %w", err)
	}
	if err := RequireJSONEOF(decoder); err != nil {
		return ItemRuntimeMaster{}, err
	}
	if err := validateItemRuntimeMaster(master); err != nil {
		return ItemRuntimeMaster{}, err
	}
	return master, nil
}

func validateItemRuntimeMaster(master ItemRuntimeMaster) error {
	if master.SchemaVersion != 9 || master.ClientProfile != "cn602-bootstrap" {
		return errors.New("CN item runtime master identity is invalid")
	}
	if len(master.Items) == 0 {
		return errors.New("CN item runtime master source is incomplete")
	}
	seen := make(map[int]struct{}, len(master.Items))
	definitions := make(map[int]gamestate.ItemDefinition, len(master.Items))
	gachaItems := make(map[int]struct{})
	for _, item := range master.Items {
		if item.ItemID <= 0 || item.Name == "" || item.PictID <= 0 ||
			item.ItemType == "" || item.MaxOwned <= 0 ||
			item.LoveUpPrice < 0 ||
			(item.DailyLimited != 0 && item.DailyLimited != 1) {
			return fmt.Errorf("invalid CN item %d", item.ItemID)
		}
		if item.ItemType == "LOVE_UP" {
			if (item.Function != "LOVEUP_NORMAL" && item.Function != "LOVEUP_ALL") ||
				item.FunctionValue <= 0 || item.LoveUpPrice <= 0 {
				return fmt.Errorf("invalid CN love-up item %d", item.ItemID)
			}
		} else if item.LoveUpPrice != 0 {
			return fmt.Errorf("non-love-up CN item %d has a love-up price", item.ItemID)
		}
		if _, exists := seen[item.ItemID]; exists {
			return fmt.Errorf("duplicate CN item %d", item.ItemID)
		}
		seen[item.ItemID] = struct{}{}
		definitions[item.ItemID] = item
		if item.ItemType == "GACHA" {
			if item.Function != "GACHA_EXEC" || item.FunctionValue <= 0 {
				return fmt.Errorf("invalid CN item gacha definition %d", item.ItemID)
			}
			gachaItems[item.ItemID] = struct{}{}
		}
	}
	if len(master.UserBuffProfiles) != 527 {
		return errors.New("CN item runtime master user-buff profile coverage is incomplete")
	}
	seenUserBuffs := make(map[int]struct{}, len(master.UserBuffProfiles))
	for _, profile := range master.UserBuffProfiles {
		if profile.UserBuffID <= 0 || profile.ItemID <= 0 || profile.RequiredNum <= 0 ||
			profile.NotEnoughErrorText == "" || profile.DurationSeconds <= 0 {
			return fmt.Errorf("invalid CN user-buff profile %d", profile.UserBuffID)
		}
		if _, exists := definitions[profile.ItemID]; !exists {
			return fmt.Errorf("CN user-buff profile %d references unknown item %d", profile.UserBuffID, profile.ItemID)
		}
		if _, duplicate := seenUserBuffs[profile.UserBuffID]; duplicate {
			return fmt.Errorf("duplicate CN user-buff profile %d", profile.UserBuffID)
		}
		seenUserBuffs[profile.UserBuffID] = struct{}{}
	}
	coveredGachaItems := make(map[int]struct{}, len(gachaItems))
	functionValues := make(map[int]struct{}, len(master.ItemGachaProfiles))
	for _, profile := range master.ItemGachaProfiles {
		definition, exists := definitions[profile.ItemID]
		if !exists || definition.ItemType != "GACHA" || definition.Function != "GACHA_EXEC" ||
			definition.FunctionValue != profile.FunctionValue || (len(profile.Rewards) == 0 && len(profile.RewardPool) == 0) {
			return fmt.Errorf("invalid CN item gacha profile %d", profile.ItemID)
		}
		if _, duplicate := coveredGachaItems[profile.ItemID]; duplicate {
			return fmt.Errorf("duplicate CN item gacha profile %d", profile.ItemID)
		}
		if _, duplicate := functionValues[profile.FunctionValue]; duplicate {
			return fmt.Errorf("duplicate CN item gacha function value %d", profile.FunctionValue)
		}
		for _, reward := range profile.Rewards {
			if gamestate.ValidateGachaReward(reward) != nil {
				return fmt.Errorf("invalid CN item gacha reward for item %d", profile.ItemID)
			}
		}
		if len(profile.RewardPool) > 0 {
			if err := gamestate.ValidateRewardPool(profile.RewardPool); err != nil {
				return err
			}
		}
		coveredGachaItems[profile.ItemID] = struct{}{}
		functionValues[profile.FunctionValue] = struct{}{}
	}
	for _, itemID := range master.ItemGachaSourceGapIDs {
		if _, exists := gachaItems[itemID]; !exists {
			return fmt.Errorf("CN item gacha source gap %d is not a GACHA item", itemID)
		}
		if _, duplicate := coveredGachaItems[itemID]; duplicate {
			return fmt.Errorf("duplicate or overlapping CN item gacha source gap %d", itemID)
		}
		coveredGachaItems[itemID] = struct{}{}
	}
	if len(coveredGachaItems) != len(gachaItems) {
		return errors.New("CN item gacha profiles and source gaps do not cover the official GACHA item set")
	}

	exchangePolicy := master.ItemExchangePolicy
	if exchangePolicy.ConfigVersion != 1 || exchangePolicy.NeedNum != 1 ||
		exchangePolicy.EventIDOffset != 60280000 ||
		exchangePolicy.Selection != "exact_card_name_else_unique_same_card_family_substring" {
		return errors.New("CN item exchange local policy is invalid")
	}
	exchangeItems := make(map[int]struct{}, len(master.ItemExchangeProfiles))
	exchangeEvents := make(map[int]struct{}, len(master.ItemExchangeProfiles))
	for _, profile := range master.ItemExchangeProfiles {
		if _, exists := definitions[profile.ItemID]; !exists ||
			(profile.IsAppearEvent != 0 && profile.IsAppearEvent != 1) ||
			profile.IsAppearEvent != 0 ||
			profile.EventID != exchangePolicy.EventIDOffset+profile.ItemID ||
			profile.NeedNum <= 0 || profile.NeedNum > 1000000000 ||
			!validItemExchangeReward(profile.Reward, definitions) {
			return fmt.Errorf("invalid CN item exchange profile %d", profile.ItemID)
		}
		if _, duplicate := exchangeItems[profile.ItemID]; duplicate {
			return fmt.Errorf("duplicate CN item exchange profile %d", profile.ItemID)
		}
		if _, duplicate := exchangeEvents[profile.EventID]; duplicate {
			return fmt.Errorf("duplicate CN item exchange event %d", profile.EventID)
		}
		exchangeItems[profile.ItemID] = struct{}{}
		exchangeEvents[profile.EventID] = struct{}{}
	}

	lackPolicy := master.ItemLackTipPolicy
	if lackPolicy.ConfigVersion != 2 ||
		len(lackPolicy.ReachableIndices) != 2 ||
		lackPolicy.ReachableIndices[0] != 2 || lackPolicy.ReachableIndices[1] != 8 ||
		lackPolicy.NavigationPublication != "NON_NAVIGATING_LOCAL_CLIENT_CONTRACT_ROW" {
		return errors.New("CN item lack-tip local policy is invalid")
	}
	crystalTicket, exists := definitions[9010]
	if !exists || crystalTicket.Name != "水晶扭蛋币" ||
		crystalTicket.ItemType != "GACHA_TICKET" ||
		!strings.Contains(crystalTicket.Description, "水晶扭蛋币商店") {
		return errors.New("CN item lack-tip crystal-ticket identity is invalid")
	}
	seenLackTips := make(map[int]struct{}, len(master.ItemLackTipProfiles))
	for _, profile := range master.ItemLackTipProfiles {
		if profile.Index < 1 || profile.Index > 8 ||
			profile.Title == "" || profile.Description == "" || profile.Way == "" ||
			profile.TextURLs == nil || len(profile.TextURLs) != 1 ||
			profile.TextURLs[0].Text != "暂不前往" || profile.TextURLs[0].URL != "" {
			return fmt.Errorf("invalid CN item lack-tip profile %d", profile.Index)
		}
		if _, duplicate := seenLackTips[profile.Index]; duplicate {
			return fmt.Errorf("duplicate CN item lack-tip profile %d", profile.Index)
		}
		seenLackTips[profile.Index] = struct{}{}
	}
	if len(seenLackTips) != 8 {
		return errors.New("CN item lack-tip local policy coverage is incomplete")
	}
	if len(master.EventShopProfiles) != 0 {
		return errors.New("CN event shop source audit is invalid")
	}
	if len(master.TradeShopProfiles) != 1 {
		return errors.New("CN trade shop source audit is invalid")
	}
	policy := master.TradeShopPolicy
	if policy.ConfigVersion != 1 || policy.CurrencyItemID != 4000 ||
		policy.PricePerCard != 1000 || policy.StockPerCard != 1 ||
		policy.CardAcquisitionText != "BOSS币交换所" ||
		policy.Selection != "lowest_rarity_then_card_id_per_official_same_card_family" {
		return errors.New("CN trade shop policy is invalid")
	}
	currency, exists := definitions[policy.CurrencyItemID]
	if !exists || currency.Name != "大硬币" || currency.ItemType != "EVENT_POINT" {
		return errors.New("CN trade shop currency identity is invalid")
	}
	lineupIDs := make(map[int]struct{})
	rewardCardIDs := make(map[int]struct{})
	for _, shop := range master.TradeShopProfiles {
		if shop.TradeShopID != 60290001 || shop.Name != "BOSS币交换所" ||
			shop.Text == "" || shop.ShopType != 1 || shop.TabType != 0 ||
			shop.EndTime != 2147483647 || shop.IsNew != 0 || shop.PictID != 0 ||
			len(shop.Lineups) != 49 {
			return fmt.Errorf("invalid CN trade shop %d", shop.TradeShopID)
		}
		for _, lineup := range shop.Lineups {
			if lineup.LineupID <= 0 || lineup.LineupName == "" || lineup.StockNum != 1 ||
				lineup.IsLineupNew != 0 || lineup.IsLineupOld != 0 || lineup.PictID != 0 ||
				len(lineup.Prices) != 1 || len(lineup.Rewards) != 1 {
				return fmt.Errorf("invalid CN trade shop lineup %d", lineup.LineupID)
			}
			if _, duplicate := lineupIDs[lineup.LineupID]; duplicate {
				return fmt.Errorf("duplicate CN trade shop lineup %d", lineup.LineupID)
			}
			lineupIDs[lineup.LineupID] = struct{}{}
			price := lineup.Prices[0]
			if price.Type != 4 || price.ID != policy.CurrencyItemID ||
				price.Num != policy.PricePerCard || price.PointCardCondition == nil ||
				len(price.PointCardCondition) != 0 {
				return fmt.Errorf("invalid CN trade shop lineup price %d", lineup.LineupID)
			}
			reward := lineup.Rewards[0]
			if reward.Type != 6 || reward.Num != 1 || reward.RewardTypeID <= 0 ||
				reward.CardLevel != 1 || reward.CardFame != 1 || reward.CardLove != 0 ||
				len(reward.CardSkillLevels) != 1 || reward.CardSkillLevels[0] != 1 {
				return fmt.Errorf("invalid CN trade shop reward %d", lineup.LineupID)
			}
			if _, duplicate := rewardCardIDs[reward.RewardTypeID]; duplicate {
				return fmt.Errorf("duplicate CN trade shop reward card %d", reward.RewardTypeID)
			}
			rewardCardIDs[reward.RewardTypeID] = struct{}{}
		}
	}
	shopIDs := map[int]bool{60290001: true}
	for _, shop := range master.LocalTradeShopProfiles {
		if shop.TradeShopID <= 60291000 || shopIDs[shop.TradeShopID] || shop.Name == "" || shop.Text == "" ||
			shop.ShopType != 1 || shop.TabType != 1 || shop.EndTime != 2147483647 ||
			shop.IsNew != 0 || shop.PictID != 0 || len(shop.Lineups) == 0 {
			return fmt.Errorf("invalid local CN trade shop %d", shop.TradeShopID)
		}
		shopIDs[shop.TradeShopID] = true
		for _, lineup := range shop.Lineups {
			if _, duplicate := lineupIDs[lineup.LineupID]; duplicate {
				return fmt.Errorf("duplicate local CN trade lineup %d", lineup.LineupID)
			}
			lineupIDs[lineup.LineupID] = struct{}{}
			if lineup.LineupID <= 60291000 || lineup.LineupName == "" || lineup.StockNum <= 0 ||
				lineup.StockNum > 999999 || lineup.PictID != 0 ||
				lineup.IsLineupNew != 0 || lineup.IsLineupOld != 0 || len(lineup.Prices) != 1 || len(lineup.Rewards) != 1 {
				return fmt.Errorf("invalid local CN trade lineup %d", lineup.LineupID)
			}
			price := lineup.Prices[0]
			if _, exists := definitions[price.ID]; !exists || price.Type != 4 || price.Num <= 0 ||
				price.PointCardCondition == nil || len(price.PointCardCondition) != 0 {
				return fmt.Errorf("invalid local CN trade price %d", lineup.LineupID)
			}
			// The domain store validates reward identities and capacity against the
			// complete item/card/stack masters before publishing these catalogs.
			reward := lineup.Rewards[0]
			if reward.Type != 6 && reward.Type != 8 && reward.Type != 13 {
				return fmt.Errorf("unsupported local CN trade reward %d", lineup.LineupID)
			}
		}
	}
	if master.LocalAccountConfigVersion < 0 ||
		(master.LocalAccountConfigVersion > 0 && len(master.LocalAccountInitialItems) == 0) {
		return errors.New("CN item runtime master local account configuration is invalid")
	}
	initialSeen := make(map[int]struct{}, len(master.LocalAccountInitialItems))
	for _, item := range master.LocalAccountInitialItems {
		if item.ItemID <= 0 || item.Num <= 0 || item.LimitTime < 0 {
			return errors.New("CN item runtime master initial item is invalid")
		}
		if _, exists := seen[item.ItemID]; !exists {
			return fmt.Errorf("CN item runtime master initial item %d is absent from official items", item.ItemID)
		}
		if _, exists := initialSeen[item.ItemID]; exists {
			return fmt.Errorf("CN item runtime master initial item %d is duplicated", item.ItemID)
		}
		initialSeen[item.ItemID] = struct{}{}
	}
	if master.TeamBattleMedalItemID < 0 {
		return errors.New("CN item runtime master team battle medal item is invalid")
	}
	if master.TeamBattleMedalItemID > 0 {
		if _, exists := seen[master.TeamBattleMedalItemID]; !exists {
			return fmt.Errorf("CN item runtime master team battle medal item %d is absent from official items", master.TeamBattleMedalItemID)
		}
	}
	return nil
}

func ApplyItemRuntimeMaster(state *gamestate.State, master ItemRuntimeMaster) (bool, error) {
	state.ItemDefinitions = append([]gamestate.ItemDefinition(nil), master.Items...)
	state.UserBuffProfiles = append([]gamestate.UserBuffProfile(nil), master.UserBuffProfiles...)
	state.ItemGachaProfiles = append([]gamestate.ItemGachaProfile(nil), master.ItemGachaProfiles...)
	state.ItemExchangeProfiles = append([]gamestate.ItemExchangeProfile(nil), master.ItemExchangeProfiles...)
	state.ItemLackTipProfiles = cloneItemLackTipProfiles(master.ItemLackTipProfiles)
	state.EventShopProfiles = append([]gamestate.EventShopProfile(nil), master.EventShopProfiles...)
	state.TradeShopProfiles = append([]gamestate.TradeShopProfile(nil), master.TradeShopProfiles...)
	state.TradeShopProfiles = append(state.TradeShopProfiles, master.LocalTradeShopProfiles...)
	state.TeamBattleMedalItemID = master.TeamBattleMedalItemID
	state.BossCoinItemID = master.TradeShopPolicy.CurrencyItemID
	available := make(map[int]struct{}, len(master.Items))
	for _, item := range master.Items {
		available[item.ItemID] = struct{}{}
	}
	changed := false
	if state.LocalAccountConfigVersion < master.LocalAccountConfigVersion {
		state.LocalAccountConfigVersion = master.LocalAccountConfigVersion
		changed = true
	}
	for _, item := range state.Items {
		if _, exists := available[item.ItemID]; !exists {
			return false, fmt.Errorf("persisted item %d is absent from official CN item master", item.ItemID)
		}
	}
	for _, tab := range state.ItemShopTabs {
		for _, lineup := range tab.Lineup {
			for _, interior := range lineup.Interiors {
				if interior.BuyType != 1 {
					continue
				}
				if _, exists := available[interior.BuyTypeID]; !exists {
					return false, fmt.Errorf("item shop lineup %d references unknown official CN item %d", lineup.LineupID, interior.BuyTypeID)
				}
			}
		}
	}
	return changed, nil
}

func cloneItemLackTipProfiles(source []gamestate.ItemLackTipProfile) []gamestate.ItemLackTipProfile {
	result := make([]gamestate.ItemLackTipProfile, len(source))
	for index, profile := range source {
		profile.TextURLs = make([]gamestate.ItemLackTipLink, len(profile.TextURLs))
		copy(profile.TextURLs, source[index].TextURLs)
		result[index] = profile
	}
	return result
}

func validItemExchangeReward(reward gamestate.Reward, definitions map[int]gamestate.ItemDefinition) bool {
	if reward.Num <= 0 || reward.CardSkillLevels == nil {
		return false
	}
	switch reward.Type {
	case 4, 10, 12:
		return reward.RewardTypeID == 0
	case 6, 13, 15, 19:
		return reward.RewardTypeID > 0
	case 8:
		_, exists := definitions[reward.RewardTypeID]
		return exists
	default:
		return false
	}
}
