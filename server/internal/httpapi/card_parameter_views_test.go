package httpapi

import (
	"encoding/json"
	"reflect"
	"testing"

	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/clientbridge"
	"kairisei.local/server/internal/game"
	"kairisei.local/server/internal/gamestate"
)

func TestCardParameterViewsPreserveOwnedAndStoredPartnerState(t *testing.T) {
	definitions := &battlemodel.Definitions{
		Cards:   map[int]battlemodel.CombatCardDefinition{123: {ID: 123, HasParameterFamilies: true, MaxLevel: 1, LoveMax: 100, SummonID: 17, AlchemistParameters: [3][4]int{{11, 12, 13, 14}, {21, 22, 23, 24}, {31, 32, 33, 34}}}},
		Summons: map[int]battlemodel.CombatSummonDefinition{17: {ID: 17, Initial: [6]int32{101, 102, 103, 104, 105, 106}}},
	}
	a := &API{cardParameters: clientbridge.NewCardParameterCatalog(definitions, gamestate.CardProgressionPolicy{})}
	card := game.CardInfo{UniqueID: 9223372036, CardID: 123, Level: 1, LevelMax: 1, Fame: 90, HP: 901, Attack: 902, Magic: 903, Mind: 904}
	before := card
	encoded, err := json.Marshal(a.toWireCard(card))
	if err != nil {
		t.Fatal(err)
	}
	var owned map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &owned); err != nil || string(owned["0"]) != "9223372036" || string(owned["7"]) != "901" ||
		string(owned["11"]) != `[{"hp":11,"atkp":12,"intp":13,"mndp":14}]` ||
		string(owned["12"]) != `[{"summonsid":17,"hp":101,"atkp":102,"intp":103,"mndp":104,"defp":105,"mdefp":106}]` || !reflect.DeepEqual(card, before) {
		t.Fatalf("owned card view altered fields: %s %v", encoded, err)
	}
	deck := map[string]any{"name": "saved", "deck": []any{game.PartnerCardWire(card)}, "support_deck": []any{game.PartnerCardWire(game.CardInfo{})}}
	rawBefore, _ := json.Marshal(deck)
	for _, value := range []any{deck, json.RawMessage(rawBefore)} {
		raw, err := json.Marshal(a.partnerDeckParameterView(value))
		if err != nil {
			t.Fatal(err)
		}
		var decoded struct {
			Deck []struct {
				HP        int                                  `json:"hp"`
				Alchemist []clientbridge.CardParameterResult   `json:"alchemist_param"`
				Summons   []clientbridge.SummonParameterResult `json:"summons_param"`
			} `json:"deck"`
		}
		if err := json.Unmarshal(raw, &decoded); err != nil || len(decoded.Deck) != 1 || decoded.Deck[0].HP != 901 || len(decoded.Deck[0].Alchemist) != 1 || len(decoded.Deck[0].Summons) != 1 || decoded.Deck[0].Summons[0].MagicDefense != 106 {
			t.Fatalf("partner parameter view: %s %v", raw, err)
		}
	}
	rawAfter, _ := json.Marshal(deck)
	if string(rawBefore) != string(rawAfter) {
		t.Fatal("rendering a partner view mutated the stored deck")
	}
	if _, err := json.Marshal(a.partnerDeckParameterView(json.RawMessage(append(rawBefore, []byte("{}")...)))); err == nil {
		t.Fatal("projection accepted trailing data that raw JSON serialization rejects")
	}
}
