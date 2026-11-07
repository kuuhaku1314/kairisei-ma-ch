package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestNativeAIVariableBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_ENEMY_AI_VARIABLES_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_ENEMY_AI_VARIABLES_RECEIPT to the original native receipt")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Operations   []struct {
			Operation            string
			Member, Index, Delta int
			Variables            [2][5]int32
			Conditions           []struct {
				Member          int
				Parameters      [3]int
				Trigger, Branch bool
			}
		}
	}
	if err := json.Unmarshal(raw, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-pve-ai-variable-state-predicates" ||
		receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Operations) == 0 {
		t.Fatal("invalid native AI variable receipt")
	}
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType, engine.enemyCount = 6, 2
	for _, op := range receipt.Operations {
		if op.Operation == "turn_reset" {
			engine.phase = PhaseChaliceEnemy
			if _, err := engine.TurnPhase(); err != nil {
				t.Fatal(err)
			}
		} else {
			role := CombatSkillRole{Function: "ENEMY_AI_VAR_ADD", Target: "SELECT",
				Parameters: [10]string{strconv.Itoa(op.Index), strconv.Itoa(op.Delta)}}
			if _, err := engine.executeEnemyRole(&engine.enemies[0], op.Member, role, nil); err != nil {
				t.Fatal(err)
			}
		}
		for index, want := range op.Variables {
			if engine.enemies[index].AIVariables != want {
				t.Fatalf("operation %+v: member %d variables %v want %v", op, index+5, engine.enemies[index].AIVariables, want)
			}
		}
		for _, condition := range op.Conditions {
			var params [5]string
			for index, value := range condition.Parameters {
				params[index] = strconv.Itoa(value)
			}
			enemy := &engine.enemies[condition.Member-5]
			if got := engine.enemyBranchConditionSatisfied(enemy, 0, "", "SELF_ENEMY_AI_VAR", params); got != condition.Branch {
				t.Fatalf("native branch mismatch: %+v got %v", condition, got)
			}
			// The receipt directly tests the predicate; scheduler turn and HP
			// gates are covered by the existing full native scheduler receipts.
			if got := enemyAIVariableInRange(enemy, params[0], params[1], params[2], false); got != condition.Trigger {
				t.Fatalf("native trigger mismatch: %+v got %v", condition, got)
			}
		}
	}
}
