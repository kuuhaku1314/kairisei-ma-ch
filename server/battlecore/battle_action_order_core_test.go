package battlecore

import (
	"testing"
)

func TestNativeActionSortConsumesTieRollsInBubbleOrder(t *testing.T) {
	engine := &BattleEngine{rng: newXorShift128(1)}
	actions := []battleAction{{memberType: 1}, {memberType: 2}, {memberType: 3}, {memberType: 4}}
	engine.sortPlayerActions(actions)
	// FUN_000c6e10 compares (2,3),(1,2),(0,1),(2,3),(1,2),(2,3).
	// With native seed 1 the six low bits are 1,0,1,1,0,1.
	for index, want := range []int{2, 1, 4, 3} {
		if got := actions[index].memberType; got != want {
			t.Fatalf("action %d actor=%d, want %d", index, got, want)
		}
	}
	if got := engine.rng.next(); got != 3556250659 {
		t.Fatalf("next native RNG word=%d, want 3556250659", got)
	}
	before := engine.rng
	actions = []battleAction{{memberType: 1, automatic: true}, {memberType: 2}, {memberType: 2}}
	engine.sortPlayerActions(actions)
	if actions[0].automatic || actions[1].automatic || !actions[2].automatic || engine.rng != before {
		t.Fatal("manual/automatic or same-actor ties must not consume RNG")
	}
}

func TestNativeChainUsesDistinctCardActorsAndDualAttributeUnion(t *testing.T) {
	actions := []battleAction{
		{memberType: 1, cardType: 1, skill: CombatSkillDefinition{Attribute: "FIRE_ICE"}},
		{memberType: 1, cardType: 2, skill: CombatSkillDefinition{Attribute: "FIRE"}},
		{memberType: 2, cardType: 1, skill: CombatSkillDefinition{Attribute: "FIRE"}},
		{memberType: 3, cardType: 1, skill: CombatSkillDefinition{Attribute: "ICE"}},
		{memberType: 4, sphereSlot: 1, skill: CombatSkillDefinition{Attribute: "FIRE_ICE"}},
	}
	engine := &BattleEngine{turnActions: actions}
	counts := engine.currentChainCounts()
	for attribute, want := range map[string]int{"FIRE_ICE": 3, "FIRE": 2, "ICE": 2} {
		if got := maxCombatChain(attribute, counts); got != want {
			t.Errorf("%s chain=%d; want %d (distinct cards, excluding spheres)", attribute, got, want)
		}
	}
	if got := maxCombatChain("FIRE", battleActionChainCounts(actions[:2])); got != 0 {
		t.Fatalf("one actor must project native chain 0, got %d", got)
	}
}

func TestUserAttackBranchesObserveEarlierActions(t *testing.T) {
	for _, condition := range []string{"SELF_BUFF", "BUFF_EXEC", "SELF_HP_PER"} {
		t.Run(condition, func(t *testing.T) {
			first := CombatSkillDefinition{ID: 1, FunctionID: 1, PriorityPVE: 1, Kind: "SUPPORT", Target: "SELF", Attribute: "FIRE"}
			second := CombatSkillDefinition{ID: 2, FunctionID: 2, PriorityPVE: 2, Kind: "SUPPORT", Target: "SELF", Attribute: "FIRE"}
			branch := second
			branch.FunctionID, branch.BranchPriority = 3, 1
			branch.BranchCondition, branch.BranchParameters = condition, [5]string{"ATK_UP_BY_ATK"}
			buff := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "1000", "100"}}
			if condition == "SELF_HP_PER" {
				branch.BranchParameters = [5]string{"100", "100"}
				buff = CombatSkillRole{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"100"}}
			}
			catalog := &CombatCatalog{
				Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}, 2: {ID: 2, NormalSkillID: 2}},
				Skills:     map[int][]CombatSkillDefinition{1: {first}, 2: {second, branch}},
				SkillRoles: map[int][]CombatSkillRole{1: {buff}, 2: {buff}, 3: {buff}},
			}
			engine := &BattleEngine{catalog: catalog, phase: PhaseUser, turn: 1, enemyCount: 1,
				selectedPlays: map[int]cardPlaySubmission{1: {CardTypes: [5]int{1, 2}, Targets: [5]int{1, 1}}, 2: {}, 3: {}, 4: {}}}
			for i := range engine.players {
				engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 50, MaxHP: 100, BaseMaxHP: 100}
			}
			engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
			engine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 2, Level: 1}
			engine.players[0].Hand = [5]int{1, 2}
			engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100}
			rows, err := engine.UserAttack()
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, row := range rows {
				if row.Command == CommandCardSkill && row.Args[1] == 2 {
					found = true
					if row.Args[8] != 3 || row.Args[9] != 1 {
						t.Fatalf("second card ignored earlier action: %+v", row)
					}
				}
			}
			if !found {
				t.Fatal("second card did not execute")
			}
		})
	}
}

