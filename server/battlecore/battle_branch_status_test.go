package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
)

func TestNativeStatusBranchesAfterRealApplication(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	actor := &engine.enemies[0]
	regen := CombatSkillRole{Function: "REGENERATE_FIXED", Target: "SELECT", Parameters: [10]string{"3", "100", "0", "0", "0", "MND"}}
	if _, err := engine.executeEnemyPersistentEffect(actor, 1, regen); err != nil {
		t.Fatal(err)
	}
	debuff := CombatSkillRole{Function: "ATK_BREAK_FIXED", Target: "ENEMY_ONE", Parameters: [10]string{"3", "INT", "1000", "100"}}
	if _, err := engine.executePlayerRole(battleAction{memberType: 1, target: 5, cardLevel: 1}, debuff, 1); err != nil {
		t.Fatal(err)
	}
	if len(actor.Effects) != 1 || actor.Effects[0].Delta >= 0 {
		t.Fatalf("test did not apply a real magic debuff: %+v", actor.Effects)
	}
	base := CombatSkillDefinition{ID: 777, FunctionID: 777, Target: "USER_ONE"}
	boost := base
	boost.FunctionID, boost.BranchPriority = 778, 1
	boost.BranchCondition, boost.BranchParameters = "USER_SIDE_BUFF", [5]string{"ENCHANT", "HEAL"}
	boost.BranchCondition2, boost.BranchParameters2 = "SELF_DEBUFF", [5]string{"ATK_BREAK_BY_INT", "ATK_BREAK_BY_INT"}
	engine.catalog.Skills = map[int][]CombatSkillDefinition{777: {base, boost}}
	selected, _, marker, ok := engine.selectEnemySkillBranchWithIndex(actor, 777, 1)
	if !ok || selected.FunctionID != 778 || marker != 1 {
		t.Fatalf("real regen and INT debuff must choose enhanced branch: %+v marker=%d", selected, marker)
	}
	engine.players[0].HP = 0
	selected, _, _, _ = engine.selectEnemySkillBranchWithIndex(actor, 777, 1)
	if selected.FunctionID != 777 {
		t.Fatal("KO member must not satisfy a side-wide condition")
	}
	if !engine.enemyBranchConditionSatisfied(actor, 1, "USER_ONE", "TARGET_BUFF", [5]string{"HEAL", "HEAL"}) {
		t.Fatal("a concrete KO target still has the retained status")
	}
	engine.players[0].HP = 1000
	playerBranch := boost
	playerBranch.BranchCondition, playerBranch.BranchParameters = "SELF_BUFF", [5]string{"ENCHANT", "HEAL"}
	playerBranch.BranchCondition2, playerBranch.BranchParameters2 = "TARGET_DEBUFF", [5]string{"POISON", "ATK_BREAK_BY_INT"}
	engine.catalog.Skills = map[int][]CombatSkillDefinition{777: {base, playerBranch}}
	selected, marker = engine.selectCombatSkillBranchWithIndex(battleAction{memberType: 1, target: 5, skill: base}, nil, nil)
	if selected.FunctionID != 778 || marker != 1 {
		t.Fatal("player branch did not recognize typed applied states")
	}
}

