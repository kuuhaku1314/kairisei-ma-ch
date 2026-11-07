package game

import (
	"encoding/json"
	"testing"
)

func TestMarkStandaloneTeamBattleClearOnlyClearsSelectedDifficulty(t *testing.T) {
	configuration, err := json.Marshal(map[string]any{
		"9": []any{},
		"10": []any{map[string]any{
			"0": 760000012,
			"9": 0,
			"10": []any{
				map[string]any{"0": 30010102, "10": 0},
				map[string]any{"0": 30010103, "10": 0},
				map[string]any{"0": 30010104, "10": 2},
			},
		}},
		"11": []any{},
		"12": []any{},
	})
	if err != nil {
		t.Fatal(err)
	}

	state := battleCatalogFixture(t, configuration)
	firstClear, err := state.firstClear(30010102, 0)
	state.markClear(30010102, 0)
	updated := state.saved()
	if err != nil {
		t.Fatalf("mark clear: %v", err)
	}
	if !firstClear {
		t.Fatal("first clear = false, want true")
	}
	var top map[string][]struct {
		Bosses []struct {
			BossID int `json:"0"`
			State  int `json:"10"`
		} `json:"10"`
	}
	if err := json.Unmarshal(updated, &top); err != nil {
		t.Fatal(err)
	}
	want := map[int]int{30010102: 2, 30010103: 0, 30010104: 2}
	for _, boss := range top["10"][0].Bosses {
		if expected, exists := want[boss.BossID]; !exists || boss.State != expected {
			t.Fatalf("boss %d state = %d, want %d", boss.BossID, boss.State, expected)
		}
	}

	firstClear, err = state.firstClear(30010102, 0)
	if err != nil {
		t.Fatalf("repeat clear: %v", err)
	}
	if firstClear {
		t.Fatal("repeat first clear = true, want false")
	}
}

func TestMarkStageTeamBattleClearUsesMatchingArea(t *testing.T) {
	configuration := json.RawMessage(`{"9":[{"0":100001,"9":100001,"10":[{"0":10000101,"10":0}]},{"0":100002,"9":100002,"10":[{"0":10000101,"10":0}]}],"10":[],"11":[],"12":[]}`)
	state := battleCatalogFixture(t, configuration)
	firstClear, err := state.firstClear(10000101, 100001)
	state.markClear(10000101, 100001)
	updated := state.saved()
	if err != nil {
		t.Fatalf("mark stage clear: %v", err)
	}
	if !firstClear {
		t.Fatal("stage first clear = false, want true")
	}
	var top map[string][]struct {
		AreaID int `json:"9"`
		Bosses []struct {
			State int `json:"10"`
		} `json:"10"`
	}
	if err := json.Unmarshal(updated, &top); err != nil {
		t.Fatal(err)
	}
	if top["9"][0].Bosses[0].State != 2 || top["9"][1].Bosses[0].State != 0 {
		t.Fatalf("stage clear leaked across areas: %+v", top["9"])
	}
}

func TestMarkStageQuestClearUpdatesNestedBossState(t *testing.T) {
	configuration := json.RawMessage(`{"stage_quest":{"areaid":100001,"stage_object":[{"stageid":10000101,"is_clear_done":0,"raid_boss":[{"boss_group":{"bosses":[{"bossid":10000101,"state":0}]},"clear_reward":[]}]}]},"stage_clear":[],"new_clear_stage":[]}`)
	updated, firstClear, err := markStageQuestClear(configuration, 100001, 10000101)
	if err != nil {
		t.Fatalf("mark StageQuest clear: %v", err)
	}
	if !firstClear {
		t.Fatal("StageQuest first clear = false, want true")
	}
	var progress struct {
		StageQuest struct {
			Stages []struct {
				IsClearDone int `json:"is_clear_done"`
				Raids       []struct {
					BossGroup struct {
						Bosses []struct {
							State int `json:"state"`
						} `json:"bosses"`
					} `json:"boss_group"`
				} `json:"raid_boss"`
			} `json:"stage_object"`
		} `json:"stage_quest"`
	}
	if err := json.Unmarshal(updated, &progress); err != nil {
		t.Fatal(err)
	}
	stage := progress.StageQuest.Stages[0]
	if stage.IsClearDone != 1 || stage.Raids[0].BossGroup.Bosses[0].State != 2 {
		t.Fatalf("StageQuest progress is not synchronized: %+v", stage)
	}
}
