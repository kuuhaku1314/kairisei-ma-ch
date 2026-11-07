package httpapi

import (
	"crypto/sha1"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) gachaShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ShowType int `json:"show_type"`
	}
	if err := decodeExact(request, []string{"show_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.ShowType != 0 && payload.ShowType != 1 {
		writeError(writer, http.StatusBadRequest, "invalid gacha show type")
		return
	}
	gachas := a.account.GachaState()
	a.writeProtocol(writer, map[string]any{
		"gacha_category_list":   gachaCategories(gachas),
		"gacha_list":            a.gachaInfos(gachas),
		"is_valid_lineup_cache": 0,
		"user":                  a.userPayload(),
	})
}

func (a *API) gachaPlay(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID      int                `json:"gachaid"`
		PayType      int                `json:"pay_type"`
		GachaHash    string             `json:"gacha_hash"`
		SelectLineup []gamestate.Reward `json:"select_lineup_list"`
		PopupID      int                `json:"popupid"`
	}
	if err := decodeExact(request, []string{
		"gachaid", "pay_type", "gacha_hash", "select_lineup_list", "popupid",
	}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.GachaHash == "" {
		writeError(writer, http.StatusBadRequest, "gacha hash is empty")
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, game.ErrGachaUnavailable)
		return
	}
	result, err := a.account.PlayGacha(payload.GachaID, payload.PayType, payload.SelectLineup)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards := make([]any, len(result.Reward.Rewards))
	rewardAdds := make([]int, len(result.Reward.Rewards))
	for index, received := range result.Reward.Rewards {
		if received.InPresentBox {
			rewardAdds[index] = 1
		}
		uniqueIDs := received.UniqueID
		if len(uniqueIDs) == 0 && !received.InPresentBox {
			// GachaResult reads uniqid[0] for direct rewards, including items
			// without an instance ID. Its native null-array fallback is ID 0.
			uniqueIDs = []int64{0}
		}
		rewards[index] = map[string]any{
			"reward":           received.Reward,
			"uniqid":           uniqueIDs,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
	}
	a.writeProtocol(writer, map[string]any{
		"cards":              a.toWireCards(result.Reward.Cards),
		"stack_cards":        toWireStackCards(result.Reward.StackCards),
		"sphrs":              toWireSpheres(result.Reward.Spheres),
		"new_items":          a.itemInfosWire(gachaItemDeltas(result.Reward)),
		"buddys":             toWireBuddies(result.Reward.Buddies),
		"user":               a.userPayload(),
		"item":               a.itemInfosWire([]gamestate.Item{result.Item})[0],
		"gacha_list":         a.gachaInfos(result.Gachas),
		"gift_direct":        []gamestate.Reward{},
		"gift_present":       result.PresentGifts,
		"rewards":            rewards,
		"reward_adds":        rewardAdds,
		"expectancy":         result.Expectancy,
		"cutin":              []any{},
		"auto_fusion_result": []any{},
		"auto_loveup_result": []any{},
	})
}

const cnGachaItemHashSalt = "R-.m(NjG8!-v"

func (a *API) gachaItemPlay(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ItemID    int    `json:"itemid"`
		PlayCount int    `json:"play_count"`
		GachaHash string `json:"gacha_hash"`
	}
	if err := decodeExact(request, []string{"itemid", "play_count", "gacha_hash"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	hashInput := strconv.Itoa(a.initialState.User.UserID) + cnGachaItemHashSalt
	hash := sha1.Sum([]byte(hashInput))
	if payload.GachaHash != base64.StdEncoding.EncodeToString(hash[:]) {
		writeError(writer, http.StatusBadRequest, "invalid item gacha hash")
		return
	}
	result, err := a.account.PlayItemGacha(payload.ItemID, payload.PlayCount)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	rewards, presentBoxCards := []any{}, []any{}
	for _, received := range result.Reward.Rewards {
		entry := map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		}
		if received.InPresentBox {
			presentBoxCards = append(presentBoxCards, entry)
		} else {
			rewards = append(rewards, entry)
		}
	}
	a.writeProtocol(writer, map[string]any{
		"cards":             a.toWireCards(result.Reward.Cards),
		"stack_cards":       toWireStackCards(result.Reward.StackCards),
		"sphrs":             toWireSpheres(result.Reward.Spheres),
		"new_items":         a.itemInfosWire(gachaItemDeltas(result.Reward)),
		"buddys":            toWireBuddies(result.Reward.Buddies),
		"user":              a.userPayload(),
		"item":              a.itemInfosWire([]gamestate.Item{result.Item})[0],
		"gacha_list":        []any{},
		"gift_present":      []any{},
		"rewards":           rewards,
		"present_box_cards": presentBoxCards,
	})
}

