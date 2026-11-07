package battlecore

import (
	"encoding/json"
	"os"
	"testing"
)

func TestBuffExecutionBelongsToCasterNotTargetOrParty(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "USER_ONE", Parameters: [10]string{"2", "ATK", "1000", "100"}}
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, target: 2, cardLevel: 1}, role, 1); err != nil {
		t.Fatal(err)
	}
	for member := 1; member <= 4; member++ {
		if got := engine.branchConditionSatisfied("BUFF_EXEC", [5]string{"ATK_UP_BY_ATK"}, battleAction{memberType: member}, nil, nil); got != (member == 1) {
			t.Fatalf("member%d BUFF_EXEC=%t", member, got)
		}
	}
	if _, err := engine.executeEnemyRole(&engine.enemies[0], 2, role, nil); err != nil {
		t.Fatal(err)
	}
	if engine.players[1].ExecutedBuffKinds[1] != 0 || engine.enemies[0].ExecutedBuffKinds[1] == 0 {
		t.Fatal("enemy-cast buff was attributed to its player target")
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if engine.players[0].ExecutedBuffKinds != ([skillRoleBuffKindCount]uint32{}) || engine.enemies[0].ExecutedBuffKinds != ([skillRoleBuffKindCount]uint32{}) {
		t.Fatal("new turn retained prior caster events")
	}
}

func TestBuffExecutionRequiresAcceptedTypedGoodStatus(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "USER_ONE", Parameters: [10]string{"2", "ATK", "0"}}
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, target: 2, cardLevel: 1}, role, 1); err != nil {
		t.Fatal(err)
	}
	if engine.players[0].ExecutedBuffKinds[1] != 1 {
		t.Fatal("JP fixed-parameter minimum1 must register accepted ATK_UP")
	}
	role.Function, role.Parameters = "DEAL_BONUS", [10]string{"1"}
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, target: 2, cardLevel: 1}, role, 1); err != nil {
		t.Fatal(err)
	}
	if engine.players[0].ExecutedBuffKinds[12] != 0 {
		t.Fatal("86a00 draw bonus must not count as BUFF_EXEC")
	}
	engine.players[1].Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 200, Remaining: 3, Source: 1}}
	role.Function = "REGENERATE_FIXED"
	rows, handled := engine.registerRetainedStatus(2, &engine.players[1].Effects, role, battleEffect{Function: "REGENERATE_FIXED", Value: 100, Remaining: 2, Source: 3})
	if !handled || len(rows) != 0 || engine.players[2].ExecutedBuffKinds[8] != 0 {
		t.Fatal("rejected weaker regeneration counted as executed")
	}
	_, handled = engine.registerRetainedStatus(2, &engine.players[1].Effects, role, battleEffect{Function: "REGENERATE_FIXED", Value: 300, Remaining: 2, Source: 3})
	if !handled || engine.players[2].ExecutedBuffKinds[8] != 1 {
		t.Fatal("accepted regeneration did not count on its caster")
	}
}

func TestBuffExecutionOriginalBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_BUFF_EXECUTION_RECEIPT")
	if path == "" {
		t.Skip("original-binary receipt is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		LibSHA       string `json:"lib_sha256"`
		Inputs       []map[int]int32
		Vectors      []struct {
			Member, Result int
			Reset          bool
			Parameters     [5]int
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-buff-exec-predicate-reset" ||
		receipt.LibSHA != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Inputs) != 4 || len(receipt.Vectors) != 56 {
		t.Fatal("unexpected original-binary identity or scope")
	}
	for _, vector := range receipt.Vectors {
		engine, _ := coreBattleFixture(t)
		for member, counters := range receipt.Inputs {
			for kind, value := range counters {
				engine.players[member].ExecutedBuffKinds[kind] = uint32(value)
			}
		}
		if vector.Reset {
			engine.phase = PhaseChaliceEnemy
			if _, err := engine.TurnPhase(); err != nil {
				t.Fatal(err)
			}
		}
		names := map[int]string{0: "NULL", 1: "ATK_UP_BY_ATK", 2: "ATK_UP_BY_INT", 3: "ATK_UP_BY_MND", 4: "DEF_UP_BY_DEF", 5: "DEF_UP_BY_MDEF", 8: "HEAL"}
		var params [5]string
		for i, kind := range vector.Parameters {
			var ok bool
			if params[i], ok = names[kind]; !ok {
				t.Fatalf("unmapped native kind %d", kind)
			}
		}
		if got := engine.branchConditionSatisfied("BUFF_EXEC", params, battleAction{memberType: vector.Member}, nil, nil); got != (vector.Result >= 0) {
			t.Fatalf("Go=%t native=%d vector=%+v", got, vector.Result, vector)
		}
	}
}
