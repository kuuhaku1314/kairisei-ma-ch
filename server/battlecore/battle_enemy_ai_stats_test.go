package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestEnemyAIStatsStayOnDirectTarget(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType, engine.enemies[1].Parent = 6, 1
	engine.enemyCount = 2
	engine.players[0].Effects = []battleEffect{{Function: "ENCHANT", Value: 30, Attribute: "FIRE", Remaining: 2}}
	role := engine.catalog.SkillRoles[1][0]
	role.Parameters[8] = "PHYSICS"
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, cardType: 1, cardLevel: 1, target: 6}, role, 1); err != nil {
		t.Fatal(err)
	}
	child := &engine.enemies[1]
	if child.AITurn.Damage != [3]int64{200, 0, 30} || child.AITurn.Hits != [3]int{1, 0, 1} {
		t.Fatalf("direct target AI counters=%+v", child.AITurn)
	}
	if engine.enemies[0].HP == 10000 || engine.enemies[0].AITurn != (battleEnemyAITurnStats{}) {
		t.Fatal("parent should lose propagated HP without inheriting the child's AI hit counters")
	}
	if engine.damageEventTotal(5, "NULL", "ALL", "NULL", false) != 0 || engine.damageEventTotal(6, "NULL", "ALL", "NULL", false) != 230 {
		t.Fatal("ALL_DAMAGE actor counters must exclude parent HP propagation")
	}
	if engine.damageEventTotal(5, "FIRE", "ALL", "NULL", true) != 0 || engine.damageEventTotal(6, "FIRE", "ALL", "NULL", true) != 30 {
		t.Fatal("ENCHANT_DAMAGE must remain on its direct target")
	}
	if n, _ := enemyAIHitCount(child, "PHYSICS"); n != 2 {
		t.Fatal("physical filter must include ALL enchant hit")
	}
	if n, _ := enemyAIHitCount(child, "MAGIC"); n != 1 {
		t.Fatal("magic filter must include ALL enchant hit")
	}
	engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{1: {Fields: enemyAIConditionFields("DAMAGE", "200")}}
	if !engine.enemyAIConditionSatisfied(child, 1) || engine.enemyAIConditionSatisfied(&engine.enemies[0], 1) {
		t.Fatal("one part's damage incorrectly satisfied another enemy's AI condition")
	}
}

func TestEnemyAIBadStatusRemembersApplicationUntilTurnReset(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.turn = 1
	engine.players[0].Effects = []battleEffect{{Function: "STAN", Remaining: 2}}
	if enemyAIBadStatusPresent(&engine.enemies[0], []string{"STAN"}) {
		t.Fatal("player status leaked into enemy AI")
	}
	role := CombatSkillRole{Function: "STAN", Target: "ENEMY_ONE", Parameters: [10]string{"2", "100"}}
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 1}, role, 1); err != nil {
		t.Fatal(err)
	}
	if !enemyAIBadStatusPresent(&engine.enemies[0], []string{"STAN"}) {
		t.Fatal("successful enemy status application was not recorded")
	}
	engine.enemies[0].Effects = nil // removal must not erase the same-turn application event.
	if !enemyAIBadStatusPresent(&engine.enemies[0], []string{"STAN"}) {
		t.Fatal("release erased the AI event")
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if engine.enemies[0].AITurn != (battleEnemyAITurnStats{}) {
		t.Fatal("new turn retained old AI counters")
	}
}

func TestNativeEnemyAIStatsBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_ENEMY_AI_STATS_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_ENEMY_AI_STATS_RECEIPT to original statistics receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	type vector struct {
		Function       string
		Parameters     []int
		Member, Result int
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []vector
		AfterReset   []vector `json:"after_reset"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-enemy-ai-statistics" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid original AI receipt")
	}
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType = 6
	engine.enemyCount = 2
	for _, hit := range [][2]int{{0, 25}, {0, 35}, {1, 20}, {2, 20}} {
		recordEnemyAIDamage(&engine.enemies[0], hit[1], hit[0])
	}
	recordEnemyAIBadStatus(&engine.enemies[0], "STAN")
	recordEnemyAIBadStatus(&engine.enemies[0], "POISON")
	engine.enemies[0].AITurn.DebuffKinds |= 1 << 7
	engine.enemies[0].AITurn.PlayerBuffKinds[2]++
	check := func(vectors []vector) {
		for _, v := range vectors {
			args := make([]string, len(v.Parameters))
			for i, n := range v.Parameters {
				args[i] = strconv.Itoa(n)
			}
			if v.Function == "DAMAGE_NUM" {
				args[0] = map[int]string{0: "PHYSICS", 1: "MAGIC", 2: "ALL"}[v.Parameters[0]]
			}
			if v.Function == "BAD_STATUS" {
				for i, n := range v.Parameters {
					args[i] = map[int]string{0: "NULL", 1: "STAN", 2: "SILENCE", 3: "CHARM", 4: "POISON"}[n]
				}
			}
			if v.Function == "SKILL_ROLE_KIND_DEBUFF_NOW_TURN" || v.Function == "SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER" {
				names := map[int]string{0: "NULL", 1: "ATK_BREAK_BY_ATK", 2: "ATK_BREAK_BY_INT", 7: "POISON"}
				if v.Function == "SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER" {
					names = map[int]string{0: "NULL", 1: "ATK_UP_BY_ATK", 2: "ATK_UP_BY_INT", 3: "ATK_UP_BY_MND"}
				}
				for i, n := range v.Parameters {
					args[i] = names[n]
				}
			}
			engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{1: {Fields: enemyAIConditionFields(v.Function, args...)}}
			got := engine.enemyAIConditionSatisfied(&engine.enemies[v.Member-5], 1)
			if got != (v.Result == 0) {
				t.Fatalf("%s %v member%d Go=%t original=%d", v.Function, v.Parameters, v.Member, got, v.Result)
			}
		}
	}
	check(receipt.Vectors)
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	check(receipt.AfterReset)
}
