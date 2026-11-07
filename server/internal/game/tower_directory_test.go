package game

import (
	"encoding/json"
	"kairisei.local/server/internal/gamestate"
	"testing"
	"time"
)

func TestTowerDirectoryProjectsProgressWithoutConsumingResults(t *testing.T) {
	profile := gamestate.TowerQuestProfile{TowerID: 7, Name: "塔", Floors: []gamestate.TowerQuestFloorProfile{
		{Floor: 1, Boss: json.RawMessage(`{"9":99}`)},
		{Floor: 2, Story: gamestate.TowerQuestStoryProfile{TalkID: 77, Title: "已读"}},
		{Floor: 3, Story: gamestate.TowerQuestStoryProfile{TalkID: 88, Title: "未读"}},
	}}
	for _, tc := range []struct {
		floor, last, state int
		cleared            []int
	}{
		{1, 0, 0, nil}, {3, 1, 1, []int{1, 2}}, {0, 3, 2, []int{1, 2, 3}},
	} {
		a := &Account{towerQuestProfiles: map[int]gamestate.TowerQuestProfile{7: profile}, towerQuestProgress: map[int]gamestate.TowerQuestProgress{
			7: {TowerID: 7, Floor: tc.floor, LastBattleFloor: tc.last, ClearedFloors: tc.cleared, LastResult: "win", LastRankUp: true},
		}}
		before, _ := json.Marshal(a.towerQuestProgress)
		entries := a.TowerQuestDirectory()
		if len(entries) != 1 || entries[0].TowerID != 7 || entries[0].State != tc.state || len(entries[0].Stories) != max(0, len(tc.cleared)-1) {
			t.Fatalf("wrong root for floor %d: %+v", tc.floor, entries)
		}
		entries[0].PictureBoss[0] = 'x'
		a.TowerQuestDirectory()
		after, _ := json.Marshal(a.towerQuestProgress)
		if string(before) != string(after) || a.towerQuestProfiles[7].Floors[0].Boss[0] != '{' {
			t.Fatal("directory mutated profile or consumed result")
		}
		a.ApplyTowerAdmission(TowerAdmission{StartUnix: time.Now().Unix() + 3600})
		if len(a.TowerQuestDirectory()) != 0 {
			t.Fatal("future tower visible")
		}
		a.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{}})
		if len(a.TowerQuestDirectory()) != 0 {
			t.Fatal("closed tower visible")
		}
		a.ApplyTowerAdmission(TowerAdmission{TowerIDs: map[int]struct{}{7: {}}})
		if len(a.TowerQuestDirectory()) != 1 {
			t.Fatal("allowed tower missing")
		}
	}
}
