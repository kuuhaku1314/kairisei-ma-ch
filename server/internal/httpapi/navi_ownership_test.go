package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestExpandedNavigatorOwnershipDoesNotWrapLegacyMask(t *testing.T) {
	s := testAccount(t, func(state *gamestate.State) {
		state.User.Coin, state.User.CoinFree = 0, 1000
		state.User.NaviPurchasePrice = 10
		state.User.NaviUnlockFlag = 1
		state.User.SelectableNaviIDs = []int8{0}
		state.User.NaviCatalogIDs = []int8{0, 62, 63, 64, 70, 127}
	})
	for _, id := range []int8{62, 63, 64, 70, 127} {
		if err := s.PurchaseNavi(id); err != nil || !s.SelectNavi(id) {
			t.Fatalf("purchase/select %d: %v", id, err)
		}
		balance := s.Snapshot(gamestate.State{}).User.CoinFree
		if err := s.PurchaseNavi(id); err == nil || s.Snapshot(gamestate.State{}).User.CoinFree != balance {
			t.Fatalf("duplicate navigator %d was charged", id)
		}
	}
	if s.SelectNavi(6) || s.SelectNavi(-1) || s.Snapshot(gamestate.State{}).User.CoinFree != 950 {
		t.Fatal("navigator IDs wrapped or purchase amount changed")
	}
	mask, ids := s.NaviUnlockState()
	want := []int8{0, 62, 63, 64, 70, 127}
	if mask != int64(1)|(int64(1)<<62) || !slices.Equal(ids, want) {
		t.Fatalf("connect ownership: mask %d, IDs %v", mask, ids)
	}
	// Exercise the real connect response, including array encoding for int8 IDs.
	a := &API{account: s, initialState: gamestate.State{}}
	w := httptest.NewRecorder()
	a.connect(w, httptest.NewRequest("POST", "/", strings.NewReader(`{"session":"test"}`)))
	var body struct {
		Mask int64  `json:"navi_unlock_flag"`
		IDs  []int8 `json:"navi_unlock_ids"`
	}
	lines := bytes.Split(w.Body.Bytes(), []byte{'\n'})
	if len(lines) < 2 || json.Unmarshal(lines[1], &body) != nil || body.Mask != mask || !slices.Equal(body.IDs, want) {
		t.Fatalf("connect response lost ownership: %s", w.Body.String())
	}
}

func TestMissionNavigatorMailSingleAndBatchReceiptSurviveReload(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.User.NaviID = 0
	state.User.NaviUnlockFlag = 1
	state.User.SelectableNaviIDs = []int8{0}
	state.User.NaviCatalogIDs = []int8{0, 1, 70}
	state.MissionDefinitions = map[int]gamestate.MissionDefinition{
		1005001: {MissionID: 1005001, Published: true, Title: "累计登录10天", ProgressMax: 10,
			Rewards: []gamestate.Reward{{Type: 11, RewardTypeID: 1, Num: 0}, {Type: 11, RewardTypeID: 70, Num: 1}}},
	}
	state.Engagement.Missions = []gamestate.Mission{{Info: gamestate.MissionProgress{MissionID: 1005001, State: 1, ProgressNow: 10}}}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	gold, crystals := state.User.Gold, state.User.CoinFree
	saves := 0
	a := &API{account: account, initialState: accountSnapshotBase(state), persistState: func(next gamestate.State) error {
		state = next
		saves++
		return nil
	}}
	response := httptest.NewRecorder()
	a.missionReward(response, httptest.NewRequest("POST", "/MissionReward", strings.NewReader(`{"missionids":[1005001]}`)))
	if response.Code != 200 || len(state.Engagement.Presents) != 2 || len(state.User.SelectableNaviIDs) != 1 {
		t.Fatal("claim did not persist reward mail without granting early", response.Body.String())
	}
	id := state.Engagement.Presents[0].PresentID
	response = httptest.NewRecorder()
	a.presentBoxRecv(response, httptest.NewRequest("POST", "/PresentBoxRecv", strings.NewReader(`{"presentid":`+strconv.FormatInt(id, 10)+`}`)))
	var body struct {
		Rewards []struct {
			Reward gamestate.Reward `json:"reward"`
		} `json:"result_rewards"`
	}
	lines := bytes.Split(response.Body.Bytes(), []byte{'\n'})
	if response.Code != 200 || len(lines) < 2 || json.Unmarshal(lines[1], &body) != nil || len(body.Rewards) != 1 || body.Rewards[0].Reward.Type != 11 || body.Rewards[0].Reward.Num != 0 || body.Rewards[0].Reward.RewardTypeID != 1 {
		t.Fatal("single receipt changed native unlock fields", response.Body.String())
	}
	response = httptest.NewRecorder()
	a.presentBoxMultiRecv(response, httptest.NewRequest("POST", "/PresentBoxMultiRecv2", strings.NewReader(`{"is_coin_recv":1,"receive_types":[2]}`)))
	if response.Code != 200 || saves != 3 || !slices.Equal(state.User.SelectableNaviIDs, []int8{0, 1, 70}) || state.User.NaviUnlockFlag != 3 || state.User.Gold != gold || state.User.CoinFree != crystals {
		t.Fatal("batch unlock was not persisted or charged currency", response.Body.String())
	}
	reloaded, err := game.New(state)
	if err != nil || !reloaded.SelectNavi(70) || reloaded.SelectNavi(6) {
		t.Fatal("reloaded navigator ownership wrapped or disappeared", err)
	}
	if _, _, err := reloaded.ReceiveMissionRewards([]int{1005001}); err == nil {
		t.Fatal("claimed navigator mission issued mail twice")
	}
	duplicate, err := reloaded.ReceivePresent(id)
	if err != nil || len(duplicate.Rewards) != 0 || len(duplicate.PresentID) != 0 || len(duplicate.FailedID) != 0 {
		t.Fatal("received navigator mail granted twice", err)
	}
}
