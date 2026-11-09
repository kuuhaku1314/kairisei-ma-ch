package admin

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"

	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
	"kairisei.local/server/internal/testfixture"
)

type towerAdmissionSink struct{ policy game.TowerAdmission }

func (s *towerAdmissionSink) ApplyTowerAdmission(policy game.TowerAdmission) { s.policy = policy }

func TestTowerPublicationIsItsOwnCatalog(t *testing.T) {
	accounts := testfixture.NewFriendCapacityTestAccounts(t)
	operator, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	operator.content = &contentStore{base: gamestate.State{TowerQuestProfiles: []gamestate.TowerQuestProfile{
		{TowerID: 1, Name: "炼金塔甲", Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1}}},
		{TowerID: 2, Name: "炼金塔乙", Floors: []gamestate.TowerQuestFloorProfile{{Floor: 1}}},
	}}}
	admin := &API{operations: operator, knownGroups: map[int]struct{}{7: {}}}
	router := chi.NewRouter()
	router.Get("/policy", admin.bossPolicy)
	router.Put("/policy", admin.setBossPolicy)
	call := func(method string, body any, status int) struct {
		Publication adminPublicationState         `json:"publication"`
		Towers      []adminTowerPublicationOption `json:"towers"`
	} {
		t.Helper()
		var result struct {
			Publication adminPublicationState         `json:"publication"`
			Towers      []adminTowerPublicationOption `json:"towers"`
		}
		content := testfixture.CallContentAdmin(t, router, method, "/policy?catalog=tower", body, status)
		if status == 200 && json.Unmarshal(content, &result) != nil {
			t.Fatal("decode tower publication")
		}
		return result
	}
	initial := call("GET", nil, 200)
	if initial.Publication.Revision != 0 || initial.Publication.Mode != "all" || len(initial.Towers) != 2 || !initial.Towers[0].EffectiveEnabled {
		t.Fatal("towers were not open by default", initial)
	}
	open := call("PUT", map[string]any{"mode": "allowlist", "group_ids": []int{2, 1, 1}, "expected_revision": 0}, 200)
	if open.Publication.Revision != 1 || len(open.Publication.GroupIDs) != 2 || open.Publication.GroupIDs[0] != 1 || !open.Towers[1].EffectiveEnabled {
		t.Fatal("tower list was not published", open)
	}
	var sink towerAdmissionSink
	operator.Runtime.PrepareBusiness(&sink)
	if !sink.policy.Allows(1, time.Now().Unix()) || !sink.policy.Allows(2, time.Now().Unix()) {
		t.Fatal("live tower publication was not applied")
	}
	call("PUT", map[string]any{"mode": "allowlist", "group_ids": []int{7}, "expected_revision": 1}, 400)
	selected := call("PUT", map[string]any{"mode": "allowlist", "group_ids": []int{2}, "expected_revision": 1}, 200)
	if selected.Publication.Revision != 2 || len(selected.Publication.GroupIDs) != 1 || selected.Towers[0].EffectiveEnabled || !selected.Towers[1].EffectiveEnabled {
		t.Fatal("tower allowlist differs from selected entries", selected)
	}
	// The activity Boss publication, closed and outside its window, leaves towers alone.
	activity := 0
	if _, err := operator.setBattlePublication(teamBattlePublicationKey, TeamBattlePublication{Mode: "allowlist", GroupIDs: []int{7}, EndUnix: 1, ExpectedRevision: &activity}); err != nil {
		t.Fatal(err)
	}
	restarted, err := NewOperations(accounts.Database(), nil)
	if err != nil {
		t.Fatal(err)
	}
	restarted.Runtime.PrepareBusiness(&sink)
	if sink.policy.Allows(1, time.Now().Unix()) || !sink.policy.Allows(2, time.Now().Unix()) {
		t.Fatal("tower publication did not survive restart")
	}
	future := time.Now().Unix() + 3600
	closedNow := call("PUT", map[string]any{"mode": "all", "start_unix": future, "expected_revision": 2}, 200)
	if closedNow.Towers[0].EffectiveEnabled {
		t.Fatal("future tower window opened early")
	}
	operator.Runtime.PrepareBusiness(&sink)
	if sink.policy.Allows(1, time.Now().Unix()) || !sink.policy.Allows(1, future+1) || !sink.policy.Allows(3, future+1) {
		t.Fatal("tower window was not applied")
	}
	call("PUT", map[string]any{"mode": "allowlist", "group_ids": []int{}, "expected_revision": 3}, 200)
	operator.Runtime.PrepareBusiness(&sink)
	if sink.policy.Allows(1, future+1) || sink.policy.Allows(2, future+1) {
		t.Fatal("empty tower selection did not close all towers")
	}
	call("PUT", map[string]any{"mode": "all", "expected_revision": 4}, 200)
	operator.Runtime.PrepareBusiness(&sink)
	if !sink.policy.Allows(1, time.Now().Unix()) || !sink.policy.Allows(2, time.Now().Unix()) {
		t.Fatal("all mode did not reopen every tower")
	}
}