func TestNativeStatusBranchListAndNullTargetBoundaries(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	if combatTriggerBuffKind("ATTR_RATE_DOWN_INVALID") != 13 {
		t.Fatal("canonical native status name must resolve separately from its producer")
	}
	for list := 0; list < 8; list++ {
		effects := []battleEffect{{Function: "REGENERATE_FIXED", ListType: list}, {Function: "POISON", ListType: list}}
		if got := branchHasStatus(effects, true, [5]string{"ENCHANT", "HEAL"}); got != (list == 0 || list == 5 || list == 6) {
			t.Fatalf("buff list %d: %t", list, got)
		}
		if got := branchHasStatus(effects, false, [5]string{"WEAKNESS", "POISON"}); got != (list == 0) {
			t.Fatalf("debuff list %d: %t", list, got)
		}
	}
	engine.players[0].Effects = []battleEffect{{Function: "REGENERATE_FIXED"}, {Function: "POISON"}}
	engine.players[0].Attribute = "FIRE_DARK"
	for _, tc := range []struct {
		condition string
		params    [5]string
	}{
		{"TARGET_BUFF", [5]string{"HEAL"}}, {"TARGET_DEBUFF", [5]string{"POISON"}},
		{"TARGET_HP_PER", [5]string{"0", "100"}}, {"TARGET_ATTR", [5]string{"FIRE"}},
		{"TARGET_DEBUFF_KIND_NUM", [5]string{"0", "100"}},
	} {
		if engine.enemyBranchConditionSatisfied(&engine.enemies[0], 0, "USER_ALL", tc.condition, tc.params) {
			t.Fatalf("null target incorrectly expanded to party: %s", tc.condition)
		}
	}
	if branchHasStatus(engine.players[0].Effects, true, [5]string{"ENCHANT", "ENCHANT", "HEAL"}) {
		t.Fatal("third parameter is not a status alternative")
	}
	if !engine.branchTargetAttributeMatches(1, [5]string{"ICE", "LIGHT_DARK"}) {
		t.Fatal("TARGET_ATTR must match either component of either filter")
	}
}

// Optional independent original-binary vectors. Ordinary tests above remain
// self-contained; this gate runs only with an explicitly generated receipt.
func TestNativeStatusBranchBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_STATUS_BRANCH_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_STATUS_BRANCH_RECEIPT to the original-binary receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State   string
		Scope   string
		LibSHA  string `json:"lib_sha256"`
		Vectors []struct {
			Name, Condition string
			Parameters      []int
			Attribute       int
			HP              *int
			NullTarget      bool `json:"null_target"`
			Effects         []struct {
				Function, Parameter    string
				Code, Delta, Attribute int
				ListType               int `json:"list_type"`
			}
			Result int
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-status-branch-predicates" || receipt.LibSHA != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("unexpected original-binary receipt identity or scope")
	}
	attribute := func(code int) string {
		names := []string{"NULL", "FIRE", "ICE", "WIND", "LIGHT", "DARK", "EARTH", "THUNDER", "WATER", "NEUTRAL"}
		if code >= 100 {
			return names[code/100] + "_" + names[code%100]
		}
		return names[code]
	}
	for _, vector := range receipt.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			engine, _ := coreBattleFixture(t)
			actor := &engine.enemies[0]
			actor.Effects = nil
			actor.Attribute = attribute(vector.Attribute)
			if vector.HP != nil {
				actor.HP = *vector.HP
			}
			for _, effect := range vector.Effects {
				if code, ok := battleBuffCodes[effect.Function]; !ok || code != effect.Code {
					t.Fatal("effect identity mismatch")
				}
				actor.Effects = append(actor.Effects, battleEffect{Function: effect.Function, Parameter: effect.Parameter, Delta: effect.Delta,
					ListType: effect.ListType, Attribute: attribute(effect.Attribute)})
			}
			var params [5]string
			for i, value := range vector.Parameters {
				switch {
				case vector.Condition == "TARGET_ATTR":
					params[i] = attribute(value)
				case vector.Condition == "TARGET_DEBUFF_KIND_NUM":
					params[i] = strconv.Itoa(value)
				default:
					params[i] = "NULL"
					kinds := map[int]string{0: "NULL", 1: "ATK_UP_BY_ATK", 8: "HEAL", 31: "ENCHANT", 32: "ATK_UP_BY_MAX_HP"}
					if strings.Contains(vector.Condition, "DEBUFF") {
						kinds = map[int]string{0: "NULL", 1: "ATK_BREAK_BY_ATK", 2: "ATK_BREAK_BY_INT", 7: "POISON", 19: "WEAKNESS"}
					}
					name, ok := kinds[value]
					if !ok {
						t.Fatalf("unmapped fixture kind %d", value)
					}
					params[i] = name
				}
			}
			target := 5
			if vector.NullTarget {
				target = 0
			}
			got := engine.enemyBranchConditionSatisfied(actor, target, "ENEMY_ALL", vector.Condition, params)
			if got != (vector.Result >= 0) {
				t.Fatalf("Go=%t original=%d params=%v effects=%+v", got, vector.Result, params, actor.Effects)
			}
		})
	}
}
