package httpapi

import (
	"math"
	"net/http"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) presentBoxShow(writer http.ResponseWriter, _ *http.Request) {
	presents, histories := a.account.PresentState()
	projectPresentTimes(presents, time.Now().Unix())
	projectPresentTimes(histories, time.Now().Unix())
	a.writeProtocol(writer, map[string]any{
		"presents":  presents,
		"histories": histories,
	})
}

func projectPresentTimes(presents []gamestate.Present, now int64) {
	for i := range presents {
		present := &presents[i]
		if present.IssuedAtUnix > 0 {
			present.AddElapsedSec = uint32(min(max(0, now-present.IssuedAtUnix), math.MaxUint32))
		}
		present.AdminIdempotencyKey = ""
		present.IssuedAtUnix = 0
	}
}

func (a *API) presentReceivePayload(
	result game.PresentReceiveResult,
) map[string]any {
	presentIDs := append([]int64{}, result.PresentID...)
	failedIDs := append([]int64{}, result.FailedID...)
	rewards := make([]any, 0, len(result.Rewards))
	for _, received := range result.Rewards {
		rewards = append(rewards, map[string]any{
			"reward":           received.Reward,
			"uniqid":           received.UniqueID,
			"is_new":           received.IsNew,
			"auto_fusion_used": 0,
			"auto_loveup_used": 0,
			"add":              []any{},
		})
	}
	newCards := make([]wireCardInfo, 0, len(result.Cards))
	for _, card := range result.Cards {
		newCards = append(newCards, a.toWireCard(card))
	}
	return map[string]any{
		"user":               a.userPayload(),
		"result_rewards":     rewards,
		"new_cards":          newCards,
		"new_stack_cards":    toWireStackCards(result.StackCards),
		"new_items":          a.itemInfosWire(result.Items),
		"new_sphrs":          toWireSpheres(result.Spheres),
		"new_buddys":         toWireBuddies(result.Buddies),
		"new_stampids":       append([]int{}, result.StampIDs...),
		"presentid":          presentIDs,
		"failed_presentid":   failedIDs,
		"auto_fusion_result": []any{},
		"auto_loveup_result": []any{},
	}
}

func (a *API) presentBoxRecv(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PresentID int64 `json:"presentid"`
	}
	if err := decodeExact(request, []string{"presentid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	result, err := a.account.ReceivePresent(payload.PresentID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(result.PresentID) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.presentReceivePayload(result))
}

func (a *API) presentBoxMultiRecv(
	writer http.ResponseWriter,
	request *http.Request,
) {
	var payload struct {
		IsCoinReceive int8  `json:"is_coin_recv"`
		ReceiveTypes  []int `json:"receive_types"`
	}
	if err := decodeExact(
		request,
		[]string{"is_coin_recv", "receive_types"},
		&payload,
	); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsCoinReceive != 0 && payload.IsCoinReceive != 1 {
		writeError(writer, http.StatusBadRequest, "invalid coin receive flag")
		return
	}
	result, err := a.account.ReceivePresents(payload.ReceiveTypes, payload.IsCoinReceive == 1)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(result.PresentID) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.presentReceivePayload(result))
}

func (a *API) presentBoxDelete(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		PresentID int64 `json:"presentid"`
	}
	if err := decodeExact(request, []string{"presentid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	deleted, err := a.account.DeletePresents(payload.PresentID)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if len(deleted) > 0 && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"presentid":        deleted,
		"failed_presentid": []int64{},
	})
}
