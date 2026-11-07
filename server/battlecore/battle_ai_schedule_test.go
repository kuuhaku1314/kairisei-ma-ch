package battlecore

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAIUsesExactTurnColumnAndBlankMeansDisabled(t *testing.T) {
	fields := enemyAIConditionFields("NULL")
	// Actual official order10000006: odd numbered turns only, loop from11.
	for i := 7; i < 18; i++ {
		fields[i] = ""
		if (i-7)%2 == 1 {
			fields[i] = "1"
		}
	}
	for i := 19; i < 29; i++ {
		fields[i] = ""
		if (i-19)%2 == 0 {
			fields[i] = "1"
		}
	}
	for turn := 0; turn < 24; turn++ {
		if got := nativeEnemyAITurnEnabled(fields, turn); got != (turn%2 == 1) {
			t.Fatalf("turn%d enabled=%t", turn, got)
		}
	}
	fields[18], fields[17] = "0", "1"
	if !nativeEnemyAITurnEnabled(fields, 10) || nativeEnemyAITurnEnabled(fields, 11) {
		t.Fatal("without a loop, native supports turn10 but no later columns")
	}
}

func TestAIPartsUseChildPositionAndActualParent(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	for i := 1; i < 4; i++ {
		engine.enemies[i] = engine.enemies[0]
		engine.enemies[i].MemberType, engine.enemies[i].Parent = i+5, 1
	}
	engine.enemyCount = 4
	engine.enemies[2].HP = 0 // Only the SECOND child is broken.
	root, child := &engine.enemies[0], &engine.enemies[1]
	if engine.enemyAIPartsCondition(root, "PARTS_1_BREAK") || !engine.enemyAIPartsCondition(root, "PARTS_2_BREAK") ||
		engine.enemyAIPartsCondition(child, "PARTS_2_BREAK") || !engine.enemyAIPartsCondition(child, "PARTS_2_BREAK2") ||
		!engine.enemyAIPartsCondition(child, "PARTS_3_ALIVE") {
		t.Fatal("indexed child/parent fallback differs from native")
	}
	engine.enemies[3].Parent = 0
	if engine.enemyAIPartsCondition(&engine.enemies[3], "PARTS_FULL") || engine.enemyAIPartsCondition(root, "PARTS_3_ALIVE") {
		t.Fatal("unrelated independent body must not count as a child")
	}
	fields := enemyAIConditionFields("NULL")
	fields[5], fields[6] = "1", "50"
	engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{1: {Fields: fields}}
	if !engine.enemyAIConditionSatisfied(root, 1) || engine.enemyAIConditionSatisfied(child, 1) {
		t.Fatal("parent HP condition must apply to parts only")
	}
	// A separate parent is not the first enemy in the party.
	engine.enemies[3].HP, engine.enemies[3].MaxHP = 25, 100
	child.Parent = 4
	if !engine.enemyAIConditionSatisfied(child, 1) {
		t.Fatal("part read the first body instead of its actual parent")
	}
}

func TestAIMemberLivenessIgnoresAbsentSlots(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	if !engine.enemyMembersAll([]string{"ENEMY4"}, true) || !engine.enemyMembersAll([]string{"ENEMY4"}, false) {
		t.Fatal("native skips an absent configured enemy slot in either predicate")
	}
	if !engine.enemyMembersAll([]string{"ENEMY1", "ENEMY4"}, true) || engine.enemyMembersAll([]string{"ENEMY1", "ENEMY4"}, false) {
		t.Fatal("present enemy still must satisfy the predicate")
	}
}

func TestNativeAIPartsReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_AI_PARTS_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_AI_PARTS_RECEIPT to original predicates receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Parents   [4]int
			DeadMask  int `json:"dead_mask"`
			Member    int
			Condition string
			Matched   bool
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-ai-party-predicates" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid native party receipt")
	}
	engine := &BattleEngine{enemyCount: 4}
	for _, v := range receipt.Vectors {
		for i, parent := range v.Parents {
			enemy := battleEnemy{MemberType: i + 5, HP: 100, MaxHP: 100}
			if parent != 0 {
				enemy.Parent = parent - 4
			}
			if v.DeadMask&(1<<i) != 0 {
				enemy.HP = 0
			}
			engine.enemies[i] = enemy
		}
		if got := engine.enemyAIPartsCondition(&engine.enemies[v.Member-5], v.Condition); got != v.Matched {
			t.Fatalf("%+v Go=%t", v, got)
		}
	}
}
