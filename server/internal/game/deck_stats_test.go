package game

import (
	"testing"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/clientbridge"
	"kairisei.local/server/internal/gamestate"
)

// CN native leader-card parameter contract.
func TestPartnerDeckStatsIncludesLeaderBonus(t *testing.T) {
	cards := map[int64]CardInfo{}
	deck := DeckInfo{LeaderCardIndex: 0}
	for i := int64(1); i <= 10; i++ {
		cards[i] = CardInfo{HP: 100, Attack: 100, Magic: 100, Mind: 100}
		deck.CardUniqueIDs = append(deck.CardUniqueIDs, i)
	}
	hp, atk, mag, mind := PartnerDeckStats(deck, cards, gamestate.JobParameter{})
	if hp != 1050 || atk != 1050 || mag != 1050 || mind != 1050 {
		t.Fatalf("10 cards each stat 100, leader multiplier 1.5: got HP/ATK/MAG/MIND=%d/%d/%d/%d; want 1050 each", hp, atk, mag, mind)
	}
	deck.SupportCardUniqueIDs = []int64{11}
	for _, sample := range []struct {
		love int
		want [4]int
	}{
		{0, [4]int{6, 20, 20, 10}},
		{50, [4]int{33, 110, 110, 55}},
		{100, [4]int{60, 200, 200, 100}},
	} {
		cards[11] = CardInfo{HP: 100, Attack: 100, Magic: 100, Mind: 100, Love: sample.love, LoveMax: 100}
		hp, atk, mag, mind = PartnerDeckStats(deck, cards, gamestate.JobParameter{})
		if got := [4]int{hp - 1050, atk - 1050, mag - 1050, mind - 1050}; got != sample.want {
			t.Fatalf("support love=%d: got %v, want %v", sample.love, got, sample.want)
		}
	}
}

func TestAlchemistDeckStatsUseAlchemyMainAndOrdinarySupport(t *testing.T) {
	definitions := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{
		12000196: {ID: 12000196, HasParameterFamilies: true, MaxLevel: 1, LoveMax: 100,
			AlchemistParameters: [3][4]int{{995, 126, 126, 63}, {995, 126, 126, 63}}},
		12000376: {ID: 12000376, HasParameterFamilies: true, MaxLevel: 1, LoveMax: 100,
			AlchemistParameters: [3][4]int{{900, 900, 900, 900}, {900, 900, 900, 900}}},
	}}
	provider := clientbridge.NewCardParameterCatalog(definitions, gamestate.CardProgressionPolicy{})
	cards := map[int64]CardInfo{
		1: {UniqueID: 1, CardID: 12000196, Level: 1, Fame: 1, HP: 205, Attack: 29, Magic: 29, Mind: 15},
		2: {UniqueID: 2, CardID: 12000376, Level: 1, Fame: 1, Love: 100, LoveMax: 100, HP: 100, Attack: 100, Magic: 100, Mind: 100},
	}
	deck := DeckInfo{ArthurType: 5, LeaderCardIndex: 0, CardUniqueIDs: []int64{1}, SupportCardUniqueIDs: []int64{2}, SphereUniqueIDs: make([]int64, DeckSphereSlots)}
	ordinaryHP, ordinaryAttack, ordinaryMagic, ordinaryMind := PartnerDeckStats(deck, cards, gamestate.JobParameter{})
	if ordinaryHP != 367 || ordinaryAttack != 243 || ordinaryMagic != 243 || ordinaryMind != 122 {
		t.Fatalf("ordinary comparison changed: %d/%d/%d/%d", ordinaryHP, ordinaryAttack, ordinaryMagic, ordinaryMind)
	}
	stats, err := PartnerDeckStatsWithAlchemist(deck, cards, gamestate.JobParameter{}, provider)
	if err != nil || stats != (gamestate.CardParameter{HP: 1552, Attack: 389, Magic: 389, Mind: 194}) {
		t.Fatalf("alchemy main/ordinary support mismatch: %+v %v", stats, err)
	}
	if cards[1].HP != 205 || cards[2].HP != 100 {
		t.Fatal("deck display changed owned card attributes")
	}
	wire, err := PartnerDeckWire(1, 5, deck, cards, gamestate.JobParameter{}, gamestate.Avatar{}, nil, nil, 1, 0, &stats)
	if err != nil || wire["hp"] != 1552 || wire["atkp"] != 389 || wire["intp"] != 389 || wire["mndp"] != 194 ||
		wire["deck"].([]any)[0].(map[string]any)["hp"] != 205 {
		t.Fatalf("deck snapshot did not share alchemy totals and ordinary card fields: %+v %v", wire, err)
	}
}
