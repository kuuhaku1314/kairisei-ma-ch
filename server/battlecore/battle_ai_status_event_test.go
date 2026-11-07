package battlecore

import "testing"

func TestAIStatusEventsSurviveRemovalAndKeepEnemyScope(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemyCount = 2
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType = 6
	action := battleAction{memberType: 1, cardLevel: 1, target: 6}
	role := CombatSkillRole{Function: "ATK_BREAK_FIXED", Target: "ENEMY_ONE", Parameters: [10]string{"2", "ATK", "1000", "20"}}
	if _, err := engine.executePlayerRole(action, role, 1); err != nil {
		t.Fatal(err)
	}
	engine.enemies[1].Effects = nil
	if engine.enemyAIStatusCondition(&engine.enemies[0], "SKILL_ROLE_KIND_DEBUFF_NOW_TURN", []string{"ATK_BREAK_BY_ATK"}) ||
		!engine.enemyAIStatusCondition(&engine.enemies[1], "SKILL_ROLE_KIND_DEBUFF_NOW_TURN", []string{"ATK_BREAK_BY_ATK"}) {
		t.Fatal("applied debuff event was erased or leaked to an unhit enemy")
	}
	engine.enemies[1].HP = 0 // the player buff is broadcast to retained KO enemies too.
	role.Function, role.Target, role.Parameters[1] = "ATK_UP_FIXED", "SELF", "INT"
	if _, err := engine.executePlayerRole(action, role, 1); err != nil {
		t.Fatal(err)
	}
	engine.players[0].Effects = nil
	for i := 0; i < 2; i++ {
		if !engine.enemyAIStatusCondition(&engine.enemies[i], "SKILL_ROLE_KIND_BUFF_NOW_TURN_BY_USER", []string{"ATK_UP_BY_INT"}) {
			t.Fatal("player buff application was lost after removal or KO exclusion")
		}
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		if engine.enemies[i].AITurn.DebuffKinds != 0 || engine.enemies[i].AITurn.PlayerBuffKinds != [skillRoleBuffKindCount]uint32{} {
			t.Fatal("status events survived native turn reset")
		}
	}
}

func TestAIPlayerBuffEventExcludesEnemyCasterAndRejectedReplacement(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	role := CombatSkillRole{Function: "REGENERATE_FIXED", Target: "USER_ONE", Parameters: [10]string{"3", "100"}}
	if _, err := engine.executeEnemyPersistentEffect(&engine.enemies[0], 1, role); err != nil {
		t.Fatal(err)
	}
	if enemyAITurnStatusPresent(&engine.enemies[0], []string{"HEAL"}, true) {
		t.Fatal("enemy-to-player status was recorded as player-to-player")
	}
	// Existing stronger status survives. A rejected weaker player cast must
	// not create an application event merely because that status exists.
	engine.players[0].Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 10000, Remaining: 3}}
	role.Target = "SELF"
	if _, err := engine.executePersistentEffect(battleAction{memberType: 1, target: 1, cardLevel: 1}, role, 1, 1); err != nil {
		t.Fatal(err)
	}
	if enemyAITurnStatusPresent(&engine.enemies[0], []string{"HEAL"}, true) {
		t.Fatal("rejected replacement created a new application event")
	}
	engine.players[0].Effects = nil
	if _, err := engine.executePersistentEffect(battleAction{memberType: 1, target: 1, cardLevel: 1}, role, 1, 1); err != nil {
		t.Fatal(err)
	}
	if !enemyAITurnStatusPresent(&engine.enemies[0], []string{"HEAL"}, true) {
		t.Fatal("accepted player regeneration did not create HEAL event")
	}
}
