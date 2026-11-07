package battlecore

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
)

// Optional release gate requested for imported cards. The normal pass uses
// production branch selection and a complete turn. The forced-branch pass
// executes each real role set, without claiming its condition was selected.
func TestImportedCardExecution(t *testing.T) {
	root := os.Getenv("CN602_IMPORT_TABLES")
	if root == "" {
		t.Skip("requires prepared import tables")
	}
	base := filepath.Join(root, "_local/control/server")
	catalog, err := LoadCombatCatalog(filepath.Join(base, "cn602-card-master/card.csv"), filepath.Join(base, "cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	var inventory struct {
		Decisions []struct {
			Kind    string   `json:"kind"`
			ID      int      `json:"id"`
			Reasons []string `json:"reasons"`
			Row     []string `json:"row"`
		} `json:"decisions"`
	}
	raw, err := os.ReadFile(filepath.Join(root, "inventory.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err = json.Unmarshal(raw, &inventory); err != nil {
		t.Fatal(err)
	}
	ids := []int{}
	requested := map[int]bool{}
	if selected := os.Getenv("CN602_IMPORT_CARD_IDS"); selected != "" {
		for _, value := range strings.Split(selected, ",") {
			id, err := strconv.Atoi(value)
			if err != nil || id <= 0 || requested[id] {
				t.Fatalf("invalid or duplicate import card ID %q", value)
			}
			requested[id] = true
		}
	}
	for _, d := range inventory.Decisions {
		if d.Kind == "card" && len(d.Reasons) == 0 && d.Row[6] != "1" {
			if len(requested) == 0 || requested[d.ID] {
				ids = append(ids, d.ID)
			}
		}
	}
	if len(requested) > 0 && len(ids) != len(requested) {
		t.Fatal("requested card is missing or excluded from this inventory")
	}
	sort.Ints(ids)
	type failure struct {
		Card, Skill, Variant, Scenario int
		Mode, Error                    string
	}
	report := struct {
		Cards, Turns, BranchExecutions, EncodedResults int
		Failures                                       []failure
		Scope                                          string
	}{Cards: len(ids), Scope: "three level/HP/chain scenarios; real turn selection plus forced role-set execution; no renderer or proof of every conditional predicate"}
	check := func(cid, sid, variant, scenario int, mode string, engine *BattleEngine, run func() ([]BattleResult, error)) {
		rows, err := run()
		if err == nil {
			_, err = encodeOptionalBattleResults(rows)
		}
		if err == nil {
			for _, p := range engine.players {
				if p.HP < 0 || p.MaxHP <= 0 {
					err = fmt.Errorf("invalid player HP %d/%d", p.HP, p.MaxHP)
					break
				}
			}
		}
		if err != nil {
			report.Failures = append(report.Failures, failure{cid, sid, variant, scenario, mode, err.Error()})
		} else {
			report.EncodedResults += len(rows)
		}
	}
	for _, cid := range ids {
		card := catalog.Cards[cid]
		for _, sid := range []int{card.NormalSkillID, card.ArthurSkillID} {
			if sid == 0 {
				continue
			}
			variants := catalog.Skills[sid]
			for scenario := 0; scenario < 3; scenario++ {
				makeCase := func(skill CombatSkillDefinition) (*BattleEngine, battleAction, int) {
					e := newSphereContractEngine(catalog)
					e.phase = PhaseUser
					e.costInitial = 10
					e.holdMax = 10
					e.turn = 1 + scenario*3
					e.enemyCount = 3
					level := card.MaxLevel
					if scenario == 0 {
						level = 1
					}
					chain := 1
					if scenario > 0 {
						chain = 4
					}
					job := map[string]int{"MERCENARY": 1, "MILLIONAIRE": 2, "THIEF": 3, "SINGER": 4}[skill.Job]
					if job == 0 {
						job = 1
					}
					for i := range e.players {
						p := &e.players[i]
						p.ArthurType = job
						p.Cost = 10
						p.Burst = 50
						p.DrawCount = 10
						p.DrawIndex = 5
						if scenario == 1 {
							p.HP = p.MaxHP / 4
							p.DamageTaken = p.MaxHP - p.HP
						}
						for slot := range p.Deck {
							p.Deck[slot] = BattleCard{CardType: slot + 1, CardID: cid, Level: level}
							p.DeckOrder[slot] = slot
						}
						p.Hand = [5]int{1, 2, 3, 4, 5}
					}
					for i := 1; i < 3; i++ {
						e.enemies[i] = e.enemies[0]
						e.enemies[i].MemberType = i + 5
					}
					attrs := []string{"FIRE", "ICE", "WIND", "LIGHT", "DARK"}
					for i := 0; i < 3; i++ {
						e.enemies[i].Attribute = attrs[(scenario+i)%5]
						e.enemies[i].BaseAttribute = e.enemies[i].Attribute
					}
					target := 5
					if !strings.HasPrefix(skill.Target, "ENEMY") {
						target = 1
					}
					a := battleAction{memberType: 1, cardID: cid, cardType: 1, cardLevel: level, target: target, skill: skill, roles: catalog.SkillRoles[skill.FunctionID]}
					if calls := catalog.Skills[card.CallSkillID]; len(calls) > 0 {
						a.callSkill = calls[0]
						a.callRoles = catalog.SkillRoles[calls[0].FunctionID]
					}
					return e, a, chain
				}
				e, a, chain := makeCase(variants[0])
				actions := []battleAction{a}
				if chain == 4 {
					for member := 2; member <= 4; member++ {
						other := a
						other.memberType = member
						actions = append(actions, other)
					}
				}
				report.Turns++
				check(cid, sid, 0, scenario, "selected-turn", e, func() ([]BattleResult, error) {
					all, err := e.executeUserActions(actions)
					if err != nil || e.endType != 0 {
						return all, err
					}
					for _, step := range []func() ([]BattleResult, error){e.ExecuteChaliceUserPhase, e.EnemyPhase, e.ExecuteChaliceEnemyPhase, e.TurnPhase, e.UserPhase} {
						rows, err := step()
						all = append(all, rows...)
						if err != nil || e.endType != 0 {
							return all, err
						}
					}
					rows, err := e.ResumeResults()
					return append(all, rows...), err
				})
				for index, skill := range variants {
					e, a, chain := makeCase(skill)
					report.BranchExecutions++
					check(cid, sid, index, scenario, "forced-role-set", e, func() ([]BattleResult, error) {
						return e.executeSkillRoleSet(a.memberType, a.target, a.skill, a.roles, false, func(role CombatSkillRole) ([]BattleResult, error) { return e.executePlayerRole(a, role, chain) })
					})
				}
			}
		}
	}
	if output := os.Getenv("CN602_IMPORT_EXECUTION_REPORT"); output != "" {
		raw, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			t.Fatal(err)
		}
		file, err := os.OpenFile(output, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Fatal(err)
		}
		_, err = file.Write(raw)
		closeErr := file.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal(closeErr)
		}
	}
	t.Logf("cards=%d turns=%d forced branches=%d encoded results=%d failures=%d", report.Cards, report.Turns, report.BranchExecutions, report.EncodedResults, len(report.Failures))
	if len(report.Failures) > 0 {
		t.Fatalf("first failures: %+v", report.Failures[:minInt(5, len(report.Failures))])
	}
}
