package battlecore

import (
	"testing"

	"kairisei.local/server/battlecontent"
)

func TestEnemyMasterResistanceUsesNativeStatusIndices(t *testing.T) {
	row := make([]string, 334)
	row[0], row[11], row[14], row[21] = "1", "100", "100", "77"
	level, err := battlecontent.ParseEnemyLevel(row)
	if err != nil {
		t.Fatal(err)
	}
	if level.StatusResistances[0] != 0 || level.StatusResistances[1] != 100 ||
		level.StatusResistances[2] != 0 || level.StatusResistances[4] != 100 ||
		level.StatusResistances[5] != 0 || level.StatusResistances[11] != 77 || level.StatusResistances[12] != 0 {
		t.Fatalf("CSV omits NULL but native BAD_STATUS includes it: %+v", level.StatusResistances)
	}
	engine := &BattleEngine{rng: newXorShift128(1)}
	enemy := &battleEnemy{Level: level}
	role := CombatSkillRole{Function: "STAN", Parameters: [10]string{"1", "100"}}
	if engine.stunHits(nil, 0, level.StatusResistances[1], role, 1) ||
		!engine.enemyResistsDOTStatus(enemy, "POISON") || engine.enemyResistsDOTStatus(enemy, "BURN") {
		t.Fatal("CSV resistance reached the wrong control/DOT family")
	}
}

func TestNativeEnemyAttributeSentinelDualSelectionAndRewrite(t *testing.T) {
	for _, tc := range []struct {
		attack, enemy string
		master, want  int
		selected      string
	}{
		{"FIRE", "WIND", 100, 200, "FIRE"}, {"FIRE", "ICE", 100, 50, "FIRE"},
		{"ICE", "FIRE", 100, 200, "ICE"}, {"ICE", "WIND", 100, 50, "ICE"},
		{"WIND", "ICE", 100, 200, "WIND"}, {"WIND", "FIRE", 100, 50, "WIND"},
		{"LIGHT", "DARK", 100, 200, "LIGHT"}, {"DARK", "LIGHT", 100, 200, "DARK"},
		{"FIRE", "WIND", 0, 100, "FIRE"}, {"FIRE", "WIND", 75, 75, "FIRE"},
		{"FIRE_ICE", "FIRE", 100, 200, "ICE"}, {"FIRE_ICE", "LIGHT", 100, 100, "FIRE"},
		{"NEUTRAL", "WIND", 100, 100, "NEUTRAL"}, {"NULL", "WIND", 100, 100, "NULL"},
	} {
		enemy := &battleEnemy{Attribute: tc.enemy}
		for i := range enemy.Level.AttributeRates {
			enemy.Level.AttributeRates[i] = tc.master
		}
		if rate, selected := enemyBaseAttackAttribute(enemy, tc.attack); rate != tc.want || selected != tc.selected {
			t.Errorf("%s vs %s master %d: %d/%s want %d/%s", tc.attack, tc.enemy, tc.master, rate, selected, tc.want, tc.selected)
		}
	}
	enemy := &battleEnemy{Attribute: "WIND", BaseAttribute: "WIND", Effects: []battleEffect{{Function: "REWRITE", Attribute: "ICE", Remaining: 1}}}
	enemy.Level.AttributeRates[0] = 100
	refreshEnemyAttribute(enemy)
	if rate := enemyAttributeRate(enemy, "FIRE"); rate != 50 {
		t.Fatalf("rewrite must affect elemental damage and hand weak projection: %d", rate)
	}
}

