package httpapi

import (
	"encoding/json"
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) itemShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemType int `json:"item_type"`
	}
	if err := decodeExact(request, []string{"item_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"items":  a.itemInfosWire(a.account.ItemState()),
		"gachas": []any{},
	})
}

func (a *API) itemUse(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID int `json:"itemid"`
	}
	if err := decodeExact(request, []string{"itemid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.UseItem(payload.ItemID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	item := a.itemInfosWire([]gamestate.Item{result.Item})[0]
	a.writeProtocol(writer, map[string]any{
		"item":        item,
		"ap":          result.AP.Current,
		"ap_max":      result.AP.Max,
		"ap_next_sec": result.AP.NextSeconds,
		"bp":          result.BP.Current,
		"bp_max":      result.BP.Max,
		"bp_next_sec": result.BP.NextSeconds,
	})
}

func (a *API) itemExchange(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID     int `json:"itemid"`
		ChangeSets int `json:"change_sets"`
	}
	if err := decodeExact(request, []string{"itemid", "change_sets"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.ExchangeItem(payload.ItemID, payload.ChangeSets)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards := make([]any, len(result.Reward.Rewards))
	for index, received := range result.Reward.Rewards {
		rewards[index] = map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
	}
	a.writeProtocol(writer, map[string]any{
		"result_rewards":           rewards,
		"user":                     a.userPayload(),
		"new_cards":                a.toWireCards(result.Reward.Cards),
		"new_stack_cards":          toWireStackCards(result.Reward.StackCards),
		"new_items":                a.itemInfosWire(result.Reward.Items),
		"new_sphrs":                toWireSpheres(result.Reward.Spheres),
		"new_buddys":               toWireBuddies(result.Reward.Buddies),
		"use_item":                 a.itemInfosWire([]gamestate.Item{result.Item})[0],
		"is_reward_in_present_box": 0,
	})
}

func (a *API) itemLackTips(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Index int `json:"idx"`
	}
	if err := decodeExact(request, []string{"idx"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	profile, err := a.account.ItemLackTipState(payload.Index)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"title":        profile.Title,
		"desc":         profile.Description,
		"way":          profile.Way,
		"txt_url_info": profile.TextURLs,
	})
}

func (a *API) itemShopShow(writer http.ResponseWriter, _ *http.Request) {
	tabs, owned := a.account.ItemShopState()
	a.writeProtocol(writer, map[string]any{
		"tabs": itemShopTabsWire(tabs, owned),
		"user": a.userPayload(),
	})
}

func (a *API) itemShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID int `json:"item_shop_lineupid"`
		BuyNum   int `json:"buy_num"`
		PopupID  int `json:"popupid"`
	}
	if err := decodeExact(request, []string{"item_shop_lineupid", "buy_num", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	updated, err := a.account.BuyItemShop(payload.LineupID, payload.BuyNum)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	tabs, owned := a.account.ItemShopState()
	coin, coinFree := a.account.CoinState()
	a.writeProtocol(writer, map[string]any{
		"items":                  a.itemInfosWire(updated.Items),
		"new_stampids":           updated.StampIDs,
		"pay_item":               []any{},
		"is_item_in_present_box": 0,
		"user":                   a.userPayload(),
		"tabs":                   itemShopTabsWire(tabs, owned),
		"coin":                   coin,
		"coin_free":              coinFree,
	})
}

func (a *API) eventShopShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		EventID int `json:"eventid"`
	}
	if err := decodeExact(request, []string{"eventid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	shop, err := a.account.EventShopState(payload.EventID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{"shops": eventShopsWire(shop)})
}

func (a *API) eventShopBuy(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		LineupID int `json:"event_shop_lineupid"`
	}
	if err := decodeExact(request, []string{"event_shop_lineupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.BuyEventShop(payload.LineupID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"res_code":        0,
		"res_str":         "",
		"use_item":        a.itemInfosWire([]gamestate.Item{result.UseItem})[0],
		"buy_lineup_name": result.LineupName,
		"buy_price":       result.Price,
		"shops":           eventShopsWire(result.Shop),
	})
}

func (a *API) tradeShopShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{
		"shops": tradeShopsWire(a.account.TradeShopState()),
	})
}