func TestBurstModifierNeverMutatesSharedRoles(t *testing.T) {
	role := CombatSkillRole{Function: "ATTACK_AA", Parameters: [10]string{"", "", "", "", "1"}}
	catalog := &CombatCatalog{
		SkillRoles: map[int][]CombatSkillRole{1: {role},

			2: {{Function: "ATTACK_MULTISTAGE", Parameters: [10]string{"", "PHYSICS", "3"}}}},
		Skills: map[int][]CombatSkillDefinition{2: {{ID: 2, FunctionID: 2}}},
	}
	engine := &BattleEngine{catalog: catalog}
	engine.players[0].CardBurstSkills[1] = []battleCardBurstSkill{{SkillID: 2}}
	action := battleAction{memberType: 1, cardType: 1, skill: CombatSkillDefinition{DamageKind: "PHYSICS"}, roles: catalog.SkillRoles[1]}
	if err := engine.attachBurstCardModifiers(&action); err != nil {
		t.Fatal(err)
	}
	if action.roles[0].Parameters[4] != "3" || catalog.SkillRoles[1][0].Parameters[4] != "1" {
		t.Fatalf("burst effect leaked into the catalog: action=%+v catalog=%+v", action.roles, catalog.SkillRoles[1])
	}
}

func TestLaterCardRetargetsDestroyedPartBeforeDirectionAndBranch(t *testing.T) {
	first := CombatSkillDefinition{ID: 1, FunctionID: 1, PriorityPVE: 1, Target: "ENEMY_ONE", Attribute: "FIRE"}
	second := first
	second.ID, second.FunctionID, second.PriorityPVE = 2, 2, 2
	branch := second
	branch.FunctionID, branch.BranchPriority = 3, 1
	branch.BranchCondition, branch.BranchParameters = "TARGET_ATTR", [5]string{"FIRE"}
	attack := CombatSkillRole{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"100", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
	catalog := &CombatCatalog{
		Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}, 2: {ID: 2, NormalSkillID: 2}},
		Skills:     map[int][]CombatSkillDefinition{1: {first}, 2: {second, branch}},
		SkillRoles: map[int][]CombatSkillRole{1: {attack}, 2: {attack}, 3: {attack}},
	}
	engine := &BattleEngine{catalog: catalog, phase: PhaseUser, turn: 1, enemyCount: 2, rng: newXorShift128(602),
		selectedPlays: map[int]cardPlaySubmission{1: {CardTypes: [5]int{1, 2}, Targets: [5]int{6, 6}}, 2: {}, 3: {}, 4: {}}}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 100, BaseMaxHP: 100}
	}
	engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
	engine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 2, Level: 1}
	engine.players[0].Hand = [5]int{1, 2}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 10000, MaxHP: 10000, Attribute: "FIRE"}
	engine.enemies[1] = battleEnemy{MemberType: 6, HP: 1, MaxHP: 1, Attribute: "ICE"}
	rows, err := engine.UserAttack()
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, row := range rows {
		if row.Command == CommandCardSkill && row.Args[1] == 2 {
			found = true
			if row.Args[3] != 5 || row.Args[8] != 3 {
				t.Fatalf("direction/branch still targets the destroyed part: %+v", row)
			}
		}
	}
	if !found || engine.enemies[1].HP != 0 || engine.enemies[0].HP >= 10000 {
		t.Fatal("first card must destroy the part and second card must hit the living body")
	}
}
