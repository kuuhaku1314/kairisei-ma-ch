package battlecore

import (
	"reflect"
	"testing"
)

func TestSkillStatusWaitsForRoleSetAndKeepsSourceSkill(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 100000, 100000
	skill := CombatSkillDefinition{ID: 111, FunctionID: 999, Target: "SELF", Attribute: "FIRE", Kind: "SUPPORT"}
	call := CombatSkillDefinition{ID: 222, FunctionID: 222, Target: "SELF", Attribute: "FIRE", DisplayRole: 1,
		AppendTrigger: "USER_ATTACK_END", AppendCondition: "TURN", AppendParameters: [5]string{"1", "0"}}
	roles := []CombatSkillRole{
		{RoleIndex: 0, Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "100", "1000"}},
		{RoleIndex: 1, Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "100", "1000"}},
		{RoleIndex: 2, Function: "BLESS", Target: "SELF", Parameters: [10]string{"1"}},
	}
	heal := []CombatSkillRole{{Function: "HEAL_FIXED", Target: "SELF", Parameters: [10]string{"10"}}}
	engine.catalog.Skills[111], engine.catalog.SkillRoles[999] = []CombatSkillDefinition{skill}, roles
	engine.catalog.Skills[222], engine.catalog.SkillRoles[222] = []CombatSkillDefinition{call}, heal
	engine.selectedPlays = map[int]cardPlaySubmission{1: {}, 2: {}, 3: {}, 4: {}}
	rows, err := engine.executeUserActions([]battleAction{{memberType: 1, cardID: 1, cardType: 1, cardLevel: 1,
		skill: skill, roles: roles, callSkill: call, callRoles: heal}})
	if err != nil {
		t.Fatal(err)
	}
	holdSeen, buffs := false, 0
	for i, row := range rows {
		if row.Command == CommandHoldSet {
			holdSeen = true
		}
		if row.Command == CommandBuff {
			buffs++
			if !holdSeen || i+1 >= len(rows) || rows[i+1].Command != CommandBattleParam || rows[i+1].Args[3] != 300 {
				t.Fatalf("status preceded HOLD_SET or retained intermediate parameter: %+v", rows)
			}
		}
	}
	if buffs != 2 || len(engine.players[0].Effects) != 2 {
		t.Fatalf("missing executed buff roles: %+v", rows)
	}
	for _, effect := range engine.players[0].Effects {
		if effect.Source != 1 || effect.SourceSkillID != 111 {
			t.Fatalf("caster, source skill and role-function identity were confused: %+v", effect)
		}
	}
	lost := 0
	for _, row := range appendNaturalExpiryRows(nil, 1, engine.players[0].Effects, nil, "NEUTRAL") {
		if row.Command == CommandBuffLostOne {
			lost++
			if !reflect.DeepEqual(row.Args, []int64{1, 0, 111}) {
				t.Fatalf("71 did not use outer source skill: %+v", row)
			}
		}
	}
	if lost != 2 {
		t.Fatal("individual source skill releases were merged")
	}
}
