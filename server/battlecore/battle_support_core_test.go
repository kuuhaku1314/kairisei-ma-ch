package battlecore

import "testing"

func TestBeginningDrawPassiveStartupAndResume(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	c := engine.catalog
	card := c.Cards[1]
	card.PassiveSkillID = 99
	c.Cards[1] = card
	c.Skills[99] = []CombatSkillDefinition{{ID: 99, FunctionID: 99, Type: "PASSIVE", Target: "SELF"}}
	c.SkillRoles[99] = []CombatSkillRole{{Function: "BEGINNING_DRAW", Target: "SELECT"}}
	rows, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, row := range rows {
		if row.Command == CommandCardSkill && row.Args[2] == 99 {
			count++
		}
	}
	if count != 40 {
		t.Fatalf("main-card startup passive headers=%d, want 40", count)
	}
	engine.turn = 2
	if _, err = engine.tickPersistentEffects(); err != nil {
		t.Fatal(err)
	}
	for _, player := range engine.players {
		if len(player.Effects) != 10 {
			t.Fatal("zero-duration native PASSIVE disappeared at turn end")
		}
		for i, effect := range player.Effects {
			if effect.CardType != i+1 || effect.Remaining != 0 || effect.Function != "BEGINNING_DRAW" {
				t.Fatalf("bad card-bound passive: %+v", effect)
			}
		}
	}
	resume, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	count = 0
	for _, row := range resume {
		if row.Command == CommandResumeBuff && row.Args[2] == 412 {
			count++
		}
	}
	if count != 0 || len(engine.players[0].Effects) != 10 {
		t.Fatalf("native resume must retain passive state without projecting RESUME_BUFF, got %d rows", count)
	}
}

func TestNativeResumeBuffListBoundary(t *testing.T) {
	for list := 0; list < 8; list++ {
		_, ok := resumeBuffResult(1, battleEffect{Function: "ATK_UP_FIXED", ListType: list, Remaining: 3})
		want := list == 0 || list == 3 || list == 5 || list == 6
		if ok != want {
			t.Fatalf("native RESUME_BUFF list %d inclusion=%v want %v", list, ok, want)
		}
	}
}

func TestSupportHealCriticalAndConditions(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	role := CombatSkillRole{Function: "HEAL_BOOST", Parameters: [10]string{"99", "100", "", "1000", "", "LIGHT_DARK", "2", "4"}}
	effect, err := sphereSupportEffect(role, 60, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.players[0].Effects = []battleEffect{effect}
	engine.players[1].HP = 1
	skill := CombatSkillDefinition{Attribute: "LIGHT", Cost: 2, Target: "USER_ONE"}
	heal := CombatSkillRole{Function: "HEAL_FIXED", Target: "USER_ONE", Parameters: [10]string{"100", "0", "0", "0", "MND"}}
	_, err = engine.executeFixedHeal(battleAction{memberType: 1, target: 2, skill: skill, cardLevel: 60}, heal, 1)
	if err != nil || engine.players[1].HP != 401 {
		t.Fatalf("caster's heal boost not applied: HP=%d err=%v", engine.players[1].HP, err)
	}
	for _, sample := range []struct {
		attr       string
		cost, want int
	}{{"DARK", 4, 400}, {"FIRE", 2, 100}, {"LIGHT", 1, 100}, {"LIGHT", 5, 100}} {
		if got := supportHealValue(engine.players[0].Effects, CombatSkillDefinition{Attribute: sample.attr, Cost: sample.cost}, 100); got != sample.want {
			t.Fatalf("heal filter %+v got %d", sample, got)
		}
	}
	role = CombatSkillRole{Function: "CRITICAL_BOOST", Parameters: [10]string{"99", "1000", "0", "LIGHT_DARK", "MAGIC", "2", "4"}}
	effect, err = sphereSupportEffect(role, 60, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	for _, sample := range []struct {
		attr, physics string
		cost, want    int
	}{{"LIGHT", "MAGIC", 2, 1000}, {"DARK", "MAGIC", 4, 1000}, {"LIGHT", "PHYSICS", 2, 0}, {"FIRE", "MAGIC", 2, 0}, {"LIGHT", "MAGIC", 5, 0}} {
		if got := nativeCriticalRate([]battleEffect{effect}, 0, CombatSkillDefinition{Attribute: sample.attr, DamageKind: sample.physics, Cost: sample.cost}); got != sample.want {
			t.Fatalf("critical filter %+v got %d", sample, got)
		}
	}
	effect.CostMin = 0
	if got := nativeCriticalRate([]battleEffect{effect}, 0); got != 1000 {
		t.Fatalf("native AI empty-skill critical metric lost EX boost: %d", got)
	}
	parameterBoost := battleEffect{Function: "ATK_UP_BOOST", ListType: 1, Parameter: "ATK", Attribute: "NULL", Rate: 3000, Remaining: 99}
	if got := sphereSupportBoostedValue([]battleEffect{parameterBoost, parameterBoost}, "ATK_UP_BOOST", "FIRE", "ATK", 100); got != 700 {
		t.Fatalf("parameter boosts incorrectly share the damage boost cap: %d", got)
	}
}
