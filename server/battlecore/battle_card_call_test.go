package battlecore

import (
	"reflect"
	"testing"

	"kairisei.local/server/battlecontent"
)

func TestCardCallFieldFlowsThroughUserAttackToIndependentBlessPayload(t *testing.T) {
	row := make([]string, 34)
	row[0], row[26], row[32] = "101", "1", "2"
	card, err := battlecontent.ParseCard(row)
	if err != nil || card.CallSkillID != 2 {
		t.Fatalf("CALL column 32 lost: %+v, %v", card, err)
	}
	main := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "USER_ONE", Attribute: "FIRE"}
	call := CombatSkillDefinition{ID: 2, FunctionID: 2, Target: "USER_ALL", Attribute: "FIRE", AppendTrigger: "USER_ATTACK_END", AppendCondition: "TURN", AppendParameters: [5]string{"1", "0"}, AppendDuration: 3}
	registration := CombatSkillRole{Function: "BLESS", Target: "SELECT"} // zero uses CALL's duration
	heal := CombatSkillRole{Function: "HEAL_FIXED", Target: "SELECT", Parameters: [10]string{"0", "0", "1000", "0", "ATK"}}
	catalog := &CombatCatalog{Cards: map[int]CombatCardDefinition{101: card}, Skills: map[int][]CombatSkillDefinition{1: {main}, 2: {call}}, SkillRoles: map[int][]CombatSkillRole{1: {registration}, 2: {heal}}}
	engine := &BattleEngine{catalog: catalog, phase: PhaseUser, turn: 1, enemyCount: 1, rng: newXorShift128(1), selectedPlays: map[int]cardPlaySubmission{1: {}, 2: {CardTypes: [5]int{1}, Targets: [5]int{1}}, 3: {}, 4: {}}}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 1000, Attack: 10, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	engine.players[1].Attack = 200
	engine.players[1].Deck[0] = BattleCard{CardType: 1, CardID: 101, Level: 1}
	engine.players[1].Hand[0] = 1
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 1000, MaxHP: 1000}
	rows, err := engine.UserAttack()
	if err != nil {
		t.Fatal(err)
	}
	var set, executed bool
	for _, result := range rows {
		if result.Command == CommandHoldSet {
			set = result.Args[0] == 1 && result.Args[4] == 2 && result.Args[6] == 3
		}
		if result.Command == CommandHoldSkill {
			targetCode, _ := combatSkillTargetCode("USER_ALL")
			executed = result.Args[0] == 2 && result.Args[3] == 2 && result.Args[4] == 1 && result.Args[6] == int64(targetCode) && result.Args[10] == 0
		}
	}
	if !set || !executed {
		t.Fatalf("CALL registration/source/target wire wrong: %+v", rows)
	}
	for i, player := range engine.players {
		if player.HP != 300 || len(player.BlessHolds) != 0 {
			t.Fatalf("member %d: heal must use original caster ATK and USER_ALL; HP=%d", i+1, player.HP)
		}
	}
	if got, err := engine.executePlayerBless(battleAction{memberType: 1, skill: main}, registration, 0); len(got) != 0 || err != nil {
		t.Fatal("missing CALL must not fabricate a recursive main-skill payload")
	}
	delete(catalog.Skills, 2)
	if _, _, err := catalog.CardCallSkill(101); err == nil {
		t.Fatal("missing referenced CALL must be reported")
	}
}

func TestNativeAppendPassSnapshotsConditionsAndDefersLostRows(t *testing.T) {
	engine := &BattleEngine{catalog: &CombatCatalog{}, rng: newXorShift128(1)}
	for i := range engine.players {
		skill := CombatSkillDefinition{ID: i + 1, Target: "SELF", PriorityPVE: 1}
		engine.players[i] = battlePlayer{MemberType: i + 1, HP: 100, MaxHP: 1000, BlessHolds: []battleBlessHold{{SourceMember: i + 1, CardType: 22, Skill: skill, Remaining: 1, Roles: []CombatSkillRole{{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"10"}}}}}}
	}
	rows, err := engine.executeBlessHolds("USER_ATTACK_END", 22)
	if err != nil {
		t.Fatal(err)
	}
	var actors []int64
	lastSkill, firstLost := -1, len(rows)
	for i, row := range rows {
		if row.Command == CommandHoldSkill {
			actors = append(actors, row.Args[0])
			lastSkill = i
		}
		if row.Command == CommandHoldLost && firstLost == len(rows) {
			firstLost = i
		}
	}
	if !reflect.DeepEqual(actors, []int64{2, 1, 4, 3}) || firstLost <= lastSkill || engine.rng.next() != 3556250659 {
		t.Fatalf("global native hold ordering/lost tail wrong: %+v", rows)
	}
	// A newly granted buff may enable another hold only in the following pass.
	player := &engine.players[0]
	player.BlessHolds = []battleBlessHold{
		{SourceMember: 1, CardType: 22, Skill: CombatSkillDefinition{ID: 10, Target: "SELF"}, Remaining: 1, Roles: []CombatSkillRole{{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "1000", "10"}}}},
		{SourceMember: 1, CardType: 22, Skill: CombatSkillDefinition{ID: 11, Target: "SELF", AppendCondition: "SELF_BUFF", AppendParameters: [5]string{"ATK_UP_BY_ATK"}}, Remaining: 1, Roles: []CombatSkillRole{{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"20"}}}},
	}
	before := player.HP
	if _, err := engine.executeBlessHolds("USER_ATTACK_END", 22); err != nil {
		t.Fatal(err)
	}
	if len(player.BlessHolds) != 1 || player.HP != before {
		t.Fatal("append condition was evaluated after earlier append mutation")
	}
	if _, err := engine.executeBlessHolds("USER_ATTACK_END", 22); err != nil || len(player.BlessHolds) != 0 || player.HP != before+20 {
		t.Fatal("next pass should execute the now-enabled hold", err)
	}
}