func (a *API) gachaLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaIDs []int `json:"gachaids"`
		PopupID  int   `json:"popupid"`
	}
	if err := decodeExact(request, []string{"gachaids", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	selected := make(map[int]struct{}, len(payload.GachaIDs))
	for _, gachaID := range payload.GachaIDs {
		if !gachaPublished(request, gachaID) {
			a.writeStoreError(writer, game.ErrGachaUnavailable)
			return
		}
		selected[gachaID] = struct{}{}
	}
	gachas, ownedCardIDs := a.account.GachaStateWithOwnership()
	lineups := make([]any, 0, len(selected))
	for _, gacha := range gachas {
		if _, exists := selected[gacha.GachaID]; !exists {
			continue
		}
		pool := game.GachaPoolRewards(gacha)
		cards := make([]any, len(pool))
		for index, reward := range pool {
			isNew := int8(0)
			if reward.Type == 6 {
				isNew = game.CardNewFlag(ownedCardIDs, reward.RewardTypeID)
			}
			cards[index] = map[string]any{
				"prize":  reward,
				"is_new": isNew,
			}
		}
		lineups = append(lineups, map[string]any{
			"gachaid":           gacha.GachaID,
			"gacha_lineup_list": cards,
		})
	}
	a.writeProtocol(writer, map[string]any{"gachas": lineups})
}

func (a *API) gachaSelectLineupShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
	}
	if err := decodeExact(request, []string{"gachaid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, game.ErrGachaUnavailable)
		return
	}
	lineup, err := a.account.GachaSelectLineup(payload.GachaID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"gachaid":           payload.GachaID,
		"gacha_lineup_list": gachaLineupEntriesWire(lineup),
	})
}

func (a *API) gachaSelectedListShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
	}
	if err := decodeExact(request, []string{"gachaid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, game.ErrGachaUnavailable)
		return
	}
	lineup, err := a.account.GachaSelectedLineup(payload.GachaID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"gachaid":           payload.GachaID,
		"gacha_lineup_list": gachaLineupEntriesWire(lineup),
	})
}

func gachaLineupEntriesWire(lineup []game.GachaLineupEntry) []any {
	result := make([]any, len(lineup))
	for index, entry := range lineup {
		result[index] = map[string]any{
			"prize":  entry.Prize,
			"is_new": entry.IsNew,
		}
	}
	return result
}

func (a *API) gachaOddsShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		GachaID int `json:"gachaid"`
		PopupID int `json:"popupid"`
	}
	if err := decodeExact(request, []string{"gachaid", "popupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.GachaID <= 0 {
		writeError(writer, http.StatusBadRequest, "invalid gacha ID")
		return
	}
	if !gachaPublished(request, payload.GachaID) {
		a.writeStoreError(writer, game.ErrGachaUnavailable)
		return
	}
	gachas, ownedCardIDs := a.account.GachaStateWithOwnership()
	var selected *gamestate.GachaProfile
	for index := range gachas {
		if gachas[index].GachaID == payload.GachaID {
			selected = &gachas[index]
			break
		}
	}
	if selected == nil {
		writeError(writer, http.StatusBadRequest, "unknown or unavailable gacha")
		return
	}
	stages, err := a.account.GachaOddsStages(*selected)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	lineups := make([]any, 0, len(stages))
	for _, stage := range stages {
		prizes := make([]any, len(stage.CardIDs))
		for i, id := range stage.CardIDs {
			prizes[i] = map[string]any{"prize": game.GachaCardReward(id), "is_new": game.CardNewFlag(ownedCardIDs, id), "odds": stage.Odds[i]}
		}
		if len(stage.Rewards) > 0 {
			prizes = make([]any, len(stage.Rewards))
			for i, reward := range stage.Rewards {
				isNew := int8(0)
				if reward.Type == 6 {
					isNew = game.CardNewFlag(ownedCardIDs, reward.RewardTypeID)
				}
				prizes[i] = map[string]any{"prize": reward, "is_new": isNew, "odds": stage.Odds[i]}
			}
		}
		lineups = append(lineups, map[string]any{"lineup_name": stage.Name, "select_num": stage.DrawCount, "prize_list": prizes})
	}
	a.writeProtocol(writer, map[string]any{"odds_msg": selected.OddsMessage, "lineup_infos": lineups})
}

