package httpapi

import (
	"encoding/json"
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) cardShow(writer http.ResponseWriter, request *http.Request) {
	body, err := readBody(request)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || len(fields) != 1 {
		writeError(
			writer,
			http.StatusBadRequest,
			"CardShow2 requires one JSON field",
		)
		return
	}
	raw, exists := fields["0"]
	if !exists {
		raw, exists = fields["deck_info_type"]
	}
	var deckInfoType int
	if !exists ||
		json.Unmarshal(raw, &deckInfoType) != nil ||
		deckInfoType < 0 {
		writeError(
			writer,
			http.StatusBadRequest,
			"invalid deck_info_type",
		)
		return
	}
	cards, decks := a.account.Show()
	stackCards := a.account.StackState()
	avatars := a.account.AvatarsState()
	wireAvatars := make([]wireAvatarInfo, len(avatars))
	for index, avatar := range avatars {
		wireAvatars[index] = wireAvatarInfo{
			CostumeID:     avatar.CostumeID,
			AvatarPartIDs: append([]int(nil), avatar.AvatarPartIDs...),
		}
	}
	_, unlockedSupportSlots := a.account.SupportDeckState()
	supportSlots := make([]wireSupportSlot, 0, len(unlockedSupportSlots))
	for index, unlocked := range unlockedSupportSlots {
		supportSlots = append(supportSlots, wireSupportSlot{
			ArthurType:    int8(index + 1),
			UnlockSlotNum: unlocked,
		})
	}
	view := CardShowView{
		Cards:             a.toWireCards(cards),
		StackCards:        toWireStackCards(stackCards),
		Decks:             toWireDecks(decks),
		Avatars:           wireAvatars,
		SupportSlots:      supportSlots,
		CardCollectionNum: len(a.account.CardCollectionState()),
	}
	var method any = view
	if a.cardShowProjection != nil {
		method, err = a.cardShowProjection(view)
		if err != nil {
			writeError(writer, http.StatusInternalServerError, "project card response")
			return
		}
	}
	a.writeProtocol(writer, method)
}

func (a *API) sphereShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireSphereShow{Spheres: toWireSpheres(a.account.SphereState())})
}

func (a *API) cardContainerShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireCardContainerShow{
		Cards: a.toWireCards(a.account.ContainerShow()),
	})
}

func (a *API) cardMove(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		ToSlot        int8    `json:"to_slot"`
		MoveUniqueIDs []int64 `json:"move_uniqids"`
	}
	if err := decodeExact(request, []string{"to_slot", "move_uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	cards, decks, err := a.account.MoveCards(payload.ToSlot, payload.MoveUniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireCardMove{
		UpdatedCards: a.toWireCards(cards),
		UpdatedDecks: toWireDecks(decks),
	})
}

func (a *API) setCardLock(writer http.ResponseWriter, request *http.Request, slot int, locked bool) {
	var payload struct {
		UniqueID int64 `json:"uniqid"`
	}
	if err := decodeExact(request, []string{"uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.SetCardLock(payload.UniqueID, slot, locked); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, struct{}{})
}

func (a *API) cardLock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 0, true)
}

func (a *API) cardUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 0, false)
}

func (a *API) cardContainerLock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 1, true)
}

func (a *API) cardContainerUnlock(writer http.ResponseWriter, request *http.Request) {
	a.setCardLock(writer, request, 1, false)
}

func (a *API) cardContainerSell(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueIDs []int64 `json:"uniqids"`
	}
	if err := decodeExact(request, []string{"uniqids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.SellContainerCards(payload.UniqueIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"card_container_num":               a.account.ContainerCardCount(),
		"get_gold":                         result.GetGold,
		"gold":                             result.Gold,
		"extract_reward":                   battleResultRewardsWire(result.Extract.Rewards),
		"is_extract_reward_in_present_box": game.BoolInt(result.Extract.InPresentBox),
		"decks":                            toWireDecks(result.Decks),
	})
}

