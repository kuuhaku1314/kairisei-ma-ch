package battlecore

import (
	"encoding/json"
	"os"
	"testing"
)

// D721 original x86 phase-API vectors: nonzero growth was silently ignored
// because the old BOSS masters happened to use zero in these fields.
func TestEnemyAuthoredParameterGrowth(t *testing.T) {
	for _, tc := range []struct {
		function   string
		parameters [10]string
		want       int
	}{
		{"ATK_UP_BY_TARGET_PARAM", [10]string{"3", "ATK", "MAX_HP", "13", "17", "23", "700000"}, 21023},
		{"ATK_BREAK_BY_TARGET_PARAM", [10]string{"3", "ATK", "MAX_HP", "13", "17", "23", "700000"}, -21023},
		{"GUARD_BREAK_BY_TARGET_PARAM", [10]string{"3", "DEF", "MAX_HP", "13", "17", "23", "700000"}, -21023},
		{"ATK_UP_BY_NOW_TURN_DAMAGE", [10]string{"3", "ATK", "8", "17"}, 2986},
		{"ATK_BREAK_BY_NOW_TURN_DAMAGE", [10]string{"3", "ATK", "8", "17"}, -2986},
		{"GUARD_BREAK_BY_NOW_TURN_DAMAGE", [10]string{"3", "DEF", "8", "17"}, -2986},
		{"ATK_UP_FIXED", [10]string{"3", "ATK", "1000", "200", "17", "23"}, 240},
		{"ATK_BREAK_FIXED", [10]string{"3", "ATK", "1000", "200", "17", "23"}, -240},
		{"DEF_UP_FIXED", [10]string{"3", "DEF", "1000", "200", "17", "23"}, 240},
		{"GUARD_BREAK_FIXED", [10]string{"3", "DEF", "1000", "200", "17", "23"}, -240},
		{"ATK_BREAK_BY_SELF_PARAM", [10]string{"3", "ATK", "MAX_HP", "13", "17", "23", "700000"}, -21023},
	} {
		t.Run(tc.function, func(t *testing.T) {
			engine, _ := coreBattleFixture(t)
			engine.players[0].HP, engine.players[0].MaxHP = 1000000, 1000000
			actor := &engine.enemies[0]
			actor.MaxHP = 10000000
			actor.TurnDamage = 11945
			role := CombatSkillRole{Function: tc.function, Target: "SELECT", Parameters: tc.parameters}
			if _, err := engine.executeEnemyRole(actor, 1, role, []CombatSkillRole{role}); err != nil {
				t.Fatal(err)
			}
			effects := engine.players[0].Effects
			if len(effects) != 1 || effects[0].Delta != tc.want {
				t.Fatalf("authored growth differs from native result: %+v; want delta %d", effects, tc.want)
			}
		})
	}
	t.Run("ENCHANT", func(t *testing.T) {
		engine, _ := coreBattleFixture(t)
		role := CombatSkillRole{Function: "ENCHANT", Target: "SELF", Parameters: [10]string{"3", "1000", "100", "17", "23", "FIRE"}}
		actor := &engine.enemies[0]
		if _, err := engine.executeEnemyRole(actor, 5, role, []CombatSkillRole{role}); err != nil {
			t.Fatal(err)
		}
		if len(actor.Effects) != 1 || actor.Effects[0].Value != 140 {
			t.Fatalf("BOSS enchant discarded native coefficient/fixed growth: %+v", actor.Effects)
		}
	})
}

func TestRegisteredTribalModifiersFinishRealUserAttack(t *testing.T) {
	for _, family := range []string{"DAMAGE", "ATK_UP", "DEF_UP"} {
		t.Run(family, func(t *testing.T) {
			engine, _ := coreBattleFixture(t)
			engine.phase, engine.turn = PhaseUser, 1
			engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
			engine.catalog.Enemies[1] = CombatEnemyDefinition{ID: 1, TribeIDs: []int{2}}
			engine.enemies[0].TribeIDs = []int{2}
			skill := engine.catalog.Skills[1][0]
			skill.DamageKind = "PHYSICS"
			role := engine.catalog.SkillRoles[1][0]
			role.Parameters[0] = "100"
			parameter := "ATK"
			if family == "DEF_UP" {
				parameter = "DEF"
			}
			if family != "DAMAGE" {
				skill.Target, role.Target = "SELF", "SELF"
				role.Function = family + "_FIXED"
				role.Parameters = [10]string{"2", parameter, "1000", "100"}
			}
			engine.catalog.Skills[1][0] = skill
			engine.catalog.SkillRoles[1] = []CombatSkillRole{role}
			tribal := CombatSkillRole{Function: family + "_BOOST_ORDER_TRIBAL", Parameters: [10]string{"1", "", "", "1000", "", "NULL", parameter, "0", "0", "2"}}
			if family == "DAMAGE" {
				tribal.Parameters[1], tribal.Parameters[3], tribal.Parameters[6] = "1000", "", "PHYSICS"
			}
			engine.catalog.Skills[2] = []CombatSkillDefinition{{ID: 2, FunctionID: 2}}
			engine.catalog.SkillRoles[2] = []CombatSkillRole{tribal}
			player := &engine.players[0]
			player.CardBurstSkills[1] = []battleCardBurstSkill{{SkillID: 2}}
			player.Hand[0] = 1
			player.Effects = []battleEffect{{Function: family + "_BOOST", ListType: 1, Remaining: 99, Value: 10, Rate: 500, Parameter: parameter}}
			engine.selectedPlays = map[int]cardPlaySubmission{1: {CardTypes: [5]int{1}, Targets: [5]int{5}}, 2: {}, 3: {}, 4: {}}
			if family != "DAMAGE" {
				engine.selectedPlays[1] = cardPlaySubmission{CardTypes: [5]int{1}, Targets: [5]int{1}}
			}
			if _, err := engine.UserAttack(); err != nil {
				t.Fatalf("registered modifier aborted the real action loop: %v", err)
			}
			// (100+10)*(1000+500+1000)/1000=275, not 315/330.
			if family == "DAMAGE" {
				if engine.enemies[0].HP != 9725 {
					t.Fatalf("damage=%d, want275", 10000-engine.enemies[0].HP)
				}
			} else if player.Effects[len(player.Effects)-1].Delta != 275 {
				t.Fatalf("wrong combined EX/tribal parameter: %+v", player.Effects)
			}
			if _, exists := player.cardInHand(1); exists {
				t.Fatal("successful action failed to rotate the played card")
			}
		})
	}
}

