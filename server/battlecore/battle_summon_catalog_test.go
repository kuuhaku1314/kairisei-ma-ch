package battlecore

import (
	"os"
	"path/filepath"
	"sort"
	"testing"
)

func TestSummonCatalogExecution(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("requires complete runtime set")
	}
	base := filepath.Join(root, "_local/control/server")
	catalog, err := LoadCombatCatalog(filepath.Join(base, "cn602-card-master/card.csv"), filepath.Join(base, "cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	ids := make([]int, 0)
	for id, sphere := range catalog.Spheres {
		if sphere.Type == SphereTypeChalice {
			ids = append(ids, id)
		}
	}
	sort.Ints(ids)
	ordinaryCardID := 0
	for id, card := range catalog.Cards {
		if combatRarityCode(card.Rarity) >= combatRarityCode("NORMAL") && (ordinaryCardID == 0 || id < ordinaryCardID) {
			ordinaryCardID = id
		}
	}
	for _, id := range ids {
		engine := newSphereContractEngine(catalog)
		def := catalog.Spheres[id]
		engine.phase, engine.turn = PhaseUserAttack, 7
		skill, _, err := catalog.SphereSkill(id)
		if err != nil {
			t.Fatal(err)
		}
		engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: id, Level: def.MaxLevel, Type: def.Type, Count: 1, Maximum: 1}
		engine.refreshSphereCondition(&engine.players[0].Spheres[0], def)
		for member := 1; member <= 4; member++ {
			engine.turnActions = append(engine.turnActions, battleAction{memberType: member, cardType: 1, cardID: ordinaryCardID, skill: CombatSkillDefinition{Attribute: skill.Attribute}})
		}
		for slot := 2; slot <= 3; slot++ {
			engine.turnActions = append(engine.turnActions, battleAction{memberType: 1, cardType: slot, cardID: ordinaryCardID, skill: CombatSkillDefinition{Attribute: skill.Attribute}})
		}
		if _, err := engine.chalicePlayableResults(); err != nil {
			t.Fatalf("sphere %d condition: %v", id, err)
		}
		if _, err := engine.ReserveChaliceSphere(1, 1); err != nil {
			t.Fatalf("sphere %d reservation: %v", id, err)
		}
		rows, err := engine.ExecuteChaliceUserPhase()
		if err != nil {
			t.Fatalf("sphere %d: %v", id, err)
		}
		if _, err = encodeOptionalBattleResults(rows); err != nil {
			t.Fatalf("sphere %d wire: %v", id, err)
		}
		engine.phase = PhaseEnemy
		if _, err = engine.ExecuteChaliceEnemyPhase(); err != nil {
			t.Fatalf("sphere %d following phase: %v", id, err)
		}
		if rows, err := engine.ResumeResults(); err != nil {
			t.Fatalf("sphere %d resume: %v", id, err)
		} else if _, err := encodeOptionalBattleResults(rows); err != nil {
			t.Fatalf("sphere %d resume wire: %v", id, err)
		}
	}
	t.Logf("%d summon definitions executed and encoded", len(ids))
}
