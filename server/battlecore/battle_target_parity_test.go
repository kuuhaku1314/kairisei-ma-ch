package battlecore

import "testing"

func TestNativeEnemyStatTargetsBreakTiesByMemberWithoutRNG(t *testing.T) {
	engine := permutedArthurEngine()
	engine.enemyCount = 3
	for i := 0; i < engine.enemyCount; i++ {
		engine.enemies[i] = battleEnemy{MemberType: i + 5, HP: 100, MaxHP: 100}
	}
	before := engine.rng
	for _, stat := range []string{"HP", "MAX_HP", "ATK", "INT", "MND", "DEF", "MDEF"} {
		for _, side := range []string{"ENEMY"} {
			for _, direction := range []string{"HIGH", "LOW"} {
				engine.rng = before
				kind := direction + "_" + stat + "_" + side
				want := 1
				if direction == "HIGH" {
					want = 4
				}
				if side == "ENEMY" {
					want = 5
					if direction == "HIGH" {
						want = 7
					}
				}
				got, ok := engine.selectEnemyActionTarget(&engine.enemies[0], CombatEnemyAction{Target: kind})
				if !ok || got != want || engine.rng != before {
					t.Errorf("%s: got %d/%t, want %d and unchanged RNG", kind, got, ok, want)
				}
			}
		}
	}
}

func TestNativeAppendAndCriticalTargetsUseTheirOwnMetricsAndOneRoll(t *testing.T) {
	for _, tc := range []struct {
		kind string
		want []int
	}{
		{"HIGH_CURSE_NUM_USER", []int{1}}, {"LOW_CURSE_NUM_USER", []int{2, 3, 4}},
		{"HIGH_BLESS_NUM_USER", []int{2}}, {"LOW_BLESS_NUM_USER", []int{1, 3, 4}},
		{"HIGH_CRITICAL_USER", []int{2}}, {"LOW_CRITICAL_USER", []int{1}},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			engine := permutedArthurEngine()
			engine.players[0].BlessHolds = []battleBlessHold{{CardType: 21}, {CardType: 21}, {CardType: 21}}
			engine.players[1].BlessHolds = []battleBlessHold{{CardType: 22}, {CardType: 22}}
			engine.players[0].Effects = []battleEffect{
				{Function: "CRITICAL_UP", Value: 900, Rate: 900, Remaining: 2},
				{Function: "CRITICAL_DOWN", Value: 1000, Rate: 1000, Remaining: 2},
			}
			engine.players[1].Effects = []battleEffect{{Function: "CRITICAL_UP", Value: 100, Rate: 100, Remaining: 2}}
			engine.players[2].Effects = []battleEffect{
				{Function: "WEAKNESS", Kind: 2, Remaining: 2}, {Function: "POISON", Kind: 2, Remaining: 2},
				{Function: "BURN", Kind: 2, Remaining: 2}, {Function: "COST_BLOCK", Kind: 2, Remaining: 2},
				{Function: "CRITICAL_UP", Value: 9000, Rate: 9000}, // Expired, not a live rate.
			}
			wantRNG := engine.rng
			want := tc.want[int(wantRNG.next()%uint32(len(tc.want)))]
			got, ok := engine.selectEnemyActionTarget(&battleEnemy{MemberType: 5}, CombatEnemyAction{Target: tc.kind})
			if !ok || got != want || engine.rng != wantRNG {
				t.Fatalf("got %d/%t, want %d and exactly one native RNG draw", got, ok, want)
			}
		})
	}
}