func TestEnemyCallKeepsCasterAndAppliesGroupDebuffToPlayers(t *testing.T) {
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Target: "USER_ALL", Attribute: "FIRE"}
	role := CombatSkillRole{Function: "ATK_BREAK_FIXED", Target: "SELECT", Parameters: [10]string{"2", "ATK", "1000", "100"}}
	engine := &BattleEngine{catalog: &CombatCatalog{Skills: map[int][]CombatSkillDefinition{1: {skill}}, SkillRoles: map[int][]CombatSkillRole{1: {role}}}, enemyCount: 1}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, HP: 100, MaxHP: 100, Attack: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100, Attack: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	engine.players[0].BlessHolds = []battleBlessHold{{SourceMember: 5, EnemySkill: true, CardType: 21, Skill: skill, Roles: []CombatSkillRole{role}, CardLevel: 1, Remaining: 2}}
	rows, err := engine.executeBlessHolds("USER_ATTACK_END", 21)
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Command != CommandHoldSkill || rows[0].Args[0] != 5 || rows[0].Args[4] != 1 {
		t.Fatalf("caster and holder mixed: %+v", rows)
	}
	for _, player := range engine.players {
		if player.Attack != 900 || len(player.Effects) != 1 || player.Effects[0].Source != 5 {
			t.Fatalf("curse hit wrong side/source: %+v", player)
		}
	}
	if engine.enemies[0].Attack != 1000 || len(engine.enemies[0].Effects) != 0 {
		t.Fatal("curse debuffed its source boss")
	}
}

