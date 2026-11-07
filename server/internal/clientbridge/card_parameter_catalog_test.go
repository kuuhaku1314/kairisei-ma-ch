package clientbridge

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"kairisei.local/server/battlecontent"
	"kairisei.local/server/battlemodel"
	"kairisei.local/server/internal/gamestate"
)

// Optional integration gate: use the real file adapter and the modern control
// projection, without opening a service or loading any player's save.
func TestCanonicalCardParameterFamilies(t *testing.T) {
	root := os.Getenv("CLIENT_REBUILD_CONTROL_ROOT")
	if root == "" {
		t.Skip("requires the complete canonical control projection")
	}
	definitions, err := battlecontent.LoadDefinitions(filepath.Join(root, "cn602-card-master", "card.csv"), filepath.Join(root, "cn602-battle-master"))
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(root, "cn602-card-runtime-master.json"))
	if err != nil {
		t.Fatal(err)
	}
	var config struct {
		Policy gamestate.CardProgressionPolicy `json:"card_progression_policy"`
	}
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatal(err)
	}
	catalog := NewCardParameterCatalog(definitions, config.Policy)
	modern, summoned := 0, 0
	for id, card := range definitions.Cards {
		if !card.HasParameterFamilies {
			continue
		}
		modern++
		for _, state := range [][3]int{{1, 0, 0}, {card.MaxLevel, card.LoveMax, 100}} {
			value, err := catalog.Resolve(id, state[0], state[1], state[2])
			if err != nil {
				t.Fatalf("card %d at %v: %v", id, state, err)
			}
			if len(value.Alchemist) != 1 {
				t.Fatalf("card %d lacks alchemist projection", id)
			}
			if card.SummonID != 0 && (len(value.Summons) != 1 || value.Summons[0].SummonID != card.SummonID) {
				t.Fatalf("card %d lost summon identity", id)
			}
		}
		if card.SummonID != 0 {
			summoned++
		}
	}
	if modern == 0 || summoned == 0 {
		t.Fatal("canonical gate did not reach modern and summon cards")
	}
	t.Logf("canonical cards: %d; summon cards: %d; projected initial/maximum states: %d", modern, summoned, modern*2)
}

func TestCardParameterCatalogUsesCanonicalFamiliesWithoutChangingDefinitions(t *testing.T) {
	card := battlemodel.CombatCardDefinition{ID: 123, HasParameterFamilies: true, RarityPlus: 3, MaxLevel: 65, LoveMax: 100, SummonID: 17,
		AlchemistParameters: [3][4]int{{101, 202, 303, 404}, {1101, 2202, 3303, 4404}, {11, 22, 33, 44}}}
	summon := battlemodel.CombatSummonDefinition{ID: 17, Initial: [6]int32{1, 2, 3, 4, 5, 6}, Maximum: [6]int32{1001, 1002, 1003, 1004, 1005, 1006}}
	definitions := &battlemodel.Definitions{Cards: map[int]battlemodel.CombatCardDefinition{123: card, 124: {ID: 124}}, Summons: map[int]battlemodel.CombatSummonDefinition{17: summon}}
	policy := gamestate.CardProgressionPolicy{FameNormal: gamestate.CardParameter{HP: 100, Attack: 200, Magic: 300, Mind: 400}, FamePremium: gamestate.CardParameter{HP: 1000, Attack: 2000, Magic: 3000, Mind: 4000}}
	catalog := NewCardParameterCatalog(definitions, policy)
	got, err := catalog.Resolve(123, 65, 100, 90)
	if err != nil || !reflect.DeepEqual(got.Alchemist, []CardParameterResult{{HP: 2012, Attack: 4024, Magic: 6036, Mind: 8048}}) ||
		!reflect.DeepEqual(got.Summons, []SummonParameterResult{{SummonID: 17, HP: 1001, Attack: 1002, Magic: 1003, Mind: 1004, Defense: 1005, MagicDefense: 1006}}) {
		t.Fatalf("canonical parameter projection: %+v %v", got, err)
	}
	if definitions.Cards[123] != card || definitions.Summons[17] != summon {
		t.Fatal("view projection modified shared definitions")
	}
	for _, id := range []int{0, 124} {
		got, err = catalog.Resolve(id, 0, 0, 0)
		if err != nil || got.Alchemist != nil || got.Summons != nil {
			t.Fatal("placeholder/legacy card acquired synthetic modern parameters")
		}
	}
	delete(definitions.Summons, 17)
	if _, err := catalog.Resolve(123, 65, 100, 90); err == nil {
		t.Fatal("missing summon source silently returned ordinary parameters")
	}
	if _, err := catalog.Resolve(999, 1, 0, 0); err == nil {
		t.Fatal("unknown card definition was accepted")
	}
}
