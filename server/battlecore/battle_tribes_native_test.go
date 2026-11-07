package battlecore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestJPNativeTribeConditions(t *testing.T) {
	for _, receipt := range []string{
		"20261016-r107-jp-target-tribes.json", "20261016-r107-jp-enemy-tribes.json",
		"20261016-r108-jp-target-tribes.json", "20261016-r108-jp-enemy-tribes.json",
	} {
		t.Run(receipt, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "_local", "evidence", "client-rebuild", receipt))
			if os.IsNotExist(err) {
				t.Skip("original native receipt is a local evidence input")
			}
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Library string `json:"library_sha256"`
				Cases   []struct {
					Tribes     []int  `json:"tribes"`
					Part       []int  `json:"destroyed_part_tribes"`
					Wanted     int    `json:"wanted"`
					Parameters []int  `json:"parameters"`
					Condition  string `json:"condition"`
					Selected   bool   `json:"branch_selected"`
				} `json:"cases"`
			}
			if err := json.Unmarshal(raw, &capture); err != nil {
				t.Fatal(err)
			}
			if capture.Library != "bd5ae2e9bd305abd8be348a2023ba2ae2f4ae50cee4fc0e22263dacb0ddbebff" || len(capture.Cases) != 8 {
				t.Fatal("unexpected JP source or incomplete branch controls")
			}
			for i, input := range capture.Cases {
				engine, _ := coreBattleFixture(t)
				engine.enemies[0].TribeIDs = input.Tribes
				if input.Part != nil {
					engine.enemyCount = 2
					engine.enemies[1] = battleEnemy{MemberType: 6, EnemyID: 2, HP: 0, TribeIDs: input.Part}
				}
				values := input.Parameters
				if len(values) == 0 {
					values = []int{input.Wanted, 0, 0, 0, 0}
				}
				var parameters [5]string
				for k, value := range values {
					parameters[k] = strconv.Itoa(value)
				}
				if got := engine.commonMemberCondition(input.Condition, 1, 5, parameters); got != input.Selected {
					t.Fatalf("native case %d %s tribes=%v part=%v params=%v: got %v, want %v", i, input.Condition, input.Tribes, input.Part, parameters, got, input.Selected)
				}
			}
		})
	}
}

func TestEncounterOwnsExplicitTribeSlots(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	ids := []int{1, 7, 0}
	input := Encounter{}
	input.Enemies[0] = EnemyInput{Definition: CombatEnemyDefinition{ID: 42, TribeIDs: ids}, HP: 100, MaxHP: 100}
	if err := engine.loadEncounter(input); err != nil {
		t.Fatal(err)
	}
	ids[1] = 99
	if !enemyHasTribe(&engine.enemies[0], 7) || enemyHasTribe(&engine.enemies[0], 99) || !enemyHasTribe(&engine.enemies[0], 0) {
		t.Fatal("encounter did not own original three slots")
	}
	input.Enemies[0].Definition.TribeIDs = []int{1}
	if err := engine.loadEncounter(input); err != nil {
		t.Fatal(err)
	}
	if enemyHasTribe(&engine.enemies[0], 0) {
		t.Fatal("single-slot input gained invented zero memberships")
	}
	input.Enemies[0].Definition.TribeIDs = []int{1, 2, 3, 4}
	if err := engine.loadEncounter(input); err == nil {
		t.Fatal("accepted unsupported fourth slot")
	}
}

func TestJPNativeTribalAttackAndSupportBoosts(t *testing.T) {
	type nativeRole struct {
		Function   string     `json:"function"`
		Parameters [10]string `json:"parameters"`
		Target     string     `json:"target"`
		Chain      int        `json:"chain"`
	}
	type nativeSkill struct {
		Kind      string       `json:"kind"`
		Target    string       `json:"target"`
		Attribute string       `json:"attribute"`
		Roles     []nativeRole `json:"roles"`
	}
	for _, name := range []string{"boost", "atk-boost", "def-boost"} {
		t.Run(name, func(t *testing.T) {
			raw, err := os.ReadFile(filepath.Join("..", "..", "_local", "evidence", "client-rebuild", "20261016-r108-jp-tribal-"+name+".json"))
			if os.IsNotExist(err) {
				t.Skip("original native receipt is a local evidence input")
			}
			if err != nil {
				t.Fatal(err)
			}
			var capture struct {
				Library string `json:"library_sha256"`
				Cases   []struct {
					Tribes   []int       `json:"tribes"`
					Part     []int       `json:"destroyed_part_tribes"`
					Attack   nativeSkill `json:"attack"`
					Modifier nativeSkill `json:"modifier"`
					Value    int         `json:"damage"` // Native probe stores damage or the resulting parameter by family.
				} `json:"cases"`
			}
			if err := json.Unmarshal(raw, &capture); err != nil {
				t.Fatal(err)
			}
			if capture.Library != "bd5ae2e9bd305abd8be348a2023ba2ae2f4ae50cee4fc0e22263dacb0ddbebff" || len(capture.Cases) != 8 {
				t.Fatal("native boost controls are incomplete")
			}
			for i, input := range capture.Cases {
				engine, _ := coreBattleFixture(t)
				engine.enemies[0].TribeIDs = input.Tribes
				engine.enemies[0].HP, engine.enemies[0].MaxHP = 1000000, 1000000
				if input.Part != nil {
					engine.enemyCount = 2
					engine.enemies[1] = battleEnemy{MemberType: 6, EnemyID: 2, HP: 0, TribeIDs: input.Part}
				}
				player := &engine.players[0]
				player.Attack, player.BaseAttack, player.Defense, player.BaseDefense = 100, 100, 0, 0
				var roles []CombatSkillRole
				for _, r := range append(input.Attack.Roles, input.Modifier.Roles...) {
					roles = append(roles, CombatSkillRole{Function: r.Function, Parameters: r.Parameters, Target: r.Target, ChainRate: r.Chain})
				}
				target := 5
				if name != "boost" {
					target = 1
				}
				action := battleAction{memberType: 1, cardType: 1, cardLevel: 1, target: target, roles: roles, skill: CombatSkillDefinition{Kind: input.Attack.Kind, Target: input.Attack.Target, Attribute: input.Attack.Attribute, DamageKind: "PHYSICS", Cost: 1}}
				rows, err := engine.executePlayerRole(action, roles[0], 0)
				if err != nil {
					t.Fatal(err)
				}
				value := player.Attack
				if name == "boost" {
					value = 0
					for _, row := range rows {
						if row.Command == 60 {
							value = int(row.Args[2])
							break
						}
					}
				}
				if name == "def-boost" {
					value = player.Defense
				}
				if value != input.Value {
					t.Fatalf("native case %d tribes=%v part=%v: got %d, want %d", i, input.Tribes, input.Part, value, input.Value)
				}
			}
		})
	}
}
