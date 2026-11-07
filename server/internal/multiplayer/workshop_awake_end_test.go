//go:build workshop

package multiplayer

import (
	"context"
	"testing"
)

func TestWorkshopAwakeEndUsesDeclaredWaveSequence(t *testing.T) {
	for _, types := range [][]int8{{1}, {1, 1}, {1, 4}} {
		definitions, _, members := roomFixtureInputs()
		level := definitions.EnemyLevels[1]
		level.InitialAwake = 1
		definitions.EnemyLevels[1] = level
		card := definitions.Cards[1]
		card.MaxLevel = 1
		definitions.Cards[1] = card
		skill := definitions.Skills[1][0]
		skill.Kind = "ATTACK"
		definitions.Skills[1][0] = skill
		request := WorkshopSimulationRequest{EnemyTypes: types, Turns: 3, Seed: 602, CostInitial: 3, Automatic: true}
		for range types {
			request.Parties = append(request.Parties, 1)
		}
		for i, member := range members {
			request.Players[i] = WorkshopTestMember{HP: member.HP, Attack: member.Attack}
			for slot, owned := range member.DeckCards {
				request.Players[i].Cards[slot] = owned.CardID
			}
		}
		report, err := WorkshopSimulate(context.Background(), (*CombatCatalog)(definitions), request)
		if err != nil || report.State != "victory" || report.CompletedWaves != len(types) {
			t.Fatalf("types=%v report=%+v err=%v", types, report, err)
		}
	}
}
