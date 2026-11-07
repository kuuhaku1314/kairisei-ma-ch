package battlecore

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"kairisei.local/server/internal/battlefixture"
)

// This compares new CALL behavior with original JP machine-code transcripts.
// The enemy phase includes JP's empty append delimiters. Disabled-gauge refresh
// rows are a separately tracked BOSS gap; other phases compare CALL groups,
// HP, hold lifetime and RNG rather than claiming complete protocol equality.
func TestJPEnemyActionEndCallNativeReceipts(t *testing.T) {
	paths := filepath.SplitList(os.Getenv("JP_NATIVE_DELAYED_CALL_RECEIPTS"))
	if len(paths) == 0 {
		t.Skip("set JP_NATIVE_DELAYED_CALL_RECEIPTS to original JP CALL timing receipts")
	}
	for _, path := range paths {
		t.Run(filepath.Base(path), func(t *testing.T) {
			var receipt struct {
				Library string `json:"library_sha256"`
				Input   struct {
					Seed      uint32
					InitialHP int `json:"initial_hp"`
					MaxHP     int `json:"max_hp"`
					Outer     nativeActionAPISkill
					Call      nativeActionAPISkill
				}
				Phases []struct {
					API string
					CSV string
					RNG [4]uint32
				}
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(raw, &receipt); err != nil {
				t.Fatal(err)
			}
			if receipt.Library != "bd5ae2e9bd305abd8be348a2023ba2ae2f4ae50cee4fc0e22263dacb0ddbebff" || len(receipt.Phases) != 31 {
				t.Fatal("unexpected JP native source or phase sequence")
			}
			definitions, config, members := battlefixture.Inputs()
			config.Seed, config.HoldMax = receipt.Input.Seed, 5
			registerNativeActionSkill(t, receipt.Input.Outer, definitions.Skills, definitions.SkillRoles)
			registerNativeActionSkill(t, receipt.Input.Call, definitions.Skills, definitions.SkillRoles)
			followup, _ := receipt.Input.Call.definitions()
			if err := ValidateAppendSkillDefinition("JP controlled CALL", followup); err != nil {
				t.Fatal(err)
			}
			for i := range members {
				members[i].HP, members[i].MaxHP = receipt.Input.InitialHP, &receipt.Input.MaxHP
				members[i].Attack, members[i].Magic, members[i].Mind = 100, 80, 60
				id := 91998701 + i
				definitions.Cards[id] = CombatCardDefinition{ID: id, NormalSkillID: receipt.Input.Outer.ID, CallSkillID: receipt.Input.Call.ID}
				for j := range members[i].DeckCards {
					members[i].DeckCards[j].CardID = id
				}
			}
			enemy := definitions.Enemies[1]
			enemy.HP, enemy.Attack = 1000000, 100
			definitions.Enemies[1] = enemy
			engine, err := NewBattleEngine((*CombatCatalog)(definitions), config, members)
			if err != nil {
				t.Fatal(err)
			}
			var turn, submission int
			var expectedHolds [4]int
			expectedHP := [4]int{receipt.Input.InitialHP, receipt.Input.InitialHP, receipt.Input.InitialHP, receipt.Input.InitialHP}
			for index, phase := range receipt.Phases {
				var rows []BattleResult
				switch phase.API {
				case "start":
					rows, err = engine.Start()
				case "turn_phase":
					turn++
					submission = 0
					rows, err = engine.TurnPhase()
				case "user_phase":
					rows, err = engine.UserPhase()
				case "user_card_play":
					submission++
					play := cardPlaySubmission{}
					if turn == 1 && submission == 1 {
						play.CardTypes[0] = engine.players[0].Hand[0]
					}
					rows, err = engine.Submit(submission, play)
				case "user_attack":
					rows, err = engine.UserAttack()
				case "chalice_sphr_exec_user_phase":
					rows, err = engine.ExecuteChaliceUserPhase()
				case "enemy_phase":
					rows, err = engine.EnemyPhase()
				case "chalice_sphr_exec_enemy_phase":
					rows, err = engine.ExecuteChaliceEnemyPhase()
				default:
					t.Fatal("unhandled original API", phase.API)
				}
				if err != nil {
					t.Fatalf("%d/%s: %v", index, phase.API, err)
				}
				var encoded []string
				for _, row := range rows {
					text, encodeErr := encodeBattleResult(row)
					if encodeErr != nil {
						t.Fatal(encodeErr)
					}
					encoded = append(encoded, text)
				}
				actual := jpCallObservation(strings.Join(encoded, "\n"))
				expected := jpCallObservation(phase.CSV)
				if phase.API == "enemy_phase" {
					// JP emits a zero gauge refresh even for this disabled gauge.
					// Do not hide any action, delimiter, HP or append lifecycle row.
					allNative := strings.Split(strings.TrimSpace(phase.CSV), "\n")
					var projected []string
					for _, line := range allNative {
						if line != "88,5,0" {
							projected = append(projected, line)
						}
					}
					if !reflect.DeepEqual(encoded, projected) {
						t.Fatalf("%d/%s full enemy sequence differs\nGo=%v\nJP=%v", index, phase.API, encoded, projected)
					}
				}
				if !reflect.DeepEqual(actual, expected) {
					t.Fatalf("%d/%s CALL rows differ\nGo=%v\nJP=%v", index, phase.API, actual, expected)
				}
				for _, line := range expected {
					cells := strings.Split(line, ",")
					member, _ := strconv.Atoi(cells[1])
					switch cells[0] {
					case "61":
						expectedHP[member-1], _ = strconv.Atoi(cells[4])
					case "310":
						expectedHolds[member-1]++
					case "316":
						expectedHolds[member-1]--
					}
				}
				for i, player := range engine.players {
					if player.HP != expectedHP[i] || len(player.BlessHolds) != expectedHolds[i] {
						t.Fatalf("%d/%s member %d: HP/holds=%d/%d expected %d/%d", index, phase.API, i+1, player.HP, len(player.BlessHolds), expectedHP[i], expectedHolds[i])
					}
				}
				if rng := [4]uint32{engine.rng.x, engine.rng.y, engine.rng.z, engine.rng.w}; rng != phase.RNG {
					t.Fatalf("%d/%s RNG: Go=%v JP=%v", index, phase.API, rng, phase.RNG)
				}
			}
		})
	}
}

func jpCallObservation(csv string) []string {
	var result []string
	for _, line := range strings.Split(strings.TrimSpace(csv), "\n") {
		code, _, _ := strings.Cut(line, ",")
		switch code {
		case "58", "61", "310", "316":
			result = append(result, line)
		}
	}
	return result
}
