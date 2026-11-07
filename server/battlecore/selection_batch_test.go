package battlecore

import (
	"errors"
	"testing"
)

func TestUnsupportedSummonIntentDoesNotBecomeOrdinarySelection(t *testing.T) {
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Cost: 1, Target: "ENEMY_ONE"}
	engine := newSphereContractEngine(&CombatCatalog{
		Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
		Skills:     map[int][]CombatSkillDefinition{1: {skill}},
		SkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA"}}},
	})
	engine.phase = PhaseUser
	engine.players[0].Cost = 3
	engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
	engine.players[0].Hand[0] = 1
	before := engine.rng
	selection := CardSelection{CardTypes: [5]int{1}, Targets: [5]int{5}, SummonsCardMask: 1}
	if rows, err := engine.Submit(1, selection); !errors.Is(err, ErrSummonSelectionUnsupported) || len(rows) != 0 {
		t.Fatal("summon intent must remain an explicit rule gap", rows, err)
	}
	standalone := &StandaloneBattle{engine: engine}
	if _, err := standalone.Submit(StandaloneCardPlay{MemberType: 1, SummonsCardMask: 1}); !errors.Is(err, ErrSummonSelectionUnsupported) {
		t.Fatal("standalone input must not discard the supplied intent", err)
	}
	if engine.rng != before || engine.players[0].Cost != 3 || len(engine.selectedPlays) != 0 {
		t.Fatal("unsupported intent changed state before rejection")
	}
	selection.SummonsCardMask = 0
	if rows, err := engine.Submit(1, selection); err != nil || len(rows) == 0 || rows[0].Command != CommandCardPlay || engine.players[0].Cost != 2 {
		t.Fatal("ordinary selection must still be usable after rejection", rows, err)
	}
}

func TestClientCardsAndSphereShareSelectionBudget(t *testing.T) {
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Cost: 1, Target: "ENEMY_ONE"}
	sphereSkill := CombatSkillDefinition{ID: 2, FunctionID: 2, Cost: 2, Target: "ENEMY_ONE"}
	catalog := &CombatCatalog{
		Cards:      map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
		Spheres:    map[int]CombatSphereDefinition{2: {ID: 2, SkillID: 2}},
		Skills:     map[int][]CombatSkillDefinition{1: {skill}, 2: {sphereSkill}},
		SkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA"}}, 2: {{Function: "ATTACK_AA"}}},
	}
	engine := newSphereContractEngine(catalog)
	engine.phase = PhaseUser
	engine.players[0].Cost = 3
	engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
	engine.players[0].Deck[1] = BattleCard{CardType: 2, CardID: 1, Level: 1}
	engine.players[0].Hand = [5]int{1, 2}
	engine.players[0].Spheres[0] = battleSphere{Slot: 1, SphereID: 2, Level: 1, Type: SphereTypeNormal, Count: 1, Playable: true}
	submission := CardSelection{CardTypes: [5]int{1, 2}, Targets: [5]int{5, 5}, SphereSlot: 1, SphereTarget: 5}
	submit := func() ([]BattleResult, error) {
		var rows []BattleResult
		err := engine.WithCardSelections(func(batch *CardSelectionBatch) error {
			var err error
			rows, err = batch.SubmitClientSelection(1, submission)
			return err
		})
		return rows, err
	}
	before := engine.rng
	if _, err := submit(); err == nil {
		t.Fatal("two one-cost cards plus a two-cost Sphere exceeded budget three")
	}
	if engine.players[0].Cost != 3 || len(engine.selectedPlays) != 0 || engine.rng != before || engine.players[0].Spheres[0].Count != 1 {
		t.Fatal("rejected combined selection changed the room")
	}
	engine.players[0].Cost = 4
	results, err := submit()
	if err != nil {
		t.Fatal(err)
	}
	cards, spheres := 0, 0
	for _, result := range results {
		if result.Command == CommandCardPlay {
			cards++
		}
		if result.Command == CommandSpherePlay {
			spheres++
		}
	}
	if cards != 2 || spheres != 1 || engine.players[0].Cost != 2 {
		t.Fatal("legal combined selection must retain native submission accounting", cards, spheres, engine.players[0].Cost)
	}
}

func TestAutomaticSelectionRNGCommitsOnlyWithBatch(t *testing.T) {
	skill := CombatSkillDefinition{ID: 1, FunctionID: 1, Cost: 1, Kind: "ATTACK", Target: "ENEMY_ONE"}
	engine := &BattleEngine{phase: PhaseUser, rng: newXorShift128(1), enemyCount: 1,
		catalog: &CombatCatalog{NPC: NPCProgram{Rules: []NPCRule{{ID: 1, EnemyType: 1, Situation: "ANYTIME", Selector: "ATTACK_RANDOM", Priority: [6]int{1, 1, 1, 1, 1, 1}}}}, Cards: map[int]CombatCardDefinition{1: {ID: 1, NormalSkillID: 1}},
			Skills: map[int][]CombatSkillDefinition{1: {skill}}, SkillRoles: map[int][]CombatSkillRole{1: {{Function: "ATTACK_AA", Target: "SELECT"}}}},
		selectedPlays: make(map[int]cardPlaySubmission)}
	for i := range engine.players {
		engine.players[i] = battlePlayer{MemberType: i + 1, ArthurType: i + 1, HP: 100, MaxHP: 100, Cost: 3}
	}
	engine.enemies[0] = battleEnemy{MemberType: 5, HP: 100, MaxHP: 100}
	engine.players[0].Deck[0] = BattleCard{CardType: 1, CardID: 1, Level: 1}
	engine.players[0].Hand[0] = 1
	apply := func(rejectLater bool) error {
		return engine.WithCardSelections(func(batch *CardSelectionBatch) error {
			selection, err := batch.NPCSubmission(1, 1)
			if err != nil {
				return err
			}
			if _, err = batch.Submit(1, selection); err != nil {
				return err
			}
			if rejectLater {
				_, err = batch.Submit(2, CardSelection{CardTypes: [5]int{99}, Targets: [5]int{5}})
			}
			return err
		})
	}
	before := engine.rng
	if err := apply(true); err == nil || engine.rng != before || engine.players[0].Cost != 3 {
		t.Fatal("failed room commit must preserve RNG and cost")
	}
	want := before
	// Native candidate, target and final-choice draws remain part of the batch.
	want.next()
	want.next()
	want.next()
	if err := apply(false); err != nil {
		t.Fatal(err)
	}
	if engine.rng != want || !engine.selectedPlays[1].Automatic || engine.players[0].Cost != 2 {
		t.Fatal("successful automatic submission must commit RNG, CPU flag and cost together")
	}
}
