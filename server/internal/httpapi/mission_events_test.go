package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"kairisei.local/server/internal/accounthttp"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

func TestHomeMissionNoticeDistinguishesCompletionFromClaiming(t *testing.T) {
	now := time.Now()
	end := now.Add(time.Hour).Unix()
	state := testfixture.RuntimeState(t)
	state.MissionDefinitions = map[int]gamestate.MissionDefinition{
		810001: {MissionID: 810001, TabType: 1, Published: true, EndsAt: end, Title: "限时任务", ProgressMax: 1},
		810002: {MissionID: 810002, TabType: 3, Published: true, EndsAt: end, Title: "活动任务", ProgressMax: 1},
		810003: {MissionID: 810003, TabType: 0, Published: true, Title: "普通成就", ProgressMax: 1},
	}
	state.Engagement.Missions = []gamestate.Mission{
		{Info: gamestate.MissionProgress{MissionID: 810001, State: 1, ProgressNow: 1, ClearLimitTime: int(end)}},
		{Info: gamestate.MissionProgress{MissionID: 810002, State: 1, ProgressNow: 1, ClearLimitTime: int(end)}},
		{Info: gamestate.MissionProgress{MissionID: 810003, State: 1, ProgressNow: 1}},
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	account.SetMissionRequestTime(now)
	a := &API{account: account, initialState: state}
	check := func(total, complete int, claimable int8) {
		t.Helper()
		response := httptest.NewRecorder()
		a.writeProtocol(response, map[string]any{})
		var common commonResponse
		if json.Unmarshal([]byte(strings.SplitN(response.Body.String(), "\n", 2)[0]), &common) != nil || len(common.Notifications) != 1 {
			t.Fatal("invalid notification", response.Body.String())
		}
		notice := common.Notifications[0]
		if notice.MissionTotal != total || notice.MissionComplete != complete || notice.MissionClearReceive != claimable {
			t.Fatalf("mission notice=%+v, want %d/%d flag=%d", notice, complete, total, claimable)
		}
	}
	check(2, 2, 1) // All complete; neither reward claimed.
	if _, _, err = account.ReceiveMissionRewards([]int{810001}); err != nil {
		t.Fatal(err)
	}
	check(2, 2, 1) // The event reward still needs claiming.
	if _, _, err = account.ReceiveMissionRewards([]int{810002}); err != nil {
		t.Fatal(err)
	}
	check(2, 2, 1) // Ordinary achievements do not change the counts, but still remind players to claim.
	if !account.MissionNotice().Claimable {
		t.Fatal("ordinary achievement lost its independent claimable state")
	}
	account.SetMissionRequestTime(now.Add(2 * time.Hour))
	check(0, 0, 1) // With no timed tasks, the ordinary reward can still flash without a count.
	account.SetMissionRequestTime(now)
	if _, _, err = account.ReceiveMissionRewards([]int{810003}); err != nil {
		t.Fatal(err)
	}
	check(2, 2, 0) // Once every reward is claimed, Home may hide the complete period counter.
	account.SetMissionRequestTime(now.Add(2 * time.Hour))
	check(0, 0, 0) // Expired timed tasks leave the current period's totals.
}

func TestExploreMissionDeltasShareCommitAndDoNotReplay(t *testing.T) {
	state := testfixture.RuntimeState(t)
	state.User.AP, state.User.APMax = 2, 3
	state.Explore.Stages = []gamestate.ExploreStage{{ExploreStageID: 1}}
	state.Explore.StageCursor = 0
	state.MissionDefinitions = map[int]gamestate.MissionDefinition{
		6002512: {MissionID: 6002512, TabType: 1, Published: true, Daily: true, Title: "每日探索一次", ProgressMax: 1, Rule: &gamestate.MissionRule{Event: gamestate.MissionExploreClear, Target: 1}},
		7000030: {MissionID: 7000030, TabType: 3, Published: true, Daily: true, Title: "每日探索三次", ProgressMax: 3, Rule: &gamestate.MissionRule{Event: gamestate.MissionExploreClear, Target: 3}},
		2007001: {MissionID: 2007001, Published: true, EndsAt: time.Now().Add(time.Hour).Unix(), Title: "成功通关三次", ProgressMax: 3, OpenURL: "teambattle", Rule: &gamestate.MissionRule{Event: gamestate.MissionQuestClear, Target: 3}},
	}
	account, err := game.New(state)
	if err != nil {
		t.Fatal(err)
	}
	if status, _, _, _, ok := account.BeginExplore(state.Decks[0].ArthurType, state.Decks[0].Index, []gamestate.Reward{}); !ok {
		t.Fatalf("test exploration did not start: AP=%+v active=%v Arthur=%d deck=%d leader=%d cards=%d", status, account.ExploreIsActive(), state.Decks[0].ArthurType, state.Decks[0].Index, state.Decks[0].LeaderCardIndex, len(state.Cards))
	}
	durable := account.Snapshot(state)
	failed, builds, saves := true, 0, 0
	router := accounthttp.New(accounthttp.Config{IdleLimit: 1, Build: func(int) (http.Handler, error) {
		builds++
		account, err := game.New(durable)
		if err != nil {
			return nil, err
		}
		a := &API{account: account, initialState: accountSnapshotBase(durable), logger: slog.New(slog.NewTextHandler(io.Discard, nil)), persistState: func(next gamestate.State) error {
			if failed {
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
		request := httptest.NewRequest(http.MethodPost, "/ExploreEnd", strings.NewReader(`{}`))
		request.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(state.User.UserID))
		router.ServeHTTP(response, request)
		return response
	}
	if response := request(); response.Code != http.StatusInternalServerError || saves != 0 || !durable.Explore.Active || len(durable.Engagement.Missions) != 0 {
		t.Fatal("failed persistence exposed or saved mission progress", response.Body.String())
	}
	failed = false
	response := request()
	var common commonResponse
	lines := strings.Split(response.Body.String(), "\n")
	if response.Code != http.StatusOK || len(lines) < 2 || json.Unmarshal([]byte(lines[0]), &common) != nil || builds != 2 || saves != 1 || len(common.Missions) != 2 {
		t.Fatal("committed exploration did not produce only its two mission deltas", response.Body.String())
	}
	if len(common.Notifications) != 1 || common.Notifications[0].MissionTotal != 2 || common.Notifications[0].MissionComplete != 1 || common.Notifications[0].MissionClearReceive != 1 {
		t.Fatalf("expected only the two timed limited/event tasks and one unclaimed completion: %+v", common.Notifications)
	}
	for _, info := range common.Missions {
		if info.ProgressNow != 1 || (info.MissionID == 6002512 && info.State != 1) || (info.MissionID == 7000030 && info.State != 0) {
			t.Fatal("increment or completion status differs from the committed event", info)
		}
	}
	response = request()
	lines = strings.Split(response.Body.String(), "\n")
	if response.Code != http.StatusOK || json.Unmarshal([]byte(lines[0]), &common) != nil || len(common.Missions) != 0 || saves != 1 {
		t.Fatal("cached result repeated a mission event or sent the whole mission list", response.Body.String())
	}
	reloaded, err := game.New(durable)
	if err != nil || len(reloaded.TakeMissionUpdates()) != 0 || len(reloaded.MissionInfos(false)) != 3 {
		t.Fatal("progress/definitions failed to reload or transient notifications were persisted", err)
	}
	for _, info := range reloaded.MissionInfos(false) {
		if (info.MissionID == 2007001 && info.ProgressNow != 0) || (info.MissionID != 2007001 && info.ProgressNow != 1) {
			t.Fatal("reloaded mission progress differs", info)
		}
		if info.MissionID == 2007001 && info.OpenURL != "mainquest" {
			t.Fatal("existing clear mission kept its old battle directory", info)
		}
	}
	claim := httptest.NewRecorder()
	claimRequest := httptest.NewRequest(http.MethodPost, "/MissionReward", strings.NewReader(`{"missionids":[6002512]}`))
	claimRequest.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(state.User.UserID))
	router.ServeHTTP(claim, claimRequest)
	if claim.Code != http.StatusOK || json.Unmarshal([]byte(strings.SplitN(claim.Body.String(), "\n", 2)[0]), &common) != nil || common.ResultCode != 0 || len(common.Notifications) != 1 || common.Notifications[0].MissionTotal != 2 || common.Notifications[0].MissionComplete != 1 || common.Notifications[0].MissionClearReceive != 0 {
		t.Fatal("claim changed completion counts or failed to clear the claimable flag", claim.Body.String())
	}
	history := httptest.NewRecorder()
	historyRequest := httptest.NewRequest(http.MethodPost, "/MissionShow", strings.NewReader(`{"is_reward":1}`))
	historyRequest.Header.Set(accounthttp.AccountUserHeader, strconv.Itoa(state.User.UserID))
	router.ServeHTTP(history, historyRequest)
	var list struct{ Missions []gamestate.MissionInfo }
	historyLines := strings.Split(history.Body.String(), "\n")
	if history.Code != http.StatusOK || len(historyLines) < 2 || json.Unmarshal([]byte(historyLines[1]), &list) != nil ||
		len(list.Missions) != 3 || !slices.ContainsFunc(list.Missions, func(info gamestate.MissionInfo) bool {
		return info.MissionID == 6002512 && info.State == 2
	}) {
		t.Fatal("mission history did not include the claimed task", history.Body.String())
	}
}
