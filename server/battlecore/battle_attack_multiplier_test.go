package battlecore

import "testing"

func TestNativeAttackBoostWeaknessAttributeDefenseBeforeChain(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	actor, enemy := &engine.players[0], &engine.enemies[0]
	enemy.HP, enemy.MaxHP = 10000, 10000
	actor.Effects = []battleEffect{{Function: "DAMAGE_BOOST", ListType: 1, Remaining: 99, Value: 7, Rate: 17}}
	enemy.Effects = []battleEffect{
		{Function: "WEAKNESS", Remaining: 2, Rate: 123},
		{Function: "ATTR_DEF_DOWN", Remaining: 2, Attribute: "FIRE", Parameters: [4]int{37}},
	}
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", ChainRate: 20,
		Parameters: [10]string{"103", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
	action := battleAction{memberType: 1, target: 5, cardLevel: 1,
		skill: CombatSkillDefinition{Attribute: "FIRE", DamageKind: "PHYSICS"}, roles: []CombatSkillRole{role}}
	rows, err := engine.executePlayerAttack(action, role, 4)
	if err != nil {
		t.Fatal(err)
	}
	// 927b0: (103+7)*1017/1000=111; *1123/1000=124;
	// *1037/1000=128; *160/100=204. No element, armor or critical change.
	if len(rows) < 1 || rows[0].Args[2] != -204 || enemy.HP != 9796 {
		t.Fatalf("wrong native multiplier order: rows=%+v HP=%d", rows, enemy.HP)
	}
}

func TestNativeTribalBoostUsesPartyPresenceAndSameBoostGroup(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	// The selected body has no matching race; only its already destroyed part does.
	engine.enemyCount = 2
	engine.enemies[1] = battleEnemy{MemberType: 6, EnemyID: 2, TribeIDs: []int{1}, HP: 0, Broken: true}
	engine.catalog.Enemies[2] = CombatEnemyDefinition{ID: 2, TribeIDs: []int{1}}
	engine.players[0].Effects = []battleEffect{{Function: "DAMAGE_BOOST", ListType: 1, Remaining: 99, Value: 10, Rate: 500}}
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", ChainRate: 20,
		Parameters: [10]string{"100", "0", "0", "0", "1", "ATK", "0", "FIRE", "MAGIC"}}
	// Official burst role 65100068's active parameter shape.
	tribal := CombatSkillRole{Function: "DAMAGE_BOOST_ORDER_TRIBAL",
		Parameters: [10]string{"1", "1200", "", "", "", "NULL", "MAGIC", "0", "0", "1"}}
	action := battleAction{memberType: 1, target: 5, cardLevel: 1,
		skill: CombatSkillDefinition{Attribute: "FIRE", DamageKind: "MAGIC"}, roles: []CombatSkillRole{role, tribal}}
	rows, err := engine.executePlayerAttack(action, role, 4)
	if err != nil {
		t.Fatal(err)
	}
	// (100+10)*(1000+500+1200)/1000=297; Chain -> 475.
	if rows[0].Args[2] != -475 || engine.enemies[0].HP != 9525 {
		t.Fatalf("tribal boost must share the EX group and use whole party: %+v", rows)
	}
	delete(engine.catalog.Enemies, 2)
	if !engine.enemyPartyHasRace(1) {
		t.Fatal("registered enemy race must not depend on later catalog changes")
	}
}
