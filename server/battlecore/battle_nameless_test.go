package battlecore

import (
	"os"
	"path/filepath"
	"testing"
)

// Exercise the published official difficulty data at the missing transition,
// rather than replacing its AI with an invented generic awakening skill.
func TestNamelessOfficialAwakening(t *testing.T) {
	root := os.Getenv("CN602_RUNTIME_SET")
	if root == "" {
		t.Skip("set CN602_RUNTIME_SET to the complete resource directory")
	}
	master := filepath.Join(root, "_local/control/server")
	catalog, err := LoadCombatCatalog(filepath.Join(master, "cn602-card-master/card.csv"), filepath.Join(master, "cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []int{30920106, 30920107, 30920108} {
		engine, _ := coreBattleFixture(t)
		engine.catalog = catalog
		if err := engine.loadEnemyParty(catalog.EnemyParties[id], nil); err != nil {
			t.Fatal(err)
		}
		engine.turn = 1
		body := &engine.enemies[0]
		var trigger CombatEnemyAction
		for _, action := range body.Level.Actions {
			if catalog.EnemyAIOrders[action.AIConditionID].Fields[1] == "PARTS_ALL_BREAK" {
				trigger = action
				break
			}
		}
		if trigger.SkillID == 0 {
			t.Fatalf("%d missing official parts-break trigger", id)
		}
		if _, ok := engine.eligibleEnemyAction(body, trigger, 0); ok {
			t.Fatal("awakened before parts broke")
		}
		engine.enemies[1].HP = 0
		if _, ok := engine.eligibleEnemyAction(body, trigger, 0); ok {
			t.Fatal("awakened with one surviving part")
		}
		engine.enemies[2].HP = 0
		candidate, ok := engine.eligibleEnemyAction(body, trigger, 0)
		if !ok {
			t.Fatal("all broken parts did not enable awakening")
		}
		if _, err := engine.executeEnemyActionCandidate(body, candidate); err != nil {
			t.Fatal(err)
		}
		if engine.endType != 4 || body.HP <= 0 {
			t.Fatalf("%d did not preserve body during AWAKE: end=%d HP=%d", id, engine.endType, body.HP)
		}
		engine.phase = PhaseEnded
		next, err := engine.NextBattle(id+10, nil)
		if err != nil {
			t.Fatal(err)
		}
		if next.enemies[0].EnemyID != catalog.EnemyParties[id+10].Slots[0].EnemyID || next.enemies[0].Awake != 0 || next.enemyCount != 3 || next.enemies[1].HP <= 0 || next.enemies[2].HP <= 0 {
			t.Fatalf("invalid next phase %d", id)
		}
		t.Logf("%d: skill=%d -> AWAKE -> party=%d HP=%d actions=%d", id, trigger.SkillID, id+10, next.enemies[0].HP, next.enemies[0].Level.ActionsPerTurn)
	}
}