func TestNativeEnemyTargetFallbackKeepsTheSkill(t *testing.T) {
	engine := permutedArthurEngine()
	engine.catalog = &CombatCatalog{Skills: map[int][]CombatSkillDefinition{
		1: {{ID: 1, Target: "ENEMY_ONE"}}, 2: {{ID: 2, Target: "DEAD_ENEMY_ONE"}},
		3: {{ID: 3, Target: "USER_ONE"}},
	}}
	engine.enemyCount = 3
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100}
	engine.enemies[1] = battleEnemy{MemberType: 6}
	engine.enemies[2] = battleEnemy{MemberType: 7}
	before := engine.rng
	for _, tc := range []struct{ skill, want int }{{1, 5}, {2, 6}} {
		got, ok := engine.selectEnemyActionTarget(&engine.enemies[0], CombatEnemyAction{SkillID: tc.skill, Target: "ENEMY4"})
		if !ok || got != tc.want || engine.rng != before {
			t.Fatalf("missing explicit part: got %d/%t, want %d without RNG", got, ok, tc.want)
		}
	}
	engine.enemyTriggerTarget = 2
	engine.players[1].HP = 0
	wantRNG := engine.rng
	candidates := []int{1, 3, 4}
	want := candidates[int(wantRNG.next()%uint32(len(candidates)))]
	got, ok := engine.selectEnemyActionTarget(&engine.enemies[0], CombatEnemyAction{SkillID: 3, Target: "TRIGGER_TARGET"})
	if !ok || got != want || engine.rng != wantRNG {
		t.Fatalf("dead trigger target: got %d/%t, want fallback %d with one RNG draw", got, ok, want)
	}
}

func TestNativeStatTargetReachesEnemyDamageAndDirection(t *testing.T) {
	engine := permutedArthurEngine()
	engine.phase, engine.enemyCount = PhaseUserAttack, 1
	// This checks direction/damage routing with a unique HP maximum. Equal
	// user metrics are covered by complete original JP phase/RNG controls.
	for i := 0; i < 3; i++ {
		engine.players[i].HP = 97 + i
	}
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "USER_ONE", Kind: "ATTACK", DamageKind: "PHYSICS"}
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"10", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
	engine.catalog = &CombatCatalog{Skills: map[int][]CombatSkillDefinition{1: {skill}}, SkillRoles: map[int][]CombatSkillRole{1: {role}}}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, Level: CombatEnemyLevel{
		ActionsPerTurn: 1, Actions: []CombatEnemyAction{{Slot: 1, Category: "skill", SkillID: 1, Target: "HIGH_HP_USER", ActionCost: 1, MaxUses: 1, Rate: 100}},
	}}
	rows, err := engine.EnemyPhase()
	if err != nil {
		t.Fatal(err)
	}
	header, damage := false, false
	for _, row := range rows {
		if row.Command == CommandSkill {
			header = row.Args[2] == 4
		}
		if row.Command == 60 {
			damage = row.Args[0] == 4
		}
	}
	if !header || !damage || engine.players[3].HP >= 100 || engine.players[0].HP != 97 {
		t.Fatalf("selected HP maximum must agree in direction, damage and HP: rows=%+v", rows)
	}
}

func TestNativeBlessBranchesExcludeCurseAndCountPerLivingMember(t *testing.T) {
	engine := permutedArthurEngine()
	actor := &battleEnemy{MemberType: 5}
	action := battleAction{memberType: 1}
	engine.players[0].BlessHolds = []battleBlessHold{{CardType: 21, Skill: CombatSkillDefinition{Attribute: "DARK"}}}
	if engine.branchConditionSatisfied("SELF_BLESS", [5]string{"DARK", "1"}, action, nil, nil) ||
		engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "USER_SIDE_BLESS", [5]string{"NULL", "1"}) {
		t.Fatal("a curse must not satisfy a bless branch")
	}
	engine.players[0].BlessHolds[0].CardType = 22
	engine.players[1].BlessHolds = []battleBlessHold{{CardType: 22, Skill: CombatSkillDefinition{Attribute: "DARK"}}}
	if !engine.branchConditionSatisfied("SELF_BLESS", [5]string{"NULL", "1"}, action, nil, nil) {
		t.Fatal("native NULL attribute means any registered bless")
	}
	if engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "USER_SIDE_BLESS", [5]string{"DARK", "2"}) {
		t.Fatal("one bless on each of two users is not two blesses on one user")
	}
	if engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "USER_SIDE_BLESS", [5]string{"FIRE", "1"}) {
		t.Fatal("USER_SIDE_BLESS ignored the native attribute filter")
	}
	engine.players[1].BlessHolds = append(engine.players[1].BlessHolds, engine.players[1].BlessHolds[0])
	if !engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "USER_SIDE_BLESS", [5]string{"DARK", "2"}) {
		t.Fatal("two matching blesses on a living user must satisfy the branch")
	}
	engine.players[1].HP = 0
	if engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "USER_SIDE_BLESS", [5]string{"DARK", "2"}) {
		t.Fatal("native USER_SIDE_BLESS ignores KO members")
	}
}

