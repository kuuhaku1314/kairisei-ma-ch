package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestAIFlagsFollowResolvedEnemyAndSurviveTurn(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType = 6
	engine.enemyCount = 2
	role := CombatSkillRole{Function: "ENEMY_AI_TRIGGER_FLAG_SET", Target: "SELECT", Parameters: [10]string{"1", "1"}}
	if rows, err := engine.executeEnemyRole(&engine.enemies[0], 6, role, nil); err != nil || len(rows) != 0 {
		t.Fatalf("flag set rows=%v err=%v", rows, err)
	}
	if engine.enemies[0].hasAIFlag(1) || !engine.enemies[1].hasAIFlag(1) {
		t.Fatal("SELECT flag leaked from target part to caster")
	}
	if enemyAIFlagsAll(&engine.enemies[0], []string{"1"}, true) || !enemyAIFlagsAll(&engine.enemies[1], []string{"1"}, true) {
		t.Fatal("AI predicates ignored actor-local flag storage")
	}
	// Failed/preview operations clone engines by value: no shared map may
	// leak a tentative flag update back into the committed engine.
	probe := *engine
	role.Parameters[1] = "0"
	if _, err := probe.executeEnemyRole(&probe.enemies[0], 6, role, nil); err != nil {
		t.Fatal(err)
	}
	if !engine.enemies[1].hasAIFlag(1) || probe.enemies[1].hasAIFlag(1) {
		t.Fatal("tentative flag mutation leaked between engine snapshots")
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if !engine.enemies[1].hasAIFlag(1) {
		t.Fatal("TurnPhase erased a persistent AI flag")
	}
	engine.phase, engine.endType = PhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.enemies[0].AIFlags != 0 || next.enemies[1].AIFlags != 0 {
		t.Fatal("next wave inherited old enemy flags")
	}
}

func TestAIFlagZeroBitAndTargetScopes(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType, engine.enemies[1].HP = 6, 0
	engine.enemyCount = 2
	role := CombatSkillRole{Function: "ENEMY_AI_TRIGGER_FLAG_SET", Target: "SELF", Parameters: [10]string{"0", "1"}}
	if _, err := engine.executeEnemyRole(&engine.enemies[1], 5, role, nil); err != nil {
		t.Fatal(err)
	}
	if !engine.enemyBranchConditionSatisfied(&engine.enemies[1], 0, "", "SELF_ENEMY_AI_FLAG_AND", [5]string{"0", "0"}) ||
		engine.enemyBranchConditionSatisfied(&engine.enemies[0], 0, "", "SELF_ENEMY_AI_FLAG", [5]string{"0", "0"}) {
		t.Fatal("skill branch bit0 or KO SELF targeting differs")
	}
	role.Target, role.Parameters[0] = "SELECT", "1"
	if _, err := engine.executeEnemyRole(&engine.enemies[0], 1, role, nil); err != nil {
		t.Fatal(err)
	}
	if engine.enemies[0].AIFlags != 0 {
		t.Fatal("player target incorrectly set enemy flag")
	}
	role.Target = "ENEMY_ALL"
	if _, err := engine.executeEnemyRole(&engine.enemies[0], 0, role, nil); err != nil {
		t.Fatal(err)
	}
	if !engine.enemies[0].hasAIFlag(1) || engine.enemies[1].hasAIFlag(1) {
		t.Fatal("all-enemy flag must exclude KO parts, unlike SELF/selected targets")
	}
	role.Function, role.Target = "ENEMY_AWAKE_FLAG_SET", "SELF"
	if _, err := engine.executeEnemyRole(&engine.enemies[1], 5, role, nil); err != nil {
		t.Fatal(err)
	}
	if engine.enemies[1].Awake != 1 || engine.enemies[0].Awake != 0 {
		t.Fatal("awake state must also follow the resolved KO SELF target")
	}
}

func TestNativeAIFlagBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_ENEMY_AI_FLAGS_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_ENEMY_AI_FLAGS_RECEIPT to original flag receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Operations   []struct {
			Operation           string
			Member, Flag, Value int
			Flags               [2]uint32
			Conditions          []struct {
				Member     int
				Function   string
				Parameters []int
				Matched    bool
			}
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-enemy-ai-flags" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Operations) == 0 {
		t.Fatal("invalid original flag receipt")
	}
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType, engine.enemyCount = 6, 2
	for _, operation := range receipt.Operations {
		if operation.Operation == "turn_reset" {
			engine.phase = PhaseChaliceEnemy
			if _, err := engine.TurnPhase(); err != nil {
				t.Fatal(err)
			}
		} else {
			role := CombatSkillRole{Function: "ENEMY_AI_TRIGGER_FLAG_SET", Target: "SELECT", Parameters: [10]string{strconv.Itoa(operation.Flag), strconv.Itoa(operation.Value)}}
			if _, err := engine.executeEnemyRole(&engine.enemies[0], operation.Member, role, nil); err != nil {
				t.Fatal(err)
			}
		}
		if got := [2]uint32{engine.enemies[0].AIFlags, engine.enemies[1].AIFlags}; got != operation.Flags {
			t.Fatalf("%s Go flags=%v native=%v", operation.Operation, got, operation.Flags)
		}
		for _, c := range operation.Conditions {
			args := make([]string, len(c.Parameters))
			for i, n := range c.Parameters {
				args[i] = strconv.Itoa(n)
			}
			engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{1: {Fields: enemyAIConditionFields(c.Function, args...)}}
			if got := engine.enemyAIConditionSatisfied(&engine.enemies[c.Member-5], 1); got != c.Matched {
				t.Fatalf("%s member%d parameters%v Go=%t native=%t", c.Function, c.Member, c.Parameters, got, c.Matched)
			}
		}
	}
}
