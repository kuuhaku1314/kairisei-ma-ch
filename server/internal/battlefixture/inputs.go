// Package battlefixture holds synthetic value inputs used only by tests.
package battlefixture

import "kairisei.local/server/battlemodel"

func Inputs() (*battlemodel.Definitions, battlemodel.Config, []battlemodel.Participant) {
	catalog := &battlemodel.Definitions{
		Cards:        map[int]battlemodel.CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
		Skills:       map[int][]battlemodel.CombatSkillDefinition{1: {{ID: 1, FunctionID: 1, Cost: 1, Target: "ENEMY_ONE", Attribute: "FIRE"}}},
		SkillRoles:   map[int][]battlemodel.CombatSkillRole{1: {{Function: "ATTACK_AA", Target: "ENEMY_ONE", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}},
		EnemyParties: map[int]battlemodel.CombatEnemyParty{1: {ID: 1, Slots: [4]battlemodel.CombatEnemyPartySlot{{EnemyID: 1, HPRate: 1}}}},
		Enemies:      map[int]battlemodel.CombatEnemyDefinition{1: {ID: 1, HP: 100, Attribute: "FIRE"}},
		EnemyLevels:  map[int]battlemodel.CombatEnemyLevel{1: {ID: 1, HPBars: 1, AttributeRates: [9]int{100, 100, 100, 100, 100, 100, 100, 100, 100}}},
	}
	members := make([]battlemodel.Participant, 4)
	for i := range members {
		members[i] = battlemodel.Participant{MemberType: i + 1, ArthurType: i + 1, HP: 1000, Attack: 100}
		for slot := 1; slot <= 10; slot++ {
			members[i].DeckCards = append(members[i].DeckCards, battlemodel.Card{CardType: slot, CardID: 1, Level: 1})
		}
	}
	return catalog, battlemodel.Config{EnemyPartyID: 1, CostInitial: 3, HoldMax: 2, Seed: 602}, members
}