func TestNativeInvolveDeadTargetsExcludeGameOver(t *testing.T) {
	engine := permutedArthurEngine()
	engine.players[0].HP, engine.players[0].GameOver = 0, true
	engine.players[1].HP = 0 // KO, but not yet retired by gameover.
	engine.players[0].Effects = []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 2}}
	engine.players[1].Effects = []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 2}}
	got, ok := engine.selectEnemyActionTarget(&battleEnemy{MemberType: 5}, CombatEnemyAction{Target: "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM"})
	if !ok || got != 2 {
		t.Fatalf("native weakness selector must skip retired member but retain KO: %d/%t", got, ok)
	}
	for _, kind := range []string{"RANDOM_INVOLVE_DEAD", "SINGER_INVOLVE_DEAD"} {
		wantRNG := engine.rng
		want := []int{2, 3, 4}[wantRNG.next()%3]
		got, ok = engine.selectEnemyActionTarget(&battleEnemy{MemberType: 5}, CombatEnemyAction{Target: kind})
		if !ok || got != want || engine.rng != wantRNG {
			t.Fatalf("%s target/RNG: %d/%t, want %d with retired member excluded", kind, got, ok, want)
		}
	}
}

func TestNativeRepeatedWeaknessAttackCanHitKOWithoutAbortingPhase(t *testing.T) {
	engine := permutedArthurEngine()
	engine.phase, engine.enemyCount = PhaseUserAttack, 1
	engine.players[0].HP = 1
	engine.players[0].Effects = []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 2}}
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "USER_ONE", Kind: "ATTACK", DamageKind: "MAGIC"}
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "INT", "0", "ICE", "MAGIC"}}
	engine.catalog = &CombatCatalog{Skills: map[int][]CombatSkillDefinition{1: {skill}}, SkillRoles: map[int][]CombatSkillRole{1: {role}}}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, Level: CombatEnemyLevel{
		ActionsPerTurn: 2, Actions: []CombatEnemyAction{
			{Slot: 1, Category: "skill", SkillID: 1, Target: "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM", ActionCost: 1, MaxUses: 1, Rate: 100},
			{Slot: 2, Category: "skill", SkillID: 1, Target: "WEAKNESS_USER_INVOLVE_DEAD_NOT_FOR_RANDOM", ActionCost: 1, MaxUses: 1, Rate: 100},
		},
	}}
	rows, err := engine.EnemyPhase()
	if err != nil {
		t.Fatal(err)
	}
	headers, damage, gameover := 0, 0, 0
	for _, row := range rows {
		switch row.Command {
		case CommandSkill:
			headers++
			if row.Args[2] != 1 {
				t.Fatalf("direction retargeted KO: %+v", row)
			}
		case 60:
			damage++
			if row.Args[0] != 1 || (damage == 2 && row.Args[3] != 0) {
				t.Fatalf("damage retargeted KO: %+v", row)
			}
		case CommandGameOver:
			gameover++
		}
	}
	if headers != 2 || damage != 2 || gameover != 1 || engine.phase != PhaseEnemy {
		t.Fatalf("phase must complete both native attacks before gameover: headers=%d damage=%d gameover=%d phase=%d", headers, damage, gameover, engine.phase)
	}
	if engine.players[1].HP != 100 {
		t.Fatal("KO strike damaged another member")
	}
}
