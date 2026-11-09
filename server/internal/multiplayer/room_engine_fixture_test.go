package multiplayer

import (
	"testing"

	"kairisei.local/server/battlecore"
	"kairisei.local/server/internal/battlefixture"
)

// Room tests add account identity outside the shared core fixture.
func nextBattleFixture(t *testing.T) (*BattleEngine, []Member) {
	t.Helper()
	return configuredRoomBattle(t, nil)
}

// Explicit synthetic NPC content belongs to room transaction tests only.
// Runtime initialization loads the original tables through battlecontent.
func roomFixtureInputs() (*battlecore.Definitions, battlecore.Config, []battlecore.Participant) {
	definitions, config, members := battlefixture.Inputs()
	for enemyType := 0; enemyType <= 4; enemyType++ {
		definitions.NPC.Rules = append(definitions.NPC.Rules, battlecore.NPCRule{
			ID: enemyType + 1, EnemyType: enemyType, Situation: "ANYTIME", Selector: "ATTACK_RANDOM", Priority: [6]int{1, 1, 1, 1, 1, 1},
		})
	}
	return definitions, config, members
}

func configuredRoomBattle(t *testing.T, configure func(*battlecore.Definitions, *battlecore.Config, []battlecore.Participant)) (*BattleEngine, []Member) {
	t.Helper()
	definitions, config, participants := roomFixtureInputs()
	if configure != nil {
		configure(definitions, &config, participants)
	}
	engine, err := NewBattleEngine((*CombatCatalog)(definitions), config, participants)
	if err != nil {
		t.Fatal(err)
	}
	members := make([]Member, len(participants))
	for i, p := range participants {
		members[i] = Member{MemberType: p.MemberType, ArthurType: p.ArthurType, UserID: 1001 + i,
			HP: p.HP, Attack: p.Attack, Magic: p.Magic, Mind: p.Mind, DeckCards: p.DeckCards}
	}
	return engine, members
}

// Room fixtures reach lifecycle states through the same operations as callers.
func openFixtureUserPhase(t *testing.T, engine *BattleEngine) {
	t.Helper()
	for _, phase := range []func() ([]BattleResult, error){engine.Start, engine.TurnPhase, engine.UserPhase} {
		if _, err := phase(); err != nil {
			t.Fatal(err)
		}
	}
}

func finishFixtureUserAttack(t *testing.T, engine *BattleEngine, attack bool) {
	t.Helper()
	for member := 1; member <= 4; member++ {
		selection := cardPlaySubmission{}
		if attack && member == 1 {
			selection.CardTypes[0], selection.Targets[0] = firstFixtureHandCard(t, engine, member), 5
		}
		if _, err := engine.Submit(member, selection); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := engine.UserAttack(); err != nil {
		t.Fatal(err)
	}
}

func wonRoomBattle(t *testing.T, drops ...BattleDrop) (*BattleEngine, []Member) {
	t.Helper()
	engine, members := configuredRoomBattle(t, func(_ *battlecore.Definitions, config *battlecore.Config, _ []battlecore.Participant) {
		config.Drops = drops
	})
	openFixtureUserPhase(t, engine)
	finishFixtureUserAttack(t, engine, true)
	if engine.EndType() != 1 {
		t.Fatal("fixture did not win", engine.StateView())
	}
	return engine, members
}

func knockedOutRoomBattle(t *testing.T, dead int, continuation, score bool) (*BattleEngine, []Member) {
	t.Helper()
	engine, members := configuredRoomBattle(t, func(definitions *battlecore.Definitions, config *battlecore.Config, participants []battlecore.Participant) {
		config.ContinueAllowed, config.MemberDeadEnd = continuation, score
		enemy := definitions.Enemies[1]
		enemy.HP = 1000000
		definitions.Enemies[1] = enemy
		level := definitions.EnemyLevels[1]
		level.ActionsPerTurn = 1
		level.Actions = []battlecore.CombatEnemyAction{{Slot: 0, Category: "normal", SkillID: 9001, Target: "USER_ALL", ActionCost: 1, MaxUses: 100, Rate: 100}}
		definitions.EnemyLevels[1] = level
		definitions.Skills[9001] = []battlecore.CombatSkillDefinition{{ID: 9001, FunctionID: 9001, Target: "USER_ALL"}}
		definitions.SkillRoles[9001] = []battlecore.CombatSkillRole{{Function: "ATTACK_AA", Target: "SELECT", Parameters: [10]string{"200", "0", "0", "0", "1", "ATK", "0", "FIRE", "PHYSICS"}}}
		for i := range participants {
			participants[i].HP = 100000
			if i < dead {
				participants[i].HP = 1
			}
		}
	})
	openFixtureUserPhase(t, engine)
	finishFixtureUserAttack(t, engine, false)
	for _, phase := range []func() ([]BattleResult, error){engine.ExecuteChaliceUserPhase, engine.EnemyPhase} {
		if _, err := phase(); err != nil {
			t.Fatal(err)
		}
	}
	for i := 0; i < dead; i++ {
		if engine.StateView().Members[i].HP != 0 {
			t.Fatal("fixture member survived", engine.StateView())
		}
	}
	return engine, members
}

func coreBattleFixture(t *testing.T) (*BattleEngine, []battlecore.Participant) {
	t.Helper()
	definitions, config, members := roomFixtureInputs()
	engine, err := NewBattleEngine((*CombatCatalog)(definitions), config, members)
	if err != nil {
		t.Fatal(err)
	}
	return engine, members
}

func roomFixtureCatalog() *CombatCatalog {
	definitions, _, _ := roomFixtureInputs()
	return (*CombatCatalog)(definitions)
}

func firstFixtureHandCard(t *testing.T, engine *BattleEngine, member int) int {
	t.Helper()
	rows, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range rows {
		if row.Command != CommandResumeCardHand || len(row.Args) != 11 || row.Args[0] != int64(member) {
			continue
		}
		for i := 1; i < len(row.Args); i += 2 {
			if row.Args[i] != 0 {
				return int(row.Args[i])
			}
		}
	}
	t.Fatal("member has no hand in public resume snapshot", member)
	return 0
}
