package battlecore

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"kairisei.local/server/battlecontent"
)

func TestJPCardSearchCollectorReceipt(t *testing.T) {
	paths := os.Getenv("JP_CARD_SEARCH_RECEIPTS")
	if paths == "" {
		t.Skip("set JP_CARD_SEARCH_RECEIPTS to the original JP collector witnesses")
	}
	for _, path := range filepath.SplitList(paths) {
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var receipt struct {
			Library string `json:"original_library_sha256"`
			Cases   []struct {
				Name  string
				Input struct{ Parameters []any }
				Cards []struct {
					Slot            int
					Attribute, Kind string
					Tags            []int
				}
				States []struct {
					Draw, Discard [10]int
					RNG           [4]uint32
				} `json:"search_states"`
			}
		}
		decoder := json.NewDecoder(bytes.NewReader(data))
		decoder.UseNumber()
		if err := decoder.Decode(&receipt); err != nil {
			t.Fatal(err)
		}
		if receipt.Library != "bd5ae2e9bd305abd8be348a2023ba2ae2f4ae50cee4fc0e22263dacb0ddbebff" || len(receipt.Cases) == 0 {
			t.Fatal("wrong original search source")
		}
		for _, item := range receipt.Cases {
			t.Run(item.Name, func(t *testing.T) {
				if len(item.States) != 2 {
					t.Fatal("missing native search entry/return")
				}
				before, after := item.States[0], item.States[1]
				catalog := &CombatCatalog{Cards: map[int]CombatCardDefinition{}, Skills: map[int][]CombatSkillDefinition{}, SkillRoles: map[int][]CombatSkillRole{}}
				engine := &BattleEngine{catalog: catalog, rng: xorShift128{before.RNG[0], before.RNG[1], before.RNG[2], before.RNG[3]}}
				player := &engine.players[0]
				player.MemberType, player.ArthurType, player.DrawCount = 1, 1, 10
				for i := range player.DeckOrder {
					player.DeckOrder[i] = before.Draw[i] - 1
					player.Discard = append(player.Discard, before.Discard[i]-1)
				}
				for len(player.Discard) > 0 && player.Discard[len(player.Discard)-1] < 0 {
					player.Discard = player.Discard[:len(player.Discard)-1]
				}
				for _, card := range item.Cards {
					definition := CombatCardDefinition{ID: card.Slot, NormalSkillID: card.Slot}
					copy(definition.ProfileTags[:], card.Tags)
					catalog.Cards[card.Slot] = definition
					catalog.Skills[card.Slot] = []CombatSkillDefinition{{ID: card.Slot, FunctionID: card.Slot, Attribute: card.Attribute, Kind: card.Kind}}
					catalog.SkillRoles[card.Slot] = []CombatSkillRole{{Function: "ATTACK_AA"}}
					player.Deck[card.Slot-1] = BattleCard{CardID: card.Slot, CardType: card.Slot, Level: 1}
				}
				role := CombatSkillRole{Function: "CARD_SEARCH"}
				for i, value := range item.Input.Parameters {
					role.Parameters[i] = fmt.Sprint(value)
				}
				if err := engine.searchCards(player, role); err != nil {
					t.Fatal(err)
				}
				var draw, discard [10]int
				for i, slot := range player.DeckOrder {
					draw[i] = slot + 1
				}
				for i, slot := range player.Discard {
					discard[i] = slot + 1
				}
				rng := [4]uint32{engine.rng.x, engine.rng.y, engine.rng.z, engine.rng.w}
				if draw != after.Draw || discard != after.Discard || rng != after.RNG {
					t.Fatalf("draw=%v discard=%v rng=%v; native draw=%v discard=%v rng=%v", draw, discard, rng, after.Draw, after.Discard, after.RNG)
				}
			})
		}
	}
}

func TestNativeRecycleShufflesAllTenSlotsIncludingEmptyOnes(t *testing.T) {
	engine := &BattleEngine{rng: newXorShift128(1)}
	player := &engine.players[0]
	player.Discard = []int{1, 3, 7}
	// 49830 fills the first empty slots, 49ed8 shuffles all TEN entries,
	// then 48d00 skips nulls. Seed 1 produces deck slots 8,2,4, not a
	// Fisher-Yates shuffle over only the three available cards.
	for index, want := range []int{8, 2, 4} {
		if got := engine.drawCard(player); got != want {
			t.Fatalf("recycled draw %d = %d, want native %d", index, got, want)
		}
	}
	if len(player.Discard) != 0 || engine.rng.next() != 496576104 {
		t.Fatal("recycle must consume nine native RNG words and clear the trash")
	}
}

