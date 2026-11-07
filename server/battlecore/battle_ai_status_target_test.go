package battlecore

import "testing"

func TestAIHandCountRetainsTargetAndClosedRange(t *testing.T) {
	engine := permutedArthurEngine()
	engine.players[0].Hand = [5]int{1, 2}
	engine.players[1].Hand = [5]int{1, 2}
	engine.players[1].HP = 0
	wantRNG := engine.rng
	wantRNG.next()
	if !engine.userHandCountMatches("NULL", "2", "2") || engine.enemyTriggerTarget != 1 || engine.rng != wantRNG {
		t.Fatal("hand-count target, NULL job, KO filter or singleton RNG differs")
	}
	if engine.userHandCountMatches("SINGER", "0", "0") {
		t.Fatal("hand count upper=0 was treated as unlimited")
	}
}

func TestAICurrentStatusListScopeAndRetainedDelta(t *testing.T) {
	engine := permutedArthurEngine()
	actor := &battleEnemy{MemberType: 5}
	engine.players[0].Effects = []battleEffect{{Function: "ATK_UP_FIXED", Parameter: "INT", Delta: 100, ListType: 5}}
	if !engine.enemyAIStatusCondition(actor, "SKILL_ROLE_KIND_BUFF_BY_USER", []string{"ATK_UP_BY_INT"}) {
		t.Fatal("actor-owned status with zero remaining was omitted")
	}
	if engine.enemyAIStatusCondition(actor, "SKILL_ROLE_KIND_BUFF_BY_USER_ONE", []string{"SINGER", "ATK_UP_BY_INT"}) {
		t.Fatal("by-user-one must only inspect NORMAL")
	}
	engine.players[0].Effects[0].ListType = 0
	wantRNG := engine.rng
	wantRNG.next()
	if !engine.enemyAIStatusCondition(actor, "SKILL_ROLE_KIND_BUFF_BY_USER_ONE", []string{"NULL", "ATK_UP_BY_INT"}) || engine.enemyTriggerTarget != 1 || engine.rng != wantRNG {
		t.Fatal("normal status target was not retained with native RNG")
	}
	engine.players[0].Effects[0].Delta = 0
	if engine.enemyAIStatusCondition(actor, "SKILL_ROLE_KIND_BUFF_BY_USER", []string{"ATK_UP_BY_INT"}) {
		t.Fatal("zero delta was classified by function name rather than retained value")
	}
	if !engine.enemyAIStatusCondition(actor, "SKILL_ROLE_KIND_BUFF_BY_USER", []string{"NULL"}) {
		t.Fatal("known zero-delta parameter status must retain NULL kind")
	}
}

func TestAIEnemyStatusExactTargetIncludesKOAndSideTargetRolls(t *testing.T) {
	engine := permutedArthurEngine()
	engine.enemyCount = 2
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100}
	engine.enemies[1] = battleEnemy{MemberType: 6, Effects: []battleEffect{{Function: "POISON", ListType: 6}}}
	unchanged := engine.rng
	if !engine.enemyAIStatusCondition(&engine.enemies[0], "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY_ONE_AND", []string{"ENEMY2", "POISON", "NULL", "NULL"}) || engine.enemyTriggerTarget != 6 || engine.rng != unchanged {
		t.Fatal("exact KO target, NULL AND selector or no-roll retention differs")
	}
	if engine.enemyAIStatusCondition(&engine.enemies[0], "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", []string{"POISON"}) {
		t.Fatal("side-wide selection included a KO enemy")
	}
	engine.enemies[1].HP = 100
	wantRNG := engine.rng
	wantRNG.next()
	if !engine.enemyAIStatusCondition(&engine.enemies[0], "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", []string{"POISON"}) || engine.enemyTriggerTarget != 6 || engine.rng != wantRNG {
		t.Fatal("side-wide enemy selection did not retain target/RNG")
	}
	engine.enemies[1].Effects[0].ListType = 7
	if engine.enemyAIStatusCondition(&engine.enemies[0], "SKILL_ROLE_KIND_DEBUFF_BY_ENEMY", []string{"POISON"}) {
		t.Fatal("card-owned list leaked into enemy current status")
	}
}
