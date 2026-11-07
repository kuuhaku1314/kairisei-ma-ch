package battlecore

import (
	"reflect"
	"testing"
)

func TestNextBattleNativeStateAndRotation(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	engine.phase, engine.endType, engine.turn = PhaseEnded, 1, 7
	player := &engine.players[0]
	player.HP, player.Cost, player.Burst = 123, 8, 42
	player.DamageTaken = 877
	player.Hand = [5]int{1, 2, 3}
	player.DeckOrder, player.DrawIndex, player.DrawCount = [10]int{3, 4, 5, 6}, 0, 4
	player.Discard = []int{7, 8, 9}
	player.Spheres[0] = battleSphere{SphereID: 123, Count: 0, Maximum: 1, Remaining: 3, ChalicePlayable: true}
	engine.players[1].HP = 0 // KO in the winning action, not formally retired.
	before := *engine
	next, err := engine.NextBattle(1, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(*engine, before) {
		t.Fatal("next wave mutated its predecessor")
	}
	got := next.players[0]
	if got.Spheres[0].ChalicePlayable {
		t.Fatal("previous wave chalice eligibility survived the native handover reset")
	}
	if got.HP != 123 || got.Burst != 42 || got.Cost != 3 || got.DamageTaken != 0 || got.Hand != player.Hand || next.turn != 0 || next.endType != 0 || next.phase != PhaseCreated || next.players[1].HP != 1 || next.players[1].GameOver || got.Spheres[0].Count != 1 || got.Spheres[0].Remaining != 0 {
		t.Fatal("native wave handover lost player state or failed to reset wave state")
	}
	seen := map[int]bool{}
	for _, slot := range got.Hand {
		if slot != 0 {
			seen[slot] = true
		}
	}
	for _, slot := range got.DeckOrder[:got.DrawCount] {
		if slot < 0 {
			continue
		}
		if seen[slot+1] {
			t.Fatal("card duplicated across hand and rotation pool")
		}
		seen[slot+1] = true
	}
	if len(seen) != 10 || len(got.Discard) != 0 || next.rng == newXorShift128(602) || next.rng == engine.rng {
		t.Fatal("rotation lost cards or RNG was reseeded instead of advancing")
	}
}

func TestDefaultNativeTerminalRetainsBurstListsAndCleansKO(t *testing.T) {
	engine, _ := coreBattleFixture(t)
	player := &engine.players[0]
	player.HP, player.BurstBreak, player.BurstRecharge[0] = 0, 2, 3
	for _, list := range []int{0, 1, 2, 3, 4, 5, 6, 7} {
		player.Effects = append(player.Effects, battleEffect{Function: "BURN", ListType: list, Remaining: 3})
	}
	rows, err := engine.appendTerminalBuffCleanup([]BattleResult{battleEndResult(1)})
	if err != nil {
		t.Fatal(err)
	}
	if len(player.Effects) != 3 || player.Effects[0].ListType != 2 || player.Effects[1].ListType != 5 || player.Effects[2].ListType != 6 || player.BurstBreak != 1 || player.BurstRecharge[0] != 2 {
		t.Fatal("default wave cleanup lost preserved lists/counters or retained KO debuffs")
	}
	if battleResultsContainCommand(rows, CommandBuffPartition) || battleResultsContainCommand(rows, CommandBuffLostOne) {
		t.Fatal("default terminal took the optional expiry branch")
	}
	engine.players[0].HP = 100
	engine.players[0].BurstState = burstGaugeBurst
	role := CombatSkillRole{Function: "ATK_UP_FIXED", Target: "SELF", Parameters: [10]string{"2", "ATK", "10"}}
	rows, err = engine.executeBurstRoleWithListType(battleAction{memberType: 1, target: 1, cardLevel: 1}, role, nil, 6)
	if err != nil {
		t.Fatal(err)
	}
	last := player.Effects[len(player.Effects)-1]
	if last.ListType != 6 || !last.BurstPassive {
		t.Fatal("burst passive lost its durable list identity")
	}
	found := false
	for _, row := range rows {
		if row.Command == CommandPassiveBuff && row.Args[2] == 6 {
			found = true
		}
	}
	if !found {
		t.Fatal("burst passive projected as a normal buff")
	}
}

func TestAwakeningCarriesCostProgression(t *testing.T) {
	for _, turn := range []int{1, 4, 7, 12} {
		engine, _ := coreBattleFixture(t)
		engine.phase, engine.endType, engine.turn = PhaseEnded, 4, turn
		engine.awakeTakeover.KeepCost = true
		next, err := engine.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if _, err = next.Start(); err != nil {
			t.Fatal(err)
		}
		if _, err = next.TurnPhase(); err != nil {
			t.Fatal(err)
		}
		want := minInt(10, 3+turn)
		if next.turnCost() != want || next.players[0].Cost != want {
			t.Fatalf("turn %d: cost %d, want %d", turn, next.players[0].Cost, want)
		}
		// Starting the second post-awakening turn advances the cost normally.
		next.turn++
		if next.turnCost() != minInt(10, want+1) {
			t.Fatal("post-awakening cost did not resume normal growth")
		}
		// A further awakening at that turn must also inherit its current cost.
		next.phase, next.endType = PhaseEnded, 4
		again, err := next.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		if again.turnCost() != next.turnCost() {
			t.Fatal("repeated awakening changed current cost")
		}
	}
}

func TestAwakeningPreservesFirstDrawCycle(t *testing.T) {
	for _, endType := range []int{1, 4} {
		engine, _ := coreBattleFixture(t)
		if _, err := engine.Start(); err != nil {
			t.Fatal(err)
		}
		player := &engine.players[0]
		seen := map[int]bool{}
		// Five cards reached the hand, and two were used before the transition.
		for i := 0; i < 5; i++ {
			slot := engine.drawCard(player)
			seen[slot] = true
			if i < 2 {
				player.Discard = append(player.Discard, slot-1)
			} else {
				player.Hand[i-2] = slot
			}
		}
		engine.phase, engine.endType, engine.turn = PhaseEnded, endType, 2
		engine.awakeTakeover.KeepDeckTrash = endType == 4
		before := *player
		rng := engine.rng
		next, err := engine.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		p := &next.players[0]
		if p.Hand != before.Hand {
			t.Fatal("transition replaced retained hand")
		}
		if endType == 1 {
			if len(p.Discard) != 0 || p.remainingDeckCount() != 7 {
				t.Fatal("ordinary wave must still recycle used cards")
			}
			continue
		}
		if p.DeckOrder != before.DeckOrder || p.DrawIndex != before.DrawIndex || p.DrawCount != before.DrawCount || next.rng != rng {
			t.Fatal("awakening reset the draw order or consumed shuffle RNG")
		}
		for i := 0; i < 5; i++ {
			slot := next.drawCard(p)
			if slot == 0 || seen[slot] {
				t.Fatalf("awakening repeated slot %d before all ten cards appeared", slot)
			}
			seen[slot] = true
		}
		if len(seen) != 10 || len(p.Discard) != 2 {
			t.Fatal("awakening lost unused cards or discarded cards")
		}
		if slot := next.drawCard(p); slot != before.Discard[0]+1 && slot != before.Discard[1]+1 {
			t.Fatal("depleted draw pool did not recycle the discarded cards")
		}
		if player.DeckOrder != before.DeckOrder || len(player.Discard) != 2 {
			t.Fatal("advancing the next stage mutated the preceding engine")
		}
	}
}
