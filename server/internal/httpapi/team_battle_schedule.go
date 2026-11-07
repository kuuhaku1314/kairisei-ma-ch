package httpapi

import (
	"errors"
	"net/http"
	"time"

	"kairisei.local/server/internal/gamestate"
)

func (a *API) teamBattleScheduleShow(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		IsSolo           int  `json:"is_solo"`
		ActiveArthurType int8 `json:"active_arthur_type"`
	}
	if err := decodeExact(request, []string{"is_solo", "active_arthur_type"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if (payload.IsSolo != 0 && payload.IsSolo != 1) ||
		payload.ActiveArthurType < 1 || payload.ActiveArthurType > 4 {
		writeError(writer, http.StatusBadRequest, "invalid team battle schedule request")
		return
	}
	schedules, err := a.teamBattleScheduleEntries(payload.IsSolo)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"schedules": schedules})
}

func (a *API) teamBattleScheduleUpdate(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		IsSolo      int `json:"is_solo"`
		BossGroupID int `json:"boss_groupid"`
	}
	if err := decodeExact(request, []string{"is_solo", "boss_groupid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if (payload.IsSolo != 0 && payload.IsSolo != 1) ||
		!teamBattleScheduleContains(a.initialState.TeamBattleScheduleGroupIDs, payload.BossGroupID) {
		writeError(writer, http.StatusBadRequest, "unknown team battle schedule group")
		return
	}
	enabled := a.account.ToggleTeamBattleSchedulePush(payload.IsSolo, payload.BossGroupID)
	if !a.persistOrError(writer) {
		a.account.ToggleTeamBattleSchedulePush(payload.IsSolo, payload.BossGroupID)
		return
	}
	entry, err := a.teamBattleScheduleEntry(
		payload.BossGroupID,
		teamBattleScheduleAnchorUnix(time.Now()),
		enabled,
	)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.writeProtocol(writer, map[string]any{"upd_info": []any{entry}})
}

func (a *API) teamBattleScheduleEntries(isSolo int) ([]any, error) {
	groupIDs := a.initialState.TeamBattleScheduleGroupIDs
	if len(groupIDs) == 0 || len(groupIDs) > 50 {
		return nil, errors.New("team battle schedule profile is unavailable")
	}
	enabledIDs := a.account.TeamBattleSchedulePushState(isSolo)
	enabled := make(map[int]struct{}, len(enabledIDs))
	for _, groupID := range enabledIDs {
		enabled[groupID] = struct{}{}
	}
	anchor := teamBattleScheduleAnchorUnix(time.Now())
	result := make([]any, 0, len(groupIDs))
	for _, groupID := range groupIDs {
		_, isEnabled := enabled[groupID]
		entry, err := a.teamBattleScheduleEntry(groupID, anchor, isEnabled)
		if err != nil {
			return nil, err
		}
		result = append(result, entry)
	}
	return result, nil
}

func (a *API) teamBattleScheduleEntry(groupID int, anchor int64, enabled bool) (map[string]any, error) {
	group, found := teamBattleGroupForID(a.account.TeamBattleSoloState(), groupID)
	if !found {
		return nil, errors.New("team battle schedule group is absent from the active catalog")
	}
	named, err := teamBattleBossGroupNamedDTO(group)
	if err != nil {
		return nil, err
	}
	validPush := 0
	if enabled {
		validPush = 1
	}
	return map[string]any{
		"info":            named,
		"appear_schedule": anchor,
		"valid_push":      validPush,
	}, nil
}

func teamBattleScheduleContains(groupIDs []int, target int) bool {
	for _, groupID := range groupIDs {
		if groupID == target {
			return true
		}
	}
	return false
}

func teamBattleScheduleAnchorUnix(now time.Time) int64 {
	return gamestate.BattleClearDay(now.Unix())*86400 - 8*3600
}