func TestNativeWaveRecyclePreservesUndrawnSlotPositions(t *testing.T) {
	engine := &BattleEngine{rng: newXorShift128(1)}
	player := &engine.players[0]
	player.DeckOrder = [10]int{0, 1, 2, 3, 4, 5, 6}
	player.DrawIndex, player.DrawCount = 3, 7
	player.Discard = []int{7, 8, 9}
	// Existing slots 3..6 stay put. Trash fills 0..2 before the full
	// native shuffle; compacting the old deck ahead of trash is different.
	engine.recycleDrawPool(player)
	for index, want := range []int{5, 6, 10, 7, 8, 4, 9} {
		if got := engine.drawCard(player); got != want {
			t.Fatalf("wave draw %d = %d, want native %d", index, got, want)
		}
	}
	if engine.rng.next() != 496576104 {
		t.Fatal("wave recycle changed native RNG consumption")
	}
}

func TestNativeFirstTurnDrawPenaltyAppliesToEmptyHandSlots(t *testing.T) {
	for _, tc := range []struct{ held, penalty, want int }{{0, 2, 3}, {3, 1, 4}} {
		engine, _ := coreBattleFixture(t)
		engine.phase, engine.turn = PhaseTurn, 1
		player := &engine.players[0]
		for index := range player.DeckOrder {
			player.DeckOrder[index] = index
		}
		for index := 0; index < tc.held; index++ {
			player.Hand[index] = index + 1
		}
		player.DrawIndex = tc.held
		player.Effects = []battleEffect{{Function: "DEAL_PENALTY", Value: tc.penalty, Kind: 2, Remaining: 1}}
		engine.prepareTurnDraw() // This focused fixture bypasses TurnPhase.
		if _, err := engine.UserPhase(); err != nil {
			t.Fatal(err)
		}
		got := 0
		for _, slot := range player.Hand {
			if slot != 0 {
				got++
			}
		}
		if got != tc.want {
			t.Fatalf("first turn with %d held and penalty %d: hand=%d, want %d", tc.held, tc.penalty, got, tc.want)
		}
	}
}

func TestNativeOpeningDrawUsesCardPassiveAndSuffixShuffle(t *testing.T) {
	// Vectors from the 4a66e prefix-swap / 4a114 suffix-shuffle sequence.
	for _, tc := range []struct {
		cards []int
		want  [10]int
		next  uint32
	}{
		{nil, [10]int{4, 9, 5, 8, 2, 6, 0, 3, 7, 1}, 496576104},
		{[]int{9}, [10]int{9, 0, 1, 8, 4, 6, 2, 5, 7, 3}, 496576104},
		{[]int{2, 9}, [10]int{2, 9, 1, 3, 6, 8, 0, 7, 4, 5}, 1081452313},
		{[]int{1, 3, 5, 7, 8, 9}, [10]int{1, 3, 5, 7, 8, 2, 9, 4, 0, 6}, 3316842567},
	} {
		base, members := coreBattleFixture(t)
		row := make([]string, 34)
		row[0], row[26], row[33] = "2", "1", "63000001"
		card, err := battlecontent.ParseCard(row)
		if err != nil {
			t.Fatal(err)
		}
		base.catalog.Cards[2] = card
		base.catalog.Skills[63000001] = []CombatSkillDefinition{{ID: 63000001, FunctionID: 63000001, Type: "PASSIVE", Target: "SELF"}}
		base.catalog.SkillRoles[63000001] = []CombatSkillRole{{Function: "BEGINNING_DRAW", Target: "SELECT"}}
		for _, index := range tc.cards {
			members[0].DeckCards[index].CardID = 2
		}
		engine, err := NewBattleEngine(base.catalog, Config{EnemyPartyID: 1, CostInitial: 3, HoldMax: 2, Seed: 1}, members)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := engine.Start(); err != nil {
			t.Fatal(err)
		}
		if engine.players[0].DeckOrder != tc.want {
			t.Fatalf("passive slots %v: opening order %v, want %v", tc.cards, engine.players[0].DeckOrder, tc.want)
		}
		// Isolate the first member's RNG boundary from the three other decks.
		probe := &BattleEngine{rng: newXorShift128(1)}
		player := &probe.players[0]
		player.DrawCount = 10
		var guaranteed [10]bool
		for i := range player.DeckOrder {
			player.DeckOrder[i] = i
		}
		for _, i := range tc.cards {
			guaranteed[i] = true
		}
		probe.shuffleOpeningDeck(player, guaranteed)
		if probe.rng.next() != tc.next {
			t.Fatal("opening shuffle changed native RNG consumption")
		}
		// Neither the next wave nor the discard rotation gets a second prefix.
		engine.phase, engine.endType = PhaseEnded, 1
		next, err := engine.NextBattle(1, nil)
		if err != nil {
			t.Fatal(err)
		}
		control := *engine
		for i := range control.players {
			control.recycleDrawPool(&control.players[i])
		}
		if next.players[0].DeckOrder != control.players[0].DeckOrder || next.rng != control.rng {
			t.Fatal("next wave reapplied opening-draw priority")
		}
	}
}