func TestEnemyAttributeArmorAndDefenseProjectThroughUserAttack(t *testing.T) {
	for _, tc := range []struct {
		name, function  string
		defense, damage int
	}{
		{"base", "", 1000, 10700},
		{"guard break counted once", "GUARD_BREAK_FIXED", 800, 10900},
		{"defense buff counted once", "DEF_UP_FIXED", 1200, 10500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			row := make([]string, 32)
			row[0], row[9], row[10], row[14], row[17], row[18] = "1", "WIND", "100000", "1000", "300", "400"
			definition, err := battlecontent.ParseEnemy(row)
			if err != nil {
				t.Fatal(err)
			}
			levelRow := make([]string, 334)
			levelRow[0], levelRow[2], levelRow[3] = "1", "100", "100"
			level, err := battlecontent.ParseEnemyLevel(levelRow)
			if err != nil {
				t.Fatal(err)
			}
			skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "ENEMY_ONE", Attribute: "FIRE", Kind: "ATTACK", DamageKind: "PHYSICS"}
			role := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"6000", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
			catalog := &CombatCatalog{
				Cards:  map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
				Skills: map[int][]CombatSkillDefinition{1: {skill}}, SkillRoles: map[int][]CombatSkillRole{1: {role}},
				Enemies: map[int]CombatEnemyDefinition{1: definition}, EnemyLevels: map[int]CombatEnemyLevel{1: level},
			}
			engine := &BattleEngine{catalog: catalog, phase: PhaseUser, turn: 1, rng: newXorShift128(1), selectedPlays: map[int]cardPlaySubmission{1: {CardTypes: [5]int{1}, Targets: [5]int{5}}, 2: {}, 3: {}, 4: {}}}
			for i := range engine.players {
				engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 100}
			}
			engine.players[0].Deck[0] = BattleCard{CardID: 1, CardType: 1, Level: 1}
			engine.players[0].Hand[0] = 1
			engine.players[0].Effects = []battleEffect{{Function: "ENCHANT", Attribute: "ICE", Value: 2000, Remaining: 2}}
			party := CombatEnemyParty{}
			party.Slots[0].EnemyID, party.Slots[0].HPRate = 1, 1
			if err := engine.loadEnemyParty(party, nil); err != nil {
				t.Fatal(err)
			}
			enemy := &engine.enemies[0]
			if tc.function != "" {
				buff := CombatSkillRole{Function: tc.function, Target: "ENEMY_ONE", Parameters: [10]string{"2", "DEF", "1000", "200"}}
				if _, err := engine.executeEnemyParameterRole(enemy, 5, buff); err != nil {
					t.Fatal(err)
				}
			}
			if enemy.Defense != tc.defense || engine.enemyEffectiveDefense(enemy, "PHYSICS") != tc.defense {
				t.Fatal("parameter projection and consumed defense diverged", enemy.Defense)
			}
			rows, err := engine.UserAttack()
			if err != nil {
				t.Fatal(err)
			}
			var hits []BattleResult
			for _, row := range rows {
				if row.Command == 60 {
					hits = append(hits, row)
				}
			}
			if len(hits) != 2 || hits[0].Args[2] != -int64(tc.damage) || hits[0].Args[4] != 6000 || hits[0].Args[5] != 1 || hits[0].Args[6] != 200 ||
				hits[1].Args[2] != -600 || hits[1].Args[4] != -1000 || hits[1].Args[5] != 2 || hits[1].Args[6] != 50 || enemy.HP != 100000-tc.damage-600 {
				t.Fatalf("ordinary/enchant armor or wire mismatch: %+v HP=%d", hits, enemy.HP)
			}
		})
	}
}

func TestNativeDualAttributeChoosesOneFixedArmorAndDOTIgnoresIt(t *testing.T) {
	engine := &BattleEngine{enemyCount: 1, rng: newXorShift128(1)}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 100, MaxHP: 100}
	enemy := &engine.enemies[0]
	*enemy = battleEnemy{MemberType: 5, Attribute: "FIRE", HP: 10000, MaxHP: 10000, AttributeFixed: [5]int{1500000, 400}}
	enemy.Level.AttributeRates[0], enemy.Level.AttributeRates[1] = 100, 100
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"1000", "0", "0", "0", "1", "ATK", "0", "FIRE_ICE", "PHYSICS"}}
	rows, err := engine.executePlayerAttack(battleAction{memberType: 1, target: 5}, role, 1)
	if err != nil || rows[0].Command != 60 || rows[0].Args[2] != -1600 || rows[0].Args[5] != 2 || rows[0].Args[6] != 200 {
		t.Fatalf("dual attribute must use ICE and only ICE armor: %+v %v", rows, err)
	}
	dot := engine.dotEffectForEnemy(battleEffect{Function: "BURN", Value: 1000}, enemy)
	if dot.Value != 1000 {
		t.Fatal("8bb40 does not subtract fixed attribute armor from DOT", dot.Value)
	}
	if damage, _ := nativeAttributeEnchantDamage(1000, 0, 100); damage != 100 {
		t.Fatal("enchant clamp must follow fixed adjustment", damage)
	}
}
