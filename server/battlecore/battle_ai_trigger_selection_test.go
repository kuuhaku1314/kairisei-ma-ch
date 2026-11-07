package battlecore

import "testing"

func TestAISelectedCardTriggerExcludesSphereAndUsesClosedBounds(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.selectedPlays[1] = cardPlaySubmission{CardTypes: [5]int{1}}
	engine.turnActions = []battleAction{
		{memberType: 1, cardType: 1, skill: CombatSkillDefinition{Attribute: "FIRE", Kind: "ATTACK"}},
		{memberType: 1, sphereSlot: 1, skill: CombatSkillDefinition{Attribute: "FIRE", Kind: "ATTACK"}},
	}
	if engine.userPlayCardCountMatches("MERCENARY", "FIRE", "ATTACK", "2", "2") {
		t.Fatal("sphere was included in enemy AI card count")
	}
	if engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "0", "0") {
		t.Fatal("zero upper bound was incorrectly treated as unlimited")
	}
	wantRNG := engine.rng
	wantRNG.next()
	if !engine.userPlayCardCountMatches("MERCENARY", "FIRE_ICE", "ATTACK", "1", "1") || engine.enemyTriggerTarget != 1 || engine.rng != wantRNG {
		t.Fatal("typed attribute, retained singleton or native RNG advancement differs")
	}
	engine.players[0].HP = 0
	unchanged := engine.rng
	if engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "1", "1") || engine.rng != unchanged {
		t.Fatal("KO/no-candidate condition must fail without a roll")
	}
}

func TestAIDrawTriggerUsesEffectsAndRetainsHighestLivingTarget(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{991: {ID: 991, Fields: enemyAIConditionFields("DEAL_NUM_BY_HIGH_USER", "3", "5")}}
	engine.turnStats.PlayedByUser = [8]int{5, 0, 0, 0}
	if engine.enemyAIConditionSatisfied(&engine.enemies[0], 991) {
		t.Fatal("played-card count was mistaken for draw effects")
	}
	engine.players[1].Effects = []battleEffect{{Function: "DEAL_BONUS", Value: 4}, {Function: "DEAL_PENALTY", Value: 1}}
	engine.players[2].Effects = []battleEffect{{Function: "DEAL_BONUS", Value: 3}}
	engine.players[3].Effects = []battleEffect{{Function: "DEAL_BONUS", Value: 5}}
	engine.players[3].HP = 0
	wantRNG := engine.rng
	wantTarget := []int{2, 3}[wantRNG.next()%2]
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 991) || engine.enemyTriggerTarget != wantTarget || engine.rng != wantRNG {
		t.Fatal("highest draw effect tie, KO exclusion or retained RNG target differs")
	}
	if !enemyAITriggerRetainsTarget("DEAL_NUM_BY_HIGH_USER") || engine.highestUserDrawMatches("0", "0") {
		t.Fatal("draw trigger target contract or closed upper bound differs")
	}
	for index := range engine.players {
		engine.players[index].Effects = []battleEffect{{Function: "DEAL_PENALTY", Value: 2}}
	}
	if !engine.highestUserDrawMatches("-2", "-2") {
		t.Fatal("negative draw changes were incorrectly clamped to zero")
	}
}

func TestAISelectedCardPassRegistrationAndPhaseLifetime(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.UserPhase(); err != nil {
		t.Fatal(err)
	}
	unchanged := engine.rng
	if engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "0", "0") || engine.rng != unchanged {
		t.Fatal("unregistered player was treated as PASS")
	}
	for member := 1; member <= 4; member++ {
		if _, err := engine.Submit(member, cardPlaySubmission{}); err != nil {
			t.Fatal(err)
		}
	}
	if !engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "0", "0") {
		t.Fatal("explicit PASS was not registered")
	}
	if _, err := engine.UserAttack(); err != nil {
		t.Fatal(err)
	}
	// Keep this an engine-phase regression; no boss action is needed to reach
	// the next turn. The selected action sentinel detects premature clearing.
	engine.turnActions = []battleAction{{memberType: 1, cardType: 1}}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if !engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "1", "1") {
		t.Fatal("TurnPhase discarded the native retained selection")
	}
	if _, err := engine.UserPhase(); err != nil {
		t.Fatal(err)
	}
	if len(engine.turnActions) != 0 || len(engine.selectedPlays) != 0 || engine.userPlayCardCountMatches("MERCENARY", "NULL", "NULL", "0", "0") {
		t.Fatal("UserPhase did not clear selection registration and actions together")
	}
}

func TestAIParameterTriggerSingletonConsumesRNG(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.players[0].Attack = 100
	if engine.userParameterMatches("MERCENARY", "ATK", "0", "0", 0) {
		t.Fatal("parameter trigger upper=0 is not an unbounded maximum")
	}
	wantRNG := engine.rng
	wantRNG.next()
	if !engine.userParameterMatches("MERCENARY", "ATK", "100", "100", -1) || engine.enemyTriggerTarget != 1 || engine.rng != wantRNG {
		t.Fatal("native nonpositive rank/singleton target RNG differs")
	}
}
