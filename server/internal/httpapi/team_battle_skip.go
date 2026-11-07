package httpapi

import (
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func (a *API) teamBattleSkip(writer http.ResponseWriter, request *http.Request) {
	var payload struct {
		BossID int `json:"bossid"`
		Num    int `json:"num"`
	}
	if err := decodeExact(request, []string{"bossid", "num"}, &payload); err != nil {
		a.writeStoreError(writer, err)
		return
	}
	replay, found := teamBattleReplayForBoss(a.initialState.TeamBattleReplays, payload.BossID)
	if !found {
		a.writeStoreError(writer, &game.BusinessError{Code: -1, Message: "这个关卡暂不可快速通关。"})
		return
	}
	segments, err := teamBattleReplayBattles(replay)
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	results, err := a.account.SkipTeamBattle(payload.BossID, payload.Num, a.initialState.TeamBattleRewards, teamBattleEnemyTypes(segments))
	if err != nil {
		a.writeStoreError(writer, err)
		return
	}
	if !a.persistOrError(writer) {
		return
	}
	battles := make([]any, 0, len(results))
	var cards []game.CardInfo
	var stacks []gamestate.CardStack
	var items []gamestate.Item
	var spheres []gamestate.Sphere
	var buddies []gamestate.Buddy
	inPresent := false
	for _, result := range results {
		battles = append(battles, map[string]any{"reward": battleResultRewardsWire(result.Rewards)})
		cards = append(cards, result.Cards...)
		stacks = append(stacks, result.StackCards...)
		items = append(items, result.Items...)
		spheres = append(spheres, result.Spheres...)
		buddies = append(buddies, result.Buddies...)
		inPresent = inPresent || result.InPresentBox
	}
	a.writeProtocol(writer, map[string]any{
		"user": a.userPayload(), "battle_rewards": battles,
		"deck_cards": []any{}, "new_cards": a.toWireCards(cards),
		"new_stack_cards": toWireStackCards(stacks), "new_items": a.itemInfosWire(items),
		"new_sphrs": toWireSpheres(spheres), "new_buddys": toWireBuddies(buddies),
		"is_result_reward_in_present_box": game.BoolInt(inPresent), "unlock_notice": []any{},
	})
}
