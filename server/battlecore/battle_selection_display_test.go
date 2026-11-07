package battlecore

import (
	"encoding/json"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestArthurSkillRequiresExactBaseJob(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	catalog := engine.catalog
	card := catalog.Cards[1]
	card.ArthurSkillID = 2
	catalog.Cards[1] = card
	for _, job := range []string{"", "NULL", "SINGER", "MERCENARY"} {
		base := catalog.Skills[1][0]
		base.ID, base.Job, base.ArthurJob = 2, job, job
		later := base
		later.Job, later.ArthurJob = "MERCENARY", "MERCENARY" // Later extend metadata cannot authorize the base.
		catalog.Skills[2] = []CombatSkillDefinition{base, later}
		skill, _, err := catalog.CardSkill(1, 1)
		if err != nil || (skill.ID == 2) != (job == "MERCENARY") {
			t.Fatalf("base job %q resolved %d: %v", job, skill.ID, err)
		}
	}
	base := catalog.Skills[2][0]
	base.FunctionID = 9
	catalog.SkillRoles[9] = []CombatSkillRole{{Function: "NONE"}}
	catalog.Skills[2] = []CombatSkillDefinition{base, base, base}
	if catalog.cardUsesArthurSkill(1, 1) {
		t.Fatal("three explicit NONE branches are the original empty-skill sentinel")
	}
}

func TestSelectionPreviewIsAtomicAndIncludesHypotheticalCard(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	if _, err := engine.Start(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if _, err := engine.UserPhase(); err != nil {
		t.Fatal(err)
	}
	base := engine.catalog.Skills[1][0]
	base.DisplayRole = 1
	boost := base
	boost.FunctionID, boost.BranchPriority = 2, 1
	boost.BranchCondition, boost.BranchParameters[0] = "DECK_COMBO_COUNT", "2"
	role := engine.catalog.SkillRoles[1][0]
	role.Parameters[0] = "500"
	engine.catalog.Skills[1] = []CombatSkillDefinition{base, boost}
	engine.catalog.SkillRoles[2] = []CombatSkillRole{role}
	beforeRNG := engine.rng
	card := engine.players[0].Hand[0]
	rows, err := engine.Submit(1, cardPlaySubmission{CardTypes: [5]int{card}, Targets: [5]int{5}})
	if err != nil {
		t.Fatal(err)
	}
	if engine.rng != beforeRNG || len(engine.turnActions) != 1 || len(engine.selectedPlays) != 1 {
		t.Fatal("hypothetical hand previews escaped into committed actions/RNG")
	}
	// Unsubmitted member 2 previews itself alongside member 1: two Chain,
	// but only member 1 is a contributor light. Member 1 itself remains base.
	if len(rows) != 21 || rows[1].Command != CommandCardUpdate ||
		!equalBattleArgs(rows[1].Args, []int64{2, int64(engine.players[1].Hand[0]), 0, 1, 0, 0, 0, 500, 1, 0}) ||
		rows[16].Args[0] != 1 || rows[16].Args[7] != 200 || rows[16].Args[8] != 0 {
		t.Fatalf("selection preview did not use native unsubmitted/selected groups: %+v", rows)
	}
	// Force failure in another member's hand preview, after input validation.
	engine.players[3].Deck[engine.players[3].Hand[0]-1].CardID = 999
	before := *engine
	before.selectedPlays = make(map[int]cardPlaySubmission)
	for k, v := range engine.selectedPlays {
		before.selectedPlays[k] = v
	}
	if _, err := engine.Submit(2, cardPlaySubmission{}); err == nil {
		t.Fatal("missing preview skill was accepted")
	}
	if !reflect.DeepEqual(*engine, before) {
		t.Fatal("failed display projection changed the committed engine")
	}
}

func TestNativeSelectionAPIReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_SELECTION_API_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_SELECTION_API_RECEIPT to the original API receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		Scope   string `json:"scope"`
		Library string `json:"lib_sha256"`
		Cases   []struct {
			Input struct {
				Name            string `json:"name"`
				Attribute       int    `json:"attribute"`
				ArthurJob       int    `json:"arthur_job"`
				ArthurAttribute int    `json:"arthur_attribute"`
				Branch          bool   `json:"branch"`
				Plays           [4]int `json:"plays"`
			} `json:"input"`
			Hands  map[int][]int `json:"hands"`
			Phases []struct {
				Name string    `json:"name"`
				CSV  string    `json:"csv"`
				RNG  [4]uint32 `json:"rng"`
			} `json:"phases"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.Scope != "original-cn-x86-synthetic-selection-api-chain" || receipt.Library != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Cases) == 0 {
		t.Fatal("wrong native receipt")
	}
	for _, specimen := range receipt.Cases {
		t.Run(specimen.Input.Name, func(t *testing.T) {
			engine, members := coreBattleFixture(t)
			engine.enemies[0].HP, engine.enemies[0].MaxHP, engine.enemies[0].BaseMaxHP = 10000, 10000, 10000
			engine.enemies[0].Attack, engine.enemies[0].BaseAttack = 100, 100
			card := engine.catalog.Cards[1]
			card.ArthurSkillID = 2
			engine.catalog.Cards[1] = card
			attributes := map[int]string{1: "FIRE", 2: "ICE", 102: "FIRE_ICE"}
			for id := 1; id <= 2; id++ {
				attribute, job, power := attributes[specimen.Input.Attribute], "NULL", 200
				if id == 2 {
					attribute, job, power = attributes[specimen.Input.ArthurAttribute], ArthurJob(specimen.Input.ArthurJob), 300
				}
				base := CombatSkillDefinition{ID: id, FunctionID: id, DisplayRole: 1, Kind: "ATTACK", Attribute: attribute, Job: job, Target: "ENEMY_ONE", Cost: 1, HateRatio: 100}
				engine.catalog.Skills[id] = []CombatSkillDefinition{base}
				role := CombatSkillRole{Function: "ATTACK_AA", Target: "SELECT", ChainRate: 20, HateLimit: 100,
					HasTargetAttributes: true, Attributes: [9]bool{true, true, true, true, true, true, true, true, true},
					Parameters: [10]string{strconv.Itoa(power), "0", "0", "0", "1", "ATK", "0", attribute, "PHYSICS"}}
				engine.catalog.SkillRoles[id] = []CombatSkillRole{role}
				if specimen.Input.Branch {
					boost := base
					boost.FunctionID, boost.BranchPriority = id+10, 1
					boost.BranchCondition, boost.BranchParameters[0] = "DECK_COMBO_COUNT", "3"
					engine.catalog.Skills[id] = append(engine.catalog.Skills[id], boost)
					role.Parameters[0] = strconv.Itoa(power * 2)
					engine.catalog.SkillRoles[id+10] = []CombatSkillRole{role}
				}
			}
			for phaseIndex, phase := range specimen.Phases {
				var rows []BattleResult
				var err error
				switch phase.Name {
				case "start":
					rows, err = engine.Start()
				case "turn_phase":
					rows, err = engine.TurnPhase()
				case "user_phase":
					rows, err = engine.UserPhase()
				case "user_attack":
					rows, err = engine.UserAttack()
				case "chalice_sphr_exec_user_phase":
					rows, err = engine.ExecuteChaliceUserPhase()
				case "enemy_phase":
					rows, err = engine.EnemyPhase()
				case "chalice_sphr_exec_enemy_phase":
					rows, err = engine.ExecuteChaliceEnemyPhase()
				case "play1", "play2", "play3", "play4":
					member, _ := strconv.Atoi(strings.TrimPrefix(phase.Name, "play"))
					var submission cardPlaySubmission
					for i := 0; i < specimen.Input.Plays[member-1]; i++ {
						submission.CardTypes[i], submission.Targets[i] = specimen.Hands[member][i], 5
					}
					rows, err = engine.Submit(member, submission)
				default:
					t.Fatal("unknown native phase", phase.Name)
				}
				if err != nil {
					t.Fatalf("phase %d/%s: %v", phaseIndex, phase.Name, err)
				}
				actual, err := encodeOptionalBattleResults(rows)
				if err != nil {
					t.Fatal(err)
				}
				expected := phase.CSV
				if phase.Name == "start" {
					// Multiplayer transport owns CARD identities; the engine
					// does not emit this startup family a second time.
					actual, err = testStartResult(members, rows)
					if err != nil {
						t.Fatal(err)
					}
					expected = strings.TrimPrefix(expected, "999,105,0\n") // original version diagnostic only
				}
				if strings.TrimSpace(actual) != strings.TrimSpace(expected) {
					a, b := strings.Split(strings.TrimSpace(actual), "\n"), strings.Split(strings.TrimSpace(expected), "\n")
					for i := 0; i < maxInt(len(a), len(b)); i++ {
						if i >= len(a) || i >= len(b) {
							t.Fatalf("phase %d/%s row count: Go=%d native=%d", phaseIndex, phase.Name, len(a), len(b))
						}
						if a[i] != b[i] {
							t.Fatalf("phase %d/%s row %d: Go=%s native=%s", phaseIndex, phase.Name, i, a[i], b[i])
						}
					}
				}
				if rng := [4]uint32{engine.rng.x, engine.rng.y, engine.rng.z, engine.rng.w}; rng != phase.RNG {
					t.Fatalf("phase %d/%s RNG: Go=%v native=%v", phaseIndex, phase.Name, rng, phase.RNG)
				}
			}
		})
	}
}
