package battlecore

import (
	"encoding/json"
	"os"
	"testing"

	"kairisei.local/server/battlecontent"
)

func TestRoleTargetCSVMaskAndNativePlayerAttribute(t *testing.T) {
	row := make([]string, 32)
	row[0], row[8], row[9] = "12600031", "ATK_BREAK_FIXED", "SELECT"
	row[10], row[11], row[12], row[19] = "2", "1", "-1", "1"
	role, err := battlecontent.ParseSkillRole(row)
	if err != nil {
		t.Fatal(err)
	}
	if role.ExcludeSelf || !role.HasTargetAttributes || !role.Attributes[0] || role.Attributes[1] || !role.Attributes[8] {
		t.Fatalf("CSV target rule disagrees with managed ParseRole: %+v", role)
	}
	if !combatRoleAllowsTarget(role, 1, 1, "NEUTRAL") || combatRoleAllowsTarget(role, 1, 2, "NULL") || combatRoleAllowsTarget(role, 1, 2, "ICE") {
		t.Fatal("NULL, neutral and disabled attributes were conflated")
	}
	role.Attributes = [9]bool{}
	if combatRoleAllowsTarget(role, 1, 2, "NEUTRAL") {
		t.Fatal("explicit empty CSV mask must reject, not mean unrestricted")
	}
	engine, _ := coreBattleFixture(t)
	for _, player := range engine.players {
		if player.Attribute != "NEUTRAL" || player.BaseAttribute != "NEUTRAL" {
			t.Fatal("battle5_api_user_set initializes players to ATTR=9")
		}
	}
}

func TestRoleTargetFilterIsPostSelectionAndUsesCurrentAttribute(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "USER_ALL", ExcludeSelf: true, HasTargetAttributes: true,
		Parameters: [10]string{"2", "ATK", "100"}}
	role.Attributes[8] = true
	engine.players[1].Effects = []battleEffect{{Function: "REWRITE", Attribute: "FIRE", Remaining: 1}}
	refreshPlayerAttribute(&engine.players[1])
	rows, err := engine.executePlayerRole(battleAction{memberType: 1, cardLevel: 1}, role, 1)
	if err != nil || len(rows) != 4 || len(engine.players[0].Effects) != 0 || len(engine.players[1].Effects) != 1 || len(engine.players[2].Effects) != 1 || len(engine.players[3].Effects) != 1 {
		t.Fatalf("self/rewritten target was not filtered: rows=%+v err=%v", rows, err)
	}
	role.Target = "SELECT"
	before := engine.rng
	if rows, err := engine.executePlayerRole(battleAction{memberType: 1, target: 2, cardLevel: 1}, role, 1); err != nil || len(rows) != 0 || engine.rng != before {
		t.Fatalf("rejected selection was replaced or consumed RNG: rows=%+v err=%v", rows, err)
	}
	engine.players[1].Effects = nil
	refreshPlayerAttribute(&engine.players[1])
	if rows, err := engine.executePlayerRole(battleAction{memberType: 1, target: 2, cardLevel: 1}, role, 1); err != nil || len(rows) != 2 {
		t.Fatalf("expired rewrite did not restore neutral target eligibility: rows=%+v err=%v", rows, err)
	}
	attack := engine.catalog.SkillRoles[1][0]
	attack.HasTargetAttributes = true // explicit empty mask
	beforeHP := engine.enemies[0].HP
	before = engine.rng
	if rows, err := engine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 1}, attack, 1); err != nil || len(rows) != 0 || engine.enemies[0].HP != beforeHP || engine.rng != before {
		t.Fatalf("masked attack must silently skip damage and critical RNG: rows=%+v err=%v", rows, err)
	}
	// Even the native explicit-KO-target exception cannot bypass the mask.
	engine.players[0].HP = 0
	attack.Target = "SELECT"
	if rows, err := engine.executeEnemyAttack(&engine.enemies[0], 1, attack, nil); err != nil || len(rows) != 0 || engine.rng != before {
		t.Fatalf("enemy KO target fallback bypassed mask: rows=%+v err=%v", rows, err)
	}
}

func TestRoleTargetOriginalBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_ROLE_TARGET_RECEIPT")
	if path == "" {
		t.Skip("original-binary target gate receipt is opt-in")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope     string
		LibSHA           string `json:"lib_sha256"`
		ConsumerExecuted bool   `json:"consumer_executed"`
		Vectors          []struct {
			Mask        uint32
			Attribute   int
			ExcludeSelf bool `json:"exclude_self"`
			SameID      bool `json:"same_id"`
			NullTarget  bool `json:"null_target"`
			Accepted    bool
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-resolved-role-target-gate" || receipt.ConsumerExecuted ||
		receipt.LibSHA != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) != 1440 {
		t.Fatal("unexpected binary identity or target-gate scope")
	}
	attributes := []string{"NULL", "FIRE", "ICE", "WIND", "LIGHT", "DARK", "EARTH", "THUNDER", "WATER", "NEUTRAL"}
	for _, vector := range receipt.Vectors {
		role := CombatSkillRole{HasTargetAttributes: true, ExcludeSelf: vector.ExcludeSelf}
		for i := range role.Attributes {
			role.Attributes[i] = vector.Mask&(1<<uint(i+1)) != 0
		}
		attribute := ""
		if vector.Attribute < 10 {
			attribute = attributes[vector.Attribute]
		} else {
			attribute = attributes[vector.Attribute/100] + "_" + attributes[vector.Attribute%100]
		}
		target := 5
		if vector.SameID {
			target = 1
		}
		got := !vector.NullTarget && combatRoleAllowsTarget(role, 1, target, attribute)
		if got != vector.Accepted {
			t.Fatalf("Go=%t native=%t vector=%+v", got, vector.Accepted, vector)
		}
	}
}

func TestEnemyReflectionExcludesCasterAfterTargetResolution(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemyCount = 3
	for i := 0; i < engine.enemyCount; i++ {
		engine.enemies[i] = battleEnemy{MemberType: i + 5, HP: 100, MaxHP: 100}
	}
	// Official 44246007: enemy skill targets ENEMY_ALL; SELECT inherits
	// that side and is_ignore_self=1. The buff must not protect its caster.
	role := CombatSkillRole{SkillID: 44246007, Function: "REFLECTION", Target: "ENEMY_ALL", ExcludeSelf: true,
		Parameters: [10]string{"2", "1500", "0", "MAGIC"}}
	rows, err := engine.executeEnemyRole(&engine.enemies[1], 0, role, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 4 || rows[1].Command != CommandBattleParam || rows[3].Command != CommandBattleParam || len(engine.enemies[0].Effects) != 1 || len(engine.enemies[1].Effects) != 0 || len(engine.enemies[2].Effects) != 1 {
		t.Fatalf("reflection included caster or omitted siblings: rows=%+v", rows)
	}
	for _, row := range rows {
		if row.Args[0] == 6 {
			t.Fatal("excluded caster emitted a status message")
		}
	}
	role.Target = "SELECT"
	before := engine.rng
	if rows, err := engine.executeEnemyRole(&engine.enemies[1], 6, role, nil); err != nil || len(rows) != 0 || engine.rng != before {
		t.Fatalf("excluded selection was replaced or failed: rows=%+v err=%v", rows, err)
	}
	role.ExcludeSelf = false
	if _, err := engine.executeEnemyRole(&engine.enemies[1], 6, role, nil); err != nil || len(engine.enemies[1].Effects) != 1 {
		t.Fatalf("ordinary self reflection rejected: %v", err)
	}
}
