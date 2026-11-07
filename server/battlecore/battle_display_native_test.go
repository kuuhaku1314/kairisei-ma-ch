package battlecore

import "kairisei.local/server/battlecontent"

import "testing"

func TestNativeDisplayUsesConfiguredRoleAndRestoresPreviewRandomState(t *testing.T) {
	row := make([]string, 50)
	row[0], row[5], row[49] = "1", "2", "1"
	base, err := battlecontent.ParseSkill(row)
	if err != nil {
		t.Fatal(err)
	}
	attack := CombatSkillRole{Function: "ATTACK_AA", Parameters: [10]string{"500"}}
	regen := CombatSkillRole{Function: "REGENERATE_FIXED", Parameters: [10]string{"2", "100", "300", "1000", "5", "MND"}}
	heal := CombatSkillRole{Function: "HEAL_FIXED", Parameters: [10]string{"500", "1000", "1000", "2", "MND"}}
	roles := []CombatSkillRole{attack, regen, heal}
	engine := &BattleEngine{rng: newXorShift128(1)}
	player := &engine.players[0]
	*player = battlePlayer{MemberType: 1, ArthurType: 1, HP: 100, MaxHP: 1000, Recovery: 1000, LimitAttack: 99999, LimitMagic: 99999, LimitRecovery: 99999}
	if got := engine.cardDisplayPower(player, 20, base, roles); got != 1206 {
		t.Fatalf("column5 must select regeneration's own columns, not first attack or duration: %d", got)
	}
	if got := engine.cardDisplayPower(player, 20, CombatSkillDefinition{}, roles); got != 0 {
		t.Fatal("zero display role must stay zero", got)
	}
	branch := base
	branch.DisplayRole, branch.FunctionID, branch.BranchPriority = 3, 2, 1
	branch.BranchCondition, branch.BranchParameters[0] = "TURN", "1"
	engine.catalog = &CombatCatalog{
		Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
		Spheres:    map[int]CombatSphereDefinition{1: {ID: 1, SkillID: 1}},
		Skills:     map[int][]CombatSkillDefinition{1: {base, branch}},
		SkillRoles: map[int][]CombatSkillRole{1: roles, 2: roles},
	}
	engine.turn = 1
	before := engine.rng
	card := BattleCard{CardID: 1, CardType: 1, Level: 20}
	sphere := battleSphere{SphereID: 1, Level: 20}
	hold := battleBlessHold{SourceMember: 1, CardType: 22, CardLevel: 20, Skill: base, Roles: roles}
	for _, preview := range []func() (battleDisplayPower, error){
		func() (battleDisplayPower, error) { return engine.cardDisplayState(player, card) },
		func() (battleDisplayPower, error) { return engine.sphereDisplayState(player, &sphere) },
		func() (battleDisplayPower, error) { return engine.holdDisplayState(player, hold) },
	} {
		for i := 0; i < 2; i++ {
			state, err := preview()
			if err != nil || state.Power != 1560 || state.Marker != 1 || engine.rng != before {
				t.Fatalf("preview must resolve branch role3 without consuming combat RNG: %+v %v", state, err)
			}
		}
	}
	delete(engine.catalog.SkillRoles, 2)
	if _, err := engine.cardDisplayState(player, card); err == nil || engine.rng != before {
		t.Fatal("failed preview must also restore RNG", err)
	}
	engine.catalog.Skills[1][1].BranchCondition = "RANDOM"
	engine.catalog.Skills[1][1].BranchParameters[0] = "100"
	for _, preview := range []func() (battleDisplayPower, error){
		func() (battleDisplayPower, error) { return engine.cardDisplayState(player, card) },
		func() (battleDisplayPower, error) { return engine.sphereDisplayState(player, &sphere) },
		func() (battleDisplayPower, error) { return engine.holdDisplayState(player, hold) },
	} {
		state, err := preview()
		if err != nil || state.Power != 1206 || state.Marker != 0 || engine.rng != before {
			t.Fatalf("native mode0 must skip even 100%% RANDOM before role lookup: %+v %v", state, err)
		}
	}
	want := before
	want.next()
	engine.selectCombatSkillBranch(battleAction{memberType: 1, skill: base}, nil, nil)
	if engine.rng != want {
		t.Fatal("real skill branch selection must still advance RNG")
	}
}

func TestNativeEnemySourceIsNotClippedByPlayerParameterLimit(t *testing.T) {
	engine := &BattleEngine{enemyCount: 1, rng: newXorShift128(1)}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 1000, MaxHP: 1000, Attack: 150000, LimitAttack: 99999}
	engine.players[0] = battlePlayer{MemberType: 1, HP: 1000000, MaxHP: 1000000, Attack: 150000, LimitAttack: 99999}
	role := CombatSkillRole{Function: "ATTACK_AA", Target: "USER_ONE", Parameters: [10]string{"0", "0", "1000", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
	rows, err := engine.executeEnemyAttack(&engine.enemies[0], 1, role, []CombatSkillRole{role})
	if err != nil || len(rows) < 1 || rows[0].Command != 60 || rows[0].Args[2] != -150000 || engine.players[0].HP != 850000 {
		t.Fatalf("73ff0 player flag must not clamp Boss ATK: %+v %v", rows, err)
	}
	view := enemyDisplaySource(&engine.enemies[0])
	skill := CombatSkillDefinition{DisplayRole: 1}
	if got := engine.cardDisplayPower(&view, 1, skill, []CombatSkillRole{role}); got != 150000 {
		t.Fatal("enemy CALL preview lost source identity", got)
	}
	if got := engine.cardDisplayPower(&engine.players[0], 1, skill, []CombatSkillRole{role}); got != 99999 {
		t.Fatal("preview must retain the player cap", got)
	}
	engine.players[0].LimitAttack, engine.players[0].Attack = 2000000, 1500000
	if got := combatStatValue(&engine.players[0], "ATK"); got != 999999 {
		t.Fatal("native player limit ceiling is 999999", got)
	}
}
