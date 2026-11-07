package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestTeamBattleSkipPersistsWholeBatchAndReloadsAfterSaveFailure(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.User.Gold, state.User.BP, state.User.BPMax = 0, 100, 100
	state.Items = append(state.Items, gamestate.Item{ItemID: 900001, Num: 10})
	state.ItemDefinitions = append(state.ItemDefinitions, gamestate.ItemDefinition{ItemID: 900001, MaxOwned: 9999})
	state.TowerQuestProfiles = []gamestate.TowerQuestProfile{{TowerID: 7, LoseCountMax: 3,
		Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1, BPUse: 30, Boss: json.RawMessage(`{"0":123,"skip_itemid":900001}`)}}}}
	state.TowerQuestProgress = []gamestate.TowerQuestProgress{{TowerID: 7, ClearedFloors: []int{1}, ClearRatings: map[int]int{1: 7}}}
	state.TeamBattleRewards = []gamestate.TeamBattleRewardProfile{{BossID: 123, TowerID: 7, TowerFloor: 1, ResultRewards: []gamestate.Reward{{Type: 4, Num: 5}}}}
	state.TeamBattleReplays = []gamestate.TeamBattleReplay{{BossID: 123, EnemyPartyID: 1}}
	durable := state
	fail, builds, saves := true, 0, 0
	router := accounthttp.New(accounthttp.Config{IdleLimit: 1, Build: func(int) (http.Handler, error) {
		builds++
		account, err := game.New(durable)
		if err != nil {
			t.Fatal(err)
		}
		account.ApplyTowerAdmission(game.TowerAdmission{TowerIDs: map[int]struct{}{7: {}}})
		a := &API{initialState: accountSnapshotBase(durable), account: account,
			logger: slog.New(slog.NewTextHandler(io.Discard, nil)), persistState: func(next gamestate.State) error {
				if fail {
					return errors.New("forced save failure")
				}
				durable = next
				saves++
				return nil
			}}
		mux := chi.NewRouter()
		a.register(mux)
		return mux, nil
	}})
	defer router.InvalidateAll()
	request := func() *httptest.ResponseRecorder {
		response := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, "/TeamBattleSkip", strings.NewReader(`{"bossid":123,"num":2}`))
		request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(state.User.UserID))
		router.ServeHTTP(response, request)
		return response
	}
	if response := request(); response.Code != http.StatusInternalServerError || durable.User.Gold != 0 || saves != 0 {
		t.Fatal("failed save exposed or persisted the batch", response.Body.String())
	}
	fail = false
	response := request()
	lines := strings.Split(strings.TrimSpace(response.Body.String()), "\n")
	var payload struct {
		Battles []struct {
			Rewards []struct {
				Reward struct {
					Type int `json:"type"`
					Num  int `json:"num"`
				} `json:"reward"`
			} `json:"reward"`
		} `json:"battle_rewards"`
	}
	if response.Code != http.StatusOK || len(lines) != 3 || json.Unmarshal([]byte(lines[1]), &payload) != nil || len(payload.Battles) != 2 {
		t.Fatal("skip response did not preserve per-run results", response.Body.String())
	}
	for _, battle := range payload.Battles {
		if len(battle.Rewards) != 1 || battle.Rewards[0].Reward.Type != 4 || battle.Rewards[0].Reward.Num != 5 {
			t.Fatal("skip response changed the granted reward")
		}
	}
	if builds != 2 || saves != 1 || durable.User.Gold != 10 || durable.User.BP != 40 {
		t.Fatalf("batch was partially saved or charged twice: builds=%d saves=%d gold=%d BP=%d", builds, saves, durable.User.Gold, durable.User.BP)
	}
	for _, item := range durable.Items {
		if item.ItemID == 900001 && item.Num != 8 {
			t.Fatal("ticket debit did not share the saved batch")
		}
	}
}
