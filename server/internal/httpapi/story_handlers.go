package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) storyMainShow(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		CNStory int `json:"cn_story"`
	}
	if err := decodeExact(request, []string{"cn_story"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.CNStory != 0 && payload.CNStory != 1 {
		writeError(writer, http.StatusBadRequest, "cn_story must be zero or one")
		return
	}
	a.writeProtocol(writer, map[string]any{
		"parts": a.account.StoryMainState(payload.CNStory == 1),
	})
}

func (a *API) storyMainStart(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		StoryMainID int `json:"story_mainid"`
	}
	if err := decodeExact(request, []string{"story_mainid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.account.BeginMainStory(payload.StoryMainID) {
		writeError(writer, http.StatusBadRequest, "unknown main story ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyStartPayload())
}

func (a *API) storyMainEnd(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsClear int `json:"is_clear"`
	}
	if err := decodeExact(request, []string{"is_clear"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsClear != 0 && payload.IsClear != 1 {
		writeError(writer, http.StatusBadRequest, "is_clear must be zero or one")
		return
	}
	result, err := a.account.EndMainStory(payload.IsClear == 1)
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyEndPayload(result))
}

func (a *API) storySubShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	a.writeProtocol(writer, map[string]any{
		"characters": a.account.StorySubState(),
	})
}

func (a *API) storySubStart(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		StorySubID int `json:"story_subid"`
		TowerID    int `json:"towerid"`
		Floor      int `json:"floor"`
	}
	if err := decodeExact(request, []string{"story_subid"}, &payload, "towerid", "floor"); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var started bool
	if payload.TowerID == 0 && payload.Floor == 0 {
		started = a.account.BeginSubStory(payload.StorySubID)
	} else {
		var err error
		started, err = a.account.BeginTowerStory(payload.StorySubID, payload.TowerID, payload.Floor)
		if err != nil {
			a.writeStoreError(writer, err)
			return
		}
	}
	if !started {
		writeError(writer, http.StatusBadRequest, "unknown or locked sub story ID")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyStartPayload())
}

func (a *API) storySubEnd(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsClear int `json:"is_clear"`
	}
	if err := decodeExact(request, []string{"is_clear"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsClear != 0 && payload.IsClear != 1 {
		writeError(writer, http.StatusBadRequest, "is_clear must be zero or one")
		return
	}
	result, err := a.account.EndSubStory(payload.IsClear == 1)
	if err != nil {
		writeError(writer, http.StatusConflict, err.Error())
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyEndPayload(result))
}

func (a *API) storyStartPayload() map[string]any {
	_, _, hp, _, _, _, _ := a.activeDeckProfile()
	if hp < 1 {
		hp = 1
	}
	return map[string]any{
		"seed": 0, "hp": hp, "hp_max": hp,
		"cost_initial": 3, "burst_gauge_initial": 0, "hold_max": 6,
	}
}

func (a *API) storyEndPayload(result game.PresentReceiveResult) map[string]any {
	return map[string]any{
		"user":                           a.userPayload(),
		"clear_rewards":                  battleResultRewardsWire(result.Rewards),
		"new_cards":                      a.toWireCards(result.Cards),
		"new_stack_cards":                toWireStackCards(result.StackCards),
		"new_items":                      a.itemInfosWire(result.Items),
		"new_sphrs":                      toWireSpheres(result.Spheres),
		"new_buddys":                     toWireBuddies(result.Buddies),
		"is_clear_reward_in_present_box": game.BoolInt(result.InPresentBox),
		"unlock_notice":                  []any{},
	}
}

func (a *API) storyEventShow(
	writer http.ResponseWriter,
	_ *http.Request,
) {
	a.writeProtocol(writer, map[string]any{
		"events": a.account.StoryEventState(),
	})
}

func (a *API) storyEventUnlock(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StorySubID int `json:"story_subid"`
	}
	if err := decodeExact(request, []string{"story_subid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	items, events, err := a.account.UnlockEventStory(payload.StorySubID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"user":   a.userPayload(),
		"items":  items,
		"events": events,
	})
}

func (a *API) storyStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StoryBattleID       int    `json:"story_battleid"`
		DeckArthurType      int8   `json:"deck_arthur_type"`
		DeckArthurTypeIndex []int8 `json:"deck_arthur_type_idxs"`
	}
	if err := decodeExact(
		request,
		[]string{"story_battleid", "deck_arthur_type", "deck_arthur_type_idxs"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.AuthorizeStoryBattle(
		payload.StoryBattleID,
		payload.DeckArthurType,
		payload.DeckArthurTypeIndex,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{})
}
