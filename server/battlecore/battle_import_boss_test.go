package battlecore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// Import gate only. Exercise untouched candidate AI over turns and then each
// real role branch separately. It does not assert Android presentation or that
// controlled branch execution proves every predicate is naturally reachable.
func TestImportedBossExecution(t *testing.T) {
	root := os.Getenv("CN602_IMPORT_BOSSES")
	if root == "" {
		t.Skip("requires isolated projected BOSS tables")
	}
	base := filepath.Join(root, "_local/control/server")
	catalog, err := LoadCombatCatalog(filepath.Join(base, "cn602-card-master/card.csv"), filepath.Join(base, "cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Decisions []struct {
			ID int `json:"party_id"`
		} `json:"decisions"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "bosses.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	type partyReport struct {
		ID       int      `json:"party_id"`
		Errors   []string `json:"errors"`
		Turns    int      `json:"turns"`
		Branches int      `json:"branches"`
		Results  int      `json:"encoded_results"`
	}
	report := struct {
		Scope   string        `json:"scope"`
		Parties []partyReport `json:"parties"`
	}{Scope: "12-turn full/low-HP and broken-parts scenarios; passive/first-strike and forced role branches; no renderer or complete predicate proof"}
	cardIDs := []int{}
	for id, card := range catalog.Cards {
		if card.NormalSkillID > 0 {
			cardIDs = append(cardIDs, id)
		}
	}
	sort.Ints(cardIDs)
	if len(cardIDs) == 0 {
		t.Fatal("missing player fixture card")
	}
	for _, entry := range inventory.Decisions {
		pr := partyReport{ID: entry.ID, Errors: []string{}}
		party, exists := catalog.EnemyParties[entry.ID]
		if !exists {
			t.Fatalf("missing import party %d", entry.ID)
		}
		failures := map[string]bool{}
		check := func(label string, run func() ([]BattleResult, error)) {
			results, err := run()
			if err == nil {
				_, err = encodeOptionalBattleResults(results)
			}
			if err != nil {
				failures[label+": "+err.Error()] = true
			} else {
				pr.Results += len(results)
			}
		}
		makeEngine := func(scenario int) *BattleEngine {
			e := newSphereContractEngine(catalog)
			e.phase, e.turn, e.costInitial, e.holdMax = PhaseCreated, 0, 3, 5
			e.rng = newXorShift128(uint32(602 + scenario))
			if err := e.loadEnemyParty(party, nil); err != nil {
				t.Fatal(err)
			}
			for i := range e.players {
				p := &e.players[i]
				p.HP, p.MaxHP, p.BaseMaxHP = 1000000000, 1000000000, 1000000000
				for j := range p.Deck {
					p.Deck[j] = BattleCard{CardType: j + 1, CardID: cardIDs[0], Level: 1}
					p.DeckOrder[j] = j
				}
				p.DrawCount, p.DrawIndex = 10, 5
				p.Hand = [5]int{1, 2, 3, 4, 5}
			}
			if scenario == 1 {
				for i := 0; i < e.enemyCount; i++ {
					e.enemies[i].HP = maxInt(1, e.enemies[i].MaxHP/4)
				}
			}
			if scenario == 2 {
				for i := 1; i < e.enemyCount; i++ {
					e.enemies[i].HP = 0
				}
			}
			return e
		}
		for scenario := 0; scenario < 3; scenario++ {
			e := makeEngine(scenario)
			check("start", e.Start)
			for turn := 0; turn < 12 && e.endType == 0; turn++ {
				pr.Turns++
				check("turn", e.TurnPhase)
				if e.endType != 0 {
					break
				}
				check("user", e.UserPhase)
				e.phase = PhaseUserAttack // controlled no-card player input
				for _, step := range []func() ([]BattleResult, error){e.ExecuteChaliceUserPhase, e.EnemyPhase, e.ExecuteChaliceEnemyPhase} {
					check("enemy-turn", step)
					if e.endType != 0 {
						break
					}
				}
			}
		}
		for ei, slot := range party.Slots {
			if slot.EnemyID == 0 {
				continue
			}
			lv := catalog.EnemyLevels[slot.EnemyID]
			skills := map[int]bool{lv.PassiveSkillID: true}
			for _, id := range lv.CallSkillIDs {
				skills[id] = true
			}
			for _, a := range lv.Actions {
				skills[a.SkillID] = true
				if !SupportsEnemyActionTarget(a.Target) {
					failures[fmt.Sprintf("unsupported action target %s", a.Target)] = true
				}
			}
			delete(skills, 0)
			for sid := range skills {
				for vi, skill := range catalog.Skills[sid] {
					e := makeEngine(0)
					e.turn, e.phase = 5, PhaseUserAttack
					actor := &e.enemies[ei]
					roles := catalog.SkillRoles[skill.FunctionID]
					target := 1
					if strings.HasPrefix(skill.Target, "ENEMY") || skill.Target == "SELF" {
						target = actor.MemberType
					}
					pr.Branches++
					check(fmt.Sprintf("skill %d branch %d", sid, vi), func() ([]BattleResult, error) {
						return e.executeSkillRoleSet(actor.MemberType, target, skill, roles, false, func(role CombatSkillRole) ([]BattleResult, error) {
							role.SourceSkillID = skill.ID
							if role.Target == "SELECT" && (skill.Target == "SELF" || skill.Target == "USER_ALL" || skill.Target == "ENEMY_ALL") {
								role.Target = skill.Target
							}
							return e.executeEnemyRole(actor, target, role, roles)
						})
					})
				}
			}
		}
		for f := range failures {
			pr.Errors = append(pr.Errors, f)
		}
		sort.Strings(pr.Errors)
		report.Parties = append(report.Parties, pr)
	}
	if output := os.Getenv("CN602_IMPORT_BOSS_REPORT"); output != "" {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		f, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = f.Write(raw)
		closeErr := f.Close()
		if err != nil || closeErr != nil {
			t.Fatalf("report write: %v/%v", err, closeErr)
		}
	}
	failed := 0
	for _, pr := range report.Parties {
		if len(pr.Errors) > 0 {
			failed++
			if failed <= 3 {
				t.Logf("%d: %v", pr.ID, pr.Errors)
			}
		}
	}
	if failed > 0 {
		t.Fatalf("%d/%d import parties failed; see report", failed, len(report.Parties))
	}
	t.Logf("%d import parties executed and encoded", len(report.Parties))
}