func (a *API) tradeShopLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		TradeShopID int `json:"trade_shopid"`
	}
	if err := decodeExact(request, []string{"trade_shopid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	found := false
	for _, shop := range a.account.TradeShopState() {
		if shop.Profile.TradeShopID == payload.TradeShopID {
			found = true
			break
		}
	}
	if !found {
		writeError(writer, http.StatusBadRequest, "trade shop is unavailable")
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) tradeShopBuy(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || (len(fields) != 2 && len(fields) != 3) {
		writeError(writer, http.StatusBadRequest, "request fields differ from the endpoint contract")
		return
	}
	if _, exists := fields["lineupid"]; !exists {
		writeError(writer, http.StatusBadRequest, "request is missing lineupid")
		return
	}
	if _, exists := fields["num"]; !exists {
		writeError(writer, http.StatusBadRequest, "request is missing num")
		return
	}
	if len(fields) == 3 {
		if _, exists := fields["uniqids"]; !exists {
			writeError(writer, http.StatusBadRequest, "request fields differ from the endpoint contract")
			return
		}
	}
	var payload struct {
		LineupID int     `json:"lineupid"`
		UniqueID []int64 `json:"uniqids"`
		Num      int     `json:"num"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(writer, http.StatusBadRequest, "decode trade shop purchase")
		return
	}
	result, err := a.account.BuyTradeShop(payload.LineupID, payload.Num, payload.UniqueID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"buy_lineup_name": result.LineupName,
		"shops":           tradeShopsWire(result.Shops),
		"decks":           toWireDecks(result.Decks),
		"user":            a.userPayload(),
	})
}

func (a *API) itemInfosWire(items []gamestate.Item) []any {
	profiles := a.account.ItemExchangeProfileState()
	result := make([]any, len(items))
	for index, item := range items {
		exchange := map[string]any{
			"is_appear_event": 0,
			"eventid":         0,
			"need_num":        0,
			"reward": gamestate.Reward{
				CardSkillLevels: []int16{},
			},
		}
		if profile, exists := profiles[item.ItemID]; exists {
			exchange = map[string]any{
				"is_appear_event": profile.IsAppearEvent,
				"eventid":         profile.EventID,
				"need_num":        profile.NeedNum,
				"reward":          profile.Reward,
			}
		}
		result[index] = map[string]any{
			"itemid":     item.ItemID,
			"num":        item.Num,
			"limit_time": item.LimitTime,
			"exchange":   exchange,
		}
	}
	return result
}

func itemShopTabsWire(tabs []gamestate.ItemShopTab, owned map[[2]int]int) []any {
	result := make([]any, len(tabs))
	for tabIndex, tab := range tabs {
		lineups := make([]any, 0, len(tab.Lineup))
		for _, lineup := range tab.Lineup {
			if lineup.Hidden || lineup.Disabled {
				continue
			}
			interiors := make([]any, len(lineup.Interiors))
			for interiorIndex, interior := range lineup.Interiors {
				interiors[interiorIndex] = map[string]any{
					"buy_type":   interior.BuyType,
					"buy_typeid": interior.BuyTypeID,
					"num":        interior.Num,
					"own_num":    owned[[2]int{interior.BuyType, interior.BuyTypeID}],
				}
			}
			lineups = append(lineups, map[string]any{
				"item_shop_lineupid": lineup.LineupID,
				"pictid":             lineup.PictID,
				"lineup_name":        lineup.LineupName,
				"pay_type":           lineup.PayType,
				"pay_typeid":         lineup.PayTypeID,
				"price":              lineup.Price,
				"stock_num":          lineup.StockNum,
				"stock_remain_num":   lineup.StockRemain,
				"stock_type":         lineup.StockType,
				"appear_end":         lineup.AppearEnd,
				"note":               lineup.Note,
				"buy_num_max":        lineup.BuyNumMax,
				"interiors":          interiors,
			})
		}
		result[tabIndex] = map[string]any{
			"tab_type": tab.TabType,
			"lineup":   lineups,
		}
	}
	return result
}

func eventShopsWire(shop game.EventShopState) []any {
	lineups := make([]any, len(shop.Lineups))
	for index, state := range shop.Lineups {
		lineup := state.Profile
		lineups[index] = map[string]any{
			"event_shop_lineupid": lineup.LineupID,
			"lineup_name":         lineup.Name,
			"image_url":           lineup.ImageURL,
			"price":               lineup.Price,
			"stock_num":           lineup.StockNum,
			"stock_remain_num":    state.StockRemain,
			"stock_type":          lineup.StockType,
			"is_feature":          lineup.IsFeature,
		}
	}
	return []any{map[string]any{
		"eventid":        shop.EventID,
		"point_itemid":   shop.PointItemID,
		"point_item_num": shop.PointItemNum,
		"lineups":        lineups,
	}}
}

func tradeShopsWire(shops []game.TradeShopState) []any {
	result := make([]any, len(shops))
	for shopIndex, state := range shops {
		profile := state.Profile
		lineups := make([]any, len(state.Lineups))
		for lineupIndex, lineupState := range state.Lineups {
			lineup := lineupState.Profile
			lineups[lineupIndex] = map[string]any{
				"lineupid":         lineup.LineupID,
				"lineup_name":      lineup.LineupName,
				"stock_num":        lineup.StockNum,
				"stock_remain_num": lineupState.StockRemain,
				"is_lineup_new":    lineup.IsLineupNew,
				"is_lineup_old":    lineup.IsLineupOld,
				"is_new":           lineupState.IsNew,
				"pictid":           lineup.PictID,
				"prices":           lineup.Prices,
				"reward":           lineup.Rewards,
			}
		}
		result[shopIndex] = map[string]any{
			"trade_shopid": profile.TradeShopID,
			"name":         profile.Name,
			"text":         profile.Text,
			"shop_type":    profile.ShopType,
			"tab_type":     profile.TabType,
			"end_time":     profile.EndTime,
			"is_new":       profile.IsNew,
			"pictid":       profile.PictID,
			"lineups":      lineups,
			"owns":         state.Owns,
		}
	}
	return result
}
