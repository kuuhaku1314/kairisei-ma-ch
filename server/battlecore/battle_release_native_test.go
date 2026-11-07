package battlecore

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"testing"
)

func TestNativeReleaseGroupsKeepIntermediateStateAndOtherLists(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	p := &engine.players[0]
	p.BaseAttack, p.Attack, p.BaseMagic, p.Magic = 1000, 1100, 1000, 1200
	p.Effects = []battleEffect{
		{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 100, Kind: 1},
		{Function: "ATK_UP_FIXED", Parameter: "INT", Delta: 200, Kind: 1},
		{Function: "REGENERATE_FIXED", ListType: 5, Kind: 1},
		{Function: "DEAL_BONUS", Value: 2, Kind: 1},
		{Function: "ENDURE", Kind: 1},
	}
	role := CombatSkillRole{Function: "BUFF_RELEASE_ONE", Target: "SELECT", Parameters: [10]string{"100", "0", "ATK_UP_BY_ATK", "ATK_UP_BY_INT"}}
	rows, err := engine.executeEnemyRelease(&engine.enemies[0], 1, role)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{66, 72, 71, 6, 72, 71, 6}
	if len(rows) != len(want) {
		t.Fatalf("wrong per-selector lifecycle: %+v", rows)
	}
	for i, command := range want {
		if rows[i].Command != command {
			t.Fatalf("wrong command %d: %+v", i, rows)
		}
	}
	if rows[3].Args[3] != 1000 || rows[3].Args[4] != 1200 || rows[6].Args[4] != 1000 {
		t.Fatalf("parameter snapshots were flattened: %+v", rows)
	}
	role.Function, role.Parameters = "BUFF_RELEASE", [10]string{"100"}
	rows, err = engine.executeEnemyRelease(&engine.enemies[0], 1, role)
	if err != nil || len(rows) != 1 || rows[0].Command != CommandBuffReleaseFailed || len(p.Effects) != 3 {
		t.Fatalf("ALL must preserve Burst and positive SPECIAL states: %+v %+v", rows, p.Effects)
	}
	role.Function, role.Parameters = "BUFF_RELEASE_ONE", [10]string{"100", "0", "DEAL_BONUS", "ENDURE"}
	rows, err = engine.executeEnemyRelease(&engine.enemies[0], 1, role)
	if err != nil || len(p.Effects) != 1 || p.Effects[0].ListType != 5 || len(rows) != 7 {
		t.Fatalf("typed ONE can remove SPECIAL but not Burst: %+v %+v", rows, p.Effects)
	}
}

func TestNativeAllReleaseCategoryOrderAndEventUI(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	p := &engine.players[0]
	p.Effects = []battleEffect{
		{Function: "REGENERATE_FIXED", Kind: 1}, // GOOD_STATUS precedes BUFF in insertion order.
		{Function: "CRITICAL_UP", Kind: 1},
		{Function: "CRITICAL_UP", Kind: 1, ListType: 2}, // EVENT keeps its grouped UI.
	}
	role := CombatSkillRole{Function: "BUFF_RELEASE", Target: "SELECT", Parameters: [10]string{"100"}}
	rows, err := engine.executeEnemyRelease(&engine.enemies[0], 1, role)
	if err != nil {
		t.Fatal(err)
	}
	want := []int{66, 71, 6, 72, 71, 6}
	if len(rows) != len(want) {
		t.Fatalf("wrong category/Event release: %+v", rows)
	}
	for i, command := range want {
		if rows[i].Command != command {
			t.Fatalf("wrong row %d: %+v", i, rows)
		}
	}
	if rows[1].Args[1] != 10 || rows[4].Args[1] != 200 || len(p.Effects) != 1 || p.Effects[0].ListType != 2 {
		t.Fatalf("ALL must release BUFF before GOOD_STATUS and retain EVENT: %+v %+v", rows, p.Effects)
	}
}

func TestNativeTypedReleaseNullPreflightDoesNotDeleteZeroDelta(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.players[0].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "ATK", Delta: 0, Kind: 1}}
	role := CombatSkillRole{Function: "BUFF_RELEASE_ONE", Target: "SELECT", Parameters: [10]string{"100", "0", "HEAL", "NULL"}}
	rows, err := engine.executeEnemyRelease(&engine.enemies[0], 1, role)
	if err != nil || len(rows) != 1 || rows[0].Command != 66 || len(engine.players[0].Effects) != 1 {
		t.Fatalf("native preflight NULL hit must not remove a state or fail: %+v %v", rows, err)
	}
}

func TestNativeReleaseSelectionBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_RELEASE_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_RELEASE_RECEIPT to original native selector receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Name, Mode string
			Good       bool
			Kinds      []int
			Limit      int
			Effects    []struct {
				Function, Parameter string
				Code, Delta         int
				ListType            int `json:"list_type"`
			}
			Groups [][]int
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-release-selectors" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid original-binary release receipt")
	}
	for _, vector := range receipt.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			var effects []battleEffect
			for _, effect := range vector.Effects {
				if code, ok := battleBuffCodes[effect.Function]; !ok || code != effect.Code {
					t.Fatal("effect identity mismatch")
				}
				effects = append(effects, battleEffect{Function: effect.Function, Parameter: effect.Parameter, Delta: effect.Delta, ListType: effect.ListType})
			}
			role := CombatSkillRole{Function: "DEBUFF_RELEASE", Parameters: [10]string{"100"}}
			kinds := map[int]string{2: "ATK_BREAK_BY_INT", 7: "POISON", 16: "ATTR_SEE", 21: "DEAL_PENALTY"}
			if vector.Good {
				role.Function = "BUFF_RELEASE"
				kinds = map[int]string{1: "ATK_UP_BY_ATK", 8: "HEAL", 12: "DEAL_BONUS", 32: "ATK_UP_BY_MAX_HP", 36: "ENDURE"}
			}
			if vector.Mode == "ONE" {
				role.Function += "_ONE"
				for i, kind := range vector.Kinds {
					name, ok := kinds[kind]
					if !ok {
						t.Fatalf("unmapped typed kind %d", kind)
					}
					role.Parameters[i+2] = name
				}
				if vector.Limit > 0 {
					role.Function += "_NUM"
					role.Parameters[4] = strconv.Itoa(vector.Limit)
				}
			}
			engine := &BattleEngine{rng: newXorShift128(1)}
			got := engine.selectReleasedEffectIndexes(effects, role, 1)
			if !reflect.DeepEqual(got.groups, vector.Groups) {
				t.Fatalf("Go groups=%v original=%v", got.groups, vector.Groups)
			}
		})
	}
}
