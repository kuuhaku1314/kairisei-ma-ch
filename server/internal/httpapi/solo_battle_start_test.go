package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/accountstore"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/multiplayer"
	"kairisei.local/server/internal/testfixture"
)

func TestModernSoloStartRetainsIdentityAcrossReloadAndStartsAfterAbandon(t *testing.T) {
	const boss = 30010102
	var initial gamestate.State
	account := testAccount(t, func(state *gamestate.State) {
		state.User.Name = "入场测试"
		state.User.BP = 100
		helper := &state.PlayerProgressionPolicy.Friends.HelperReward
		helper.OtherPerPartner, helper.FriendPerPartner = 5, 10
		state.TeamBattleSolo = json.RawMessage(`{"9":[],"10":[{"0":300101,"4":"活动","9":0,"10":[{"0":30010102,"1":0,"4":"超级","5":1,"6":1,"10":0,"16":0}]}],"11":[],"12":[]}`)
		initial = *state
	})
	state := account.Snapshot(initial)
	state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1, CostInitial: 3, HoldMax: 6}}
	state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: boss}}
	api := &API{initialState: state, account: account}
	payload := fmt.Sprintf(`{"bossid":30010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"partner_deck_selects":[{"userid":%d,"arthur_type":1,"deck_idx":0},{"userid":%d,"arthur_type":2,"deck_idx":0},{"userid":%d,"arthur_type":4,"deck_idx":0}]}`, state.User.UserID, state.User.UserID, state.User.UserID)
	for _, extra := range []string{`,"starttime":1`, `,"flag":0`, `,"pay_type":1`} {
		old := strings.TrimSuffix(payload, "}") + extra + "}"
		response := httptest.NewRecorder()
		api.teamBattleSoloStart(response, httptest.NewRequest("POST", "/TeamBattleSoloStart", strings.NewReader(old)))
		if response.Code != 400 {
			t.Fatal("ordinary entry accepted a retired or archive-only field")
		}
	}
	start := func() (int, int) {
		t.Helper()
		response := httptest.NewRecorder()
		api.teamBattleSoloStart(response, httptest.NewRequest("POST", "/TeamBattleSoloStart", strings.NewReader(payload)))
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != 200 || len(lines) != 3 {
			t.Fatalf("start: HTTP %d %s", response.Code, response.Body.String())
		}
		var common struct {
			Code int `json:"res_code"`
		}
		var result struct {
			StartTime int `json:"start_time"`
			Seed      int `json:"seed"`
			HoldMax   int `json:"hold_max"`
		}
		if json.Unmarshal([]byte(lines[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(lines[1]), &result) != nil {
			t.Fatal("start response", response.Body.String())
		}
		if result.HoldMax != 6 {
			t.Fatal("start omitted the configured six hold slots", result.HoldMax)
		}
		return result.StartTime, result.Seed
	}
	startedAt, seed := start()
	if delta := time.Now().Unix() - int64(startedAt); delta < 0 || delta > 5 {
		t.Fatal("start time did not come from the server")
	}
	saved := api.account.Snapshot(state)
	identity := saved.ActiveTeamBattle.FameSeed
	if saved.User.BP != state.User.BP-1 || saved.ActiveTeamBattle.StartTime != startedAt || saved.ActiveTeamBattle.SoloRequestKey == "" {
		t.Fatal("entry payment or runtime identity missing")
	}
	// Account handlers can be evicted while the small battle session remains
	// in Accounts. This is a serving-lifetime cache, not durable account data.
	sessions := testfixture.NewFriendCapacityTestAccounts(t)
	sessions.RememberBattle(accountstore.PrimaryUserID, saved.ActiveTeamBattle, gamestate.StoryTeamBattleSession{})
	cached, err := sessions.LoadState(accountstore.PrimaryUserID)
	if err != nil {
		t.Fatal(err)
	}
	saved.ActiveTeamBattle = cached.ActiveTeamBattle
	api.account, err = game.New(saved)
	if err != nil {
		t.Fatal(err)
	}
	retryTime, retrySeed := start()
	afterRetry := api.account.Snapshot(state)
	if retryTime != startedAt || retrySeed != seed || afterRetry.User.BP != saved.User.BP || afterRetry.ActiveTeamBattle.FameSeed != identity {
		t.Fatal("retry after reload started or charged a different run")
	}
	api.account.AbandonSoloBattle()
	start()
	next := api.account.Snapshot(state)
	if next.User.BP != saved.User.BP-1 || next.ActiveTeamBattle.FameSeed == identity {
		t.Fatal("new run after Home/abandon reused the prior payment")
	}
	api.account.AbandonSoloBattle()
	api.multiplayer = multiplayer.NewHub()
	api.battleSV = multiplayer.Endpoint{Host: "127.0.0.1", Port: 26021}
	roomPayload := `{"bossid":30010102,"deck_arthur_type":3,"deck_arthur_type_idx":0,"room_type":0,"pass":"","deck_rank":0,"hp":0,"fame":0,"comment":"","allow_to_leave":0,"use_punished_free_piont":0,"is_battle_msg_skip":1,"battle_speed_rate":150}`
	for _, old := range []bool{true, false} {
		body := roomPayload
		if old {
			body = strings.TrimSuffix(body, "}") + `,"flag":1}`
		}
		response := httptest.NewRecorder()
		api.teamBattleMultiRoomCreate(response, httptest.NewRequest("POST", "/TeamBattleMultiRoomCreate", strings.NewReader(body)))
		if old {
			if response.Code != 400 {
				t.Fatal("room creation accepted retired archive flag")
			}
			continue
		}
		lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
		if response.Code != 200 || len(lines) != 3 {
			t.Fatal(response.Body.String())
		}
		var result struct {
			MemberType int `json:"member_type"`
		}
		if json.Unmarshal([]byte(lines[1]), &result) != nil || result.MemberType != 3 {
			t.Fatal("HTTP authorization did not return the assigned room slot", response.Body.String())
		}
	}
}
