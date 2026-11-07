package battlecore

import "testing"

func TestNativeRegenerationReplacementOnBothSides(t *testing.T) {
	for _, casterEnemy := range []bool{false, true} {
		for _, targetEnemy := range []bool{false, true} {
			engine, _ := coreBattleFixture(t)
			engine.players[0].HP = 400
			engine.enemies[0].HP, engine.enemies[0].MaxHP = 400, 1000
			member, target := 1, "USER_ONE"
			effects := &engine.players[0].Effects
			if targetEnemy {
				member, target, effects = 5, "ENEMY_ONE", &engine.enemies[0].Effects
			}
			apply := func(amount string) []BattleResult {
				role := CombatSkillRole{Function: "REGENERATE_FIXED", Target: target,
					Parameters: [10]string{"4", amount, "0", "0", "0", "MND"}}
				var rows []BattleResult
				var err error
				if casterEnemy {
					role.Target = "SELECT"
					rows, err = engine.executeEnemyRole(&engine.enemies[0], member, role, nil)
				} else {
					rows, err = engine.executePlayerRole(battleAction{memberType: 1, target: member, cardLevel: 1}, role, 1)
				}
				if err != nil {
					t.Fatal(err)
				}
				return rows
			}
			apply("100")
			rng := engine.rng
			if rows := apply("50"); len(rows) != 0 || len(*effects) != 1 || (*effects)[0].Value != 100 || engine.rng != rng {
				t.Fatalf("weaker regeneration must silently retain old status: casterEnemy=%t targetEnemy=%t rows=%+v effects=%+v", casterEnemy, targetEnemy, rows, *effects)
			}
			rows := apply("200")
			if len(rows) != 3 || rows[0].Command != 72 || rows[1].Command != CommandBuff || rows[2].Command != CommandBattleParam || len(*effects) != 1 || (*effects)[0].Value != 200 {
				t.Fatalf("stronger regeneration must replace, not stack: %+v %+v", rows, *effects)
			}
			engine.regenerateMembers()
			hp := engine.players[0].HP
			if targetEnemy {
				hp = engine.enemies[0].HP
			}
			if hp != 600 {
				t.Fatalf("regeneration stacked after replacement: HP=%d", hp)
			}
			(*effects)[0].Remaining = 1
			if rows := apply("50"); len(rows) != 3 || rows[2].Command != CommandBattleParam || len(*effects) != 1 || (*effects)[0].Value != 50 {
				t.Fatal("last-turn status must permit weaker refresh")
			}
		}
	}
}

func TestNativeGoodStatusReplacementUsesStatusSpecificStrength(t *testing.T) {
	for _, tc := range []struct {
		function  string
		old, next battleEffect
		accept    bool
	}{
		{"ATTACK_BARRIER", battleEffect{Value: 10000, Uses: 3}, battleEffect{Value: 100, Uses: 1}, true},
		{"REFLECTION", battleEffect{Rate: 100, DamageKind: "PHYSICS"}, battleEffect{Rate: 10, DamageKind: "MAGIC"}, true},
		{"REFLECTION", battleEffect{Rate: 100, DamageKind: "MAGIC"}, battleEffect{Rate: 10, DamageKind: "MAGIC"}, false},
		{"GUTS", battleEffect{Rate: 20, Uses: 1}, battleEffect{Rate: 10, Uses: 5}, false},
		{"GUTS", battleEffect{Rate: 20, Uses: 5}, battleEffect{Rate: 30, Uses: 1}, true},
	} {
		t.Run(tc.function, func(t *testing.T) {
			tc.old.Function, tc.next.Function = tc.function, tc.function
			tc.old.Remaining, tc.next.Remaining = 3, 3
			effects := []battleEffect{tc.old}
			rows, handled := (&BattleEngine{}).registerRetainedStatus(1, &effects, CombatSkillRole{Function: tc.function}, tc.next)
			if !handled || len(effects) != 1 || (len(rows) == 2) != tc.accept {
				t.Fatalf("incorrect replacement decision: rows=%+v effects=%+v", rows, effects)
			}
		})
	}
}

func TestBurstGoodStatusReplacementKeepsItsListBeforeComparison(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.players[0].Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 1000, Remaining: 5}}
	role := CombatSkillRole{Function: "REGENERATE_FIXED", Target: "SELF", Parameters: [10]string{"3", "100", "0", "0", "0", "MND"}}
	action := battleAction{memberType: 1, target: 1, cardLevel: 1}
	if _, err := engine.executeBurstRoleWithListType(action, role, nil, 5); err != nil {
		t.Fatal(err)
	}
	role.Parameters[1] = "200"
	rows, err := engine.executeBurstRoleWithListType(action, role, nil, 5)
	if err != nil || len(rows) != 3 || rows[0].Command != 72 || rows[0].Args[1] != 5 || rows[1].Args[2] != 5 || rows[2].Command != CommandBattleParam {
		t.Fatalf("Burst replacement must use BURST_NORMAL, not NORMAL: rows=%+v err=%v", rows, err)
	}
	effects := engine.players[0].Effects
	if len(effects) != 2 || effects[0].ListType != 0 || effects[0].Value != 1000 || effects[1].ListType != 5 || effects[1].Value != 200 {
		t.Fatalf("Burst comparison/replacement corrupted another list: %+v", effects)
	}
}
