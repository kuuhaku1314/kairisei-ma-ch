package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) exploreStart(writer http.ResponseWriter, request *http.Request) {
	fields, err := decodeFlexibleFields(request, []string{"arthur_type", "deck_idx"})
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	var arthurType, deckIndex int8
	if json.Unmarshal(fields["arthur_type"], &arthurType) != nil ||
		json.Unmarshal(fields["deck_idx"], &deckIndex) != nil ||
		arthurType < 1 || arthurType > 4 || deckIndex < 0 {
		writeError(writer, http.StatusBadRequest, "invalid Explore selection")
		return
	}
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	var seed [32]byte
	if _, err := rand.Read(seed[:]); err != nil {
		writeError(writer, http.StatusInternalServerError, "prepare Explore rewards")
		return
	}
	events, rewards, err := planExploreRewards(a.initialState.Explore.Events, seed[:])
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	ap, leaderCardID, avatar, stage, started := a.account.BeginExplore(arthurType, deckIndex, rewards)
	if !started {
		writeError(writer, http.StatusBadRequest, "Explore points are empty or an exploration is already active")
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, map[string]any{
		"ap":            ap.Current,
		"ap_max":        ap.Max,
		"ap_next_sec":   ap.NextSeconds,
		"arthur_type":   arthurType,
		"leader_cardid": leaderCardID,
		"stage":         stage,
		"floor_rarity":  a.initialState.Explore.FloorRarity,
		"events":        events,
		"avatar":        avatar,
	})
}

func (a *API) exploreEnd(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodPost {
		if _, err := decodeFlexibleFields(request, []string{}); err != nil {
			a.writeStoreError(writer, err)
			return
		}
	}
	rewards, err := decodeExploreRewards(a.initialState.Explore.Events)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	a.clientResultMu.Lock()
	defer a.clientResultMu.Unlock()
	if !a.account.ExploreIsActive() {
		if cached, exists := a.account.ExploreResultReceipt(); exists {
			a.writeProtocol(writer, cached)
			return
		}
	}
	result, deckCards, completed, startedAtUnix, err := a.account.EndExplore(rewards)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, err.Error())
		return
	}
	resultRewards := make([]any, 0, len(result.Rewards))
	if completed {
		for _, received := range result.Rewards {
			resultRewards = append(resultRewards, map[string]any{
				"reward":           received.Reward,
				"uniqid":           received.UniqueID,
				"is_new":           received.IsNew,
				"auto_fusion_used": 0,
				"auto_loveup_used": 0,
				"add":              []any{},
			})
		}
	}
	response := map[string]any{
		"user":                            a.userPayload(),
		"result_rewards":                  resultRewards,
		"deck_cards":                      a.toWireCards(deckCards),
		"new_cards":                       a.toWireCards(result.Cards),
		"new_stack_cards":                 toWireStackCards(result.StackCards),
		"new_items":                       a.itemInfosWire(result.Items),
		"new_sphrs":                       toWireSpheres(result.Spheres),
		"new_buddys":                      toWireBuddies(result.Buddies),
		"is_result_reward_in_present_box": game.BoolInt(result.InPresentBox),
		"unlock_notice":                   []any{},
	}
	encodedResponse, err := json.Marshal(response)
	if err != nil {
		writeError(writer, http.StatusInternalServerError, "encode Explore settlement")
		return
	}
	if completed {
		if err := a.account.RecordExploreResultReceipt(startedAtUnix, encodedResponse, time.Now()); err != nil {
			writeError(writer, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if completed && !a.persistOrError(writer) {
		return
	}
	a.writeProtocol(writer, json.RawMessage(encodedResponse))
}

func decodeExploreRewards(events []json.RawMessage) ([]gamestate.Reward, error) {
	type treasureBox struct {
		Rewards []gamestate.Reward `json:"reward"`
	}
	type symbol struct {
		Rewards []gamestate.Reward `json:"reward"`
	}
	type exploreEvent struct {
		TreasureBoxes []treasureBox `json:"treasureboxes"`
		Symbols       []symbol      `json:"symbols"`
	}
	results := make([]gamestate.Reward, 0)
	for _, raw := range events {
		var event exploreEvent
		if err := json.Unmarshal(raw, &event); err != nil {
			return nil, fmt.Errorf("decode Explore result reward: %w", err)
		}
		rewardGroups := make([][]gamestate.Reward, 0, len(event.TreasureBoxes)+len(event.Symbols))
		for _, box := range event.TreasureBoxes {
			rewardGroups = append(rewardGroups, box.Rewards)
		}
		for _, eventSymbol := range event.Symbols {
			rewardGroups = append(rewardGroups, eventSymbol.Rewards)
		}
		for _, group := range rewardGroups {
			for _, reward := range group {
				if reward.Num <= 0 || reward.Type < 0 || reward.Type >= 20 ||
					reward.CardSkillLevels == nil {
					return nil, errors.New("Explore result reward is incomplete")
				}
				results = append(results, reward)
			}
		}
	}
	return results, nil
}
