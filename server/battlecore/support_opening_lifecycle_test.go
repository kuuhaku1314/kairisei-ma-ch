package battlecore

import (
	"reflect"
	"strings"
	"testing"
)

func TestSupportLoveAndNativeOpeningLifecycle(t *testing.T) {
	base, members := coreBattleFixture(t)
	c := base.catalog
	c.SupportLoveRates = [3]int{25, 50, 100}
	c.Cards[2] = CombatCardDefinition{ID: 2, LoveMax: 10000, SupportSkillIDs: [4]int{0, 11, 12, 13}}

	for _, id := range []int{11, 12, 13} {
		c.Skills[id] = []CombatSkillDefinition{{ID: id, FunctionID: id, Type: "PASSIVE", Target: "SELF"}}
		c.SkillRoles[id] = []CombatSkillRole{{Function: "DAMAGE_BOOST", Target: "SELECT", Parameters: [10]string{"99", "1000", "", "", "", "FIRE_DARK", "PHYSICS", "0", "0"}}}
	}
	for _, sample := range []struct{ love, want int }{{0, 0}, {2499, 0}, {2500, 11}, {4999, 11}, {5000, 12}, {9999, 12}, {10000, 13}, {10001, 13}} {
		skill, _, err := c.CardSupportSkill(BattleCard{CardID: 2, Level: 1, Love: sample.love})
		if err != nil || skill.ID != sample.want {
			t.Fatalf("love %d: skill=%d err=%v", sample.love, skill.ID, err)
		}
	}
	members[0].SupportCards = []BattleCard{{CardType: 12, CardID: 2, Level: 60, Love: 10000}}
	identities, err := DeckIdentityResults(members)
	if err != nil {
		t.Fatal(err)
	}
	text, err := encodeOptionalBattleResults(identities)
	if err != nil || !strings.Contains(text, "23,1,12,2,60") {
		t.Fatalf("support identity not sent before start: %v", err)
	}
	engine, err := NewBattleEngine(c, Config{EnemyPartyID: 1, Seed: 602, HoldMax: 5, CostInitial: 3}, members)
	if err != nil {
		t.Fatal(err)
	}
	wantRNG := engine.rng
	// Start now owns the four opening shuffles (nine native words each).
	// This DAMAGE_BOOST passive itself must not consume another word.
	for range 36 {
		wantRNG.next()
	}
	results, err := engine.Start()
	if err != nil {
		t.Fatal(err)
	}
	if engine.rng != wantRNG || len(engine.players[0].Effects) != 1 || len(engine.players[1].Effects) != 0 {
		t.Fatal("support setup consumed extra RNG or leaked SELF effects")
	}
	foundHeader := false
	for _, row := range results {
		if row.Command == CommandCardSkill {
			foundHeader = reflect.DeepEqual(row.Args, []int64{1, 12, 13, 1, 60, 0, 0, 0, 13, 0, 1, 0})
		}
	}
	if !foundHeader {
		t.Fatal("native passive CARD_SKILL header missing")
	}
	skill, roles, err := c.CardSkill(1, 1)
	if err != nil {
		t.Fatal(err)
	}
	engine.enemies[0].HP, engine.enemies[0].MaxHP = 10000, 10000
	_, err = engine.executePlayerAttack(battleAction{memberType: 1, cardID: 1, cardType: 1, cardLevel: 1, target: 5, skill: skill, roles: roles}, roles[0], 1)
	if err != nil || engine.enemies[0].HP != 9600 {
		t.Fatalf("EX damage not used by actual attack: hp=%d err=%v", engine.enemies[0].HP, err)
	}
	resume, err := engine.ResumeResults()
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range resume {
		if row.Command == CommandResumeBuff && row.Args[1] == 1 {
			t.Fatal("resume incorrectly serialized PASSIVE as a temporary UI buff")
		}
	}
	if effect := engine.players[0].Effects[0]; effect.Attribute != "FIRE_DARK" || effect.Rate != 1000 {
		t.Fatal("read-only resume lost passive state")
	}
	engine.turn = 100
	if _, err := engine.tickPersistentEffects(); err != nil {
		t.Fatal(err)
	}
	if len(engine.players[0].Effects) != 1 || engine.players[0].Effects[0].Remaining != 99 {
		t.Fatal("EX passive incorrectly counted down with turn buffs")
	}
	engine.phase, engine.endType = PhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := next.Start(); err != nil {
		t.Fatal(err)
	}
	if len(next.players[0].Effects) != 1 || next.players[0].SupportDeck != engine.players[0].SupportDeck || len(engine.players[0].Effects) != 1 {
		t.Fatal("wave handover duplicated/lost support or mutated preceding state")
	}
}
