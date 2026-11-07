package httpapi

import (
	"net/http"
	"time"
)

func (a *API) cardDecompose(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
		Type     int   `json:"type"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	stive, decks, err := a.account.DecomposeCard(payload.UniqueID, payload.Type)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"stive": stive, "decompose_type": payload.Type, "uniqid": payload.UniqueID,
		"decks": toWireDecks(decks),
	})
}

func (a *API) cardFameTrainInfo(writer http.ResponseWriter, _ *http.Request) {
	policy := a.account.FameTrainingPolicy()
	state, changed := a.account.CardFameInfo(time.Now())
	if changed && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "begin_tm": state.BeginTime, "remain_tm": state.Remain,
		"fame": state.Fame, "time_every_fame": policy.TimeEveryFameSeconds,
		"coin_every_hour": policy.CoinEveryHour,
		"url":             a.baseURL + policy.HelpPath,
	})
}

func (a *API) cardFameStartTrain(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
		Fame     int   `json:"base_fame"`
	}
	if err := decodeExact(request, []string{"base_uniqid", "base_fame"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.account.StartCardFameTraining(payload.UniqueID, payload.Fame, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "begin_tm": state.BeginTime, "remain_tm": state.Remain,
		"fame": state.Fame, "stive": state.Stive, "is_lock": state.IsLock,
	})
}

func (a *API) cardFameCancelTrain(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
	}
	if err := decodeExact(request, []string{"base_uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.account.CancelCardFameTraining(payload.UniqueID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "fame": state.Fame, "stive": state.Stive, "is_lock": state.IsLock,
	})
}

func (a *API) cardFameTrainFinish(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		UniqueID int64 `json:"base_uniqid"`
	}
	if err := decodeExact(request, []string{"base_uniqid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	state, err := a.account.FinishCardFameTraining(payload.UniqueID, time.Now())
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"uniqid": state.UniqueID, "fame": state.Fame, "coin": state.Coin,
		"coin_free": state.CoinFree, "is_lock": state.IsLock,
	})
}

func (a *API) howToGetCardShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		CardIDs []int `json:"cardids"`
	}
	if err := decodeExact(request, []string{"cardids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	lists, err := a.account.HowToGetCards(payload.CardIDs, a.initialState.TeamBattleRewards, cardSourcePolicy(request))
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{"how_to_list": lists})
}
