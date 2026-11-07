package battlecore

import "testing"

func permutedArthurEngine() *BattleEngine {
	engine := &BattleEngine{rng: newXorShift128(42)}
	for index, arthurType := range []int{4, 1, 2, 3} {
		engine.players[index] = battlePlayer{MemberType: index + 1, ArthurType: arthurType, HP: 100, MaxHP: 100, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	return engine
}

func TestArthurTargetsFollowProfessionNotRoomPosition(t *testing.T) {
	engine := permutedArthurEngine()
	enemy := &battleEnemy{MemberType: 5}
	for job, member := range map[string]int{"SINGER": 1, "MERCENARY": 2, "MILLIONAIRE": 3, "THIEF": 4} {
		before := engine.rng
		wantRNG := before
		wantRNG.next()
		if got, ok := engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: job}); !ok || got != member || engine.rng != wantRNG {
			t.Fatalf("%s target=%d ok=%t, want %d and one native RNG draw", job, got, ok, member)
		}
		if got := engine.playerTargets(job, 1, 4); len(got) != 1 || got[0] != member {
			t.Fatalf("%s player role targets=%v", job, got)
		}
		if got := engine.enemyRolePlayerTargets(enemy, 4, CombatSkillRole{Target: job}); len(got) != 1 || got[0] != member {
			t.Fatalf("%s enemy role targets=%v", job, got)
		}
		for attempt := 0; attempt < 12; attempt++ {
			if got, ok := engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: "RANDOM_EXCEPT_" + job}); !ok || got == member {
				t.Fatalf("excluding %s selected %d ok=%t", job, got, ok)
			}
		}
	}
	engine.players[0].HP = 0
	if got, ok := engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: "SINGER"}); !ok || got == 1 {
		t.Fatalf("dead singer must fall back to a living member: %d %t", got, ok)
	}
	if got, ok := engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: "SINGER_INVOLVE_DEAD"}); !ok || got != 1 {
		t.Fatalf("include-dead singer target=%d ok=%t", got, ok)
	}
	for index := range engine.players {
		engine.players[index].HP = 0
	}
	engine.players[0].HP = 100
	before := engine.rng
	for _, kind := range []string{"RANDOM_EXCEPT_SINGER", "MERCENARY"} {
		if got, ok := engine.selectEnemyActionTarget(enemy, CombatEnemyAction{Target: kind}); !ok || got != 1 || engine.rng != before {
			t.Fatalf("sole survivor fallback %s: %d %t", kind, got, ok)
		}
	}
}

func TestArthurConditionsReadTheMatchingPlayer(t *testing.T) {
	engine := permutedArthurEngine()
	engine.players[0].Attack = 789
	if !engine.userParameterMatches("SINGER", "ATK", "789", "789", 0) || engine.enemyTriggerTarget != 1 {
		t.Fatal("singer stat condition read the room's fourth slot")
	}
	engine.turnActions = []battleAction{{memberType: 1, cardType: 1, skill: CombatSkillDefinition{Attribute: "DARK", Kind: "ATTACK"}}}
	engine.selectedPlays = map[int]cardPlaySubmission{1: {CardTypes: [5]int{1}}}
	if !engine.userPlayCardCountMatches("SINGER", "NULL", "NULL", "1", "1") || engine.enemyTriggerTarget != 1 {
		t.Fatal("singer card count did not retain the real member")
	}
	engine.players[0].Effects = []battleEffect{{Function: "WEAKNESS", Kind: 2, Remaining: 1}}
	if !engine.enemyAIStatusCondition(&battleEnemy{}, "SKILL_ROLE_KIND_DEBUFF_BY_USER_ONE", []string{"SINGER", "WEAKNESS"}) || engine.enemyTriggerTarget != 1 {
		t.Fatal("singer debuff condition read the wrong player")
	}
	engine.players[0].HP = 0
	if !engine.userDeadMatches("SINGER", 1) || engine.userDeadMatches("THIEF", 1) {
		t.Fatal("death condition confused Arthur profession and member ID")
	}
}
