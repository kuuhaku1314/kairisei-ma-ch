package battlecore

import "testing"

func TestNativeDOTChainAfterAttributeAndBeforeReduction(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	enemy := &engine.enemies[0]
	enemy.HP, enemy.MaxHP = 10000, 10000
	enemy.Level.AttributeRates[combatAttributeIndex("FIRE")] = 133
	enemy.Level.DOTReductions[combatDOTIndex("BURN")] = 25
	engine.players[0].Magic = 503
	role := CombatSkillRole{Function: "BURN", Target: "SELECT", ChainRate: 20,
		Parameters: [10]string{"3", "100", "0", "100", "0", "1000", "0", "INT"}}
	action := battleAction{memberType: 1, target: 5, cardLevel: 1, skill: CombatSkillDefinition{Target: "ENEMY_ONE"}}
	rows, err := engine.executePlayerRole(action, role, 4)
	if err != nil {
		t.Fatal(err)
	}
	// 8bb40: floor(603*133/100)=801; floor(801*160/100)=1281;
	// floor(1281*75/100)=960. Changing the order changes integer rounding.
	if len(enemy.Effects) != 1 || enemy.Effects[0].Value != 960 {
		t.Fatalf("DOT value=%+v, want 960", enemy.Effects)
	}
	if len(rows) != 2 || rows[1].Command != CommandBattleParam {
		t.Fatalf("DOT apply rows: %+v", rows)
	}
	for _, n := range rows[0].Args[7:11] {
		if n != 0 {
			t.Fatal("DOT apply must not publish producer parameters as notification values")
		}
	}
	resume, ok := resumeBuffResult(5, enemy.Effects[0])
	if !ok {
		t.Fatal("DOT missing from resume")
	}
	for _, n := range resume.Args[7:11] {
		if n != 0 {
			t.Fatal("453cc does not emit DOT numeric resume parameters")
		}
	}
	engine.players[0].Magic = 9000
	enemy.Level.AttributeRates[combatAttributeIndex("FIRE")] = 50
	if _, err := engine.tickEnemyDOTEffects(); err != nil {
		t.Fatal(err)
	}
	if enemy.HP != 9040 || enemy.Effects[0].Value != 960 {
		t.Fatal("DOT tick must keep registration-time attribute and Chain")
	}
}

func TestJPDOTDamageLimitAndCut(t *testing.T) {
	for _, tc := range []struct {
		name       string
		stored     int
		rate       int
		wantDamage int
	}{
		{"above old cap", 30_000_000, 0, 30_000_000},
		{"cut at cap", 1_000_000_000, 500, 500_000_000},
		{"cut above cap", 1_000_000_001, 500, 1_000_000_000},
		{"full cut", 30_000_000, 1000, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine, _ := coreBattleFixture(t)
			enemy := &engine.enemies[0]
			enemy.HP, enemy.MaxHP = 1_500_000_000, 1_500_000_000
			enemy.Effects = []battleEffect{
				{Function: "POISON", Remaining: 2, Value: tc.stored},
				{Function: "ATTR_CUT_DOT", Parameter: "POISON", Remaining: 2, Uses: 1, Rate: tc.rate},
			}
			if _, err := engine.tickEnemyDOTEffects(); err != nil {
				t.Fatal(err)
			}
			if got := enemy.MaxHP - enemy.HP; got != tc.wantDamage || len(enemy.Effects) != 1 {
				t.Fatalf("damage=%d remaining effects=%+v, want %d damage and a consumed cut", got, enemy.Effects, tc.wantDamage)
			}
		})
	}

	effects := []battleEffect{{Function: "ELECTRIC", Remaining: 2, Value: 800_000_000}}
	boost := CombatSkillRole{Function: "DOT_VALUE_UP", Parameters: [10]string{"1", "100", "0", "0", "0", "ELECTRIC"}}
	effects, _ = applyDOTValueUp(effects, 5, boost, 1, 1)
	if effects[0].Value != 1_000_000_000 || effects[0].Remaining != 3 {
		t.Fatalf("unexpected boosted electric status: %+v", effects[0])
	}
}
