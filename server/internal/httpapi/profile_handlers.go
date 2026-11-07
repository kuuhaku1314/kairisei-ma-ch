package httpapi

import (
	"encoding/json"
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) honorDeckShow(writer http.ResponseWriter, _ *http.Request) {
	deckHonorIDs, _ := a.account.HonorState()
	a.writeProtocol(writer, map[string]any{"deck_honorids": deckHonorIDs})
}

func (a *API) honorShow(writer http.ResponseWriter, _ *http.Request) {
	deckHonorIDs, honorIDs := a.account.HonorState()
	a.writeProtocol(writer, map[string]any{
		"deck_honorids": deckHonorIDs,
		"honorids":      honorIDs,
		"new_honorids":  []int{},
	})
}

func (a *API) honorDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		DeckHonorIDs []int `json:"deck_honorids"`
	}
	if err := decodeExact(request, []string{"deck_honorids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SetHonorDeck(payload.DeckHonorIDs) {
		writeError(writer, http.StatusBadRequest, "invalid honor deck")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) userCreate(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name       string `json:"name"`
		ArthurType int8   `json:"arthur_type"`
	}
	if err := decodeExact(request, []string{"name", "arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.CreateUser(payload.Name, payload.ArthurType) {
		writeError(writer, http.StatusBadRequest, "user is already created or creation fields are invalid")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	_, pushOptionFlag := a.account.OptionState()
	a.writeProtocol(writer, map[string]any{
		"user":        a.userPayload(),
		"push_option": map[string]int{"enable_flag": pushOptionFlag},
	})
}

func (a *API) userSetName(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Name string `json:"name"`
	}
	if err := decodeExact(request, []string{"name"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SetUserName(payload.Name) {
		writeError(writer, http.StatusBadRequest, "name must not be empty")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) userSetComment(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Comment string `json:"comment"`
	}
	if err := decodeExact(request, []string{"comment"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SetUserComment(payload.Comment) {
		writeError(writer, http.StatusBadRequest, "comment exceeds the official client byte limit")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) cardCollectionShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	pages, count, total := a.account.CardCollection()
	a.writeProtocol(writer, map[string]any{
		"pages":         pages,
		"find_card_num": count,
		"find_card_max": total,
	})
}

func (a *API) supportCardSlotUnlock(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ArthurType int8 `json:"arthur_type"`
	}
	if err := decodeExact(request, []string{"arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	unlocked, gold, err := a.account.UnlockSupportCardSlot(payload.ArthurType)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	slots := make([]map[string]any, 4)
	for index := range slots {
		slots[index] = map[string]any{
			"arthur_type":     index + 1,
			"unlock_slot_num": unlocked[index],
		}
	}
	a.writeProtocol(writer, map[string]any{
		"support_card_slots": slots,
		"gold":               gold,
	})
}

func (a *API) userPayload() map[string]any {
	state := a.initialState
	user := state.User
	leaderUniqueID, leaderCardID, leaderFound := a.account.ActiveArthurLeaderState()
	if !leaderFound {
		leaderUniqueID = user.LeaderCardUniqueID
		leaderCardID = user.LeaderCardID
	}
	ap := a.account.ApState()
	bp := a.account.BattlePointState()
	progression := a.account.PlayerProgressionState()
	coin, coinFree := a.account.CoinState()
	cardCapacity := a.account.CardCapacity()
	containerCapacity := a.account.CardContainerCapacity()
	supportDeckSetCardNum, _ := a.account.SupportDeckState()
	jobs := make([]map[string]int, len(progression.Jobs))
	for index, job := range progression.Jobs {
		jobs[index] = map[string]int{
			"hp":   job.HP,
			"atkp": job.Attack,
			"intp": job.Magic,
			"mndp": job.Mind,
		}
	}
	return map[string]any{
		"userid":                       user.UserID,
		"name":                         a.account.UserName(),
		"active_arthur_type":           a.account.ActiveArthurType(),
		"arthur_rank":                  a.account.MaximumDeckRank(),
		"lv":                           progression.Level,
		"exp":                          progression.Experience,
		"now_lv_exp":                   progression.NowLevelExperience,
		"next_lv_exp":                  progression.NextLevelExperience,
		"fame":                         0,
		"leader_card_uniqid":           leaderUniqueID,
		"leader_cardid":                leaderCardID,
		"comment":                      a.account.UserComment(),
		"ap":                           ap.Current,
		"ap_max":                       ap.Max,
		"ap_next_sec":                  ap.NextSeconds,
		"ap_heal_sec":                  ap.IntervalSeconds,
		"bp":                           bp.Current,
		"bp_max":                       bp.Max,
		"bp_next_sec":                  bp.NextSeconds,
		"bp_heal_sec":                  bp.IntervalSeconds,
		"card_max_extend":              max(0, cardCapacity-cardCapacityBase),
		"card_num":                     a.account.CardCount(),
		"card_max":                     cardCapacity,
		"card_extend_limit":            game.CardCapacityLimit - cardCapacityBase,
		"card_container_num":           a.account.ContainerCardCount(),
		"card_container_max_extend":    max(0, containerCapacity-cardCapacityBase),
		"card_container_max":           containerCapacity,
		"card_container_extend_limit":  game.CardCapacityLimit - cardCapacityBase,
		"sphr_num":                     a.account.SphereCount(),
		"sphr_max":                     user.SphereMax,
		"follow_max":                   progression.FollowMax,
		"friend_max_extend":            0,
		"friend_extend_limit":          0,
		"gold":                         a.account.GoldState(),
		"fp":                           a.account.FriendPointState(),
		"coin":                         coin,
		"coin_free":                    coinFree,
		"enter_state":                  0,
		"navi_type":                    a.account.NaviID(),
		"inviteid":                     user.InviteID,
		"jobs":                         jobs,
		"deck_max_per_arthur":          user.DeckMaxPerArthur,
		"deck_max_alchemist":           user.DeckMaxAlchemist,
		"tag_name":                     "",
		"total_cost":                   0,
		"support_deck_set_card_num":    supportDeckSetCardNum,
		"rookie_type":                  0,
		"fresher_left_time":            0,
		"rookie_point":                 0,
		"punished_free_point":          0,
		"punished_free_point_max":      0,
		"punished_free_point_next_sec": 0,
		"punished_free_point_heal_sec": 0,
		"bridgeid":                     "",
		"buddy_num":                    a.account.BuddyCount(),
		"buddy_max":                    user.BuddyMax,
	}
}

func (a *API) deckLimitShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, map[string]any{"infos": []any{}})
}

func (a *API) costumeShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, a.account.CostumeState())
}

func (a *API) costumeSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ArthurType int8 `json:"arthur_type"`
		CostumeID  int  `json:"costumeid"`
	}
	if err := decodeExact(
		request,
		[]string{"arthur_type", "costumeid"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SelectCostume(payload.ArthurType, payload.CostumeID) {
		writeError(writer, http.StatusBadRequest, "unknown costume selection")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"arthur_type": payload.ArthurType,
		"costumeid":   payload.CostumeID,
	})
}

func (a *API) setTutorialFlag(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var fields map[string]json.RawMessage
	if err := decodeExact(request, []string{"flag"}, &fields); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var flag int64
	if err := json.Unmarshal(fields["flag"], &flag); err != nil {
		writeError(writer, http.StatusBadRequest, "flag must be an integer")
		return
	}
	if !a.account.MergeTutorialFlag(flag) {
		writeError(writer, http.StatusBadRequest, "invalid tutorial flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) updateGameOption(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		GameOption struct {
			EnableFlag int `json:"enable_flag"`
		} `json:"game_option"`
	}
	if err := decodeExact(request, []string{"game_option"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SetGameOption(payload.GameOption.EnableFlag) {
		writeError(writer, http.StatusBadRequest, "invalid game option flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) updatePushOption(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		Option struct {
			EnableFlag int `json:"enable_flag"`
		} `json:"option"`
	}
	if err := decodeExact(request, []string{"option"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SetPushOption(payload.Option.EnableFlag) {
		writeError(writer, http.StatusBadRequest, "invalid push option flag")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) naviSelect(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		NaviType int8 `json:"navi_type"`
	}
	if err := decodeExact(request, []string{"navi_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.SelectNavi(payload.NaviType) {
		writeError(writer, http.StatusBadRequest, "unknown navigator ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) getNaviShow(writer http.ResponseWriter, _ *http.Request) {
	// Navi names, models and presentation remain client-local in navi.csv. The
	// server supplies the official catalog IDs plus acquisition state from the
	// player save. Historical service prices are absent, so the replaceable local
	// runtime profile owns the crystal price used for unowned entries.
	catalogIDs := a.initialState.User.NaviCatalogIDs
	if len(catalogIDs) == 0 {
		catalogIDs = a.initialState.User.SelectableNaviIDs
	}
	acquired := a.account.NaviOwnershipState()
	prices := a.account.NaviPrices()
	navigators := make([]map[string]any, len(catalogIDs))
	for index, naviID := range catalogIDs {
		setting := prices[naviID]
		isGet := 0
		getType := 4
		notice := "暂未开放购买"
		orgValue := 0
		getValue := 0
		if _, exists := acquired[naviID]; exists {
			isGet = 1
			getType = 0
			notice = ""
		} else if setting.Enabled {
			getType = 0
			orgValue = setting.Price
			getValue = setting.Price
			notice = ""
		}
		navigators[index] = map[string]any{
			"navi_id":      naviID,
			"order":        len(catalogIDs) - index,
			"is_hot":       0,
			"get_type":     getType,
			"org_value":    orgValue,
			"get_value":    getValue,
			"begin_time":   0,
			"end_time":     0,
			"exchangeitem": 0,
			"now":          0,
			"is_get":       isGet,
			"notice":       notice,
		}
	}
	a.writeProtocol(writer, map[string]any{"NaviList": navigators})
}

func (a *API) stampShow(writer http.ResponseWriter, _ *http.Request) {
	stampIDs, deckStampIDs := a.account.StampState()
	a.writeProtocol(writer, map[string]any{
		"stampids": stampIDs,
		"deck": map[string]any{
			"stampids": deckStampIDs,
		},
	})
}

func (a *API) stampDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Deck struct {
			StampIDs []int `json:"stampids"`
		} `json:"deck"`
	}
	if err := decodeExact(request, []string{"deck"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.SetStampDeck(payload.Deck.StampIDs); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}
