package battlecore

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func newChargedEnemyTestEngine() *BattleEngine {
	engine := &BattleEngine{phase: PhaseStarted, enemyCount: 1, costInitial: 3, rng: newXorShift128(1), catalog: &CombatCatalog{
		Skills:     map[int][]CombatSkillDefinition{1: {{ID: 1, FunctionID: 1, Target: "USER_ONE", Kind: "ATTACK", DamageKind: "PHYSICS"}}},
		SkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"100", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}},
	}}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 1000, MaxHP: 1000}
	}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 1000, MaxHP: 1000, Level: CombatEnemyLevel{ActionsPerTurn: 1, Actions: []CombatEnemyAction{
		{Slot: 15, Category: "special", SkillID: 1, Target: "MERCENARY", ActionCost: 1, MaxUses: 1, Rate: 100},
	}}}
	return engine
}

func TestNativeChargeQueueSurvivesUntilEnemyPhaseWithoutRescheduling(t *testing.T) {
	engine := newChargedEnemyTestEngine()
	rows, err := engine.TurnPhase()
	enemy := &engine.enemies[0]
	if err != nil || !battleResultsContainCommand(rows, CommandChargeStart) || enemy.ChargedActionCount != 1 || engine.players[0].HP != 1000 {
		t.Fatalf("TurnPhase must retain, not execute, charged action: %+v %v", rows, err)
	}
	uses := engine.enemyUses[enemyActionUseKey(enemy, enemy.Level.Actions[0])]
	if uses != 1 || enemy.ActionConsumed != 1 || engine.enemyAttackSign(enemy) != 1 {
		t.Fatal("charged action missing from attack sign or not charged exactly once")
	}
	// Simulate an intervening user phase: scheduling now fails. A queued
	// charge keeps its original trigger, but resolves its target/branch later.
	enemy.Level.Actions[0].MaxUses = 0
	engine.phase = PhaseUserAttack
	rows, err = engine.EnemyPhase()
	if err != nil || !battleResultsContainCommand(rows, CommandSkill) || engine.players[0].HP != 900 {
		t.Fatalf("charged skill never reached actual attack: %+v %v", rows, err)
	}
	if enemy.ActionConsumed != 1 || engine.enemyUses[enemyActionUseKey(enemy, enemy.Level.Actions[0])] != uses {
		t.Fatal("charged skill rolled/debited its schedule a second time")
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil || enemy.ChargedActionCount != 0 {
		t.Fatal("next turn retained a consumed charge", err)
	}
}

func TestNativeChargedEnemyStillHonorsStunAndDeath(t *testing.T) {
	for _, suppressed := range []string{"stun", "dead"} {
		t.Run(suppressed, func(t *testing.T) {
			engine := newChargedEnemyTestEngine()
			if _, err := engine.TurnPhase(); err != nil {
				t.Fatal(err)
			}
			if suppressed == "stun" {
				engine.enemies[0].Effects = []battleEffect{{Function: "STAN", Remaining: 2}}
			} else {
				engine.enemies[0].HP = 0
			}
			engine.phase = PhaseUserAttack
			rows, err := engine.EnemyPhase()
			if err != nil || battleResultsContainCommand(rows, CommandSkill) || engine.players[0].HP != 1000 {
				t.Fatalf("suppressed charge executed: %+v %v", rows, err)
			}
		})
	}
}

func TestNativeAttackSignSkipsUncertainSkillButIncludesNormalWithoutRateRoll(t *testing.T) {
	engine := newChargedEnemyTestEngine()
	enemy := &engine.enemies[0]
	before := engine.rng
	enemy.Level.Actions = []CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 1, ActionCost: 1, MaxUses: 1, Rate: 99}}
	if sign := engine.enemyAttackSign(enemy); sign != 0 || engine.rng != before {
		t.Fatalf("uncertain ordinary skill cannot promise an attack sign: %d", sign)
	}
	enemy.Level.Actions[0].Rate = 100
	if sign := engine.enemyAttackSign(enemy); sign != 1 || engine.rng != before {
		t.Fatalf("guaranteed ordinary skill missing from attack sign: %d", sign)
	}
	enemy.Level.Actions[0].Category, enemy.Level.Actions[0].Rate = "normal", 0
	if sign := engine.enemyAttackSign(enemy); sign != 1 || engine.rng != before || len(engine.enemyUses) != 0 || enemy.ActionConsumed != 0 {
		t.Fatalf("normal preview must ignore rate without changing counters/RNG: %d", sign)
	}
}

func TestNativeZeroBudgetStillAllowsFreeActionsAndEqualPrioritySwaps(t *testing.T) {
	engine := newChargedEnemyTestEngine()
	enemy := &engine.enemies[0]
	enemy.Level.ActionsPerTurn = 0
	if plan := engine.buildEnemyChargeStartPlan(enemy); len(plan) != 0 {
		t.Fatal("positive-cost charge must not fit zero budget")
	}
	enemy.Level.Actions[0].ActionCost = 0
	if plan := engine.buildEnemyChargeStartPlan(enemy); len(plan) != 1 || enemy.ActionConsumed != 0 {
		t.Fatal("zero-cost charge is legal at zero budget")
	}
	enemy.Level.Actions = append(enemy.Level.Actions,
		CombatEnemyAction{Slot: 1, Category: "skill", SkillID: 1, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 2, Category: "skill", SkillID: 1, MaxUses: 1, Rate: 100},
		CombatEnemyAction{Slot: 3, Category: "skill", SkillID: 1, ActionCost: 1, MaxUses: 1, Rate: 100},
	)
	before := engine.rng
	plan := engine.buildEnemyActionPlan(enemy)
	wantRNG := before
	wantRNG.next()
	wantRNG.next()
	if len(plan) != 3 || plan[0].action.Slot != 2 || plan[1].action.Slot != 1 || plan[2].action.Slot != 15 || engine.rng != wantRNG {
		t.Fatalf("native equal-priority order/zero-cost budget must preserve charged action without another roll: %+v", plan)
	}
}

// Optional direct-original-binary oracle. The normal regression above remains
// self-contained; proprietary native bytes/receipts are never committed.
func TestNativeEnemySortBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_ENEMY_SORT_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_ENEMY_SORT_RECEIPT to a fresh audit_cn_native_enemy_sort.py receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State   string `json:"state"`
		Scope   string `json:"scope"`
		Hash    string `json:"lib_sha256"`
		Vectors []struct {
			Name       string `json:"name"`
			Priorities []int  `json:"priorities"`
			Order      []int  `json:"order"`
		} `json:"vectors"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	validScope := receipt.Scope == "original-cn-x86-enemy-sort-only" || receipt.Scope == "original-cn-x86-enemy-sort-and-preview-random"
	if receipt.State != "PASS" || !validScope || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("not a verified original CN enemy-sort receipt")
	}
	for _, vector := range receipt.Vectors {
		plan := make([]enemyActionCandidate, len(vector.Priorities))
		for i, priority := range vector.Priorities {
			plan[i].action = CombatEnemyAction{Slot: i, Priority: priority}
		}
		sortEnemyActionPlan(plan)
		got := make([]int, len(plan))
		for i, candidate := range plan {
			got[i] = candidate.action.Slot
		}
		if !slices.Equal(got, vector.Order) {
			t.Fatalf("%s priorities %v: Go order %v, original binary %v", vector.Name, vector.Priorities, got, vector.Order)
		}
	}
}
