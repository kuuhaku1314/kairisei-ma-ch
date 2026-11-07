package battlecore

import (
	"testing"
)

func TestPlayedCardsReturnThroughDiscardPool(t *testing.T) {
	engine := &BattleEngine{rng: newXorShift128(602)}
	player := &engine.players[0]
	player.MemberType, player.DrawCount = 1, 10
	for index := range player.Deck {
		player.Deck[index] = BattleCard{CardType: index + 1, CardID: index + 1, Level: 1}
		player.DeckOrder[index] = index
	}
	for index := range player.Hand {
		player.Hand[index] = engine.drawCard(player)
	}
	seenAgain := false
	for turn := 0; turn < 20; turn++ {
		cardType := player.Deck[player.Hand[0]-1].CardType
		engine.removeCardFromHand(1, cardType)
		before := len(player.Discard)
		engine.removeCardFromHand(1, cardType)
		if len(player.Discard) != before {
			t.Fatal("same card discarded twice")
		}
		next := engine.drawCard(player)
		if next == 0 {
			t.Fatal("played card disappeared from the rotation pool")
		}
		if next == 1 {
			seenAgain = true
		}
		player.Hand[0] = next
		locations := make(map[int]int)
		for _, slot := range player.Hand {
			if slot != 0 {
				locations[slot]++
			}
		}
		for _, slot := range player.DeckOrder[player.DrawIndex:player.DrawCount] {
			if slot >= 0 {
				locations[slot+1]++
			}
		}
		for _, slot := range player.Discard {
			locations[slot+1]++
		}
		if len(locations) != 10 {
			t.Fatalf("turn %d lost a deck card: %v", turn, locations)
		}
		for _, count := range locations {
			if count != 1 {
				t.Fatalf("turn %d duplicated a deck card: %v", turn, locations)
			}
		}
	}
	if !seenAgain {
		t.Fatal("first played card was never recycled")
	}
}
