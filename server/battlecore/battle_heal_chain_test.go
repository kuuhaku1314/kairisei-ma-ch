package battlecore

import "testing"

func TestNativeHealAndRegenerationChainAfterEX(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	player := &engine.players[0]
	player.Recovery = 1000
	player.Effects = []battleEffect{{Function: "HEAL_BOOST", ListType: 1, Value: 50, Rate: 100, Remaining: 1}}
	role := CombatSkillRole{Function: "HEAL_FIXED", Target: "SELF", ChainRate: 20,
		Parameters: [10]string{"100", "0", "1000", "0", "MND"}}
	action := battleAction{memberType: 1, cardLevel: 1, target: 1}
	rows, err := engine.executeFixedHeal(action, role, 4)
	if err != nil {
		t.Fatal(err)
	}
	// 822a0: ((100 + 1000*1000/1000 + 50)*1100/1000)*160/100.
	if len(rows) != 1 || rows[0].Args[2] != 2024 || engine.turnStats.Heal != 2024 {
		t.Fatalf("full heal/EX/Chain order: %+v", rows)
	}
	if got := engine.cardDisplayPower(player, 1, CombatSkillDefinition{DisplayRole: 1}, []CombatSkillRole{role}); got != 1100 {
		t.Fatalf("preview must stay at producer value, not execution's EX/Chain value: %d", got)
	}
	regen := CombatSkillRole{Function: "REGENERATE_FIXED", Target: "SELF", ChainRate: 20,
		Parameters: [10]string{"2", "100", "0", "1000", "0", "MND"}}
	if _, err := engine.executePersistentEffect(action, regen, 1, 4); err != nil {
		t.Fatal(err)
	}
	if got := player.Effects[len(player.Effects)-1].Value; got != 2024 {
		t.Fatalf("regen must freeze EX then full Chain at registration: %d", got)
	}
	player.Recovery = 9000
	rows = engine.regenerateMembers()
	if len(rows) != 2 || rows[1].Args[2] != 2024 || engine.turnStats.Heal != 2024 {
		t.Fatal("regen recomputed the frozen heal or entered direct-heal AI totals")
	}
}

func TestNativeSelfParameterHealChainAddsToCoefficient(t *testing.T) {
	role := CombatSkillRole{Function: "HEAL_BY_SELF_PARAM", Parameters: [10]string{"MND", "1000", "0", "20", "1000"}, ChainRate: 20}
	// a207d converts Chain's percentage points into per-mille coefficient:
	// min(5000,1000)*(1000+3*20*10)/1000 + 20 = 1620.
	if got := selfScaledHealRoleValue(role, 1, 4, 5000); got != 1620 {
		t.Fatalf("self-parameter heal=%d, want 1620", got)
	}
}