func TestBurstDeckBonusIsOneShotFirstSegmentAndPerCardTruncated(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	bonus := CombatSkillRole{Function: "PARAM_UP_SKILL_BONUS", Parameters: [10]string{"ATK", "5", "0", "0", "FIRE"}}
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF", ChainRate: 20,
		Parameters: [10]string{"2", "ATK", "1000", "103", "0", "7"}}
	action := battleAction{memberType: 1, cardLevel: 1, skillBonus: &battleSkillBonus{}}
	if _, err := engine.executeBurstRole(action, bonus, nil); err != nil {
		t.Fatal(err)
	}
	wrong := role
	wrong.Parameters[1] = "INT"
	if got := engine.fixedParameterActionValue(action, wrong, 4); got != 170 || !action.skillBonus.active {
		t.Fatal("wrong parameter consumed a pending deck bonus")
	}
	// Ten FIRE cards: 103 + (103*5/100)*10 + 7 + Chain60 = 220.
	// Multiplying the total, or rounding after summing all ten, differs.
	if got := engine.fixedParameterActionValue(action, role, 4); got != 220 || action.skillBonus.active {
		t.Fatalf("first matching segment=%d, want220 and consumed", got)
	}
	if got := engine.fixedParameterActionValue(action, role, 4); got != 170 {
		t.Fatalf("bonus applied more than once: %d", got)
	}
	// Flat fallback and replacement, rather than adding every modifier in
	// the full role array before those modifiers have actually executed.
	bonus.Parameters[1], bonus.Parameters[2], bonus.Parameters[3] = "0", "2", "3"
	if _, err := engine.executeBurstRole(action, bonus, nil); err != nil {
		t.Fatal(err)
	}
	if got := engine.fixedParameterActionValue(action, role, 1); got != 160 {
		t.Fatalf("flat per-card bonus=%d, want160", got)
	}
}

func TestNativeParameterBoostSelectorReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_PARAMETER_BOOST_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_PARAMETER_BOOST_RECEIPT to original selector receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Name                              string
			Attribute, Parameter, Cost, Owner int
			Remaining, Rate, Fixed            int
			ListType                          int `json:"list_type"`
			CardType                          int `json:"card_type"`
			CostMin                           int `json:"cost_min"`
			CostMax                           int `json:"cost_max"`
		}
		DeckVectors []struct {
			Value, Percent, Fixed int32
			Count                 int
			Result                int32
			SecondCall            int32 `json:"second_call"`
		} `json:"deck_vectors"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-parameter-boost-selector" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid original parameter-boost receipt")
	}
	for _, v := range receipt.Vectors {
		attribute := map[int]string{0: "NULL", 4: "LIGHT", 5: "DARK"}[v.Attribute]
		parameter := map[int]string{2: "ATK", 3: "INT"}[v.Parameter]
		effect := battleEffect{Function: "ATK_UP_BOOST", ListType: v.ListType, Attribute: attribute,
			Parameter: parameter, CardType: v.Owner, Remaining: v.Remaining,
			CostMin: v.CostMin, CostMax: v.CostMax, Rate: 250, Value: 17}
		fixed, rate := sphereSupportBoostTerms([]battleEffect{effect}, "ATK_UP_BOOST", "LIGHT", "ATK", v.Cost, v.CardType)
		if fixed != v.Fixed || rate != v.Rate {
			t.Fatalf("%s Go=(%d,%d) native=(%d,%d)", v.Name, fixed, rate, v.Fixed, v.Rate)
		}
	}
	for _, v := range receipt.DeckVectors {
		if got := deckBonusSegment(v.Value, v.Percent, v.Fixed, v.Count); got != v.Result || v.SecondCall != v.Value {
			t.Fatalf("deck bonus Go=%d original=%d, second call=%d", got, v.Result, v.SecondCall)
		}
	}
}
