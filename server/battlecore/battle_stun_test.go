package battlecore

import "testing"

func TestNativeStunResistanceOrderAndRandomConsumption(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		cooldown, base, accuracy int
		effects                  []battleEffect
		want                     bool
		draws                    int
	}{
		{name: "duplicate", accuracy: 100, effects: []battleEffect{{Function: "STAN", Remaining: 1}}, draws: 0},
		{name: "temporary resistance", cooldown: 4, accuracy: 100, draws: 1},
		{name: "master resistance", base: 100, accuracy: 100, draws: 2},
		{name: "role miss", accuracy: 0, draws: 3},
		{name: "typed resistance skipped", accuracy: 100, effects: []battleEffect{{Function: "DEBUFF_REGIST", Parameter: "STAN", Remaining: 2, Value: 100}}, want: true, draws: 3},
		{name: "success", accuracy: 100, want: true, draws: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			engine := &BattleEngine{rng: newXorShift128(1)}
			role := CombatSkillRole{Function: "STAN", Parameters: [10]string{"1", "0", "1"}}
			// p1+p2*level uses the supplied skill level as this vector's rate.
			got := engine.stunHits(tc.effects, tc.cooldown, tc.base, role, tc.accuracy)
			wantRNG := newXorShift128(1)
			for i := 0; i < tc.draws; i++ {
				wantRNG.next()
			}
			if got != tc.want || engine.rng != wantRNG {
				t.Fatalf("hit=%t want=%t; native RNG draws should be %d", got, tc.want, tc.draws)
			}
		})
	}
}

func TestNativeEnemyStunRecoveryCooldownUsesEnemyTail(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{}, enemyCount: 1, rng: newXorShift128(1)}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 100, MaxHP: 100}
	enemy := &engine.enemies[0]
	*enemy = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, Effects: []battleEffect{{Function: "STAN", Remaining: 1, Kind: 2}}}
	if _, err := engine.tickPersistentEffects(); err != nil {
		t.Fatal(err)
	}
	if len(enemy.Effects) != 0 || enemy.StatusCooldown[1] != 3 {
		t.Fatalf("enemy-tail expiry must set4 then decrement3: %+v", enemy.StatusCooldown)
	}
	for want := 2; want >= 0; want-- {
		if _, err := engine.tickPersistentEffects(); err != nil {
			t.Fatal(err)
		}
		if enemy.StatusCooldown[1] != want {
			t.Fatalf("cooldown=%d want%d", enemy.StatusCooldown[1], want)
		}
	}
	// A removal outside the enemy-tail does not immediately consume a turn.
	enemy.recordReleasedStatusCooldown([]battleEffect{{Function: "STAN"}})
	role := CombatSkillRole{Function: "STAN", Target: "ENEMY_ONE", Parameters: [10]string{"1", "100"}}
	rows, err := engine.executePersistentEffect(battleAction{memberType: 1, target: 5}, role, 1, 0)
	if err != nil || len(rows) != 1 || rows[0].Command != CommandDebuffFailed || len(enemy.Effects) != 0 {
		t.Fatalf("immediate re-stun must resist: %+v %v", rows, err)
	}
	enemy.StatusCooldown[1] = 0
	enemy.Level.StatusResistances[1] = 100
	rows, err = engine.executePersistentEffect(battleAction{memberType: 1, target: 5}, role, 1, 0)
	if err != nil || len(rows) != 1 || rows[0].Command != CommandDebuffFailed || len(enemy.Effects) != 0 {
		t.Fatalf("enemy master STAN immunity ignored: %+v %v", rows, err)
	}
	enemy.Level.StatusResistances[1] = 0
	rows, err = engine.executePersistentEffect(battleAction{memberType: 1, target: 5}, role, 1, 0)
	if err != nil || len(rows) != 1 || rows[0].Command != CommandBuff || len(enemy.Effects) != 1 {
		t.Fatalf("stun must work after resistance expires: %+v %v", rows, err)
	}
	// FIELD expiry is silent (43f96), not the 72a18 release callback.
	enemy.Effects = []battleEffect{{Function: "STAN", ListType: 3, Remaining: 1}}
	if _, err := engine.tickPersistentEffects(); err != nil || enemy.StatusCooldown[1] != 0 {
		t.Fatal("silent FIELD removal started resistance", err)
	}
}