func TestNativeBlessProducerAndRecipientPreviewsUseDifferentChainButExcludeRandom(t *testing.T) {
	main := CombatSkillDefinition{ID: 1, FunctionID: 1, DisplayRole: 1, Target: "USER_ALL", Attribute: "FIRE"}
	call := CombatSkillDefinition{ID: 2, FunctionID: 2, DisplayRole: 1, Target: "USER_ALL", Attribute: "FIRE"}
	random, chained := call, call
	random.FunctionID, random.BranchPriority = 3, 1
	random.BranchCondition, random.BranchParameters[0] = "RANDOM", "100"
	chained.FunctionID, chained.BranchPriority = 4, 2
	chained.BranchCondition, chained.BranchParameters = "DECK_COMBO_COUNT", [5]string{"4", "4"}
	registration := CombatSkillRole{Function: "BLESS", Target: "USER_ALL", Parameters: [10]string{"3"}}
	engine := &BattleEngine{rng: newXorShift128(1), catalog: &CombatCatalog{
		Cards:   map[int]CombatCardDefinition{101: {ID: 101, NormalSkillID: 1, CallSkillID: 2}},
		Spheres: map[int]CombatSphereDefinition{101: {ID: 101, SkillID: 1, CallSkillID: 2}},
		Skills:  map[int][]CombatSkillDefinition{1: {main}, 2: {call, random, chained}},
		SkillRoles: map[int][]CombatSkillRole{
			1: {registration},
			2: {{Function: "HEAL_FIXED", Parameters: [10]string{"100"}}},
			3: {{Function: "HEAL_FIXED", Parameters: [10]string{"200"}}},
			4: {{Function: "HEAL_FIXED", Parameters: [10]string{"300"}}},
		},
	}}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 1000}
		if i > 0 { // The caster/holder contributes even without a matching selected card.
			engine.turnActions = append(engine.turnActions, battleAction{memberType: i + 1, cardType: 1, skill: main})
		}
	}
	before := engine.rng
	cardDisplay, err := engine.cardDisplayState(&engine.players[0], BattleCard{CardID: 101, CardType: 1, Level: 1})
	if err != nil || cardDisplay.Power != 300 || engine.rng != before {
		t.Fatalf("BLESS card preview must use CALL display/current Chain with RNG rollback: %+v %v", cardDisplay, err)
	}
	engine.players[0].Spheres[0] = battleSphere{SphereID: 101, Slot: 1, Level: 1}
	engine.players[0].ExecutionSource = battleExecutionSource{SphereSlot: 1}
	sphereDisplay, err := engine.sphereDisplayState(&engine.players[0], &engine.players[0].Spheres[0])
	if err != nil || sphereDisplay.Power != 300 || engine.rng != before {
		t.Fatalf("sphere BLESS producer must independently compute CALL Chain: %+v %v", sphereDisplay, err)
	}
	action := battleAction{memberType: 1, cardLevel: 1, skill: main, callSkill: call, callRoles: engine.catalog.SkillRoles[2]}
	rows, err := engine.executePlayerBless(action, registration, 3)
	wantRNG := before
	// Both a275b and80510 call7982a in branch mode0: RANDOM is excluded,
	// independently of their different RNG snapshot/Chain arguments.
	if err != nil || len(rows) != 4 || engine.rng != wantRNG {
		t.Fatalf("BLESS producer RNG boundary/target count: %+v %v", rows, err)
	}
	for i, player := range engine.players {
		hold := player.BlessHolds[0]
		if hold.Power != 100 || hold.Marker != 0 || !hold.PowerKnown || rows[i].Args[8] != 100 {
			t.Fatalf("recipient must initialize at Chain zero, not producer Chain: %+v", hold)
		}
	}
	// Native 3fa8a visits only members registered in 3b32c, including PASS.
	engine.selectedPlays = map[int]cardPlaySubmission{1: {}, 2: {}, 3: {}, 4: {}}
	updates, err := engine.refreshBattleDisplayPowers()
	if err != nil || len(updates) != 1 || updates[0].Command != CommandCardUpdate2 || updates[0].Args[0] != 1 || engine.rng != wantRNG {
		t.Fatalf("only holder 1 reaches four Chain at refresh: %+v %v", updates, err)
	}
	if hold := engine.players[0].BlessHolds[0]; hold.Power != 300 || hold.Marker != 2 {
		t.Fatalf("refresh lost CALL branch display: %+v", hold)
	}
}

func TestNativeEnemyAppendInitialBranchMarkerAndCurseDoesNotCommitPreviewRNG(t *testing.T) {
	base := CombatSkillDefinition{ID: 1, FunctionID: 1, DisplayRole: 1, Target: "USER_ONE"}
	branch := base
	branch.FunctionID, branch.BranchPriority = 2, 1
	branch.BranchCondition, branch.BranchParameters[0] = "TURN", "1"
	random := base
	random.FunctionID, random.BranchPriority = 3, 2
	random.BranchCondition, random.BranchParameters[0] = "RANDOM", "100"
	engine := &BattleEngine{turn: 1, enemyCount: 1, rng: newXorShift128(1), catalog: &CombatCatalog{
		Skills: map[int][]CombatSkillDefinition{1: {base, branch, random}},
		SkillRoles: map[int][]CombatSkillRole{
			1: {{Function: "HEAL_FIXED", Parameters: [10]string{"100"}}},
			2: {{Function: "HEAL_FIXED", Parameters: [10]string{"200"}}},
			3: {{Function: "HEAL_FIXED", Parameters: [10]string{"300"}}},
		},
	}}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 100, MaxHP: 1000}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 1000}
	engine.enemies[0].Level.CallSkillIDs[0] = 1
	role := CombatSkillRole{Target: "USER_ONE", Parameters: [10]string{"3", "0"}}
	before := engine.rng
	for _, cardType := range []int{21, 22} {
		engine.players[0].BlessHolds = nil
		rows, err := engine.executeEnemyCallSkillAppendCard(&engine.enemies[0], 1, role, cardType)
		if err != nil || len(rows) != 1 || engine.rng != before {
			t.Fatalf("enemy append %d producer/preview RNG: %+v %v", cardType, rows, err)
		}
		hold := engine.players[0].BlessHolds[0]
		if hold.Power != 200 || hold.Marker != 1 || rows[0].Args[8] != 200 {
			t.Fatalf("enemy CALL initial display branch was lost: %+v", hold)
		}
		state, err := engine.holdDisplayState(&engine.players[0], hold)
		if err != nil || state.Power != 200 || state.Marker != 1 || engine.rng != before {
			t.Fatalf("enemy CALL refresh must retain marker and restore RNG: %+v %v", state, err)
		}
	}
}
