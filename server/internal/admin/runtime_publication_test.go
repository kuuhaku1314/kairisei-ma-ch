package admin

import (
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

type runtimeSettingsConsumer struct{ settings game.RuntimeSettings }

func (consumer *runtimeSettingsConsumer) ApplyRuntimeSettings(settings game.RuntimeSettings) {
	consumer.settings = settings
}

func TestMissionDraftDoesNotInvalidatePublishedPlayerConfiguration(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	editor, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := editor.missionConfig
	api := &API{operations: editor}
	request := httptest.NewRequest("POST", "http://localhost/api/mission-series", strings.NewReader(`{"expected_revision":0,"action":"save","series":{"name":"未完成的草稿","kind":"mission","layout":"independent","tasks":[{"title":"强化","rule":{"event":"card_fusion","target":1}}]}}`))
	request.RemoteAddr = "127.0.0.1:12345"
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("X-Kairisei-Admin-Action", "apply")
	response := httptest.NewRecorder()
	api.missionSeriesAction(response, request)
	if response.Code != 200 {
		t.Fatal(response.Code, response.Body.String())
	}
	doc, err := accounts.Database().ReadDocument(missionRegistryKey)
	if err != nil || doc.Revision != 1 {
		t.Fatal("draft was not saved", err)
	}
	if editor.missionConfig != previous || editor.Runtime.MissionCatalog() != previous.Catalog {
		t.Fatal("saving an unpublished draft invalidated player configuration")
	}
}

func TestMissionPreviewAndPublishRequireActionsInTimedTabs(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	editor, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	api := &API{operations: editor, catalogByKey: map[string]AdminCatalogEntry{
		adminCatalogKey(4, 0): {Kind: "gold", RewardType: 4, Name: "金币"},
	}}
	call := func(action string, tab int, rule string, want int, wantMessage string) {
		t.Helper()
		body := fmt.Sprintf(`{"expected_revision":0,"action":%q,"series":{"name":"测试系列","kind":"mission","tab_type":%d,"layout":"independent","ends_at":%d,"tasks":[{"title":"测试任务","rule":%s,"rewards":[{"type":4,"num":1}]}]}}`, action, tab, time.Now().Add(time.Hour).Unix(), rule)
		request := httptest.NewRequest("POST", "http://localhost/api/mission-series", strings.NewReader(body))
		request.RemoteAddr = "127.0.0.1:12345"
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("X-Kairisei-Admin-Action", "apply")
		response := httptest.NewRecorder()
		api.missionSeriesAction(response, request)
		if response.Code != want || !strings.Contains(response.Body.String(), wantMessage) {
			t.Fatalf("%s tab %d: status=%d body=%s", action, tab, response.Code, response.Body.String())
		}
	}
	const stateRule = `{"kind":"state","metric":"player_level","target":10}`
	const actionRule = `{"event":"card_fusion","target":1}`
	for _, tab := range []int{1, 3} {
		for _, action := range []string{"preview", "publish"} {
			call(action, tab, stateRule, 400, "只能使用动作计数")
		}
		call("preview", tab, actionRule, 200, "card_fusion")
	}
	call("publish", 3, actionRule, 200, "card_fusion")
}

func TestPlayerRuntimeDoesNotWaitForUncommittedEditor(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	editor, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	editor.configMu.Lock()
	defer editor.configMu.Unlock()
	previous := editor.runtimeSettings.TeamBattleSpeed
	editor.runtimeSettings.TeamBattleSpeed = 200 // Staged work, not a committed publication.
	consumer := &runtimeSettingsConsumer{}
	done := make(chan struct{})
	go func() { editor.Runtime.PrepareBusiness(consumer); close(done) }()
	select {
	case <-done:
		if consumer.settings.TeamBattleSpeed != previous {
			t.Fatal("player saw uncommitted operator settings")
		}
	case <-time.After(time.Second):
		t.Fatal("player request waited for the editor lock")
	}
}

func TestMissionStoryTargetsAdmitCurrentReadingAndLearningCatalog(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	catalog, err := accounts.Database().CatalogState()
	if err != nil {
		t.Fatal(err)
	}
	const readingID = 13001001
	catalog.MainQuestReadingSections = []gamestate.MainQuestReadingSectionDefinition{{
		BossGroups: []gamestate.MainQuestReadingGroupDefinition{{Stories: []gamestate.MainQuestReadingStoryDefinition{{StoryTeamBattleID: readingID}}}},
	}}
	accounts.Database().SetCatalog(catalog)
	operations, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	a := &API{operations: operations}
	rule := gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionStoryCleared, Target: 1}
	var targets missionTargetIndex
	for _, id := range []int{readingID, 45001000, 45001010} {
		rule.SubjectID = id
		if err := a.validateMissionTargets(rule, &targets); err != nil {
			t.Fatalf("published story target %d rejected: %v", id, err)
		}
	}
	rule.SubjectID = 99999999
	if err := a.validateMissionTargets(rule, &targets); err == nil {
		t.Fatal("unknown story identity was admitted")
	}
}

func TestMissionPVPRankTargetsFollowRankCatalog(t *testing.T) {
	a := &API{pvpRanks: []game.PVPRankConfig{{RankID: 0}, {RankID: 1}, {RankID: 2}}}
	rule := gamestate.MissionRule{Kind: gamestate.MissionStateValue, Metric: gamestate.MissionPVPRank}
	for target, valid := range map[int]bool{1: true, 2: true, 0: false, 999: false} {
		rule.Target = target
		if err := a.validateMissionTargets(rule, &missionTargetIndex{}); (err == nil) != valid {
			t.Fatalf("rank %d: err=%v, want valid=%v", target, err, valid)
		}
	}
}
