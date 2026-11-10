package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestSoloStartUsesEnabledSlotsThroughClearHistory(t *testing.T) {
	for _, tc := range []struct {
		name     string
		types    [4]int
		partners []int8
	}{
		{"one member", [4]int{3, -1, -1, -1}, nil},
		{"two members", [4]int{0, 2, -1, -1}, []int8{2}},
		{"slot order", [4]int{4, 3, 1, -1}, []int8{4, 1}},
		{"repeated profession", [4]int{3, 3, 3, -1}, []int8{3, 3}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			const boss = 30010102
			limit := game.TeamBattleEntryLimit{Slot1ArthurType: tc.types[0], Slot2ArthurType: tc.types[1], Slot3ArthurType: tc.types[2], Slot4ArthurType: tc.types[3]}
			limits, _ := json.Marshal([]game.TeamBattleEntryLimit{limit})
			var initial gamestate.State
			account := testAccount(t, func(state *gamestate.State) {
				state.User.Name = "成员槽位测试"
				state.User.BP = 100
				helper := &state.PlayerProgressionPolicy.Friends.HelperReward
				helper.OtherPerPartner, helper.FriendPerPartner = 5, 10
				state.TeamBattleSolo = json.RawMessage(fmt.Sprintf(`{"9":[],"10":[{"0":300101,"9":0,"10":[{"0":30010102,"1":1,"5":1,"10":0,"16":0,"entry_limit":%s}]}],"11":[],"12":[]}`, limits))
				initial = *state
			})
			state := account.Snapshot(initial)
			state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: boss, EnemyPartyID: 1}}
			state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: boss}}
			history := &historyFixture{}
			api := &API{initialState: state, account: account, battleHistory: history, logger: slog.New(slog.NewTextHandler(io.Discard, nil))}
			payload := soloBattleStartRequest{BossID: boss, DeckArthurType: 3}
			for _, arthur := range tc.partners {
				payload.PartnerDeckSelects = append(payload.PartnerDeckSelects, teamBattlePartnerDeckSelect{UserID: state.User.UserID, ArthurType: arthur})
			}
			call := func(fn http.HandlerFunc, value any) (int, map[string]json.RawMessage) {
				t.Helper()
				body, _ := json.Marshal(value)
				response := httptest.NewRecorder()
				fn(response, httptest.NewRequest("POST", "/", strings.NewReader(string(body))))
				if response.Code != 200 {
					t.Log(response.Body.String())
					return response.Code, nil
				}
				parts := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
				var common struct {
					Code int `json:"res_code"`
				}
				var result map[string]json.RawMessage
				if len(parts) != 3 || json.Unmarshal([]byte(parts[0]), &common) != nil || common.Code != 0 || json.Unmarshal([]byte(parts[1]), &result) != nil {
					t.Fatal("invalid response", response.Body.String())
				}
				return response.Code, result
			}
			bad := payload
			bad.PartnerDeckSelects = append(append([]teamBattlePartnerDeckSelect(nil), payload.PartnerDeckSelects...), teamBattlePartnerDeckSelect{UserID: state.User.UserID, ArthurType: 1})
			if code, _ := call(api.teamBattleSoloStart, bad); code != 400 || account.Snapshot(state).User.BP != 100 {
				t.Fatal("disabled battle slot started or charged BP")
			}
			code, result := call(api.teamBattleSoloStart, payload)
			var decks []struct {
				Arthur int8 `json:"arthur_type"`
			}
			if code != 200 || json.Unmarshal(result["partner_deck"], &decks) != nil || len(decks) != len(tc.partners) {
				t.Fatal("actual party size lost", code, result)
			}
			for i, arthur := range tc.partners {
				if decks[i].Arthur != arthur {
					t.Fatal("party was sorted instead of preserving slots")
				}
			}
			started := account.Snapshot(state)
			if started.User.BP != 99 || len(started.ActiveTeamBattle.SelectedPartners) != len(tc.partners) || len(started.ActiveTeamBattle.ClearDecks) != 1+len(tc.partners) {
				t.Fatal("actual party/context/clear snapshot differs")
			}
			if code, _ := call(api.teamBattleSoloStart, payload); code != 200 || account.Snapshot(state).User.BP != 99 {
				t.Fatal("retry charged the sparse party twice")
			}
			if len(tc.partners) != 0 {
				_, _, _, err := account.BeginSoloBattle(game.SoloBattleEntry{DeckArthurType: 3, BossID: boss, RequestKey: "invalid-party", FameSources: []game.TeamBattleFameSource{{MemberType: 1, LeaderFame: 1}}})
				if err == nil || account.Snapshot(state).User.BP != 99 {
					t.Fatal("domain entry bypassed slot validation")
				}
			}
			end := `{"bossid":30010102,"progress":1,"is_clear":1,"input_cmd":["0,22` + strings.Repeat(",0", 20) + `"],"enemy_dead_bit":[0]}`
			code, result = call(api.teamBattleSoloEnd, json.RawMessage(end))
			if code != 200 || history.saves != 1 || len(history.records[0].Decks) != 1+len(tc.partners) {
				t.Fatal("sparse party could not settle and retain its actual decks")
			}
			if string(result["scene_transition"]) != "[]" {
				t.Fatal("ordinary battle result gained a tower transition")
			}
			var resultPartners []struct {
				MemberType int `json:"member_type"`
				IsOnline   int `json:"is_online"`
				Member     struct {
					ArthurType int8 `json:"arthur_type"`
				} `json:"member"`
			}
			if json.Unmarshal(result["partners"], &resultPartners) != nil || len(resultPartners) != len(tc.partners) {
				t.Fatal("result lost actual selected partner count")
			}
			for i, partner := range resultPartners {
				if partner.MemberType != i+2 || partner.IsOnline != 0 || partner.Member.ArthurType != tc.partners[i] {
					t.Fatal("result confused a partner's battle position with its profession")
				}
			}
			settled, _ := json.Marshal(result)
			restored, err := game.New(account.Snapshot(state))
			if err != nil {
				t.Fatal(err)
			}
			api.account = restored
			code, retried := call(api.teamBattleSoloEnd, json.RawMessage(end))
			replayed, _ := json.Marshal(retried)
			if code != 200 || history.saves != 1 || string(settled) != string(replayed) {
				t.Fatal("saved solo result changed or granted again after account reload")
			}
			_, result = call(api.teamBattleClearDeckShow, json.RawMessage(`{"bossid":30010102}`))
			var shown []json.RawMessage
			if json.Unmarshal(result["partner_deck"], &shown) != nil || len(shown) != 1+len(tc.partners) {
				t.Fatal("recent clear discarded sparse party")
			}
			_, result = call(api.dailyClearRankShow, json.RawMessage(`{"bossid":30010102,"is_multi":0}`))
			var buckets []struct {
				Ranks []json.RawMessage `json:"ranks"`
			}
			if json.Unmarshal(result["deck"], &buckets) != nil || len(buckets) != 4 {
				t.Fatal("rank slots changed")
			}
			for i, bucket := range buckets {
				want := 0
				if i <= len(tc.partners) {
					want = 1
				}
				if len(bucket.Ranks) != want {
					t.Fatal("rank padded or discarded absent members")
				}
			}
		})
	}
}
