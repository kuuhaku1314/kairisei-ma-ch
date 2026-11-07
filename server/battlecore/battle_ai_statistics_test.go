package battlecore

import "testing"

func TestEnemyAIUsesExecutedChainAndKinds(t *testing.T) {
	for _, tc := range []struct {
		name        string
		secondActor bool
		knockedOut  bool
		wantChain   int
	}{
		{name: "one actor plays two fire cards"},
		{name: "two actors really chain", secondActor: true, wantChain: 2},
		{name: "selected but never executed", secondActor: true, knockedOut: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "SELF", Kind: "HEAL", Attribute: "FIRE"}
			engine := &BattleEngine{phase: PhaseUser, turn: 1, enemyCount: 1,
				catalog: &CombatCatalog{
					Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
					Skills:     map[int][]CombatSkillDefinition{1: {skill}},
					SkillRoles: map[int][]CombatSkillRole{1: {{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"100"}}}},
					EnemyAIOrders: map[int]CombatEnemyAIOrder{
						1: {Fields: enemyAIConditionFields("DECK_COMBO_COUNT", "2", "2")},
						2: {Fields: enemyAIConditionFields("SKILL_KIND", "HEAL")},
					},
				}, selectedPlays: map[int]cardPlaySubmission{1: {}, 2: {}, 3: {}, 4: {}}}
			engine.enemies[0] = battleEnemy{MemberType: 5, HP: 1000, MaxHP: 1000}
			for i := range engine.players {
				engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 100}
			}
			for i := 0; i < 2; i++ {
				owner, slot := 0, i
				if tc.secondActor {
					owner, slot = i, 0
				}
				p := &engine.players[owner]
				p.Deck[slot] = BattleCard{CardType: slot + 1, CardID: 1, Level: 1}
				p.Hand[slot] = slot + 1
				s := engine.selectedPlays[owner+1]
				s.CardTypes[slot], s.Targets[slot] = slot+1, owner+1
				engine.selectedPlays[owner+1] = s
				if tc.knockedOut {
					p.HP = 0
				}
			}
			if _, err := engine.UserAttack(); err != nil {
				t.Fatal(err)
			}
			if engine.turnStats.MaxChain != tc.wantChain {
				t.Fatalf("actual Chain=%d, want %d", engine.turnStats.MaxChain, tc.wantChain)
			}
			if engine.enemyAIConditionSatisfied(&engine.enemies[0], 1) != (tc.wantChain == 2) {
				t.Fatal("AI counted selected cards instead of actual Chain")
			}
			if engine.enemyAIConditionSatisfied(&engine.enemies[0], 2) == tc.knockedOut {
				t.Fatal("AI skill kind included a card that never executed")
			}
		})
	}
}

func TestSphereContributesExecutedKindButNoChain(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.catalog.Spheres = map[int]CombatSphereDefinition{1: {ID: 1, SkillID: 1}}
	engine.players[0].Spheres[0] = battleSphere{SphereID: 1, Slot: 1, Count: 1, Maximum: 1}
	action := battleAction{memberType: 1, sphereSlot: 1, cardLevel: 1, target: 1,
		skill: CombatSkillDefinition{ID: 1, Target: "SELF", Kind: "HEAL", Attribute: "FIRE"},
		roles: []CombatSkillRole{{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"100"}}}}
	if _, err := engine.executeSphereAction(action, 0); err != nil {
		t.Fatal(err)
	}
	if engine.turnStats.MaxChain != 0 || !engine.anySkillKind([]string{"HEAL"}) {
		t.Fatal("native sphere execution records kind, but Chain remains zero")
	}
}

func TestHealTotalAIIncludesOverhealButNotRegeneration(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.catalog.EnemyAIOrders = map[int]CombatEnemyAIOrder{
		1: {Fields: enemyAIConditionFields("HEAL_TOTAL", "200")},
		2: {Fields: enemyAIConditionFields("HEAL_TOTAL", "201")},
	}
	player := &engine.players[0]
	player.HP = player.MaxHP
	action := battleAction{memberType: 2, target: 1}
	if _, err := engine.healPlayerTargets(action, CombatSkillRole{Target: "SELECT"}, 200); err != nil {
		t.Fatal(err)
	}
	player.Effects = []battleEffect{{Function: "REGENERATE_FIXED", Value: 300, Source: 2, Remaining: 2}}
	engine.regenerateMembers()
	if !engine.enemyAIConditionSatisfied(&engine.enemies[0], 1) || engine.enemyAIConditionSatisfied(&engine.enemies[0], 2) {
		t.Fatal("HEAL_TOTAL must sum reported positive direct healing, even at full HP; regen is separate")
	}
}
