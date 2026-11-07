package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

// Permanent cached-report failures must reach ResultMgr's callback, which
// discards only its battle temporary data before showing a business result.
func (a *API) writeExpiredBattleReport(writer http.ResponseWriter) {
	a.writeProtocolResult(writer, map[string]any{}, -3207, "战斗记录已失效，无法结算本次奖励，请重新进入副本。")
}

// Fresh settlements and retries use the same serializer. Only granted result
// data is frozen; the user object always reflects the account's current state.
func (a *API) writeTeamBattleResultSnapshot(writer http.ResponseWriter, data json.RawMessage, multiplayer bool) {
	if len(data) == 0 {
		a.writeExpiredBattleReport(writer)
		return
	}
	var snapshot game.TeamBattleResultSnapshot
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber() // Preserve 64-bit score values in the existing score rows.
	if err := decoder.Decode(&snapshot); err != nil {
		writeError(writer, http.StatusInternalServerError, "decode team battle settlement snapshot")
		return
	}
	a.writeProtocol(writer, a.teamBattleResultPayload(snapshot, multiplayer))
}

func (a *API) teamBattleResultPayload(snapshot game.TeamBattleResultSnapshot, multiplayer bool) map[string]any {
	grants := []game.PresentReceiveResult{snapshot.Result, snapshot.FirstClear, snapshot.Score}
	for _, award := range snapshot.Fame {
		grants = append(grants, award.Result)
	}
	for _, award := range snapshot.Host {
		grants = append(grants, award.Result)
	}
	var cards []game.CardInfo
	var stacks []gamestate.CardStack
	var items []gamestate.Item
	var spheres []gamestate.Sphere
	var buddies []gamestate.Buddy
	for _, grant := range grants {
		cards = append(cards, grant.Cards...)
		stacks = append(stacks, grant.StackCards...)
		items = append(items, grant.Items...)
		spheres = append(spheres, grant.Spheres...)
		buddies = append(buddies, grant.Buddies...)
	}
	partners := make([]any, 0, len(snapshot.Partners))
	for _, partner := range snapshot.Partners {
		partners = append(partners, battleResultPartnerWire(partner.MemberType, partner.IsOnline, partner.Member))
	}
	response := map[string]any{
		"user":                            a.userPayload(),
		"result_rewards":                  battleResultRewardsWire(snapshot.Result.Rewards),
		"clear_rewards":                   battleResultRewardsWire(snapshot.FirstClear.Rewards),
		"fame_rewards":                    battleFameRewardsWire(snapshot.Fame),
		"score_rewards":                   battleResultRewardsWire(snapshot.Score.Rewards),
		"challenge_rewards":               []any{},
		"deck_cards":                      []any{},
		"new_cards":                       a.toWireCards(cards),
		"new_stack_cards":                 toWireStackCards(stacks),
		"new_items":                       a.itemInfosWire(items),
		"new_sphrs":                       toWireSpheres(spheres),
		"new_buddys":                      toWireBuddies(buddies),
		"partners":                        partners,
		"is_result_reward_in_present_box": game.BoolInt(snapshot.Result.InPresentBox),
		"is_clear_reward_in_present_box":  game.BoolInt(snapshot.FirstClear.InPresentBox),
		"is_fame_reward_in_present_box":   battleAwardsInPresentBox(snapshot.Fame),
		"is_stage_reward_in_present_box":  0,
		"is_score_reward_in_present_box":  game.BoolInt(snapshot.Score.InPresentBox),
		"is_rookie_bonus":                 0,
		"unlock_notice":                   []any{},
		"bonus_fame_add":                  snapshot.BonusFameAdd,
		// Permanent StageQuest routes are not suppression/world-boss results.
		"stage_quest":        []any{},
		"scene_transition":   teamBattleSceneTransition(snapshot.Return),
		"auto_fusion_result": []any{},
		"auto_loveup_result": []any{},
		"score":              scoreInfoWire(snapshot.ScoreInfo),
		"challenge_results":  []any{},
	}
	if multiplayer {
		response["member_type"] = snapshot.MemberType
		response["host_rewards"] = battleHostRewardsWire(snapshot.Host)
	} else {
		response["clear_rating_rewards"] = []any{}
		response["is_clear_rating_reward_in_present_box"] = 0
		response["old_clear_rating_flag"] = []int{}
		response["new_clear_rating_flag"] = []int{}
		if snapshot.ClearRating != nil {
			response["old_clear_rating_flag"] = []int{snapshot.ClearRating.OldFlag}
			response["new_clear_rating_flag"] = []int{snapshot.ClearRating.NewFlag}
		}
		response["is_clear"] = snapshot.IsClear
		response["show_boss_coin"] = 0
		response["message"] = ""
	}
	return response
}

// Solo and multiplayer results use the same named transition record. Ordinary
// results have no explicit destination; user data remains the outer user field.
func teamBattleSceneTransition(destination *game.TeamBattleReturn) []any {
	if destination == nil {
		return []any{}
	}
	return []any{map[string]any{
		"stageid": 0, "stage_areaid": 0, "bossid": destination.BossID,
		"is_owner": 1, "is_solo": 1, "towerid": destination.TowerID,
		"tower_floor": destination.TowerFloor, "stage_type": destination.StageType,
	}}
}
