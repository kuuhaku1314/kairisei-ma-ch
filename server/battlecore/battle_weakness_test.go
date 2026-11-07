package battlecore

import "testing"

func TestNativeWeaknessMultipliesOrdinaryPowerNotElementOrEnchantOrDOT(t *testing.T) {
	engine := &BattleEngine{enemyCount: 1, rng: newXorShift128(1)}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 100, MaxHP: 100, Effects: []battleEffect{{Function: "ENCHANT", Attribute: "FIRE", Value: 500, Remaining: 2}}}
	enemy := &engine.enemies[0]
	*enemy = battleEnemy{MemberType: 5, Attribute: "WIND", HP: 10000, MaxHP: 10000, Defense: 100, AttributeFixed: [5]int{100}, Effects: []battleEffect{{Function: "WEAKNESS", Rate: 375, Remaining: 2}}}
	enemy.Level.AttributeRates[0] = 100
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"1001", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
	rows, err := engine.executePlayerAttack(battleAction{memberType: 1, target: 5}, role, 1)
	if err != nil {
		t.Fatal(err)
	}
	var hits []BattleResult
	for _, row := range rows {
		if row.Command == 60 {
			hits = append(hits, row)
		}
	}
	// floor(1001*1375/1000)*2 - 100 DEF - 100 armor; enchant 500*2-100.
	if len(hits) != 2 || hits[0].Args[2] != -2552 || hits[0].Args[4] != 1376 || hits[0].Args[6] != 200 ||
		hits[1].Args[2] != -900 || hits[1].Args[4] != 500 || hits[1].Args[6] != 200 || enemy.HP != 6548 {
		t.Fatalf("WEAKNESS changed elemental/enchant stages: %+v HP=%d", hits, enemy.HP)
	}
	if dot := engine.dotEffectForEnemy(battleEffect{Function: "BURN", Value: 1000}, enemy); dot.Value != 2000 {
		t.Fatal("WEAKNESS or fixed armor leaked into DOT", dot.Value)
	}
}

func TestNativeWeaknessMovesOneMarkWithinSameSideAndListAfterResistance(t *testing.T) {
	engine := &BattleEngine{enemyCount: 2, rng: newXorShift128(1), turn: 1}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 100, MaxHP: 100}
	for i := 0; i < engine.enemyCount; i++ {
		engine.enemies[i] = battleEnemy{MemberType: i + 5, HP: 100, MaxHP: 100}
	}
	engine.enemies[0].Effects = []battleEffect{
		{Function: "WEAKNESS", Remaining: 2, Rate: 100},
		{Function: "WEAKNESS", ListType: 3, Remaining: 2, Rate: 200},
	}
	engine.enemies[1].Level.StatusResistances[9] = 100
	role := CombatSkillRole{Function: "WEAKNESS", Target: "ENEMY_ONE", Parameters: [10]string{"2", "375"}}
	wantRNG := engine.rng
	wantRNG.next()
	rows, err := engine.executePersistentEffect(battleAction{memberType: 1, target: 6}, role, 1, 1)
	if err != nil || len(rows) != 1 || rows[0].Command != CommandDebuffFailed || len(engine.enemies[0].Effects) != 2 || engine.rng != wantRNG {
		t.Fatalf("resisted mark must consume one roll and leave old mark: %+v %v", rows, err)
	}
	engine.enemies[1].Level.StatusResistances[9] = 0
	wantRNG.next()
	rows, err = engine.executePersistentEffect(battleAction{memberType: 1, target: 6}, role, 1, 1)
	if err != nil || len(rows) != 2 || rows[0].Command != 72 || rows[0].Args[0] != 5 || rows[0].Args[1] != 0 ||
		rows[1].Command != CommandBuff || rows[1].Args[0] != 6 || engine.rng != wantRNG ||
		len(engine.enemies[0].Effects) != 1 || engine.enemies[0].Effects[0].ListType != 3 || len(engine.enemies[1].Effects) != 1 {
		t.Fatalf("mark must move with only grouped72 then62, preserving FIELD: %+v %v", rows, err)
	}
	// Native side enumeration includes existing KO members. No duplicate reject
	// and no typed-resistance roll is performed for the WEAKNESS switch case.
	engine.players[0].HP = 0
	engine.players[0].Effects = []battleEffect{{Function: "WEAKNESS", Remaining: 2}}
	engine.players[1] = battlePlayer{MemberType: 2, HP: 100, MaxHP: 100}
	role.Target = "USER_ONE"
	wantRNG.next()
	rows, err = engine.executeEnemyPersistentEffect(&engine.enemies[0], 2, role)
	if err != nil || len(rows) != 2 || rows[0].Command != 72 || rows[0].Args[0] != 1 || rows[1].Args[0] != 2 ||
		len(engine.players[0].Effects) != 0 || len(engine.players[1].Effects) != 1 || len(engine.enemies[1].Effects) != 1 || engine.rng != wantRNG {
		t.Fatalf("player-side mark move touched the wrong side or RNG: %+v %v", rows, err)
	}
}
