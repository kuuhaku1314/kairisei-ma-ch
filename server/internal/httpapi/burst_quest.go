package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"kairisei.local/server/internal/game"
)

func (a *API) storyTeamBattleStart(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		StoryID int `json:"story_teambattleid"`
	}
	if err := decodeExact(request, []string{"story_teambattleid"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if err := a.account.BeginQuestStory(payload.StoryID); err != nil {
		a.writeProtocolResult(writer, map[string]any{}, -6800, "本段剧情不存在，请重新打开剧情目录。")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, a.storyStartPayload())
}

func (a *API) storyTeamBattleEnd(writer http.ResponseWriter, _ *http.Request) {
	if cached := a.account.QuestStoryResponse(); len(cached) > 0 {
		a.writeProtocol(writer, cached)
		return
	}
	result, err := a.account.EndQuestStory()
	if err != nil {
		storyID := a.account.QuestStoryID()
		a.logger.Warn("reject quest reading story settlement", "story_id", storyID, "error", err)
		message := "剧情奖励结算失败，请稍后重试。"
		if errors.Is(err, game.ErrNoActiveQuestStory) {
			message = "剧情记录已失效，请重新进入本段剧情。"
		}
		a.writeProtocolResult(writer, map[string]any{}, -6800, message)
		return
	}
	payload := a.storyEndPayload(result)
	payload["burst_unlock"] = []any{}
	encoded, err := json.Marshal(payload)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.account.SetQuestStoryResponse(encoded)
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, payload)
}
