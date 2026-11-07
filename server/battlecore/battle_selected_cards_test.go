package battlecore

import (
	"encoding/json"
	"os"
	"strconv"
	"testing"
)

func TestSphereDoesNotSatisfyCardSelectionBranches(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	current := battleAction{memberType: 1, cardID: 1, cardType: 1, skill: engine.catalog.Skills[1][0]}
	current.skill.Cost, current.skill.Kind = 2, "ATTACK"
	sphere := battleAction{memberType: 1, sphereSlot: 1, skill: CombatSkillDefinition{Cost: 1, Attribute: "FIRE", Kind: "ATTACK"}}
	actions := []battleAction{current, sphere}
	for _, tc := range []struct {
		condition string
		params    [5]string
	}{
		{"FRIEND_PLAY_NUM", [5]string{"2", "2"}},
		{"SELF_OTHER_PLAY_NUM", [5]string{"1", "1"}},
		{"SELF_OTHER_PLAY_ATTR", [5]string{"FIRE", "1"}},
		{"SELF_OTHER_PLAY_SKILL_KIND", [5]string{"ATTACK", "1"}},
		{"SELF_PLAY_MOST_LOW_COST", [5]string{"0", "1"}},
		{"FRIEND_PLAY_MOST_LOW_COST", [5]string{"0", "1"}},
		{"SELF_PLAY_COST_TOTAL", [5]string{"3", "3"}},
		{"SELF_PLAY_COST_NUM", [5]string{"1", "1", "1", "1"}},
	} {
		if engine.branchConditionSatisfied(tc.condition, tc.params, current, actions, nil) {
			t.Fatalf("sphere incorrectly satisfied %s", tc.condition)
		}
	}
	base := current.skill
	boost := base
	boost.FunctionID, boost.BranchPriority = 2, 1
	boost.BranchCondition, boost.BranchParameters = "SELF_OTHER_PLAY_ATTR", [5]string{"FIRE", "1"}
	engine.catalog.Skills[1] = []CombatSkillDefinition{base, boost}
	selected, marker := engine.selectCombatSkillBranchWithIndex(current, actions, nil)
	if selected.FunctionID != base.FunctionID || marker != 0 {
		t.Fatal("a sphere selected beside one card falsely enabled the enhanced skill")
	}
}

func TestNativeSelectedCardBinaryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_CARD_TAG_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_CARD_TAG_RECEIPT to a receipt containing selection_vectors")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Vectors      []struct {
			Name, Condition string
			Parameters      []int
			Cards           []struct{ Member, Cost, Attribute, Kind, Rarity int }
			SphereMember    int `json:"sphere_member"`
			Result          int
		} `json:"selection_vectors"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-card-profile-tags" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" {
		t.Fatal("invalid original selection receipt")
	}
	if len(receipt.Vectors) == 0 {
		t.Skip("older profile-only receipt; no selected-card vectors")
	}
	attrs := map[int]string{0: "NULL", 1: "FIRE", 2: "ICE", 4: "LIGHT", 102: "FIRE_ICE"}
	kinds := map[int]string{0: "NULL", 1: "ATTACK", 2: "SORCERY", 3: "RECOVERY"}
	rarities := map[int]string{0: "NULL", 4: "SUPERRARE", 5: "ULTRARARE", 6: "MILLIONRARE", 7: "EXRARE"}
	for _, v := range receipt.Vectors {
		engine, _ := coreBattleFixture(t)
		var params [5]string
		for i, n := range v.Parameters {
			params[i] = strconv.Itoa(n)
		}
		switch v.Condition {
		case "SELF_OTHER_PLAY_ATTR":
			params[0] = attrs[v.Parameters[0]]
		case "SELF_OTHER_PLAY_SKILL_KIND":
			params[0] = kinds[v.Parameters[0]]
		case "SELF_OTHER_PLAY_RARITY":
			params[0] = rarities[v.Parameters[0]]
		}
		var actions []battleAction
		var counts [4]int
		for i, c := range v.Cards {
			counts[c.Member-1]++
			engine.catalog.Cards[i+1] = CombatCardDefinition{ID: i + 1, Rarity: rarities[c.Rarity]}
			actions = append(actions, battleAction{memberType: c.Member, cardID: i + 1, cardType: counts[c.Member-1],
				skill: CombatSkillDefinition{Cost: c.Cost, Attribute: attrs[c.Attribute], Kind: kinds[c.Kind]}})
		}
		if v.SphereMember != 0 {
			actions = append(actions, battleAction{memberType: v.SphereMember, sphereSlot: 1, skill: CombatSkillDefinition{Cost: 1, Attribute: "FIRE", Kind: "ATTACK"}})
		}
		got := engine.branchConditionSatisfied(v.Condition, params, battleAction{memberType: 1, cardType: 1}, actions, nil)
		if got != (v.Result == 0) {
			t.Fatalf("%s Go=%t original=%d", v.Name, got, v.Result)
		}
	}
}