func (a *API) cardLoveUp(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BaseUniqueID int64                `json:"base_uniqid"`
		UseItems     []game.LoveUpItemUse `json:"use_items"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "use_items"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.LoveUpCard(payload.BaseUniqueID, payload.UseItems)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	card := a.toWireCard(result.Card)
	a.writeProtocol(writer, map[string]any{
		"base_card": card,
		"item":      a.itemInfosWire(result.Items),
		"result_card": map[string]any{
			"card": card,
			"love_up": []any{map[string]any{
				"old_love": result.OldLove,
				"new_love": result.NewLove,
				"is_max":   result.IsMax,
			}},
			"rewards": []any{},
		},
		"gold": result.Gold,
	})
}

func (a *API) buddyShow(writer http.ResponseWriter, _ *http.Request) {
	a.writeProtocol(writer, wireBuddyShow{Buddies: toWireBuddies(a.account.BuddyState())})
}

func (a *API) cardCategoryGet(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	categories, groups := a.account.CardCategoryState()
	wireCategories := make([]map[string]any, len(categories))
	for index, category := range categories {
		wireCategories[index] = map[string]any{
			"categoryid": category.CategoryID,
			"name":       category.Name,
			"order":      category.Order,
			"view_type":  category.ViewType,
		}
	}
	wireGroups := make([]map[string]any, len(groups))
	for index, group := range groups {
		wireGroups[index] = map[string]any{
			"groupid":           group.GroupID,
			"categoryid":        group.CategoryID,
			"name":              group.Name,
			"order":             group.Order,
			"deck_limit_bossid": group.DeckLimitBossID,
		}
	}
	a.writeProtocol(writer, map[string]any{
		"categories": wireCategories,
		"groups":     wireGroups,
	})
}

func (a *API) cardDeckSet(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		Decks []game.DeckInfo `json:"decks"`
	}
	if err := decodeExact(request, []string{"decks"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	cards, decks, err := a.account.SetDecks(payload.Decks)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, wireDeckSet{
		Cards: a.toWireCards(cards),
		Decks: toWireDecks(decks),
	})
}

func (a *API) cardFusion(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{
		"base_uniqid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_stackcards",
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var baseUniqueID int64
	var materialUniqueIDs, containerUniqueIDs []int64
	if json.Unmarshal(fields["base_uniqid"], &baseUniqueID) != nil ||
		json.Unmarshal(fields["add_uniqids"], &materialUniqueIDs) != nil ||
		json.Unmarshal(fields["add_container_uniqids"], &containerUniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid fusion card selection")
		return
	}
	cardUses, err := decodeStackUses(fields["add_cardids"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stackUses, err := decodeStackUses(fields["add_stackcards"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stackUses = append(cardUses, stackUses...)
	oldCard, resultCard, successType, decks, err := a.account.FuseCard(
		baseUniqueID, materialUniqueIDs, containerUniqueIDs, stackUses,
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	isLevelMax := 0
	if resultCard.Level >= resultCard.LevelMax {
		isLevelMax = 1
	}
	experienceUp := []any{}
	if resultCard.Experience != oldCard.Experience {
		experienceUp = append(experienceUp, map[string]any{
			"old_exp":   oldCard.Experience,
			"new_exp":   resultCard.Experience,
			"old_lv":    oldCard.Level,
			"new_lv":    resultCard.Level,
			"is_lv_max": isLevelMax,
		})
	}
	fameUp := []any{}
	if resultCard.Fame != oldCard.Fame {
		isFameMax := 0
		if a.account.CardFameMaximumReached(resultCard.CardID, resultCard.Fame) {
			isFameMax = 1
		}
		fameUp = append(fameUp, map[string]any{
			"old_fame":    oldCard.Fame,
			"new_fame":    resultCard.Fame,
			"is_fame_max": isFameMax,
		})
	}
	a.writeProtocol(writer, map[string]any{
		"result_card": map[string]any{
			"card":         a.toWireCard(resultCard),
			"old_card":     a.toWireCard(oldCard),
			"success_type": successType,
			"exp_up":       experienceUp,
			"fame_up":      fameUp,
			"rewards":      []any{},
		},
		"gold":                   a.account.GoldState(),
		"card_num":               a.account.CardCount(),
		"decks":                  toWireDecks(decks),
		"back_uniqids":           []int64{},
		"back_container_uniqids": []int64{},
		"back_stack_cards":       []any{},
	})
}

func (a *API) cardEvolution(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{
		"base_uniqid", "to_cardid", "add_uniqids", "add_container_uniqids", "add_cardids", "add_itemids",
	})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var baseUniqueID int64
	var toCardID int
	var materialUniqueIDs, containerUniqueIDs []int64
	if json.Unmarshal(fields["base_uniqid"], &baseUniqueID) != nil ||
		json.Unmarshal(fields["to_cardid"], &toCardID) != nil ||
		json.Unmarshal(fields["add_uniqids"], &materialUniqueIDs) != nil ||
		json.Unmarshal(fields["add_container_uniqids"], &containerUniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid evolution card selection")
		return
	}
	var cardIDs, itemIDs []int
	if json.Unmarshal(fields["add_cardids"], &cardIDs) != nil || json.Unmarshal(fields["add_itemids"], &itemIDs) != nil {
		writeError(writer, http.StatusBadRequest, "evolution material IDs must be arrays of integers")
		return
	}
	result, gold, decks, err := a.account.EvolveCard(
		baseUniqueID, toCardID, materialUniqueIDs, containerUniqueIDs, cardIDs, itemIDs,
	)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"base_card": a.toWireCard(result),
		"gold":      gold,
		"card_num":  a.account.CardCount(),
		"decks":     toWireDecks(decks),
		"reward":    nil,
	})
}

func (a *API) cardSell(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{"uniqids", "cardids"})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var uniqueIDs []int64
	if json.Unmarshal(fields["uniqids"], &uniqueIDs) != nil {
		writeError(writer, http.StatusBadRequest, "invalid sell card selection")
		return
	}
	stackUses, err := decodeStackUses(fields["cardids"])
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.SellCards(uniqueIDs, stackUses)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"card_num":                         a.account.CardCount(),
		"get_gold":                         result.GetGold,
		"gold":                             result.Gold,
		"extract_reward":                   battleResultRewardsWire(result.Extract.Rewards),
		"is_extract_reward_in_present_box": game.BoolInt(result.Extract.InPresentBox),
		"decks":                            toWireDecks(result.Decks),
	})
}
