package battlecore

import (
	"encoding/json"
	"os"
	"testing"
)

func TestAIDamageHistoryRetainsActualHighestSource(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.enemies[1] = engine.enemies[0]
	engine.enemies[1].MemberType, engine.enemyCount = 6, 2
	engine.recordDamageEvent(5, 1, 150, "FIRE", "PHYSICS", "", false)
	engine.recordDamageEvent(5, 2, 100, "FIRE", "PHYSICS", "", false)
	engine.recordDamageEvent(6, 2, 100, "ICE", "MAGIC", "", false)
	values := []string{"NULL", "350", "350", "NULL", "ALL", "NULL", "0", "0"}
	rng := engine.rng
	rng.next()
	if !engine.allDamageTurnMatches(values) || engine.enemyTriggerTarget != 1 || engine.rng != rng {
		t.Fatal("native selects highest enemy/player cell, not the player with greatest sum across enemies")
	}
	engine.phase = PhaseChaliceEnemy
	if _, err := engine.TurnPhase(); err != nil {
		t.Fatal(err)
	}
	if engine.allDamageTurnMatches(values) {
		t.Fatal("current turn retained previous turn damage")
	}
	values[6], values[7] = "1", "1"
	if !engine.allDamageTurnMatches(values) || engine.enemyTriggerTarget != 1 {
		t.Fatal("relative previous-turn damage/target was discarded")
	}
	engine.players[1].HP = 0
	if engine.allDamageTurnMatches(values) {
		t.Fatal("KO source must be omitted from appointed-turn damage")
	}
	engine.phase, engine.endType = PhaseEnded, 1
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if next.damageByPlayerInTurns(5, 1, "NULL", "ALL", "NULL", 0, 29, false) != 0 {
		t.Fatal("new wave inherited the former enemy's damage history")
	}
}

func TestNativeAIDamageHistoryReceipt(t *testing.T) {
	path := os.Getenv("CN_NATIVE_AI_DAMAGE_HISTORY_RECEIPT")
	if path == "" {
		t.Skip("set CN_NATIVE_AI_DAMAGE_HISTORY_RECEIPT to original reader receipt")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var receipt struct {
		State, Scope string
		Hash         string `json:"lib_sha256"`
		Events       []struct {
			Source, Value, Attribute, Physics, Dot int
			Enchant, Special                       bool
		}
		Vectors []struct {
			Shifted                                      bool
			Kind                                         string
			Attribute, Physics, Dot, Source, First, Last int
			Result                                       int64
		}
	}
	if err := json.Unmarshal(data, &receipt); err != nil {
		t.Fatal(err)
	}
	if receipt.State != "PASS" || receipt.Scope != "original-cn-x86-damage-readers-history-shift" || receipt.Hash != "7513b008d75510bf75aec37d5e41cbe5951d7c51941921671c313be06f669636" || len(receipt.Vectors) == 0 {
		t.Fatal("invalid native damage receipt")
	}
	attributes := map[int]string{0: "NULL", 1: "FIRE", 2: "ICE", 5: "DARK", 102: "FIRE_ICE"}
	physics := map[int]string{0: "PHYSICS", 1: "MAGIC", 2: "ALL"}
	dots := map[int]string{0: "NULL", 1: "POISON", 2: "BURN"}
	engine, _ := coreBattleFixture(t)
	for _, e := range receipt.Events {
		if e.Special {
			engine.recordSpecialEnemyDamage(5, e.Source, e.Value)
			continue
		}
		dot := ""
		if e.Dot != 0 {
			dot = dots[e.Dot]
		}
		engine.recordDamageEvent(5, e.Source, e.Value, attributes[e.Attribute], physics[e.Physics], dot, e.Enchant)
	}
	shifted := false
	for _, v := range receipt.Vectors {
		if v.Shifted && !shifted {
			engine.phase = PhaseChaliceEnemy
			if _, err := engine.TurnPhase(); err != nil {
				t.Fatal(err)
			}
			shifted = true
		}
		var got int64
		switch v.Kind {
		case "total":
			got = int64(engine.damageEventTotal(5, attributes[v.Attribute], physics[v.Physics], dots[v.Dot], false))
		case "enchant":
			got = int64(engine.damageEventTotal(5, attributes[v.Attribute], "ALL", "NULL", true))
		case "history":
			got = engine.damageByPlayerInTurns(5, v.Source, attributes[v.Attribute], physics[v.Physics], dots[v.Dot], v.First, v.Last, false)
		default:
			t.Fatalf("unknown vector kind %q", v.Kind)
		}
		if got != v.Result {
			t.Fatalf("%+v Go=%d", v, got)
		}
	}
}
