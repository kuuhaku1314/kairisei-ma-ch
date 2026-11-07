package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestNativeFixedParameterOverflowReachesBattleState(t *testing.T) {
	// Official enemy 38111507 and 35809601 have these exact p0..p5 values.
	// Original x86/ARM producer values remain raw; JP commit applies the
	// magnitude minimum separately (E020/E024 original native enemy-self controls).
	for _, tc := range []struct {
		function string
		params   [10]string
		want     int
	}{
		{"DEF_UP_FIXED", [10]string{"2", "DEF", "3000000", "1000", "0", "0"}, 1},
		// JP consumer clamps this negative producer output to a one-point debuff.
		{"GUARD_BREAK_FIXED", [10]string{"99", "DEF", "8500000", "1000", "0", "0"}, -1},
	} {
		t.Run(tc.function, func(t *testing.T) {
			engine, _ := coreBattleFixture(t)
			role := CombatSkillRole{Function: tc.function, Target: "SELF", Parameters: tc.params}
			rows, err := engine.executeEnemyParameterRole(&engine.enemies[0], 5, role)
			if err != nil || len(rows) != 2 || len(engine.enemies[0].Effects) != 1 {
				t.Fatalf("failed original parameter action: rows=%+v err=%v", rows, err)
			}
			if delta := engine.enemies[0].Effects[0].Delta; delta != tc.want {
				t.Fatalf("committed delta=%d, original=%d", delta, tc.want)
			}
		})
	}
}

func TestNativeFixedParameterBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_FIXED_PARAMETER_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_FIXED_PARAMETER_RECEIPT to original fixed-producer receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Side, Function string
			RoleSet        int `json:"role_set"`
			Level          int
			Parameters     [10]string
			First, Second  int32
			Display        int
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-fixed-parameter-producers" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid original fixed-producer receipt")
	}
	for i, v := range receipt.Vectors {
		role := CombatSkillRole{Function: v.Function, Parameters: v.Parameters}
		first, second := fixedBuffRoleSegments(role, v.Level)
		if first != v.First || second != v.Second || fixedBuffRoleValue(role, v.Level, 1) != v.Display {
			t.Fatalf("vector %d %s role=%d L%d: Go=(%d,%d), native=(%d,%d,%d)", i, v.Function, v.RoleSet, v.Level, first, second, v.First, v.Second, v.Display)
		}
		if v.Side == "enemy" && fixedBuffRoleValue(role, EnemySkillLevel(v.Function), 1) != v.Display {
			t.Fatalf("enemy runtime level differs at role %d", v.RoleSet)
		}
		// The production hand-display path must use the same producer segments.
		engine := &BattleEngine{}
		got := engine.cardDisplayPower(&battlePlayer{}, v.Level, CombatSkillDefinition{DisplayRole: 1}, []CombatSkillRole{role})
		if got != v.Display {
			t.Fatal("display mismatch at vector " + strconv.Itoa(i))
		}
	}
}
