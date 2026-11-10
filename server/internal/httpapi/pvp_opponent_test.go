package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

type pvpOpponentStates []gamestate.State

func (states pvpOpponentStates) ListPVPOpponents(int) ([]gamestate.State, error) {
	return append([]gamestate.State(nil), states...), nil
}

func TestPVPEnemyDeckIncludesLeaderAndSupportParameters(t *testing.T) {
	state := gamestate.State{Cards: []gamestate.Card{
		{UniqueID: 1, CardID: 11, HP: 100, Attack: 100, Magic: 100, Mind: 100},
		{UniqueID: 2, CardID: 12, HP: 100, Attack: 100, Magic: 100, Mind: 100},
		{UniqueID: 3, CardID: 13, HP: 100, Attack: 100, Magic: 100, Mind: 100, LoveMax: 100},
	}}
	state.User.Jobs = make([]gamestate.JobParameter, 5)
	for arthur := int8(1); arthur <= 4; arthur++ {
		state.User.Jobs[arthur] = gamestate.JobParameter{HP: 10, Attack: 20, Magic: 30, Mind: 40}
		state.PVP.DefenseDecks = append(state.PVP.DefenseDecks, gamestate.PVPDeckSelection{
			ArthurType: arthur, JobType: arthur, LeaderCardIndex: 1,
			CardUniqueIDs: []int64{1, 2}, SupportCardUniqueIDs: []int64{3},
		})
	}
	decks, err := buildPVPEnemyDecks(state)
	if err != nil {
		t.Fatal(err)
	}
	for _, deck := range decks {
		if deck.HP != 266 || deck.Attack != 290 || deck.Magic != 300 || deck.Mind != 300 {
			t.Fatalf("PvP enemy Arthur %d ignores leader/support parameters: %d/%d/%d/%d", deck.ArthurType, deck.HP, deck.Attack, deck.Magic, deck.Mind)
		}
	}
}

func TestPVPStartSkipsUnusableDefenseBeforeCharging(t *testing.T) {
	decks := make([]gamestate.PVPDeckSelection, 4)
	for index := range decks {
		decks[index] = gamestate.PVPDeckSelection{
			ArthurType: int8(index + 1), JobType: int8(index + 1), CardUniqueIDs: []int64{1},
		}
	}
	stale := gamestate.State{PVP: gamestate.PVPPlayerState{DefenseDecks: decks}}
	stale.User.UserID = 2
	valid := stale
	valid.User.UserID = 3
	valid.Cards = []gamestate.Card{{UniqueID: 1, CardID: 9, HP: 100}}
	for _, scenario := range []struct {
		name      string
		states    pvpOpponentStates
		challenge int
		started   bool
	}{
		{"skip stale defense", pvpOpponentStates{stale, valid}, 2, true},
		{"none usable", pvpOpponentStates{stale}, 2, false},
		{"no other accounts", pvpOpponentStates{}, 2, false},
		{"ranked allowance exhausted", pvpOpponentStates{valid}, 0, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ownDecks := make([]gamestate.PVPDeckSelection, 4)
			s := testAccount(t, func(state *gamestate.State) {
				state.User.PVPPoint = 0
				state.PVP = gamestate.PVPPlayerState{NextBattleID: 1, Challenge: scenario.challenge, ChallengeDay: (time.Now().Unix() + 8*3600) / 86400, DefenseDecks: []gamestate.PVPDeckSelection{}, DefenseUpdatedAtUnix: 1, History: []gamestate.PVPMatch{}}
				for _, deck := range state.Decks {
					if deck.Index != 0 {
						continue
					}
					ownDecks[int(deck.ArthurType)-1] = gamestate.PVPDeckSelection{ArthurType: deck.ArthurType, JobType: deck.JobType, CardUniqueIDs: deck.CardUniqueIDs, SupportCardUniqueIDs: deck.SupportCardUniqueIDs}
				}
			})
			before := game.ClonePVPState(s.Snapshot(gamestate.State{}).PVP)
			api := &API{account: s, initialState: gamestate.State{}, pvpAccounts: scenario.states,
				pvpConfig: game.PVPConfig{ChallengeMax: 5, Fields: []game.PVPFieldConfig{{FieldID: 1}}, ReplayResult: 1, RankWinPoint: 10},
			}
			body, err := json.Marshal(map[string]any{"type": 1, "select_arthur_type": 1, "pvp_my_deck": ownDecks})
			if err != nil {
				t.Fatal(err)
			}
			response := httptest.NewRecorder()
			api.pvpStart(response, httptest.NewRequest(http.MethodPost, "/PvpStart2", bytes.NewReader(body)))
			if scenario.started {
				if response.Code != http.StatusOK || s.Snapshot(gamestate.State{}).PVP.Challenge != 1 || len(s.Snapshot(gamestate.State{}).PVP.History) != 1 || s.Snapshot(gamestate.State{}).PVP.History[0].OpponentUserID != 3 {
					t.Fatalf("usable opponent blocked or charged incorrectly: status=%d challenge=%d history=%+v body=%s", response.Code, s.Snapshot(gamestate.State{}).PVP.Challenge, s.Snapshot(gamestate.State{}).PVP.History, response.Body.String())
				}
			} else {
				var common struct {
					Code int    `json:"res_code"`
					Text string `json:"res_str"`
				}
				if err := json.Unmarshal(bytes.Split(response.Body.Bytes(), []byte{'\n'})[0], &common); err != nil {
					t.Fatal(err)
				}
				if response.Code != http.StatusOK || common.Code != -1200 || common.Text == "" {
					t.Fatalf("expected stock arena error callback: status=%d body=%s", response.Code, response.Body.String())
				}
				if !reflect.DeepEqual(s.Snapshot(gamestate.State{}).PVP, before) || s.Snapshot(gamestate.State{}).User.PVPPoint != 0 {
					t.Fatalf("rejected match changed defense or currency: before=%+v after=%+v points=%d", before, s.Snapshot(gamestate.State{}).PVP, s.Snapshot(gamestate.State{}).User.PVPPoint)
				}
			}
		})
	}
}
