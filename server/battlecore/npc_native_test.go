package battlecore

import (
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func TestNPCNativeSelectionReceipt(t *testing.T) {
	path := os.Getenv("JP_NPC_PRIORITY_RECEIPT")
	jp := path != ""
	if path == "" {
		path = os.Getenv("CN_NPC_SELECTION_RECEIPT")
	}
	if path == "" {
		t.Skip("set CN_NPC_SELECTION_RECEIPT or JP_NPC_PRIORITY_RECEIPT to the original NPC phase capture")
	}
	var receipt struct {
		Library         string   `json:"lib_sha256"`
		CompareCommands []string `json:"compare_commands"`
		Cases           []struct {
			Input struct {
				Name      string
				Costs     [10]int
				BonusJob  int  `json:"bonus_job"`
				AllTarget bool `json:"all_target"`
				NullJob   bool `json:"null_job"`
				Job       *int
				EnemyHP   []int `json:"enemy_hp"`
				Rules     []NPCRule
			}
			Phases []struct {
				Name       string
				Member     int
				CSV        string
				RNG        [4]uint32
				Submission struct {
					Member         int
					Cards, Targets [5]int
				}
			}
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	library := "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636"
	if jp {
		library = "bd5ae2e9bd305abd8be348a2023ba2ae2f4ae50cee4fc0e22263dacb0ddbebff"
	}
	if receipt.Library != library || len(receipt.Cases) == 0 {
		t.Fatal("wrong native NPC capture")
	}
	selectedRows := func(value string) string {
		if len(receipt.CompareCommands) == 0 {
			return strings.TrimSpace(value)
		}
		var rows []string
		for _, row := range strings.Split(strings.TrimSpace(value), "\n") {
			command, _, _ := strings.Cut(row, ",")
			for _, wanted := range receipt.CompareCommands {
				if command == wanted {
					if command == "27" && len(strings.Split(row, ",")) == 5 {
						row += ",0"
					}
					if command == "22" && len(strings.Split(row, ",")) == 6 {
						row += ",0"
					}
					rows = append(rows, row)
					break
				}
			}
		}
		return strings.Join(rows, "\n")
	}
	for _, item := range receipt.Cases {
		t.Run(item.Input.Name, func(t *testing.T) {
			catalog := &CombatCatalog{Cards: map[int]CombatCardDefinition{}, Skills: map[int][]CombatSkillDefinition{}, SkillRoles: map[int][]CombatSkillRole{}, EnemyParties: map[int]CombatEnemyParty{1: {ID: 1, Slots: [4]CombatEnemyPartySlot{{EnemyID: 1, HPRate: 1}, {EnemyID: 2, HPRate: 1, ParentIndex: 1}}}}, Enemies: map[int]CombatEnemyDefinition{}, EnemyLevels: map[int]CombatEnemyLevel{}}
			for slot, cost := range item.Input.Costs {
				id := 101 + slot
				catalog.Cards[id] = CombatCardDefinition{ID: id, NormalSkillID: 1001 + slot, ArthurSkillID: 2001 + slot, MaxLevel: 1}
				for variant := 0; variant < 2; variant++ {
					sid, power, job := 1001+slot, 200, "NULL"
					if variant == 1 {
						sid += 1000
						power = 300
						job = ArthurJob(item.Input.BonusJob)
					}
					target := "ENEMY_ONE"
					if item.Input.AllTarget {
						target = "ENEMY_ALL"
					}
					catalog.Skills[sid] = []CombatSkillDefinition{{ID: sid, FunctionID: sid, DisplayRole: 1, Kind: "ATTACK", Attribute: "FIRE", Job: job, DamageKind: "PHYSICS", Cost: cost, PriorityPVE: slot + 1, HateRatio: 100, Target: target}}
					role := CombatSkillRole{SkillID: sid, Function: "ATTACK_AA", Target: "SELECT", ChainRate: 20, HasTargetAttributes: true, Attributes: [9]bool{true, true, true, true, true, true, true, true, true}, Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}
					if power == 300 {
						role.Parameters[0] = "300"
					}
					catalog.SkillRoles[sid] = []CombatSkillRole{role}
				}
			}
			for index, hp := range []int{1000000, 500000} {
				if len(item.Input.EnemyHP) != 0 {
					if len(item.Input.EnemyHP) != 2 {
						t.Fatal("controlled NPC receipt requires two enemy HP inputs")
					}
					hp = item.Input.EnemyHP[index]
				}
				id := index + 1
				catalog.Enemies[id] = CombatEnemyDefinition{ID: id, HP: hp, Attack: 100, Attribute: "FIRE", TribeIDs: []int{1}}
				catalog.EnemyLevels[id] = CombatEnemyLevel{ID: id, HPBars: 1, AttributeRates: [9]int{100, 100, 100, 100, 100, 100, 100, 100, 100}}
			}
			members := make([]Participant, 4)
			for i := range members {
				members[i] = Participant{MemberType: i + 1, ArthurType: i + 1, HP: 1000, Attack: 100}
				if jp {
					members[i].Magic, members[i].Mind = 200, 300
				}
				if item.Input.NullJob {
					v := 0
					members[i].JobType = &v
				}
				if i == 0 && item.Input.Job != nil {
					members[i].JobType = item.Input.Job
				}
				for slot := 1; slot <= 10; slot++ {
					members[i].DeckCards = append(members[i].DeckCards, BattleCard{CardType: slot, CardID: 100 + slot, Level: 1})
				}
			}
			engine, err := NewBattleEngine(catalog, Config{EnemyPartyID: 1, CostInitial: 3, HoldMax: 2, Seed: 602}, members)
			if err != nil {
				t.Fatal(err)
			}
			for _, phase := range item.Phases {
				var rows []BattleResult
				switch phase.Name {
				case "start":
					rows, err = engine.Start()
				case "turn_phase":
					rows, err = engine.TurnPhase()
				case "user_phase":
					rows, err = engine.UserPhase()
				case "cpu_card_play":
					rows, err = engine.SubmitNPC(phase.Member, 1, NPCProgram{Rules: item.Input.Rules})
				case "user_attack":
					rows, err = engine.UserAttack()
				case "user_card_play":
					s := phase.Submission
					rows, err = engine.Submit(s.Member, cardPlaySubmission{CardTypes: s.Cards, Targets: s.Targets})
				case "chalice_sphr_exec_user_phase":
					rows, err = engine.ExecuteChaliceUserPhase()
				case "enemy_phase":
					rows, err = engine.EnemyPhase()
				case "chalice_sphr_exec_enemy_phase":
					rows, err = engine.ExecuteChaliceEnemyPhase()
				case "resume_data_get":
					rows, err = engine.ResumeResults()
				default:
					t.Fatal("unknown phase", phase.Name)
				}
				if err != nil {
					t.Fatal(phase.Name, err)
				}
				actual, err := encodeOptionalBattleResults(rows)
				if err != nil {
					t.Fatal(err)
				}
				expected := phase.CSV
				if phase.Name == "start" {
					actual, err = testStartResult(members, rows)
					if err != nil {
						t.Fatal(err)
					}
					// 999/105 is the original registered-NPC diagnostic hash; it
					// is nonzero once rules are present, not a gameplay result.
					if strings.HasPrefix(expected, "999,105,") {
						_, expected, _ = strings.Cut(expected, "\n")
					}
				}
				actual, expected = selectedRows(actual), selectedRows(expected)
				if actual != expected {
					a, b := strings.Split(strings.TrimSpace(actual), "\n"), strings.Split(strings.TrimSpace(expected), "\n")
					for i := 0; i < max(len(a), len(b)); i++ {
						if i >= len(a) || i >= len(b) {
							t.Fatalf("%s member%d rows: Go=%d native=%d", phase.Name, phase.Member, len(a), len(b))
						}
						if a[i] != b[i] {
							t.Fatalf("%s member%d row%d: Go=%s native=%s", phase.Name, phase.Member, i, a[i], b[i])
						}
					}
				}
				if actual := [4]uint32{engine.rng.x, engine.rng.y, engine.rng.z, engine.rng.w}; actual != phase.RNG {
					t.Fatalf("%s member%d RNG Go=%v native=%v", phase.Name, phase.Member, actual, phase.RNG)
				}
			}
		})
	}
}

func TestNPCUnsupportedDecisionDoesNotCommitRNGOrSelection(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.Start()
	engine.TurnPhase()
	engine.UserPhase()
	before := *engine
	_, err := engine.SubmitNPC(1, 1, NPCProgram{Rules: []NPCRule{{ID: 1, EnemyType: 1, Situation: "ANYTIME", Selector: "ATTACK_RANDOM", Priority: [6]int{1, 1, 1, 1}}, {ID: 2, EnemyType: 1, Situation: "ANYTIME", Selector: "UNCONNECTED", Priority: [6]int{2, 2, 2, 2}}}})
	if err == nil || !reflect.DeepEqual(*engine, before) {
		t.Fatal("failed NPC planning committed partial state")
	}
}

func TestNPCNativeConditionsReceipt(t *testing.T) {
	path := os.Getenv("CN_NPC_CONDITIONS_RECEIPT")
	if path == "" {
		t.Skip("set CN_NPC_CONDITIONS_RECEIPT to the original condition dispatch capture")
	}
	var input struct {
		Library string `json:"lib_sha256"`
		Cases   []struct {
			Member   int
			Rule     NPCRule
			Expected bool
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if input.Library != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(input.Cases) == 0 {
		t.Fatal("wrong native conditions capture")
	}
	engine, _ := coreBattleFixture(t)
	for i, hp := range []int{333, 666, 999, 0} {
		engine.players[i].HP = hp
	}
	engine.enemies[0].HP, engine.enemies[0].MaxHP, engine.enemies[0].BaseMaxHP = 1500, 5000, 5000
	state := npcDecisionState{maxHate: 100}
	state.ownFeatures[0], state.ownFeatures[2] = 1, 1
	for _, item := range input.Cases {
		actual, err := engine.npcCondition(&engine.players[item.Member-1], item.Rule, &state)
		if err != nil || actual != item.Expected {
			t.Fatalf("member%d %s %v Go=%v native=%v err=%v", item.Member, item.Rule.Situation, item.Rule.Parameters, actual, item.Expected, err)
		}
	}
}

func TestNPCNativeFeaturesReceipt(t *testing.T) {
	path := os.Getenv("CN_NPC_FEATURES_RECEIPT")
	if path == "" {
		t.Skip("set CN_NPC_FEATURES_RECEIPT to the original ab9bc capture")
	}
	var input struct {
		Library string `json:"lib_sha256"`
		Cases   []struct {
			Skill    CombatSkillDefinition
			Roles    []CombatSkillRole
			Expected map[int]uint32
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(data, &input); err != nil {
		t.Fatal(err)
	}
	if input.Library != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(input.Cases) == 0 {
		t.Fatal("wrong native feature capture")
	}
	for i, item := range input.Cases {
		got := npcSkillFeatures(item.Skill, item.Roles)
		for slot, value := range got {
			if value != item.Expected[slot] {
				t.Fatalf("case%d target=%s roles=%+v feature%d: Go=%d native=%d", i, item.Skill.Target, item.Roles, slot, value, item.Expected[slot])
			}
		}
	}
}