func gachaCategories(gachas []gamestate.GachaProfile) []any {
	seen := make(map[int]struct{}, len(gachas))
	result := make([]any, 0, len(gachas))
	for _, gacha := range gachas {
		if _, exists := seen[gacha.CategoryNum]; exists {
			continue
		}
		seen[gacha.CategoryNum] = struct{}{}
		result = append(result, map[string]any{
			"category_num": gacha.CategoryNum,
			"pictid":       gacha.CategoryPictID,
		})
	}
	return result
}

func (a *API) gachaInfos(gachas []gamestate.GachaProfile) []any {
	result := make([]any, len(gachas))
	for index, gacha := range gachas {
		gacha = gacha.CurrentStep()
		bannerPath := "/local/gacha/banner.png"
		if gacha.BannerKey == "five_star_ticket" {
			bannerPath = "/local/gacha/five-star-banner.png"
		} else if gacha.BannerKey != "" {
			bannerPath = "/local/gacha/" + gacha.BannerKey + ".png"
		}
		bannerURL := a.baseURL + bannerPath
		if version := a.gachaBannerVersions[gacha.BannerKey]; version != "" {
			bannerURL += "?v=" + version
		}
		if gacha.CoverPath != "" {
			bannerURL = a.baseURL + "/local/" + gacha.CoverPath
			if base := a.account.GachaCoverBaseURL(); base != "" {
				bannerURL = base + gacha.CoverPath
			}
		}
		price := gacha.Price
		buyMessage := gacha.BuyMessage
		dailyFirst := int8(0)
		playCountOneDay := 0
		if gacha.DailyFirstAvailable {
			price = 0
			buyMessage = "今日首次友情点扭蛋免费，抽取1张骑士卡牌吗？"
			dailyFirst = 1
		} else if gacha.DailyFirstFree {
			playCountOneDay = 1
		}
		if gacha.DailyPlayMax > 0 {
			playCountOneDay = min(gacha.DailyGroupPlays, gacha.DailyPlayMax)
		}
		stepCount, isStep, isLast := 0, 0, 0
		playCount := gacha.PlayCount
		playCountMax := gacha.PlayCountMax
		if gacha.PlayCountMax > 0 {
			playCount = gacha.GroupPlayCount
		}
		// Native variable-count pools interpret this wire limit as card count.
		// Their request quota is enforced by the server without reducing batch size.
		if gacha.CardNum != gacha.CardNumMax {
			playCountMax = 0
		}
		if len(gacha.Steps) > 0 {
			stepCount, isStep = min(gacha.PlayCount+1, len(gacha.Steps)), 1
			if stepCount == len(gacha.Steps) {
				isLast = 1
			}
			buyMessage = fmt.Sprintf("第%d阶段，消耗%d，抽取%d次吗？", stepCount, price, gacha.CardNum)
		}
		result[index] = map[string]any{
			"gachaid":                   gacha.GachaID,
			"gacha_name":                gacha.Name,
			"buymsg":                    buyMessage,
			"submsg":                    gacha.SubMessage,
			"category_num":              gacha.CategoryNum,
			"order_num":                 gacha.OrderNum,
			"groupid":                   gacha.GroupID,
			"gacha_type":                gacha.GachaType,
			"arthur_type":               gacha.ArthurType,
			"pay_type":                  gacha.PayType,
			"pay_typeid":                gacha.PayTypeID,
			"price":                     price,
			"card_num":                  gacha.CardNum,
			"card_num_max":              gacha.CardNumMax,
			"play_count":                playCount,
			"play_count_priority_price": 0,
			"play_count_1day":           playCountOneDay,
			"play_count_max":            playCountMax,
			"play_count_1day_max":       gacha.DailyPlayMax,
			"user_select_max":           gacha.UserSelectMax,
			"is_stepup_price":           isStep,
			"is_daily_first":            dailyFirst,
			"image_l_url":               bannerURL,
			"image_s_url":               bannerURL,
			"info_url":                  "",
			"end_time":                  gacha.EndTime,
			"gifts":                     gachaGiftsWire(gacha),
			"fate_player_number":        0,
			"expensive_price":           0,
			"is_odds_view":              1,
			"stepup_count":              stepCount,
			"is_last_step":              isLast,
		}
	}
	return result
}
