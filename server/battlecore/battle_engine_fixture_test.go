package battlecore

import (
	"testing"

	"kairisei.local/server/internal/battlefixture"
)

func coreBattleFixture(t *testing.T) (*BattleEngine, []Participant) {
	t.Helper()
	definitions, config, members := battlefixture.Inputs()
	engine, err := NewBattleEngine((*CombatCatalog)(definitions), config, members)
	if err != nil {
		t.Fatal(err)
	}
	return engine, members
}
