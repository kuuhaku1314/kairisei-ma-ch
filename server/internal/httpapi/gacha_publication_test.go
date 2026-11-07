package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"kairisei.local/server/internal/gamestate"
)

func TestGachaSelectionFollowsPublication(t *testing.T) {
	a := &API{initialState: gamestate.State{}, account: testAccount(t, func(state *gamestate.State) {
		state.Gachas = []gamestate.GachaProfile{
			{GachaID: 101, GroupID: 1, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 10, CardIDs: []int{11000045, 11000046}, CardWeights: []int{1, 1}, UserSelectMax: 1},
			{GachaID: 202, GroupID: 2, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 10, CardIDs: []int{11000045, 11000047}, CardWeights: []int{1, 1}, UserSelectMax: 1},
		}
		state.GachaSelections = nil
		state.GachaDailyClaims = nil
		state.SupportDeck.CardCollectionIDs = []int{11000047}
		state.SupportDeck.CardCollectionLoveMaxIDs = nil
	})}
	published := func(id int) bool { return id == 202 }
	for name, handler := range map[string]http.HandlerFunc{"selection": a.gachaSelectLineupShow, "selected": a.gachaSelectedListShow} {
		t.Run(name, func(t *testing.T) {
			for _, input := range []struct {
				body   string
				result int
			}{{`{"gachaid":101}`, -3100}, {`{"gachaid":202}`, 0}} {
				request := WithGachaPublication(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(input.body)), published)
				response := httptest.NewRecorder()
				handler(response, request)
				lines := bytes.Split(bytes.TrimSpace(response.Body.Bytes()), []byte{'\n'})
				var common commonResponse
				if response.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal(lines[0], &common) != nil || common.ResultCode != input.result || common.ResultDeleteSaveData != 0 {
					t.Fatalf("%s unexpected publication response: %s", input.body, response.Body.String())
				}
			}
		})
	}
}

func TestGachaOddsMessageUsesPublishedTextWithoutChangingOdds(t *testing.T) {
	for _, selectMax := range []int{0, 1} {
		for _, message := range []string{"", "整池概率说明\n<文字> & 自选池"} {
			a := &API{account: testAccount(t, func(state *gamestate.State) {
				state.Gachas = []gamestate.GachaProfile{{GachaID: 202, GroupID: 2, PayType: 3, Price: 1, CardNum: 1, CardNumMax: 1,
					CardIDs: []int{11000045, 11000047}, CardWeights: []int{1, 3}, UserSelectMax: selectMax, OddsMessage: message}}
				state.GachaSelections = nil
			})}
			request := WithGachaPublication(httptest.NewRequest(http.MethodPost, "/", bytes.NewBufferString(`{"gachaid":202,"popupid":0}`)), func(id int) bool { return id == 202 })
			response := httptest.NewRecorder()
			a.gachaOddsShow(response, request)
			var body struct {
				Message string `json:"odds_msg"`
				Lineups []struct {
					Prizes []struct {
						Odds int `json:"odds"`
					} `json:"prize_list"`
				} `json:"lineup_infos"`
			}
			lines := bytes.Split(bytes.TrimSpace(response.Body.Bytes()), []byte{'\n'})
			if response.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal(lines[1], &body) != nil {
				t.Fatal("invalid odds response", response.Body.String())
			}
			if body.Message != message || len(body.Lineups) != 1 || len(body.Lineups[0].Prizes) != 2 || body.Lineups[0].Prizes[0].Odds != 2500000 || body.Lineups[0].Prizes[1].Odds != 7500000 {
				t.Fatalf("message or weighted odds changed: select=%d body=%s", selectMax, response.Body.String())
			}
		}
	}
}
