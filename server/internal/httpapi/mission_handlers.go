package httpapi

import (
	"net/http"
	"time"
)

func (a *API) missionShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		IsReward int `json:"is_reward"`
	}
	if err := decodeExact(request, []string{"is_reward"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if payload.IsReward != 0 && payload.IsReward != 1 {
		writeError(writer, http.StatusBadRequest, "invalid mission history flag")
		return
	}
	if a.account.RefreshStateMissions(time.Now()) && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"missions": a.account.MissionInfos(payload.IsReward != 0),
	})
}

func (a *API) missionReward(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		MissionIDs []int `json:"missionids"`
	}
	if err := decodeExact(request, []string{"missionids"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	rewardMissions, receiveMissions, err := a.account.ReceiveMissionRewards(payload.MissionIDs)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"reward_missions":  rewardMissions,
		"receive_missions": receiveMissions,
	})
}

func (a *API) missionURLOpen(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		OpenURL string `json:"open_url"`
	}
	if err := decodeExact(request, []string{"open_url"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	updatedMissions, err := a.account.CheckMissionOpenURL(payload.OpenURL)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	a.writeProtocol(writer, map[string]any{
		"upd_missions": updatedMissions,
	})
}
